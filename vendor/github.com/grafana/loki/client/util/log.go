package util

// This package provides a minimal logging interface that can wrap go-kit/log
// or other logging libraries. The actual implementation in v3 uses go-kit/log,
// but we provide a simple interface here for external consumers.

// Logger is a minimal logging interface that external consumers can implement.
// This avoids requiring go-kit/log as a direct dependency for consumers.
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
func (NoopLogger) Warn(string, ...interface{})   {}
func (NoopLogger) Error(string, ...interface{}) {}

// DefaultLogger is the default logger instance (noop by default).
// Consumers can replace this with their own logger implementation.
var DefaultLogger Logger = NoopLogger{}

// Note: The v3 module uses go-kit/log internally, but this client module
// provides a simple interface so consumers don't need to depend on go-kit/log.
// If you need to use the v3 logging utilities, import them directly:
//   import "github.com/grafana/loki/v3/pkg/util/log"
