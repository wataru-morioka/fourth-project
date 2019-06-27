package main

import (
    // "crypto/tls"
    // "crypto/x509"
    "strings"
    "context"
    "net"
    "google.golang.org/grpc"
    pb "./pb-authen"
    "database/sql"
    _"github.com/mattn/go-oci8"
    "math/rand"
    //TODO 使用ライブラリ変更→redigo
    "github.com/go-redis/redis"
    "time"
    "google.golang.org/grpc/credentials"
    "google.golang.org/grpc/grpclog"
    service "./service"
    logrus "github.com/sirupsen/logrus"
    // "io/ioutil"
)

const (
    rs2Letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
    port = ":50030"
    certFile = "./key/server-cert.pem"
    keyFile  = "./key/server-key.pem"
    logFilePath = "./logs/authen.log"
    envProduction  = "production"
    envDevelopment = "development"
    oci8 = "oci8"
    dbConnectionInfo = "Go/go@oracle-nodeport:1521/ThirdProject"
)

var redisClient = redis.NewClient(&redis.Options{
    Addr:     "redis-nodeport:6379",
    Password: "redis",
    DB:       0,  // use default DB
})

// gRPC struct
type server struct {
}

//ユーザIDがすでに登録済みか確認
func getAlreadyCount(db *sql.DB, userId string) (int, error) {
    sb := strings.Builder{}
    sb.WriteString("select")
    sb.WriteString("    count(1) as count")
    sb.WriteString(" from users")
    sb.WriteString(" where")
    sb.WriteString("    userId = :param1")
    
    stmt, err := db.Prepare(sb.String())
    if err != nil {
        return 0, err 
    }
    defer stmt.Close()
    rows, err := stmt.Query(userId)
    if err != nil {
        return 0, err
    }
    defer rows.Close()

    var count int
    for rows.Next() {
        rows.Scan(&count)
    }

    if err = rows.Err(); err != nil {
        return 0, err 
    }

    return count, nil
}

// ユーザ登録処理
func (s *server) Register(ctx context.Context, request *pb.RegistrationRequest) (*pb.RegistrationResult, error) {
	logrus.Info("Register Request from:", request.UserId)
    
    db, err := sql.Open(oci8, dbConnectionInfo)
    if err != nil {
        logrus.Error(err)
        return &pb.RegistrationResult{Result: false, Password: "", SessionId: "err"}, nil 
    }
    defer db.Close()

    //ユーザIDがすでに登録済みか確認
    count, err := getAlreadyCount(db, request.UserId)
    if err != nil {
        logrus.Error(err)
        return &pb.RegistrationResult{Result: false, Password: "", SessionId: "err"}, nil 
    }
    
    if count != 0 {
        return &pb.RegistrationResult{Result: false, Password: "", SessionId: ""}, nil 
    }

    //ランダムなパスワード文字列を生成
    password := randString(16)

    //ユーザ情報登録
    sb := strings.Builder{}
    sb.WriteString("insert into users(")
    sb.WriteString("    seq")
    sb.WriteString("    , userId")
    sb.WriteString("    , token")
    sb.WriteString("    , password")
    sb.WriteString("    , createdDateTime")
    sb.WriteString(") values(")
    sb.WriteString("    seq_users.NEXTVAL")
    sb.WriteString("    , :param1")
    sb.WriteString("    , :param2")
    sb.WriteString("    , :param3")
    sb.WriteString("    , :param4")
    sb.WriteString(")")
    
    stmt, err := db.Prepare(sb.String())
    if err != nil {
        logrus.Error(err)
        return &pb.RegistrationResult{Result: false, Password: "", SessionId: "err"}, nil 
    }

    _, err = stmt.Exec(request.UserId, request.Token, password, service.GetNow())
    if err != nil {
        logrus.Error(err)
        return &pb.RegistrationResult{Result: false, Password: "", SessionId: "err"}, nil 
    }

    //ランダムなセッション文字列生成
    sessionId := randString(64) 

    //セッションとユーザIDのペア情報をキャッシュに保存
    err = setSession(sessionId, request.UserId)
    if err != nil {
        logrus.Error(err)
        return &pb.RegistrationResult{Result: false, Password: "", SessionId: "err"}, nil  
    }

    return &pb.RegistrationResult{Result: true, Password: password, SessionId: sessionId, Status: 0}, nil
}

//ユーザトークン更新
func updateToken(db *sql.DB, token string, userId string) error {
    sb := strings.Builder{}
    if (len(token) == 0) {
        sb.WriteString("update users set")
        sb.WriteString("    modifieddatetime = :param1")
        sb.WriteString("    , deleteFlag = 0")
        sb.WriteString(" where")
        sb.WriteString("    userId = :param2")
    } else {
        sb.WriteString("update users set")
        sb.WriteString("    token = :param1")
        sb.WriteString("    , modifieddatetime = :param2")
        sb.WriteString("    , deleteFlag = 0")
        sb.WriteString(" where")
        sb.WriteString("    userId = :param3")
    }
    stmt, err := db.Prepare(sb.String())

    if err != nil {
        return err 
    }

    if (len(token) == 0) {
        _, err = stmt.Exec(service.GetNow(), userId)
    } else {
        _, err = stmt.Exec(token, service.GetNow(), userId)
    }

    if err != nil {
        return err 
    }

    return nil
}

// ログイン処理
func (s *server) Login(ctx context.Context, request *pb.LoginRequest) (*pb.LoginResult, error) {
    logrus.Info("Login Request from:", request.UserId)
    
    db, err := sql.Open(oci8, dbConnectionInfo)
    if err != nil {
        logrus.Error(err)
        return &pb.LoginResult{Result: false, SessionId: "", Status: 0}, nil 
    }
    defer db.Close()
    
    sb := strings.Builder{}
    sb.WriteString("select")
    sb.WriteString("    status")
    sb.WriteString(" from users")
    sb.WriteString(" where")
    sb.WriteString("    userid = :param1")
    sb.WriteString("    and password = :param2")

    stmt, err := db.Prepare(sb.String())
    if err != nil {
        logrus.Fatal(err)
    }
    defer stmt.Close()

    rows, err := stmt.Query(request.UserId, request.Password)
    if err != nil {
        logrus.Fatal(err)
        return &pb.LoginResult{Result: false, SessionId: "", Status: 0}, nil 
    }
    defer rows.Close()

    for rows.Next() {
        var status int32
        rows.Scan(&status)

        //ユーザトークン更新
        err := updateToken(db, request.Token, request.UserId)
        if err != nil {
            logrus.Error(err)
            return &pb.LoginResult{Result: false, SessionId: "", Status: 0}, nil 
        }

        //ランダムなセッション文字列生成
        sessionId := randString(64) 

        //セッションとユーザIDのペア情報をキャッシュに保存
        err = setSession(sessionId, request.UserId) 
        if err != nil {
            logrus.Error(err) 
            return &pb.LoginResult{Result: false, SessionId: "", Status: 0}, nil  
        }

        return &pb.LoginResult{Result: true, SessionId: sessionId, Status: status}, nil
    }
    if err = rows.Err(); err != nil {
        logrus.Error(err)
    }
    //リクエストのアカウントが無効だった場合
    return &pb.LoginResult{Result: false, SessionId: "", Status: 0}, nil
}

// ログアウト処理
func (s *server) Logout(ctx context.Context, request *pb.LogoutRequest) (*pb.LogoutResult, error) {
    logrus.Info("Logout Request from:", request.SessionId)

    _, err := redisClient.Del(request.SessionId).Result()
    if err != nil {
        logrus.Error(err)
        return &pb.LogoutResult{Result: false}, nil
    }
    logrus.Info("セッションキャッシュクリア完了:", request.SessionId)
    return &pb.LogoutResult{Result: true}, nil
}

// セッション延長処理
func (s *server) MaintainSession(ctx context.Context, request *pb.MaintenanceRequest) (*pb.MaintenanceResult, error) {
    logrus.Info("Session Request from:", request.SessionId)
    logrus.Info("Session Request from:", request.UserId)

	//セッションの有効期限延長
	err := redisClient.Set(request.SessionId, request.UserId, 100*time.Second).Err()
    if err != nil {
		logrus.Error("redis.Client.Set Error:", err)
        return &pb.MaintenanceResult{Result: false}, nil 
    }

    return &pb.MaintenanceResult{Result: true}, nil 
}

//ランダム文字列生成
func randString(n int) string {
    rand.Seed(time.Now().UnixNano())
    b := make([]byte, n)
    for i := range b {
        b[i] = rs2Letters[rand.Intn(len(rs2Letters))]
    }
    return string(b)
}

//セッションをキャッシュにセット
func setSession(sessionId string, userId string) error {
    key := sessionId
    err := redisClient.Set(key, userId, 100*time.Second).Err()
    if err != nil {
        return err
    }
    return nil
}

func main() {
    service.InitSetUpLog(envDevelopment, logFilePath)

    lis, err := net.Listen("tcp", port)
    if err != nil {
        logrus.Fatalf("lfailed to listen: %v", err)
        return
    }
    
    logrus.Info("Run server port:", port)

    // grpcServer := grpc.NewServer()

    creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
    if err != nil {
        grpclog.Fatalf("Failed to generate credentials %v", err)
        return
    }
    grpcServer := grpc.NewServer(grpc.Creds(creds))
  
    pb.RegisterAuthenServer(grpcServer, &server{})
    if err := grpcServer.Serve(lis); err != nil {
        logrus.Fatalf("failed to serve: %v", err)
    }
}