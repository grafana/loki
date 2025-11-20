package log

import "sync"

// Logger is a minimal logging interface tailored for clients that want
// structured logging without depending on go-kit or Grafana's internal stack.
type Logger interface {
	Debug(msg string, keyvals ...any)
	Info(msg string, keyvals ...any)
	Error(msg string, keyvals ...any)
}

// Func is a helper signature compatible with FuncLogger.
type Func func(level, msg string, keyvals ...any)

// FuncLogger dispatches log events to the provided function.
type FuncLogger struct {
	emit Func
}

// NewFuncLogger returns a Logger that invokes fn for every log event.
func NewFuncLogger(fn Func) Logger {
	return FuncLogger{emit: fn}
}

func (l FuncLogger) log(level, msg string, keyvals ...any) {
	if l.emit == nil {
		return
	}
	l.emit(level, msg, keyvals...)
}

func (l FuncLogger) Debug(msg string, keyvals ...any) { l.log("debug", msg, keyvals...) }
func (l FuncLogger) Info(msg string, keyvals ...any)  { l.log("info", msg, keyvals...) }
func (l FuncLogger) Error(msg string, keyvals ...any) { l.log("error", msg, keyvals...) }

// NoopLogger discards all log events.
type NoopLogger struct{}

func (NoopLogger) Debug(string, ...any) {}
func (NoopLogger) Info(string, ...any)  {}
func (NoopLogger) Error(string, ...any) {}

var (
	defaultLogger Logger = NoopLogger{}
	mu            sync.RWMutex
)

// SetDefault swaps the package-level logger that helper functions can reuse.
func SetDefault(l Logger) {
	mu.Lock()
	defer mu.Unlock()
	if l == nil {
		defaultLogger = NoopLogger{}
		return
	}
	defaultLogger = l
}

// Default returns the current package-level logger.
func Default() Logger {
	mu.RLock()
	defer mu.RUnlock()
	return defaultLogger
}
