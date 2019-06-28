package main

import (
	"sync"
	"strings"
    "database/sql"
    _"github.com/mattn/go-oci8"
    "context"
	"time"
	firebase "firebase.google.com/go"
	"firebase.google.com/go/messaging"
	"google.golang.org/api/option"
	"strconv"
	service "./service"
    logrus "github.com/sirupsen/logrus"
)

const (
    logFilePath = "./logs/notifier.log"
    envProduction  = "production"
    envDevelopment = "development"
    oci8 = "oci8"
	dbConnectionInfo = "Go/go@oracle-nodeport:1521/ThirdProject"
	accountFilePath = "./service_account.json"
	own = "own"
	others = "others"
	workerNum = 3
	activeCheck = "activeCheck"
	new = "new"
)

type AggregateResult struct {
	Seq int64
	Answer1number int32
	Answer2number int32
}

type TargetStruct struct {
	Seq int64
	Token string
}

type TargetInfo struct {
	Owner string
	QuestionId int64
	QuestionSeq int64
	Token string
	Question string
}

func workerForAggregate(innerWg *sync.WaitGroup, innerErrChan chan error, queue chan int64) {
	defer innerWg.Done()

	db, err := sql.Open(oci8, dbConnectionInfo)
	if err != nil {
		logrus.Error(err)
		innerErrChan <- err
		return
	}
	defer db.Close()

	for seq := range queue {
		//回答を集計
		aggregateResult := AggregateResult{}
		aggregateResult.Seq = seq
		err := aggregateAnswers(db, &seq, &aggregateResult)
		if err != nil {
			logrus.Error(err)
			innerErrChan <- err
			return
		}

		now := service.GetNow()

		//トランザクション開始
		transaction, err := db.Begin()
		//questionsテーブル 数値、確定フラグ更新
		err = updateQuestions(db, &aggregateResult, now)
		if err != nil {
			transaction.Rollback()
			logrus.Error(err)
			innerErrChan <- err
			return
		}
		//answersテーブル 確定フラグ更新
		err = updateAnswers(db, &aggregateResult, now)
		if err != nil {
			transaction.Rollback()
			logrus.Error(err)
			innerErrChan <- err
			return
		}
		//targetsテーブル 確定フラグ更新
		err = updateTargets(db, &aggregateResult, now)
		if err != nil {
			transaction.Rollback()
			logrus.Error(err)
			innerErrChan <- err
			return
		}
		//コミット
		transaction.Commit()
	}
}

//制限時間を過ぎた質問の結果を集計
func aggregate(wg *sync.WaitGroup, errChan chan error, quitChan chan struct{}) {
	defer wg.Done()
	innerWg := &sync.WaitGroup{} 
	innerErrChan := make(chan error)
	queue := make(chan int64, 10)
	for i := 0; i < workerNum; i++ {
        innerWg.Add(1)
        go workerForAggregate(innerWg, innerErrChan, queue)
    }

	loopFlag := true
	go func() {
		for loopFlag {
			result := false
			func() {
				//新着質問リストを取得
				aggregateTargetSlice := make([]int64, 0)
				err := extractTargetQuestions(&aggregateTargetSlice)
				if err != nil {
					errChan <- err
					return
				}

				logrus.Info("集計ループ開始")
				for _, seq := range aggregateTargetSlice {
					queue <- seq
				}
				result = true
			}()
			
			if !result {
				break
			}

			time.Sleep(30 * time.Second)
		}
	}()

	go func() {
		defer close(innerErrChan)
		defer close(queue)
		for innerErr := range innerErrChan {
			logrus.Info("内部処理エラー")
			errChan <- innerErr
			break
		}
	}()

	<- quitChan
	loopFlag = false
	innerWg.Wait()
}

//questionsテーブル 数値、確定フラグ更新
func updateQuestions(db *sql.DB, result *AggregateResult, now string) error {
	logrus.Info("questionsテーブル 数値、確定フラグ更新")

	db, err := sql.Open(oci8, dbConnectionInfo)
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer db.Close()
	
	sb := strings.Builder{}
    sb.WriteString("update questions set")
    sb.WriteString("    answer1number = :param1")
    sb.WriteString("    , answer2number = :param2")
	sb.WriteString("    , determinationFlag = 1")
	sb.WriteString("    , modifieddatetime = :param3")
    sb.WriteString(" where")
    sb.WriteString("    seq = :param4")

    stmt, err := db.Prepare(sb.String())
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(result.Answer1number, result.Answer2number, now, result.Seq)
	if err != nil {
		logrus.Error(err)
		return err
	}	
	return nil
}

//answersテーブル 確定フラグ更新
func updateAnswers(db *sql.DB, result *AggregateResult, now string) error {
	logrus.Info("answersテーブル 確定フラグ更新")
	
	sb := strings.Builder{}
    sb.WriteString("update answers set")
	sb.WriteString("    determinationFlag = 1")
	sb.WriteString("    , modifieddatetime = :param1")
    sb.WriteString(" where")
    sb.WriteString("    questionSeq = :param2")

    stmt, err := db.Prepare(sb.String())
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(now, result.Seq)
	if err != nil {
		logrus.Error(err)
		return err
	}	
	return nil
}

//targetsテーブル 確定フラグ更新
func updateTargets(db *sql.DB, result *AggregateResult, now string) error {
	logrus.Info("targetsテーブル 確定フラグ更新")
	logrus.Info("QuestionSeq:", strconv.FormatInt(result.Seq, 10))
	
	sb := strings.Builder{}
    sb.WriteString("update targets set")
	sb.WriteString("    determinationFlag = 1")
	sb.WriteString("    , modifieddatetime = :param1")
    sb.WriteString(" where")
    sb.WriteString("    questionSeq = :param2")

    stmt, err := db.Prepare(sb.String())
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(now, result.Seq)
	if err != nil {
		logrus.Error(err)
		return err
	}	
	return nil
}

//回答を集計
func aggregateAnswers(db *sql.DB, seq *int64, aggregateResult *AggregateResult) error { 
	logrus.Info("回答を集計処理開始")
	sb := strings.Builder{}
    sb.WriteString("select")
	sb.WriteString("    SUM.decision")
	sb.WriteString("    , SUM.num")
    sb.WriteString(" from (")
    sb.WriteString("    select")
	sb.WriteString("        decision")
	sb.WriteString("        , count(1) as num")
    sb.WriteString("    from answers")
    sb.WriteString("    where")
    sb.WriteString("        questionSeq = :param1")
    sb.WriteString("    group by")
    sb.WriteString("        decision")
    sb.WriteString("    ) SUM")

    stmt, err := db.Prepare(sb.String())
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer stmt.Close()
	rows, err := stmt.Query(seq)
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer rows.Close()
	for rows.Next(){
		var decision int32
		var number int32
		rows.Scan(&decision, &number)
		switch decision {
		case 1:
			aggregateResult.Answer1number = number	
		case 2:
			aggregateResult.Answer2number = number	
		default:
			break
		}
	}
	if err = rows.Err(); err != nil {
		logrus.Error(err)
		return err
	}
	return nil
}

//対象質問抽出
func extractTargetQuestions(aggregateTargetSlice *[]int64) error{
	logrus.Info("集計対象質問抽出開始")
	db, err := sql.Open(oci8, dbConnectionInfo)
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer db.Close()

	sb := strings.Builder{}
	sb.WriteString("select")
	sb.WriteString("    seq")
	sb.WriteString(" from questions")
	sb.WriteString(" where")
	sb.WriteString("    determinationFlag = 0")
	sb.WriteString("    and askFlag = 1")
	sb.WriteString("    and timeLimit < :param1")
    
	stmt, err := db.Prepare(sb.String())
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer stmt.Close()
	rows, err := stmt.Query(service.GetNow())
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer rows.Close()
	for rows.Next(){
		var seq int64
		rows.Scan(&seq)
		logrus.Info("抽出seq:", seq)
		*aggregateTargetSlice = append(*aggregateTargetSlice, seq)
	}
	if err = rows.Err(); err != nil {
		logrus.Error(err)
		return err
	}
	return nil
}

//対象者リスト抽出
func extractTargetTokens(targetMap map[int64]string) error {
	logrus.Info("対象者リスト抽出開始")

	db, err := sql.Open(oci8, dbConnectionInfo)
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer db.Close()

	sb := strings.Builder{}
	sb.WriteString("select")
	sb.WriteString("    T.seq")
	sb.WriteString("    , U.token")
	sb.WriteString(" from targets T")
	sb.WriteString(" inner join users U on")
	sb.WriteString(" 	T.askPushFlag = 0")
	sb.WriteString("	and T.userId = U.userId")
	sb.WriteString("	and U.deleteFlag = 0")
	sb.WriteString("	and U.token is not null")
    
	stmt, err := db.Prepare(sb.String())
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer rows.Close()
	for rows.Next(){
		var seq int64
		var token string
		rows.Scan(&seq, &token)
		logrus.Info("対象トークンをマップに格納:",token)
		targetMap[seq] = token
    }
	if err = rows.Err(); err != nil {
		logrus.Error(err)
		return err
	}
	return nil
} 

func workerForPushNew(innerWg *sync.WaitGroup, innerErrChan chan error, queue chan TargetStruct, app *firebase.App) {
	defer innerWg.Done()

	db, err := sql.Open(oci8, dbConnectionInfo)
	if err != nil {
		logrus.Error(err)
		innerErrChan <- err
		return
	}
	defer db.Close()

	ctx := context.Background()
	client, err := app.Messaging(ctx)
	if err != nil {
		logrus.Error("error getting Messaging client: %v\n", err)
		innerErrChan <- err
		return
	}

	for target := range queue {
		logrus.Info("新着質問を対象者にプッシュ通知")
		message := &messaging.Message{
			Data: map[string]string{
				"owner": others,
				"type": new,
				"questionId": "0",
				"questionSeq": "0",
				"question": "",
			},
			Token: target.Token,
		}

		//プッシュ通知
		response, err := client.Send(ctx, message)
		if err != nil {
			logrus.Info("Invalid Token:", target.Token)
			//ユーザのテdeleteフラグ更新
			err := deleteUser(db, target.Token)
			if err != nil {
				logrus.Error("ユーザの削除に失敗")	
				innerErrChan <- err
				break
			}
			continue
		}
		logrus.Info("Successfully sent message:", response)

		//通知完了フラグ更新処理
		sb := strings.Builder{}
		sb.WriteString("update targets set")
		sb.WriteString("    askPushFlag = 1")
		sb.WriteString("    , modifieddatetime = :param1")
		sb.WriteString(" where")
		sb.WriteString("    seq = :param2")

		stmt, err := db.Prepare(sb.String())
		if err != nil {
			logrus.Error(err)
			innerErrChan <- err
			continue
		}
		defer stmt.Close()

		_, err = stmt.Exec(service.GetNow(), target.Seq)
		if err != nil {
			logrus.Error(err)
			innerErrChan <- err
			continue
		}	
	}
}

//新着質問を対象者にプッシュ通知
func pushNewQuestion(wg *sync.WaitGroup, errChan chan error, quitChan chan struct{}, app *firebase.App) {
	defer wg.Done()
	innerWg := &sync.WaitGroup{} 
	loopFlag := true
	queue := make(chan TargetStruct, 10)
	innerErrChan := make(chan error)

	for i:= 0; i < workerNum; i++ {
		innerWg.Add(1)
		go workerForPushNew(innerWg, innerErrChan, queue, app)
	}

	go func() {
		for loopFlag {
			//対象者リスト抽出
			targetMap := make(map[int64]string, 0) 
			err := extractTargetTokens(targetMap)
			if (err != nil) {
				errChan <- err
				break
			}
			logrus.Info("対象者数:", strconv.Itoa(len(targetMap)))
		
			for targetSeq, token := range targetMap {
				queue <- TargetStruct{Seq: targetSeq, Token: token}
			}
		
			time.Sleep(10 * time.Second)
		}
	}()

	go func() {
		defer close(innerErrChan)
		defer close(queue)
		for innerErr := range innerErrChan {
			errChan <- innerErr
			break
		}
	}()

	<- quitChan
	loopFlag = false
	innerWg.Wait()
}

//集計結果をプッシュする対象を抽出
func extractResultTargetTokens(targetInfoSlice *[]TargetInfo, owner string) error {
	logrus.Info("集計結果をプッシュする対象を抽出開始")

	db, err := sql.Open(oci8, dbConnectionInfo)
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer db.Close()

	var stmt *sql.Stmt 
	switch owner {
	case own:
		sb := strings.Builder{}
		sb.WriteString("select")
		sb.WriteString("    Q.seq")
		sb.WriteString("    , Q.questionId")
		sb.WriteString("    , U.token")
		sb.WriteString("    , Q.question")
		sb.WriteString(" from questions Q")
		sb.WriteString(" inner join users U on")
		sb.WriteString(" 	Q.finalPushFlag = 0")
		sb.WriteString("	and Q.determinationFlag = 1")
		sb.WriteString("	and Q.userId = U.userId")
		sb.WriteString("	and U.deleteFlag = 0")
		
		stmt, err = db.Prepare(sb.String())
	case others:
		sb := strings.Builder{}
		sb.WriteString("select")
		sb.WriteString("    Q.seq")
		sb.WriteString("    , U.token")
		sb.WriteString("    , Q.question")
		sb.WriteString(" from targets T")
		sb.WriteString(" inner join users U on")
		sb.WriteString(" 	T.finalPushFlag = 0")
		sb.WriteString("	and T.determinationFlag = 1")
		sb.WriteString("	and T.userId = U.userId")
		sb.WriteString("	and U.deleteFlag = 0")
		sb.WriteString(" inner join questions Q on")
		sb.WriteString(" 	T.questionSeq = Q.seq")
		
		stmt, err = db.Prepare(sb.String())
	default:
		break
	}
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer rows.Close()
	for rows.Next(){
		targetInfo := TargetInfo{}

		switch owner {
		case own:
			targetInfo.Owner = own
			rows.Scan(&targetInfo.QuestionSeq, &targetInfo.QuestionId, &targetInfo.Token, &targetInfo.Question)
		case others:
			targetInfo.Owner = others
			targetInfo.QuestionId = 0
			rows.Scan(&targetInfo.QuestionSeq, &targetInfo.Token, &targetInfo.Question)
		default:
			break
		}

		*targetInfoSlice = append(*targetInfoSlice, targetInfo)
    }
	if err = rows.Err(); err != nil {
		logrus.Error(err)
		return err
	}
	return nil
} 

//現在トークンが有効な対象を抽出
func extractActiveTokens(db *sql.DB, targetMap map[string]string) error {
	logrus.Info("有効なトークンを抽出開始")

	sb := strings.Builder{}
	sb.WriteString("select")
	sb.WriteString("    userId")
	sb.WriteString("    , token")
	sb.WriteString(" from users")
	sb.WriteString(" where")
	sb.WriteString("    deleteFlag = 0")
	sb.WriteString("    and token is not null")
    
	stmt, err := db.Prepare(sb.String())

	if err != nil {
		logrus.Error(err)
		return err
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer rows.Close()
	for rows.Next(){
		logrus.Info("抽出ループ開始")
		var userId string
		var token string

		rows.Scan(&userId, &token)
		//トークンが登録されているユーザにのみプッシュ通知
		logrus.Info("対象者をマップに格納")
		targetMap[userId] = token
    }
	if err = rows.Err(); err != nil {
		logrus.Error(err)
		return err
	}
	return nil
} 

func workerForPushFinish(innerWg *sync.WaitGroup, innerErrChan chan error, queue chan TargetInfo, app *firebase.App, owner string) {
	defer innerWg.Done()

	db, err := sql.Open(oci8, dbConnectionInfo)
	if err != nil {
		logrus.Error(err)
		innerErrChan <- err
		return
	}
	defer db.Close()

	ctx := context.Background()
	client, err := app.Messaging(ctx)
	if err != nil {
		logrus.Error("error getting Messaging client: %v\n", err)
		innerErrChan <- err
		return
	}

	for targetInfo := range queue {
		logrus.Info("質問の集計結果をプッシュ: ", targetInfo.Token)
		message := &messaging.Message{
			Data: map[string]string{
				"owner": targetInfo.Owner,
				"type": "result",
				"questionId": strconv.FormatInt(targetInfo.QuestionId, 10),
				"questionSeq": strconv.FormatInt(targetInfo.QuestionSeq, 10),
				"question": targetInfo.Question,
			},
			Token: targetInfo.Token,
		}

		//プッシュ通知
		response, err := client.Send(ctx, message)
		if err != nil {
			logrus.Info("Invalid Token:", targetInfo.Token)
			//ユーザのテdeleteフラグ更新
			err := deleteUser(db, targetInfo.Token)
			if err != nil {
				logrus.Error("ユーザの削除に失敗")	
				innerErrChan <- err
				break
			}
			continue
		}

		logrus.Info("Successfully sent message:", response)

		logrus.Info("通知完了フラグ更新処理")
		//通知完了フラグ更新処理
		var stmt *sql.Stmt
		switch owner {
		case own:
			sb := strings.Builder{}
			sb.WriteString("update questions set")
			sb.WriteString("    finalPushFlag = 1")
			sb.WriteString("    , modifieddatetime = :param1")
			sb.WriteString(" where")
			sb.WriteString("    seq = :param2")

			stmt, err = db.Prepare(sb.String())
		case others:
			sb := strings.Builder{}
			sb.WriteString("update targets set")
			sb.WriteString("    finalPushFlag = 1")
			sb.WriteString("    , modifieddatetime = :param1")
			sb.WriteString(" where")
			sb.WriteString("    questionSeq = :param2")

			stmt, err = db.Prepare(sb.String())
		default:
			break
		}
		if err != nil {
			logrus.Error(err)
			innerErrChan <- err
			continue
		}
		defer stmt.Close()

		_, err = stmt.Exec(service.GetNow(), targetInfo.QuestionSeq)
		if err != nil {
			logrus.Error(err)
			innerErrChan <- err
			continue
		}	
	}
}

//集計完了したことを質問投稿者にプッシュ通知
func pushFinished(wg *sync.WaitGroup, errChan chan error, quitChan chan struct{}, app *firebase.App, owner string) {
	defer wg.Done()
	innerWg := &sync.WaitGroup{}
	loopFlag := true
	queue := make(chan TargetInfo, 10)
	innerErrChan := make(chan error)

	for i := 0; i < workerNum; i++ {
		innerWg.Add(1)
		go workerForPushFinish(innerWg, innerErrChan, queue, app, owner)
	}

	go func() {
		for loopFlag {
			//対象者リスト抽出
			targetInfoSlice := make([]TargetInfo, 0) 
			err := extractResultTargetTokens(&targetInfoSlice, owner)
			if (err != nil) {
				logrus.Error(err)
				errChan <- err
			}
			logrus.Info("対象者数:", strconv.Itoa(len(targetInfoSlice)))

			for _, targetInfo := range targetInfoSlice {
				queue <- targetInfo
			}
			
			time.Sleep(15 * time.Second)
		}
	}()

	go func() {
		defer close(innerErrChan)
		defer close(queue)
		for innerErr := range innerErrChan {
			errChan <- innerErr
			break
		}
	}()
	
	<- quitChan
	loopFlag = false
	innerWg.Wait()
}

//トークン有効確認プッシュ通知処理
func confirmActiveToken(wg *sync.WaitGroup, errChan chan error, quitChan chan struct{}, app *firebase.App) {
	defer wg.Done()
	innerWg := &sync.WaitGroup{}
	loopFlag := true
	go func() {
		for loopFlag {
			result := false
			innerWg.Add(1)	
			func() {
				defer innerWg.Done()
				logrus.Info("トークン有効確認プッシュ通知処理開始")
				ctx := context.Background()
				client, err := app.Messaging(ctx)
				if err != nil {
					logrus.Errorf("error getting Messaging client: %v\n", err)
					errChan <- err
					return
				}
			
				db, err := sql.Open(oci8, dbConnectionInfo)
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer db.Close()
		
				//対象者リスト抽出
				targetMap := make(map[string]string, 0) 
				err = extractActiveTokens(db, targetMap)
				logrus.Info("対象者数:", strconv.Itoa(len(targetMap)))
				if (err != nil) {
					logrus.Error(err)
					errChan <- err
					return
				}
		
				for userId, token := range targetMap {
					logrus.Info("トーク有効か確認プッシュ: " + token)
					message := &messaging.Message{
						Data: map[string]string{
							"owner": userId,
							"type": activeCheck,
							"questionId": "",
							"questionSeq": "",
							"question": "",
						},
						Token: token,
					}
		
					//プッシュ通知
					_, err := client.Send(ctx, message)
					if err != nil {
						logrus.Info("Invalid Token:", token)
						//ユーザのテdeleteフラグ更新
						err := deleteUser(db, token)
						if err != nil {
							logrus.Error("ユーザの削除に失敗")
							errChan <- err
							return	
						}
						continue
					}
					logrus.Info("Active Token:", token)
				}
				result = true
			}()

			if !result {
				break
			}
			
			time.Sleep(100 * time.Second)
		}
	}()
	
	<- quitChan
	loopFlag = false
	innerWg.Wait()
}

//ユーザの削除処理
func deleteUser(db *sql.DB, token string) error{
	logrus.Info("ユーザ削除:", token)
	sb := strings.Builder{}
	sb.WriteString("update users set")
	sb.WriteString("    deleteFlag = 1")
	sb.WriteString("    , modifieddatetime = :param1")
	sb.WriteString(" where")
	sb.WriteString("    token = :param2")

	stmt, err := db.Prepare(sb.String())
	if err != nil {
		logrus.Error(err)
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(service.GetNow(), token)
	if err != nil {
		logrus.Error(err)
		return err
	}
	return nil
}

//集計結果を受信した対象者リストを削除
func deleteTargets(wg *sync.WaitGroup, errChan chan error, quitChan chan struct{}) {
	defer wg.Done()
	innerWg := &sync.WaitGroup{}
	loopFlag := true
	go func() {
		for loopFlag {
			result := false
			innerWg.Add(1)
			func(){
				defer innerWg.Done()
				logrus.Info("集計結果を受信した対象者リストを削除処理開始")
				//新着質問リストを取得
				db, err := sql.Open(oci8, dbConnectionInfo)
				if err != nil {
					errChan <- err
					return
				}
				defer db.Close()
		
				sb := strings.Builder{}
				sb.WriteString("select")
				sb.WriteString("    seq")
				sb.WriteString(" from targets")
				sb.WriteString(" where")
				sb.WriteString("    resultReceiveFlag = 1")
				sb.WriteString("    and finalPushFlag = 1")
				
				stmt, err := db.Prepare(sb.String())
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer stmt.Close()
		
				rows, err := stmt.Query()
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer rows.Close()
		
				var seq int64
				
				//新着質問リスト一つずつTargetsテーブルに登録し、質問テーブルのaskFlagを更新する
				for rows.Next() {
					rows.Scan(&seq)
					sb = strings.Builder{}
					sb.WriteString("delete from targets")
					sb.WriteString(" where")
					sb.WriteString("    seq = :param1")
		
					innerStmt, err := db.Prepare(sb.String())
					if err != nil {
						logrus.Error(err)
						errChan <- err
						return
					}
					defer innerStmt.Close()
		
					_, err = innerStmt.Exec(seq)
					if err != nil {
						logrus.Error(err)
						errChan <- err
						return
					}
				}
				if err = rows.Err(); err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				result = true
			}()

			if !result {
				break
			}
		
			time.Sleep(100 * time.Second)
		}
	}()

	<- quitChan
	loopFlag = false
	innerWg.Wait()
}

//集計が完了した回答を削除
func deleteAnswers(wg *sync.WaitGroup, errChan chan error, quitChan chan struct{}) {
	defer wg.Done()
	innerWg := &sync.WaitGroup{}
	loopFlag := true
	go func() {
		for loopFlag {
			result := false
			innerWg.Add(1)
			func() {
				defer innerWg.Done()
				logrus.Info("集計が完了した回答を削除処理開始")
				//新着質問リストを取得
				db, err := sql.Open(oci8, dbConnectionInfo)
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer db.Close()
		
				sb := strings.Builder{}
				sb.WriteString("select")
				sb.WriteString("    seq")
				sb.WriteString(" from answers")
				sb.WriteString(" where")
				sb.WriteString("    determinationFlag = 1")
				
				stmt, err := db.Prepare(sb.String())
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer stmt.Close()
		
				rows, err := stmt.Query()
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer rows.Close()
		
				var seq int64
				
				//新着質問リスト一つずつTargetsテーブルに登録し、質問テーブルのaskFlagを更新する
				for rows.Next() {
					rows.Scan(&seq)
					sb = strings.Builder{}
					sb.WriteString("delete from answers")
					sb.WriteString(" where")
					sb.WriteString("    seq = :param1")
		
					innerStmt, err := db.Prepare(sb.String())
					if err != nil {
						logrus.Error(err)
						errChan <- err
					return
					}
					defer innerStmt.Close()
		
					_, err = innerStmt.Exec(seq)
					if err != nil {
						logrus.Error(err)
						errChan <- err
					return
					}
				}
				if err = rows.Err(); err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				result = true
			}()

			if !result {
				break
			}
	
			time.Sleep(100 * time.Second)
		}
	}()

	<- quitChan
	loopFlag = false
	innerWg.Wait()
}

func initializeAppWithServiceAccount() (*firebase.App, error) {
	// [START initialize_app_service_account_golang]
	opt := option.WithCredentialsFile(accountFilePath)
	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		logrus.Fatalf("error initializing app: %v\n", err)
		return nil, err
	}
	// [END initialize_app_service_account_golang]
	return app, nil
}

func main() {
	service.InitSetUpLog(envProduction, logFilePath)
	app, err := initializeAppWithServiceAccount()
	if err!= nil {
		return
	}

	wg := &sync.WaitGroup{}
	wg.Add(7)
	errChan := make(chan error)
	quitChan := make(chan struct{})

	//新着質問を対象者にプッシュ通知
	go pushNewQuestion(wg, errChan, quitChan, app)
	//集計完了したことを質問投稿者にプッシュ通知
	go pushFinished(wg, errChan, quitChan, app, own)
	go pushFinished(wg, errChan, quitChan, app, others)
	//無効なトークンのユーザを削除
	go confirmActiveToken(wg, errChan, quitChan, app)
	//集計結果を受信した対象者リストを削除
	go deleteTargets(wg, errChan, quitChan)
	//集計が完了した回答を削除
	go deleteAnswers(wg, errChan, quitChan)
	//制限時間を過ぎた質問の結果を集計する
	go aggregate(wg, errChan, quitChan)
	
	go func() {
		wg.Wait()
		close(errChan)
	}()

	for internalErr := range errChan {
		logrus.Fatal("内部処理エラー:", internalErr)	
		close(quitChan)
	}
}

