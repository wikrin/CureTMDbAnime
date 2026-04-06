package logger

import (
	"fmt"
	"log"
	"os"

	"curetmdbanime/internal/config"
)

// 日志级别
const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

var currentLogLevel int // 当前日志级别

type Logger struct {
	debugLogger *log.Logger
	infoLogger  *log.Logger
	warnLogger  *log.Logger
	errorLogger *log.Logger
	fatalLogger *log.Logger
}

var AppLogger *Logger

func init() {
	// 根据配置设置日志级别
	if config.AppSettings.Debug {
		currentLogLevel = LevelDebug
	} else {
		currentLogLevel = LevelInfo
	}

	AppLogger = &Logger{
		debugLogger: log.New(os.Stdout, "[DEBUG] ", log.LstdFlags|log.Lshortfile),
		infoLogger:  log.New(os.Stdout, "[INFO] ", log.LstdFlags|log.Lshortfile),
		warnLogger:  log.New(os.Stderr, "[WARN] ", log.LstdFlags|log.Lshortfile),
		errorLogger: log.New(os.Stderr, "[ERROR] ", log.LstdFlags|log.Lshortfile),
		fatalLogger: log.New(os.Stderr, "[FATAL] ", log.LstdFlags|log.Lshortfile),
	}
}

func Debug(msg string, args ...any) {
	if currentLogLevel <= LevelDebug {
		AppLogger.debugLogger.Output(2, fmt.Sprintf(msg, args...))
	}
}

func Info(msg string, args ...any) {
	if currentLogLevel <= LevelInfo {
		AppLogger.infoLogger.Output(2, fmt.Sprintf(msg, args...))
	}
}

func Warn(msg string, args ...any) {
	if currentLogLevel <= LevelWarn {
		AppLogger.warnLogger.Output(2, fmt.Sprintf(msg, args...))
	}
}

func Error(msg string, args ...any) {
	if currentLogLevel <= LevelError {
		AppLogger.errorLogger.Output(2, fmt.Sprintf(msg, args...))
	}
}

func Fatal(msg string, args ...any) {
	if currentLogLevel <= LevelFatal {
		AppLogger.fatalLogger.Fatalf(msg, args...)
	}
}
