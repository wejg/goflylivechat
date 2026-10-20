package tools

import (
	"github.com/sirupsen/logrus"
	"log"
	"os"
	"path"
	"sync"
	"time"
)

var (
	logrusObj *logrus.Logger
	logFile   *os.File
	logDate   string
	logMu     sync.Mutex
)

func Logger() *logrus.Logger {
	logMu.Lock()
	defer logMu.Unlock()

	if logrusObj == nil {
		logrusObj = logrus.New()
		logrusObj.SetLevel(logrus.DebugLevel)
		logrusObj.SetFormatter(&logrus.TextFormatter{TimestampFormat: "2006-01-02 15:04:05"})
	}
	if err := rotateLogLocked(time.Now()); err != nil {
		logrusObj.Out = os.Stderr
	}
	return logrusObj
}

func rotateLogLocked(now time.Time) error {
	date := now.Format("2006-01-02")
	if logFile != nil && logDate == date {
		return nil
	}

	logFilePath := path.Join(mustGetwd(), "logs")
	if err := os.MkdirAll(logFilePath, 0777); err != nil {
		return err
	}
	fileName := path.Join(logFilePath, date+".log")
	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	logFile = file
	logDate = date
	logrusObj.Out = logFile
	return nil
}

func mustGetwd() string {
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "."
}

// setOutputFile is kept for package callers that still use the old helper.
func setOutputFile() (*os.File, error) {
	logMu.Lock()
	defer logMu.Unlock()
	if logrusObj == nil {
		logrusObj = logrus.New()
	}
	if err := rotateLogLocked(time.Now()); err != nil {
		log.Println(err)
		return nil, err
	}
	return logFile, nil
}
