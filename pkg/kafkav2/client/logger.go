package client

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kgo"
)

// logger wraps a [log.Logger] and implements the [kgo.Logger] interface.
type logger struct {
	logger log.Logger
}

// newLogger wraps l to implement the [kgo.Logger] interface.
func newLogger(l log.Logger) *logger {
	return &logger{log.With(l, "component", "kafka_client_v2")}
}

// Level implements the [kgo.Logger] interface.
func (l *logger) Level() kgo.LogLevel {
	// [kgo.Client] calls Level() to check what level to log at. We force
	// it to return [kgo.LogLevelInfo] as debug level is expensive.
	return kgo.LogLevelInfo
}

// Log implements the [kgo.Logger] interface.
func (l *logger) Log(lev kgo.LogLevel, msg string, keyvals ...any) {
	keyvals = append([]any{"msg", msg}, keyvals...)
	switch lev {
	case kgo.LogLevelDebug:
		level.Debug(l.logger).Log(keyvals...)
	case kgo.LogLevelInfo:
		level.Info(l.logger).Log(keyvals...)
	case kgo.LogLevelWarn:
		level.Warn(l.logger).Log(keyvals...)
	case kgo.LogLevelError:
		level.Error(l.logger).Log(keyvals...)
	}
}
