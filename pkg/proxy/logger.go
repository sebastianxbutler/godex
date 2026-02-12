package proxy

import (
	"fmt"
	"log"
	"os"
	"strings"
)

type LogLevel int

const (
	LogLevelError LogLevel = iota
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
)

func ParseLogLevel(level string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn", "warning":
		return LogLevelWarn
	case "error":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

type Logger struct {
	level  LogLevel
	logger *log.Logger
}

func NewLogger(level LogLevel) *Logger {
	return &Logger{
		level:  level,
		logger: log.New(os.Stderr, "", log.LstdFlags),
	}
}

func (l *Logger) Info(msg string, keyvals ...string) {
	if l == nil || l.level < LogLevelInfo {
		return
	}
	l.logger.Println(formatLog("INFO", msg, keyvals...))
}

func (l *Logger) Warn(msg string, keyvals ...string) {
	if l == nil || l.level < LogLevelWarn {
		return
	}
	l.logger.Println(formatLog("WARN", msg, keyvals...))
}

func (l *Logger) Error(msg string, keyvals ...string) {
	if l == nil || l.level < LogLevelError {
		return
	}
	l.logger.Println(formatLog("ERROR", msg, keyvals...))
}

func formatLog(level, msg string, keyvals ...string) string {
	parts := []string{fmt.Sprintf("[%s] %s", level, msg)}
	for i := 0; i+1 < len(keyvals); i += 2 {
		parts = append(parts, fmt.Sprintf("%s=%s", keyvals[i], keyvals[i+1]))
	}
	return strings.Join(parts, " ")
}
