package tools

import (
	"github.com/sirupsen/logrus"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"
)

var (
	logrusObj *logrus.Logger
	logFile   *os.File
	logDate   string
	logMu     sync.Mutex
	// 当前日志文件对应的日期，供无锁快路径判断是否需要轮转。
	currentDate atomic.Value
	ready       atomic.Bool
)

// Logger 每次请求都会调用，所以常见路径（同一天、已初始化）不加锁。
// 只有跨天或首次初始化才走加锁的慢路径。
func Logger() *logrus.Logger {
	today := time.Now().Format("2006-01-02")
	if ready.Load() && currentDate.Load() == today {
		return logrusObj
	}
	return rotateLogger(today)
}

func rotateLogger(date string) *logrus.Logger {
	logMu.Lock()
	defer logMu.Unlock()

	if logrusObj == nil {
		logrusObj = logrus.New()
		logrusObj.SetLevel(logrus.InfoLevel)
		logrusObj.SetFormatter(&logrus.TextFormatter{TimestampFormat: "2006-01-02 15:04:05"})
	}
	if err := rotateLogLocked(date); err != nil {
		logrusObj.Out = os.Stderr
	}
	ready.Store(true)
	return logrusObj
}

func rotateLogLocked(date string) error {
	if logFile != nil && logDate == date {
		return nil
	}

	logFilePath := path.Join(mustGetwd(), "logs")
	if err := os.MkdirAll(logFilePath, 0o755); err != nil {
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
	currentDate.Store(date)
	return nil
}

func mustGetwd() string {
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "."
}
