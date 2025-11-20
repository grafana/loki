package util

// Logger is a minimal logging interface that external consumers can implement.
// This avoids heavy dependencies on go-kit/log or other logging libraries.
type Logger interface {
	Debug(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
}

// NoopLogger is a logger that discards all log messages.
type NoopLogger struct{}

func (NoopLogger) Debug(string, ...interface{}) {}
func (NoopLogger) Info(string, ...interface{})  {}
func (NoopLogger) Warn(string, ...interface{}) {}
func (NoopLogger) Error(string, ...interface{}) {}

// DefaultLogger is the default logger instance (noop by default).
// Consumers can replace this with their own logger implementation.
var DefaultLogger Logger = NoopLogger{}
