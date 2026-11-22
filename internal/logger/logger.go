package logger

import (
	"fmt"
	"log"
	"os"
	"sync/atomic"

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

var currentLogLevel atomic.Int32

type Logger struct {
	debugLogger *log.Logger
	infoLogger  *log.Logger
	warnLogger  *log.Logger
	errorLogger *log.Logger
	fatalLogger *log.Logger
}

var AppLogger *Logger

func init() {
	SetDebug(config.AppSettings.Debug)
	AppLogger = &Logger{
		debugLogger: log.New(os.Stdout, "[DEBUG] ", log.LstdFlags|log.Lshortfile),
		infoLogger:  log.New(os.Stdout, "[INFO] ", log.LstdFlags|log.Lshortfile),
		warnLogger:  log.New(os.Stderr, "[WARN] ", log.LstdFlags|log.Lshortfile),
		errorLogger: log.New(os.Stderr, "[ERROR] ", log.LstdFlags|log.Lshortfile),
		fatalLogger: log.New(os.Stderr, "[FATAL] ", log.LstdFlags|log.Lshortfile),
	}
}

func SetDebug(enabled bool) {
	if enabled {
		currentLogLevel.Store(LevelDebug)
	} else {
		currentLogLevel.Store(LevelInfo)
	}
}

// output 是级别日志唯一的输出路径，不再额外维护内存历史。
func output(level int, message string, args ...any) {
	if int(currentLogLevel.Load()) > level {
		return
	}
	message = fmt.Sprintf(message, args...)
	levels := [...]string{"debug", "info", "warn", "error", "fatal"}
	if store := activeStore.Load(); store != nil {
		if _, err := store.Append(levels[level], message); err != nil {
			store.report(err)
		}
	}
	outputs := [...]*log.Logger{AppLogger.debugLogger, AppLogger.infoLogger, AppLogger.warnLogger, AppLogger.errorLogger, AppLogger.fatalLogger}
	// output -> 级别方法 -> 调用方，保持原来的源码位置及 stdout/stderr 行为。
	_ = outputs[level].Output(3, message)
}

func Debug(msg string, args ...any) { output(LevelDebug, msg, args...) }
func Info(msg string, args ...any)  { output(LevelInfo, msg, args...) }
func Warn(msg string, args ...any)  { output(LevelWarn, msg, args...) }
func Error(msg string, args ...any) { output(LevelError, msg, args...) }

func Fatal(msg string, args ...any) {
	output(LevelFatal, msg, args...)
	// os.Exit 不执行 defer，必须在退出前主动同步并关闭。
	if err := Close(); err != nil {
		fmt.Fprintf(os.Stderr, "[LOGGER] 关闭日志失败: %s\n", sanitize(err.Error()))
	}
	os.Exit(1)
}
