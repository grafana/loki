/*
Copyright 2019 The logr Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// This design derives from Dave Cheney's blog:
//     http://dave.cheney.net/2015/11/05/lets-talk-about-logging

// Package logr defines a general-purpose logging API and abstract interfaces
// to back that API.  Packages in the Go ecosystem can depend on this package,
// while callers can implement logging with whatever backend is appropriate.
//
// Usage
// -----
//
// Logging is done using a Logger instance.  Logger is a concrete type with
// methods, which defers the actual logging to a LogSink interface.  The main
// methods of Logger are Info() and Error().  Arguments to Info() and Error()
// are key/value pairs rather than printf-style formatted strings, emphasizing
// "structured logging".
//
// With Go's standard log package, we might write:
//   log.Printf("setting target value %s", targetValue)
//
// With logr's structured logging, we'd write:
//   logger.Info("setting target", "value", targetValue)
//
// Errors are much the same.  Instead of:
//   log.Printf("failed to open the pod bay door for user %s: %v", user, err)
//
// We'd write:
//   logger.Error(err, "failed to open the pod bay door", "user", user)
//
// Info() and Error() are very similar, but they are separate methods so that
// LogSink implementations can choose to do things like attach additional
// information (such as stack traces) on calls to Error().
//
// Verbosity
// ---------
//
// Often we want to log information only when the application in "verbose
// mode".  To write log-lines that are more verbose, Logger has a V() method.
// The higher the V-level of a log-line, the less critical it is considered.
// Log-lines with V-levels that are not enabled (as per the LogSink) will not
// be written.  Level V(0) is the default, and logger.V(0).Info() has the same
// meaning as logger.Info().  Negative V-levels have the same meaning as V(0).
//
// Where we might have written:
//   if flVerbose >= 2 {
//       log.Printf("an unusual thing happened")
//   }
//
// We can write:
//   logger.V(2).Info("an unusual thing happened")
//
// Logger Names
// ------------
//
// Logger instances can have name strings so that all messages logged through
// that instance have additional context.  For example, you might want to add
// a subsystem name:
//
//   logger.WithName("compactor").Info("started", "time", time.Now())
//
// The WithName() method returns a new Logger, which can be passed to
// constructors or other functions for further use.  Repeated use of WithName()
// will accumulate name "segments".  These name segments will be joined in some
// way by the LogSink implementation.  It is strongly recommended that name
// segments contain simple identifiers (letters, digits, and hyphen), and do
// not contain characters that could muddle the log output or confuse the
// joining operation (e.g. whitespace, commas, periods, slashes, brackets,
// quotes, etc).
//
// Saved Values
// ------------
//
// Logger instances can store any number of key/value pairs, which will be
// logged alongside all messages logged through that instance.  For example,
// you might want to create a Logger instance per managed object:
//
// With the standard log package, we might write:
//   log.Printf("decided to set field foo to value %q for object %s/%s",
//       targetValue, object.Namespace, object.Name)
//
// With logr's we'd write:
//   // Elsewhere: set up the logger to log the object name.
//   obj.logger = mainLogger.WithValues(
//       "name", obj.name, "namespace", obj.namespace)
//
//   // later on...
//   obj.logger.Info("setting foo", "value", targetValue)
//
// Best Practices
// --------------
//
// Logger has very few hard rules, with the goal that LogSink implementations
// might have a lot of freedom to differentiate.  There are, however, some
// things to consider.
//
// The log message consists of a constant message attached to the log line.
// This should generally be a simple description of what's occurring, and should
// never be a format string.  Variable information can then be attached using
// named values.
//
// Keys are arbitrary strings, but should generally be constant values.  Values
// may be any Go value, but how the value is formatted is determined by the
// LogSink implementation.
//
// Key Naming Conventions
// ----------------------
//
// Keys are not strictly required to conform to any specification or regex, but
// it is recommended that they:
//   * be human-readable and meaningful (not auto-generated or simple ordinals)
//   * be constant (not dependent on input data)
//   * contain only printable characters
//   * not contain whitespace or punctuation
//
// These guidelines help ensure that log data is processed properly regardless
// of the log implementation.  For example, log implementations will try to
// output JSON data or will store data for later database (e.g. SQL) queries.
//
// While users are generally free to use key names of their choice, it's
// generally best to avoid using the following keys, as they're frequently used
// by implementations:
//
//   * `"caller"`: the calling information (file/line) of a particular log line.
//   * `"error"`: the underlying error value in the `Error` method.
//   * `"level"`: the log level.
//   * `"logger"`: the name of the associated logger.
//   * `"msg"`: the log message.
//   * `"stacktrace"`: the stack trace associated with a particular log line or
//                     error (often from the `Error` message).
//   * `"ts"`: the timestamp for a log line.
//
// Implementations are encouraged to make use of these keys to represent the
// above concepts, when necessary (for example, in a pure-JSON output form, it
// would be necessary to represent at least message and timestamp as ordinary
// named values).
//
// Break Glass
// -----------
//
// Implementations may choose to give callers access to the underlying
// logging implementation.  The recommended pattern for this is:
//   // Underlier exposes access to the underlying logging implementation.
//   // Since callers only have a logr.Logger, they have to know which
//   // implementation is in use, so this interface is less of an abstraction
//   // and more of way to test type conversion.
//   type Underlier interface {
//       GetUnderlying() <underlying-type>
//   }
package logr

import (
	"context"
)

// New returns a new Logger instance.  This is primarily used by libraries
// implementing LogSink, rather than end users.
func New(sink LogSink) Logger {
	logger := Logger{
		sink: sink,
	}
	if withCallDepth, ok := sink.(CallDepthLogSink); ok {
		logger.withCallDepth = withCallDepth
	}
	sink.Init(runtimeInfo)
	return logger
}

// Logger is an interface to an abstract logging implementation.  This is a
// concrete type for performance reasons, but all the real work is passed on
// to a LogSink.  Implementations of LogSink should provide their own
// constructors that return Logger, not LogSink.
type Logger struct {
	level         int
	sink          LogSink
	withCallDepth CallDepthLogSink
}

// Enabled tests whether this Logger is enabled.  For example, commandline
// flags might be used to set the logging verbosity and disable some info
// logs.
func (l Logger) Enabled() bool {
	return l.sink.Enabled(l.level)
}

// Info logs a non-error message with the given key/value pairs as context.
//
// The msg argument should be used to add some constant description to
// the log line.  The key/value pairs can then be used to add additional
// variable information.  The key/value pairs must alternate string
// keys and arbitrary values.
func (l Logger) Info(msg string, keysAndValues ...interface{}) {
	if l.Enabled() {
		l.sink.Info(l.level, msg, keysAndValues...)
	}
}

// Error logs an error, with the given message and key/value pairs as context.
// It functions similarly to Info, but may have unique behavior, and should be
// preferred for logging errors (see the package documentations for more
// information).
//
// The msg argument should be used to add context to any underlying error,
// while the err argument should be used to attach the actual error that
// triggered this log line, if present.
func (l Logger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.sink.Error(err, msg, keysAndValues...)
}

// V returns a new Logger instance for a specific verbosity level, relative to
// this Logger.  In other words, V-levels are additive.  A higher verbosity
// level means a log message is less important.  Negative V-levels are treated
// as 0.
func (l Logger) V(level int) Logger {
	if level < 0 {
		level = 0
	}
	l.level += level
	return l
}

// WithValues returns a new Logger instance with additional key/value pairs.
// See Info for documentation on how key/value pairs work.
func (l Logger) WithValues(keysAndValues ...interface{}) Logger {
	l.sink = l.sink.WithValues(keysAndValues...)
	return l
}

// WithName returns a new Logger instance with the specified name element added
// to the Logger's name.  Successive calls with WithName append additional
// suffixes to the Logger's name.  It's strongly recommended that name segments
// contain only letters, digits, and hyphens (see the package documentation for
// more information).
func (l Logger) WithName(name string) Logger {
	l.sink = l.sink.WithName(name)
	return l
}

// WithCallDepth returns a Logger instance that offsets the call stack by the
// specified number of frames when logging call site information, if possible.
// This is useful for users who have helper functions between the "real" call
// site and the actual calls to Logger methods.  If depth is 0 the attribution
// should be to the direct caller of this function.  If depth is 1 the
// attribution should skip 1 call frame, and so on.  Successive calls to this
// are additive.
//
// If the underlying log implementation supports a WithCallDepth(int) method,
// it will be called and the result returned.  If the implementation does not
// support CallDepthLogSink, the original Logger will be returned.
func (l Logger) WithCallDepth(depth int) Logger {
	if l.withCallDepth == nil {
		return l
	}
	l.sink = l.withCallDepth.WithCallDepth(depth)
	return l
}

// contextKey is how we find Loggers in a context.Context.
type contextKey struct{}

// FromContext returns a Logger from ctx or an error if no Logger is found.
func FromContext(ctx context.Context) (Logger, error) {
	if v, ok := ctx.Value(contextKey{}).(Logger); ok {
		return v, nil
	}

	return Logger{}, notFoundError{}
}

// notFoundError exists to carry an IsNotFound method.
type notFoundError struct{}

func (notFoundError) Error() string {
	return "no logr.Logger was present"
}

func (notFoundError) IsNotFound() bool {
	return true
}

// FromContextOrDiscard returns a Logger from ctx.  If no Logger is found, this
// returns a Logger that discards all log messages.
func FromContextOrDiscard(ctx context.Context) Logger {
	if v, ok := ctx.Value(contextKey{}).(Logger); ok {
		return v
	}

	return Discard()
}

// NewContext returns a new Context, derived from ctx, which carries the
// provided Logger.
func NewContext(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

// RuntimeInfo holds information that the logr "core" library knows which
// LogSinks might want to know.
type RuntimeInfo struct {
	// CallDepth is the number of call frames the logr library adds between the
	// end-user and the LogSink.  LogSink implementations which choose to print
	// the original logging site (e.g. file & line) should climb this many
	// additional frames to find it.
	CallDepth int
}

// runtimeInfo is a static global.  It must not be changed at run time.
var runtimeInfo = RuntimeInfo{
	CallDepth: 1,
}

// LogSink represents a logging implementation.  End-users will generally not
// interact with this type.
type LogSink interface {
	// Init receives optional information about the logr library for LogSink
	// implementations that need it.
	Init(info RuntimeInfo)

	// Enabled tests whether this LogSink is enabled at the specified V-level.
	// For example, commandline flags might be used to set the logging
	// verbosity and disable some info logs.
	Enabled(level int) bool

	// Info logs a non-error message with the given key/value pairs as context.
	// The level argument is provided for optional logging.  This method will
	// only be called when Enabled(level) is true. See Logger.Info for more
	// details.
	Info(level int, msg string, keysAndValues ...interface{})

	// Error logs an error, with the given message and key/value pairs as
	// context.  See Logger.Error for more details.
	Error(err error, msg string, keysAndValues ...interface{})

	// WithValues returns a new LogSink with additional key/value pairs.  See
	// Logger.WithValues for more details.
	WithValues(keysAndValues ...interface{}) LogSink

	// WithName returns a new LogSink with the specified name appended.  See
	// Logger.WithName for more details.
	WithName(name string) LogSink
}

// CallDepthLogSink represents a Logger that knows how to climb the call stack
// to identify the original call site and can offset the depth by a specified
// number of frames.  This is useful for users who have helper functions
// between the "real" call site and the actual calls to Logger methods.
// Implementations that log information about the call site (such as file,
// function, or line) would otherwise log information about the intermediate
// helper functions.
//
// This is an optional interface and implementations are not required to
// support it.
type CallDepthLogSink interface {
	// WithCallDepth returns a Logger that will offset the call stack by the
	// specified number of frames when logging call site information.  If depth
	// is 0 the attribution should be to the direct caller of this method.  If
	// depth is 1 the attribution should skip 1 call frame, and so on.
	// Successive calls to this are additive.
	WithCallDepth(depth int) LogSink
}
