package main

import (
    "sync"
    "os"
    "strings"
    "crypto/tls"
    "github.com/streadway/amqp"
    "database/sql"
    _"github.com/mattn/go-oci8"
    "time"
    "encoding/json"
    service "./service"
    logrus "github.com/sirupsen/logrus"
)

const (
    rs2Letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
    port = ":50030"
    client_certFile = "./key/client-cert.pem"
    client_keyFile  = "./key/client-key.pem"
    ca_certFile = "./key/ca-cert.pem"
    logFilePath = "./logs/consumer.log"
    envProduction  = "production"
    envDevelopment = "development"
    oci8 = "oci8"
    dbConnectionInfo = "Go/go@oracle-nodeport:1521/ThirdProject"
    question = "question"
    answer = "answer"
    host = "rabbitmq-cluster"
    scheme = "amqps" 
    rabbitmqPort = 5671
    vhost = "/third-project"
    user = "RABBITMQ_USER"
    password = "RABBITMQ_PASSWORD"
    workerNum = 3
)

var amqpURI = amqp.URI{
        Scheme:   scheme,
        Host:     host,
        Port:     rabbitmqPort,
        Username: os.Getenv(user),
        Password: os.Getenv(password),
        Vhost:    vhost,
    }.String()

// Serializable はシリアライズ可能な構造体であることを示すinterfaceです。
type Serializable interface {
    Serializable()
}

//questionのjson構造体
type QuestionMessage struct {
    UserId string             `json:"userId"`
    QuestionId int          `json:"questionId"`
    Question string           `json:"question"`
    Answer1 string           `json:"answer1"`
    Answer2 string            `json:"answer2"`
    TargetNumber int        `json:"targetNumber"` 
    TimePeriod int         `json:"timePeriod"`
}

// Serializable はシリアライズ可能であることを示します。
func (s QuestionMessage) Serializable() {}

//answerのjson構造体
type AnswerMessage struct {
    QuestionSeq int64          `json:"questionSeq"`
    UserId string              `json:"userId"`
    Decision int32             `json:"decision"`
    TimeLimit string           `json:"timeLimit"`
}

type TargetInfo struct {
    QuestionSeq int64
    UserId string
    TargetNumber int32
    TimePeriod int32
}

// Serializable はシリアライズ可能であることを示します。
func (s AnswerMessage) Serializable() {}

//キューからメッセージを取得
func consume(queueName string, quitChan chan struct{}, fatalChan chan error, wg *sync.WaitGroup) {
    defer wg.Done()

    logrus.Info(os.Getenv(user))
    logrus.Info(os.Getenv(password))

    cfg := &tls.Config{InsecureSkipVerify : true}
    conn, err := amqp.DialTLS(amqpURI, cfg)
    // conn, err := amqp.Dial(conf)
    if err != nil {
        logrus.Fatal("Failed to connect to MQ", err)
        fatalChan <- err
        return
    }
    defer conn.Close()

    channel, err := conn.Channel()
    if err != nil {
        logrus.Fatal("Failed to open a channel", err)
        return
    }

    messages, err := waitMessages(channel, queueName)
    if err != nil {
        logrus.Fatal("Failed to register a consumer", err)
        fatalChan <- err
        return
    }

    logrus.Info(" [*] Waiting for:", queueName)

    innerWg := &sync.WaitGroup{}
    innerErrChan := make(chan error)
    workerQueue := make(chan amqp.Delivery, 10)

    //insert処理を並列稼働
    for i := 0; i < workerNum; i++ {
        innerWg.Add(1)
        go worker(channel, queueName, workerQueue, innerErrChan, innerWg)
    }

    loopFlag := true 

    //親スレッドからの停止信号が来るまでキューから情報を取得ループ
    go func() {
        for data := range messages {
            if loopFlag {
                workerQueue <- data
            }
        }
    }()

    //ワーカースレッドの内部エラーを取得した場合、親スレッドに知らせる
    go func() {
        defer close(innerErrChan)
        for innerErr := range innerErrChan {
            fatalChan <- innerErr
            break 
        }
    }()
    
    <-quitChan
    loopFlag = false
    //ワーカースレッドの終了を待つ
    close(workerQueue)
    innerWg.Wait() 
}

//キューメッセージを取得
func waitMessages(channel *amqp.Channel, queueName string) (<-chan amqp.Delivery, error) {
    queue, err := channel.QueueDeclare(
        queueName, // name
        true,      // durable
        false,      // delete when unused
        false,      // exclusive
        false,      // no-wait
        nil,        // arguments
    )
    if err != nil {
        logrus.Fatal("Failed to declare a queue", err)
        return nil, err
    }

    messages, err := channel.Consume(
        queue.Name,     // queue
        "",         // consumer
        false,       // auto-ack
        false,      // exclusive
        false,      // no-local
        false,      // no-wait
        nil,        // arguments
    )

    return messages, err
}

//insert処理を並列稼働
func worker(channel *amqp.Channel, queueName string, queue chan amqp.Delivery, innerErrChan chan error, innerWg *sync.WaitGroup) {
    defer innerWg.Done() 
    for data := range queue {
        switch queueName {
        case question:
            //新規質問を登録
            err := insertQuestion(string(data.Body), innerErrChan)
            checkError(err, data.DeliveryTag, channel)
        case answer:
            //回答を登録
            err := insertAnswer(string(data.Body), innerErrChan)
            checkError(err, data.DeliveryTag, channel)
        }     
    } 
}

//キューメッセージを適切に処理できたか確認
func checkError(err error, deliveryTag uint64, channel *amqp.Channel) {
    if err != nil {
        //キューにメッセージを再格納
        channel.Nack(deliveryTag, false, true)
        // panic(fmt.Sprintf("データの登録に失敗しました: %s", err))
        logrus.Warn("キューにメッセージ再格納:", err)
        return
    }
    //正しく処理されたので、メッセージ破棄
    channel.Ack(deliveryTag, false)
    logrus.Info("キューメッセージ破棄")
}

//新規質問を登録
func insertQuestion(message string, innerErrChan chan error) error {
    logrus.Info("新着情報登録開始:", message)
    db, err := sql.Open(oci8, dbConnectionInfo)
    if err != nil {
        logrus.Error(err)
        innerErrChan <- err
        return err
    }
    defer db.Close()

    var questionMessage QuestionMessage
    err = json.Unmarshal([]byte(message), &questionMessage)
    if err != nil {
        logrus.Error("jsonデシリアライズに失敗しました:",message)
        innerErrChan <- err
        return err
    }

    sb := strings.Builder{}
    sb.WriteString("insert into questions(")
    sb.WriteString("    seq")
    sb.WriteString("    , userid")
    sb.WriteString("    , questionId")
    sb.WriteString("    , question")
    sb.WriteString("    , answer1")
    sb.WriteString("    , answer2")
    sb.WriteString("    , targetNumber")
    sb.WriteString("    , timePeriod")
    sb.WriteString("    , createdDateTime")
    sb.WriteString(") values(")
    sb.WriteString("    seq_questions.NEXTVAL")
    sb.WriteString("    , :param1")
    sb.WriteString("    , :param2")
    sb.WriteString("    , :param3")
    sb.WriteString("    , :param4")
    sb.WriteString("    , :param5")
    sb.WriteString("    , :param6")
    sb.WriteString("    , :param7")
    sb.WriteString("    , :param8")
    sb.WriteString(")")

    stmt, err := db.Prepare(sb.String())
    if err != nil {
        logrus.Error(err)
        innerErrChan <- err
        return err
    }
    defer stmt.Close()

    _, err = stmt.Exec(
        questionMessage.UserId,
        questionMessage.QuestionId,
        questionMessage.Question, 
        questionMessage.Answer1, 
        questionMessage.Answer2, 
        questionMessage.TargetNumber, 
        questionMessage.TimePeriod, 
        service.GetNow())

    if err != nil {
        logrus.Error(err)
        innerErrChan <- err
        return err
    }

    logrus.Info("新着情報登録完了")
    return nil
}

//回答を登録
func insertAnswer(message string, innerErrChan chan error) error {
    logrus.Info("回答登録開始:", message)
    db, err := sql.Open(oci8, dbConnectionInfo)
    if err != nil {
        logrus.Error(err)
        innerErrChan <- err
        return err
    }
    defer db.Close()

    var answerMessage AnswerMessage
    err = json.Unmarshal([]byte(message), &answerMessage)
    if err != nil {
        logrus.Error("jsonデシリアライズに失敗しました:",message)
        innerErrChan <- err
        return err
    }

    sb := strings.Builder{}
    sb.WriteString("insert into answers(")
    sb.WriteString("    seq")
    sb.WriteString("    , questionSeq")
    sb.WriteString("    , userid")
    sb.WriteString("    , decision")
    sb.WriteString("    , timeLimit")
    sb.WriteString("    , createdDateTime")
    sb.WriteString(") values(")
    sb.WriteString("    seq_answers.NEXTVAL")
    sb.WriteString("    , :param1")
    sb.WriteString("    , :param2")
    sb.WriteString("    , :param3")
    sb.WriteString("    , :param4")
    sb.WriteString("    , :param5")
    sb.WriteString(")")

    stmt, err := db.Prepare(sb.String())
    if err != nil {
        logrus.Error(err)
        innerErrChan <- err
        return err
    }
    defer stmt.Close()

    _, err = stmt.Exec(
        answerMessage.QuestionSeq,
        answerMessage.UserId,
        answerMessage.Decision,
        answerMessage.TimeLimit,
        service.GetNow())

    if err != nil {
        logrus.Error(err)
        innerErrChan <- err
        return err
    }
    logrus.Info("回答登録完了")
    return nil
}


//insert処理を並列稼働
func WorkerForTarget(innerWg *sync.WaitGroup, innerErrChan chan error, queue chan *TargetInfo) {
    defer innerWg.Done() 

    db, err := sql.Open(oci8, dbConnectionInfo)
    if err != nil {
        logrus.Error(err)
        innerErrChan <- err
        return
    }
    defer db.Close()

    for targetInfo := range queue {
        userSlice := make([]string, 0)
    
        //usersテーブルからランダムに対象者を抽出する
        err := extractTargetUser(db, &userSlice, targetInfo.UserId, targetInfo.TargetNumber)
        if err != nil {
            innerErrChan <- err
            continue
        }
    
        //トランザクション開始（targetsテーブルと、questionsテーブルを同時に更新する）
        err = execTransaction(db, &userSlice, targetInfo.TimePeriod, targetInfo.QuestionSeq)
        if err != nil {
            innerErrChan <- err
            continue
        }
    }
}

//新着質問をランダムに抽出した対象者に回答問い合わせ
func insertTargets(quitChan chan struct{}, fatalChan chan error, wg *sync.WaitGroup) {
    defer wg.Done()
    innerWg := &sync.WaitGroup{}
    innerErrChan := make(chan error)
    queue := make(chan *TargetInfo, 10)

     //insert処理を並列稼働
     for i := 0; i < workerNum; i++ {
        innerWg.Add(1)
        go WorkerForTarget(innerWg, innerErrChan, queue)
    }

    loopFlag := true

    go func() {
        for loopFlag {
            result := false
            func() {
                logrus.Info("新着質問リスト取得開始")
                db, err := sql.Open(oci8, dbConnectionInfo)
                if err != nil {
                    logrus.Error(err)
                    fatalChan <- err
                    return
                }
                defer db.Close()
        
                sb := strings.Builder{}
                sb.WriteString("select")
                sb.WriteString("    seq")
                sb.WriteString("    , userid")
                sb.WriteString("    , targetNumber")
                sb.WriteString("    , timePeriod")
                sb.WriteString(" from questions")
                sb.WriteString(" where")
                sb.WriteString("    askFlag = 0")
            
                stmt, err := db.Prepare(sb.String())
                if err != nil {
                    logrus.Error(err)
                    fatalChan <- err
                    return
                }
                defer stmt.Close()
        
                rows, err := stmt.Query()
                if err != nil {
                    logrus.Error(err)
                    fatalChan <- err
                    return
                }
                defer rows.Close()
                    
                //新着質問リスト一つずつTargetsテーブルに登録し、質問テーブルのaskFlagを更新する
                for rows.Next() {
                    targetInfo := TargetInfo{}
                    rows.Scan(&targetInfo.QuestionSeq, &targetInfo.UserId, &targetInfo.TargetNumber, &targetInfo.TimePeriod)
                    queue <- &targetInfo
                }
                if err = rows.Err(); err != nil {
                    logrus.Error(err)
                    fatalChan <- err
                    return
                }
                result = true
            }()

            if !result {
                break
            }
            
            time.Sleep(10 * time.Second)
        }
    }()

    //ワーカースレッドの内部エラーを取得した場合、親スレッドに知らせる
    go func() {
        defer close(innerErrChan)
        defer close(queue) 
        for innerErr := range innerErrChan {
            fatalChan <- innerErr
            break 
        }
    }()

    <-quitChan
    loopFlag = false
    //ワーカースレッドの終了を待つ
    innerWg.Wait() 
}

//usersテーブルからランダムに対象者を抽出する
func extractTargetUser(db *sql.DB, userSlice *[]string, userId string, targetNumber int32) error {
    logrus.Info("target配列に格納開始")
    sb := strings.Builder{}
    sb.WriteString("select")
    sb.WriteString("    userId")
    sb.WriteString(" from (")
    sb.WriteString("    select")
    sb.WriteString("        userId")
    sb.WriteString("    from users")
    sb.WriteString("    where")
    sb.WriteString("        deleteFlag = 0")
    sb.WriteString("        and userId <> :param1")
    sb.WriteString("    order by")
    sb.WriteString("        dbms_random.random")
    sb.WriteString("    )")
    sb.WriteString(" where")
    sb.WriteString("    ROWNUM <= :param2")

    innerStmt, err := db.Prepare(sb.String())
    if err != nil {
        logrus.Error(err)
        return err
    }
    defer innerStmt.Close()

    targetRows, err := innerStmt.Query(userId, targetNumber)
    if err != nil {
        logrus.Error(err)
        return err
    }
    defer targetRows.Close() 

    for targetRows.Next(){
        var userId string
        targetRows.Scan(&userId)
        *userSlice = append(*userSlice, userId)
    }
    return nil
}

//トランザクション開始（targetsテーブルと、questionsテーブルを同時に更新する）
func execTransaction(db *sql.DB, userSlice *[]string, timePeriod int32, questionSeq int64) error {
    logrus.Info("トランザクション開始")
    //timelimit取得
    timeLimit := time.Now().Add(time.Duration(timePeriod) * time.Minute).Format("2006-01-02 15:04:05")

    transaction, err := db.Begin() 
    if err != nil {
        logrus.Error(err)
        return err
    }

    //targetテーブル挿入
    sb := strings.Builder{}
    sb.WriteString("insert into targets(")
    sb.WriteString("    seq")
    sb.WriteString("    , userid")
    sb.WriteString("    , questionSeq")
    sb.WriteString("    , timeLimit")
    sb.WriteString("    , createdDateTime")
    sb.WriteString(") values(")
    sb.WriteString("    seq_targets.NEXTVAL")
    sb.WriteString("    , :param1")
    sb.WriteString("    , :param2")
    sb.WriteString("    , :param3")
    sb.WriteString("    , :param4")
    sb.WriteString(")")

    innerStmt, err := db.Prepare(sb.String())
    if err != nil {
        logrus.Error(err)
        return err
    }

    for _, userId := range *userSlice {
        logrus.Info("対象者:", userId)

        _, err = innerStmt.Exec(
            userId,
            questionSeq,
            timeLimit,
            service.GetNow())
    
        if err != nil {
            logrus.Fatal(err)
            transaction.Rollback()
            return err
        }
    }

    //questionsテーブル更新（timeLimit, askFlag）
    sb = strings.Builder{}
    sb.WriteString("update questions set")
    sb.WriteString("    timeLimit = :param1")
    sb.WriteString("    , askFlag = 1")
    sb.WriteString("    , modifieddatetime = :param2")
    sb.WriteString(" where")
    sb.WriteString("    seq = :param3")

    innerStmt, err = db.Prepare(sb.String())
    if err != nil {
        logrus.Error(err)
        transaction.Rollback()
        return err
    }

    _, err = innerStmt.Exec(
        timeLimit,
        service.GetNow(),
        questionSeq)

    if err != nil {
        logrus.Error(err)
        transaction.Rollback()
        return err
    }

    //コミット
    transaction.Commit()

    logrus.Info("トランザクションコミット")
    return nil
}

func main() {
    service.InitSetUpLog(envDevelopment, logFilePath)

    quitChan := make(chan struct{})
    fatalChan := make(chan error)
    wg := &sync.WaitGroup{}
    wg.Add(3)

    go consume(question, quitChan, fatalChan, wg)
    go consume(answer, quitChan, fatalChan, wg)
    go insertTargets(quitChan, fatalChan, wg)

    go func() {
        wg.Wait()
        close(fatalChan)
    }()

    for err := range fatalChan {
        logrus.Fatal("内部エラー:", err)
        close(quitChan)
    }
}

