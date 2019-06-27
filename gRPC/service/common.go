package service

import (
	// "log"
	"os"
	// "io"
	"gopkg.in/natefinch/lumberjack.v2"
	// "runtime"
	logrus "github.com/sirupsen/logrus"
	"time"
)

const (
    envProduction  = "production"
    envDevelopment = "development"
)

//ロギング設定
func InitSetUpLog(env string, filePath string) {
	logrus.SetReportCaller(true)
	switch env {
	case envProduction:
		logrus.SetLevel(logrus.InfoLevel)
		logrus.SetOutput(&lumberjack.Logger{
			Filename:  filePath,
			MaxAge:    1,
			MaxSize:    1, // megabytes
   			MaxBackups: 2,
			LocalTime: true,
			Compress:  true,
			})

		logrus.SetFormatter(&logrus.JSONFormatter{
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
				logrus.FieldKeyFile:  "file",	
				logrus.FieldKeyFunc:  "func",
			},
		})
	case envDevelopment:
		logrus.SetLevel(logrus.DebugLevel)
		logrus.SetOutput(os.Stdout)
		logrus.SetFormatter(&logrus.TextFormatter{
			DisableColors: false,
			FullTimestamp: true,
			TimestampFormat: "2006-01-02 15:04:05",
			ForceColors:            true,
		})
	default:
	}
}

func GetNow() string {
	return time.Now().Format("2006-01-02 15:04:05")
}
