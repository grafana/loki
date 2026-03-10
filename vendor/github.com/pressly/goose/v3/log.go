package goose

import (
	std "log"
)

var log Logger = &stdLogger{}

// Logger is standard logger interface
type Logger interface {
	Fatalf(format string, v ...any)
	Printf(format string, v ...any)
}

// SetLogger sets the logger for package output
func SetLogger(l Logger) {
	log = l
}

// stdLogger is a default logger that outputs to a stdlib's log.std logger.
type stdLogger struct{}

func (*stdLogger) Fatalf(format string, v ...any) { std.Fatalf(format, v...) }
func (*stdLogger) Printf(format string, v ...any) { std.Printf(format, v...) }

// NopLogger returns a logger that discards all logged output.
func NopLogger() Logger {
	return &nopLogger{}
}

type nopLogger struct{}

var _ Logger = (*nopLogger)(nil)

func (*nopLogger) Fatalf(format string, v ...any) {}
func (*nopLogger) Printf(format string, v ...any) {}
