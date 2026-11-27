package internal

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"
)

// TODO (ned): Revisit logging
// Add more standardized approach with log levels and configurability

type Logging interface {
	Printf(ctx context.Context, format string, v ...interface{})
}

type DefaultLogger struct {
	log       *log.Logger
	calldepth int
}

func (l *DefaultLogger) Printf(ctx context.Context, format string, v ...interface{}) {
	_ = l.log.Output(l.calldepth, fmt.Sprintf(format, v...))
}

// Calldepth values for NewDefaultLogger. Reads through Logger.Printf add one
// forwarding frame (atomicLogger.Printf), so the plain default logger uses
// DefaultLoggerCalldepth; wrappers that add another Printf frame (e.g.
// logging's filterLogger) use FilterLoggerCalldepth.
const (
	DefaultLoggerCalldepth = 3
	FilterLoggerCalldepth  = 4
)

// NewDefaultLogger returns a stderr logger that attributes each line to the
// frame calldepth levels up the stack.
func NewDefaultLogger(calldepth int) Logging {
	return &DefaultLogger{
		log:       log.New(os.Stderr, "redis: ", log.LstdFlags|log.Lshortfile),
		calldepth: calldepth,
	}
}

// atomicLogger holds the active Logging behind an atomic pointer so
// redis.SetLogger, logging.Enable and logging.Disable can swap it while pool
// and background goroutines read it through Printf. The interface is stored via
// a pointer rather than atomic.Value, whose single-concrete-type rule the
// swappable implementations (DefaultLogger, VoidLogger, custom loggers) break.
type atomicLogger struct {
	v atomic.Pointer[Logging]
}

func (a *atomicLogger) Store(l Logging) { a.v.Store(&l) }

func (a *atomicLogger) Load() Logging {
	if p := a.v.Load(); p != nil {
		return *p
	}
	return nil
}

func (a *atomicLogger) Printf(ctx context.Context, format string, v ...interface{}) {
	if l := a.Load(); l != nil {
		l.Printf(ctx, format, v...)
	}
}

func newAtomicLogger(l Logging) *atomicLogger {
	a := &atomicLogger{}
	a.Store(l)
	return a
}

// Logger calls Output to print to the stderr.
// Arguments are handled in the manner of fmt.Print.
// Swap it with redis.SetLogger; read it through Logger.Printf.
var Logger = newAtomicLogger(NewDefaultLogger(DefaultLoggerCalldepth))

// atomicLogLevel stores the active level as an int32 so redis.SetLogLevel can
// change it while the level guards (isHealthyConn on the Get path, the
// maintnotifications loggers) read it through the *OrAbove helpers.
type atomicLogLevel struct {
	v atomic.Int32
}

func (a *atomicLogLevel) Store(l LogLevelT) { a.v.Store(int32(l)) }
func (a *atomicLogLevel) Load() LogLevelT   { return LogLevelT(a.v.Load()) }

func (a *atomicLogLevel) WarnOrAbove() bool  { return a.Load().WarnOrAbove() }
func (a *atomicLogLevel) InfoOrAbove() bool  { return a.Load().InfoOrAbove() }
func (a *atomicLogLevel) DebugOrAbove() bool { return a.Load().DebugOrAbove() }

func newAtomicLogLevel(l LogLevelT) *atomicLogLevel {
	a := &atomicLogLevel{}
	a.Store(l)
	return a
}

var LogLevel = newAtomicLogLevel(LogLevelError)

// LogLevelT represents the logging level
type LogLevelT int

// Log level constants for the entire go-redis library
const (
	LogLevelError LogLevelT = iota // 0 - errors only
	LogLevelWarn                   // 1 - warnings and errors
	LogLevelInfo                   // 2 - info, warnings, and errors
	LogLevelDebug                  // 3 - debug, info, warnings, and errors
)

// String returns the string representation of the log level
func (l LogLevelT) String() string {
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
		return "UNKNOWN"
	}
}

// IsValid returns true if the log level is valid
func (l LogLevelT) IsValid() bool {
	return l >= LogLevelError && l <= LogLevelDebug
}

func (l LogLevelT) WarnOrAbove() bool {
	return l >= LogLevelWarn
}

func (l LogLevelT) InfoOrAbove() bool {
	return l >= LogLevelInfo
}

func (l LogLevelT) DebugOrAbove() bool {
	return l >= LogLevelDebug
}
