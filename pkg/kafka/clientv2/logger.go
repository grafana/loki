package clientv2

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kgo"
)

// logger wraps a log.Logger to implement the lgo.Logger interface.
type logger struct {
	logger log.Logger
}

func newLogger(l log.Logger) *logger {
	return &logger{
		logger: log.With(l, "component", "kafka_client"),
	}
}

func (l *logger) Level() kgo.LogLevel {
	// kgo calls Level() to check if debug level logging is enabled.
	// To keep it simple, we return Info, so kgo will never log expensive
	// debug messages.
	return kgo.LogLevelInfo
}

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
