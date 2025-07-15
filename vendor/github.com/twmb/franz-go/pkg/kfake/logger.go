package kfake

import (
	"fmt"
	"io"
)

// LogLevel designates which level the logger should log at.
type LogLevel int8

const (
	// LogLevelNone disables logging.
	LogLevelNone LogLevel = iota
	// LogLevelError logs all errors. Generally, these should not happen.
	LogLevelError
	// LogLevelWarn logs all warnings, such as request failures.
	LogLevelWarn
	// LogLevelInfo logs informational messages, such as requests. This is
	// usually the default log level.
	LogLevelInfo
	// LogLevelDebug logs verbose information, and is usually not used in
	// production.
	LogLevelDebug
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelError:
		return "ERR"
	case LogLevelWarn:
		return "WRN"
	case LogLevelInfo:
		return "INF"
	case LogLevelDebug:
		return "DBG"
	default:
		return "NON"
	}
}

// Logger can be provided to hook into the fake cluster's logs.
type Logger interface {
	Logf(LogLevel, string, ...any)
}

type nopLogger struct{}

func (*nopLogger) Logf(LogLevel, string, ...any) {}

// BasicLogger returns a logger that writes newline delimited messages to dst.
func BasicLogger(dst io.Writer, level LogLevel) Logger {
	return &basicLogger{dst, level}
}

type basicLogger struct {
	dst   io.Writer
	level LogLevel
}

func (b *basicLogger) Logf(level LogLevel, msg string, args ...any) {
	if b.level < level {
		return
	}
	fmt.Fprintf(b.dst, "[%s] "+msg+"\n", append([]any{level}, args...)...)
}
