package main

import (
	"sync"
	"strings"
    "context"
    "net"
    "google.golang.org/grpc"
    "time"
	pb "./pb-socket"
	"database/sql"
    _"github.com/mattn/go-oci8"
	"github.com/go-redis/redis"
	"strconv"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	service "./service"
    logrus "github.com/sirupsen/logrus"
)

const (
    port = ":50050"
    certFile = "./key/server-cert.pem"
	keyFile  = "./key/server-key.pem"
	logFilePath = "./logs/socket.log"
    envProduction  = "production"
    envDevelopment = "development"
    oci8 = "oci8"
	dbConnectionInfo = "Go/go@oracle-nodeport:1521/ThirdProject"
	accountFilePath = "./service_account.json"
	own = "own"
	others = "others"
)

var redisClient = redis.NewClient(&redis.Options{
	Addr:     "twemproxy-cluster:6222",
	// Password: "redis",
	DB:       0,  // use default DB
})

// gRPC struct
type server struct {
}

//会員情報更新処理
func (s *server) UpdateStatus(ctx context.Context, request *pb.UpdateRequest) (*pb.StatusResult, error) {
	logrus.Info("Update from:", request.SessionId)
	logrus.Infof("Update status: %d", request.Status)

	result := false

	//セッションIDからユーザID取得
	userId, err := getUserId(request.SessionId)
    if err != nil {
		return &pb.StatusResult{Result: result}, nil
	}
	
	logrus.Info("Update userId:", userId)

	//DBのユーザ情報更新
	db, err := sql.Open(oci8, dbConnectionInfo)
    if err != nil {
		logrus.Error(err)
		return &pb.StatusResult{Result: result}, nil
    }
	defer db.Close()
	
	sb := strings.Builder{}
	sb.WriteString("update users set")
	sb.WriteString("    status = :param1")
	sb.WriteString("    , modifieddatetime = :param2")
	sb.WriteString(" where")
	sb.WriteString("    userid = :param3")

	stmt, err := db.Prepare(sb.String())
    if err != nil {
		logrus.Error(err)
		return &pb.StatusResult{Result: result}, nil
    }
    defer stmt.Close()
 
	_, err = stmt.Exec(request.Status, service.GetNow(), userId)
	if err != nil {
		logrus.Error(err)
		return &pb.StatusResult{Result: result}, nil
	}

	logrus.Info("更新完了しました")
	result = true

    return &pb.StatusResult{Result: result}, nil
}

//サーバの情報を受信したことをサーバに通知
func (s *server) ReceiveDone(ctx context.Context, request *pb.DoneRequest) (*pb.DoneResult, error) {
	logrus.Info("サーバの情報をクライアントが受信したことをサーバが受信")

	//セッションIDからユーザID取得
	userId, err := getUserId(request.SessionId)
    if err != nil {
		return &pb.DoneResult{Result: false}, nil
	}
	
	logrus.Info("ReceiveDone userId:", userId)

	//DBのユーザ情報更新
	db, err := sql.Open(oci8, dbConnectionInfo)
    if err != nil {
		logrus.Error(err)
		return &pb.DoneResult{Result: false}, nil
    }
	defer db.Close()
	
	now := service.GetNow()

	switch request.Owner {
	case own:
		logrus.Info("自分の質問に関して、サーバの情報をクライアントが受信したことをサーバが受信したフラグを更新")
		//自分の質問の集計結果を受信したことを通知
		sb := strings.Builder{}
		sb.WriteString("update questions set")
		sb.WriteString("    resultReceiveFlag = 1")
		sb.WriteString("    , modifieddatetime = :param1")
		sb.WriteString(" where")
		sb.WriteString("    seq = :param2")

		stmt, err := db.Prepare(sb.String())
		if err != nil {
			logrus.Error(err)
			return &pb.DoneResult{Result: false}, nil
		}
		defer stmt.Close()
	
		_, err = stmt.Exec(now, request.QuestionSeq)
		if err != nil {
			logrus.Error(err)
			return &pb.DoneResult{Result: false}, nil
		}
		return &pb.DoneResult{Result: true}, nil
	case others:
		logrus.Info("他人の質問に関して、サーバの情報をクライアントが受信したことをサーバが受信したフラグを更新")
		//他人の質問の集計結果を受信したことを通知
		if request.DeterminationFlag {
			sb := strings.Builder{}
			sb.WriteString("update targets set")
			sb.WriteString("    resultReceiveFlag = 1")
			sb.WriteString("    , modifieddatetime = :param1")
			sb.WriteString(" where")
			sb.WriteString("    userId = :param2")
			sb.WriteString("    and questionSeq = :param3")

			stmt, err := db.Prepare(sb.String())
			if err != nil {
				logrus.Error(err)
				return &pb.DoneResult{Result: false}, nil
			}
			defer stmt.Close()
		
			_, err = stmt.Exec(now, userId, request.QuestionSeq)
			if err != nil {
				logrus.Error(err)
				return &pb.DoneResult{Result: false}, nil
			}
			return &pb.DoneResult{Result: true}, nil	
		}
		//新着の他人の質問を受信したことを通知
		sb := strings.Builder{}
		sb.WriteString("update targets set")
		sb.WriteString("    askReceiveFlag = 1")
		sb.WriteString("    , modifieddatetime = :param1")
		sb.WriteString(" where")
		sb.WriteString("    userId = :param2")
		sb.WriteString("    and questionSeq = :param3")

		stmt, err := db.Prepare(sb.String())
		if err != nil {
			logrus.Error(err)
			return &pb.DoneResult{Result: false}, nil
		}
		defer stmt.Close()
	
		_, err = stmt.Exec(now, userId, request.QuestionSeq)
		if err != nil {
			logrus.Error(err)
			return &pb.DoneResult{Result: false}, nil
		}
		return &pb.DoneResult{Result: true}, nil
	default:
		return &pb.DoneResult{Result: false}, nil
	}
}

//ユーザに関する新着情報を取得
func (s *server) GetNewInfo(request *pb.InfoRequest, stream pb.Socket_GetNewInfoServer) error {
	logrus.Info("Request from: %s", request.SessionId)
   
	errChan := make(chan error, 3)
	quitChan := make(chan struct{})
	wg := &sync.WaitGroup{}
	wg.Add(3)

	//自分の質問で回答集計が完了しているものを取得
	go getMyQuestionResult(request, stream, errChan, quitChan, wg)	
	//他人の質問で新着のものを取得
	go getNewOthersQuestion(request, stream, errChan, quitChan, wg)
	//他人の質問で回答が完了しているものを取得
	go getOthersQuestionResult(request, stream, errChan, quitChan, wg)

	go func() {
		wg.Wait()
		close(errChan)
	}()

	for internalErr := range errChan {
		logrus.Error("内部処理エラー:", internalErr)
		close(quitChan)
	}

	info := &pb.InfoResult{}
	info.Result = false
	if err := stream.Send(info); err != nil {
		logrus.Error("クライアントへの送信に失敗")
	}

	logrus.Info("session終了:", request.SessionId)
	return nil
}

//自分の質問で回答集計が完了しているものを取得
func getMyQuestionResult(request *pb.InfoRequest, stream pb.Socket_GetNewInfoServer, errChan chan error, quitChan chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	db, err := sql.Open(oci8, dbConnectionInfo)
	if err != nil {
		logrus.Error(err)
	}
	defer db.Close()

	info := &pb.InfoResult{}
	loopFlag := true
	innerWg := &sync.WaitGroup{}
	go func() {
		for loopFlag {
			innerWg.Add(1)
			result := false
			func() {
				defer innerWg.Done()
				//セッションIDからユーザID取得
				userId, err := getUserId(request.SessionId)
				if err != nil {
					return
				} 

				logrus.Info("ループ1 userId:", userId)

				sb := strings.Builder{}
				sb.WriteString("select")
				sb.WriteString("    seq")
				sb.WriteString("    , questionId")
				sb.WriteString("    , answer1number")
				sb.WriteString("    , answer2number")
				sb.WriteString("    , to_char(timeLimit, 'YYYY-MM-DD HH24:MI:SS')")
				sb.WriteString(" from questions")
				sb.WriteString(" where")
				sb.WriteString("    userid = :param1")
				sb.WriteString("    and resultReceiveFlag = 0")
				sb.WriteString("    and determinationFlag = 1")

				stmt, err := db.Prepare(sb.String())
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer stmt.Close()

				rows, err := stmt.Query(userId)
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer rows.Close()
				for rows.Next() {
					logrus.Info("自分の質問集計検知完了")
					//プロトコル作成
					rows.Scan(&info.QuestionSeq, &info.QuestionId, &info.Answer1Number, &info.Answer2Number, &info.TimeLimit)
					info.Result = true
					info.Owner = own
					info.DeterminationFlag = true
					info.TargetNumber = 0
					info.Question = ""
					info.Answer1 = ""
					info.Answer2 = ""

					count := 0
					for count < 10 {
						// クライアントへメッセージ送信
						if err := stream.Send(info); err != nil {
							logrus.Error("クライアントへの送信に失敗")
							time.Sleep(1 * time.Second)
							count++
							if (count == 10) {
								errChan <- err
								return
							}
							continue
						}
						break
					}
					logrus.Info("送信完了")
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
			
			time.Sleep(5 * time.Second)
		}
	}()
	
	<- quitChan
	loopFlag = false
	innerWg.Wait()
}

//他人の質問で新着のものを取得
func getNewOthersQuestion(request *pb.InfoRequest, stream pb.Socket_GetNewInfoServer, errChan chan error, quitChan chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	db, err := sql.Open(oci8, dbConnectionInfo)
	if err != nil {
		logrus.Error(err)
	}
	defer db.Close()

	info := &pb.InfoResult{}
	loopFlag := true
	innerWg := &sync.WaitGroup{}
	go func() {
		for loopFlag {
			innerWg.Add(1)
			result := false
			func() {
				defer innerWg.Done()
				//セッションIDからユーザID取得
				userId, err := getUserId(request.SessionId)
				if err != nil {
					return
				} 

				logrus.Info("ループ2 userId:", userId)

				sb := strings.Builder{}
				sb.WriteString("select")
				sb.WriteString("    Q.seq")
				sb.WriteString("    , Q.targetNumber")
				sb.WriteString("    , Q.question")
				sb.WriteString("    , Q.answer1")
				sb.WriteString("    , Q.answer2")
				sb.WriteString("    , to_char(T.timeLimit, 'YYYY-MM-DD HH24:MI:SS')")
				sb.WriteString(" from targets T")
				sb.WriteString(" inner join questions Q on")
				sb.WriteString("	T.userid = :param1")
				sb.WriteString("    and T.askReceiveFlag = 0")
				sb.WriteString("    and T.questionSeq = Q.seq")

				stmt, err := db.Prepare(sb.String())
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer stmt.Close()
				rows, err := stmt.Query(userId)
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer rows.Close()
				for rows.Next() {
					logrus.Info("新規質問発見")
					//プロトコル作成
					rows.Scan(&info.QuestionSeq, &info.TargetNumber, &info.Question, &info.Answer1, &info.Answer2, &info.TimeLimit)
					info.Result = true
					info.Owner = others
					info.QuestionId = 0
					info.DeterminationFlag = false
					info.Answer1Number = 0
					info.Answer2Number = 0

					count := 0
					for count < 10 {
						// クライアントへメッセージ送信
						if err := stream.Send(info); err != nil {
							logrus.Error("クライアントへの送信に失敗")
							time.Sleep(1 * time.Second)
							count++
							if (count == 10) {
								errChan <- err
								return
							}
							continue
						}
						break
					}
					
					logrus.Info("送信完了")
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
			
			time.Sleep(10 * time.Second)
		}
	}()
	
	<- quitChan
	loopFlag = false
	innerWg.Wait()
}

//他人の質問で回答が完了しているものを取得
func getOthersQuestionResult(request *pb.InfoRequest, stream pb.Socket_GetNewInfoServer, errChan chan error, quitChan chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	db, err := sql.Open(oci8, dbConnectionInfo)
	if err != nil {
		logrus.Error(err)
	}
	defer db.Close()

	info := &pb.InfoResult{}
	loopFlag := true
	innerWg := &sync.WaitGroup{}
	go func() {
		for loopFlag {
			innerWg.Add(1)
			result := false
			func(){
				defer innerWg.Done()
				//セッションIDからユーザID取得
				userId, err := getUserId(request.SessionId)
				if err != nil {
					return
				} 

				logrus.Info("ループ3 userId:", userId)

				sb := strings.Builder{}
				sb.WriteString("select")
				sb.WriteString("    Q.seq")
				sb.WriteString("    , Q.answer1number")
				sb.WriteString("    , Q.answer2number")
				sb.WriteString("    , to_char(Q.timeLimit, 'YYYY-MM-DD HH24:MI:SS')")
				sb.WriteString(" from targets T")
				sb.WriteString(" inner join questions Q on")
				sb.WriteString("	T.userId = :param1")
				sb.WriteString("    and T.resultReceiveFlag = 0")
				sb.WriteString("    and T.determinationFlag = 1")
				sb.WriteString("    and T.questionSeq = Q.seq")

				stmt, err := db.Prepare(sb.String())
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer stmt.Close()
				rows, err := stmt.Query(userId)
				if err != nil {
					logrus.Error(err)
					errChan <- err
					return
				}
				defer rows.Close()
				for rows.Next() {
					logrus.Info("回答した質問の集計完了")
					//プロトコル作成
					rows.Scan(&info.QuestionSeq, &info.Answer1Number, &info.Answer2Number, &info.TimeLimit)
					info.Result = true
					info.Owner = others
					info.QuestionId = 0
					info.DeterminationFlag = true
					info.TargetNumber = 0
					info.Question = ""
					info.Answer1 = ""
					info.Answer2 = ""

					logrus.Info("seq: ", strconv.FormatInt(info.QuestionSeq, 10))

					count := 0
					for count < 10 {
						// クライアントへメッセージ送信
						if err := stream.Send(info); err != nil {
							logrus.Info("クライアントへの送信に失敗")
							time.Sleep(1 * time.Second)
							count++
							if (count == 10) {
								errChan <- err
								return
							}
							continue
						}
						break
					}
					logrus.Info("送信完了")
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
			
			time.Sleep(15 * time.Second)
		}
	}()
	
	<- quitChan
	loopFlag = false
	innerWg.Wait()
}

//セッションIDからユーザID取得
func getUserId(sessionId string) (string, error){
	userId, err := redisClient.Get(sessionId).Result()
    if err != nil {
		logrus.Info("セッションのキャッシュが削除されております:", sessionId)
		return userId, err
	}

	return userId, nil
}

func main() {
	service.InitSetUpLog(envDevelopment, logFilePath)
    lis, err := net.Listen("tcp", port)
    if err != nil {
        logrus.Fatalf("lfailed to listen: %v", err)
    }
	logrus.Info("Run server port:", port)
	
	// grpcServer := grpc.NewServer()

	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
    if err != nil {
        grpclog.Fatalf("Failed to generate credentials %v", err)
    }
	grpcServer := grpc.NewServer(grpc.Creds(creds))
	
    pb.RegisterSocketServer(grpcServer, &server{})
    if err := grpcServer.Serve(lis); err != nil {
        logrus.Fatalf("failed to serve: %v", err)
    }
}