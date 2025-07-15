package kgo

import (
	"bytes"
	"fmt"
	"io"
	"strings"
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
		return "ERROR"
	case LogLevelWarn:
		return "WARN"
	case LogLevelInfo:
		return "INFO"
	case LogLevelDebug:
		return "DEBUG"
	default:
		return "NONE"
	}
}

// Logger is used to log informational messages.
type Logger interface {
	// Level returns the log level to log at.
	//
	// Implementations can change their log level on the fly, but this
	// function must be safe to call concurrently.
	Level() LogLevel

	// Log logs a message with key, value pair arguments for the given log
	// level. Keys are always strings, while values can be any type.
	//
	// This must be safe to call concurrently.
	Log(level LogLevel, msg string, keyvals ...any)
}

// BasicLogger returns a logger that will print to dst in the following format:
//
//	prefix [LEVEL] message; key: val, key: val
//
// prefixFn is optional; if non-nil, it is called for a per-message prefix.
//
// Writes to dst are not checked for errors.
func BasicLogger(dst io.Writer, level LogLevel, prefixFn func() string) Logger {
	return &basicLogger{dst, level, prefixFn}
}

type basicLogger struct {
	dst   io.Writer
	level LogLevel
	pfxFn func() string
}

func (b *basicLogger) Level() LogLevel { return b.level }
func (b *basicLogger) Log(level LogLevel, msg string, keyvals ...any) {
	buf := byteBuffers.Get().(*bytes.Buffer)
	defer byteBuffers.Put(buf)

	buf.Reset()
	if b.pfxFn != nil {
		buf.WriteString(b.pfxFn())
	}
	buf.WriteByte('[')
	buf.WriteString(level.String())
	buf.WriteString("] ")
	buf.WriteString(msg)

	if len(keyvals) > 0 {
		buf.WriteString("; ")
		format := strings.Repeat("%v: %v, ", len(keyvals)/2)
		format = format[:len(format)-2] // trim trailing comma and space
		fmt.Fprintf(buf, format, keyvals...)
	}

	buf.WriteByte('\n')
	b.dst.Write(buf.Bytes())
}

// nopLogger, the default logger, drops everything.
type nopLogger struct{}

func (*nopLogger) Level() LogLevel { return LogLevelNone }
func (*nopLogger) Log(LogLevel, string, ...any) {
}

// wrappedLogger wraps the config logger for convenience at logging callsites.
type wrappedLogger struct {
	inner Logger
}

func (w *wrappedLogger) Level() LogLevel {
	if w.inner == nil {
		return LogLevelNone
	}
	return w.inner.Level()
}

func (w *wrappedLogger) Log(level LogLevel, msg string, keyvals ...any) {
	if w.Level() < level {
		return
	}
	w.inner.Log(level, msg, keyvals...)
}
