package nats

import (
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

type NATSLogger struct {
	log.Logger
}

func NewNATSLogger(l log.Logger) *NATSLogger {
	return &NATSLogger{log.With(l, "component", "nats")}
}

// Log a notice statement
func (l NATSLogger) Noticef(format string, v ...interface{}) {
	level.Info(l.Logger).Log("msg", fmt.Sprintf(format, v...))
}

// Log a warning statement
func (l NATSLogger) Warnf(format string, v ...interface{}) {
	level.Warn(l.Logger).Log("msg", fmt.Sprintf(format, v...))
}

// Log a fatal error
func (l NATSLogger) Fatalf(format string, v ...interface{}) {
	l.Logger.Log("msg", fmt.Sprintf(format, v...), "level", "fatal")
}

// Log an error
func (l NATSLogger) Errorf(format string, v ...interface{}) {
	level.Error(l.Logger).Log("msg", fmt.Sprintf(format, v...))
}

// Log a debug statement
func (l NATSLogger) Debugf(format string, v ...interface{}) {
	level.Debug(l.Logger).Log("msg", fmt.Sprintf(format, v...))
}

// Log a trace statement
func (l NATSLogger) Tracef(format string, v ...interface{}) {
	l.Logger.Log("msg", fmt.Sprintf(format, v...), "level", "trace")
}
