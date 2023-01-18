package logging

import (
	"fmt"
	"os"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// NewGoKitFormat creates a new Interface backed by a GoKit logger
// format can be "json" or defaults to logfmt
func NewGoKitFormat(l Level, f Format) Interface {
	var logger log.Logger
	if f.s == "json" {
		logger = log.NewJSONLogger(log.NewSyncWriter(os.Stderr))
	} else {
		logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	}
	return addStandardFields(logger, l)
}

// stand-alone for test purposes
func addStandardFields(logger log.Logger, l Level) Interface {
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.Caller(5))
	logger = level.NewFilter(logger, l.Gokit)
	return gokit{logger}
}

// NewGoKit creates a new Interface backed by a GoKit logger
func NewGoKit(l Level) Interface {
	return NewGoKitFormat(l, Format{s: "logfmt"})
}

// GoKit wraps an existing gokit Logger.
func GoKit(logger log.Logger) Interface {
	return gokit{logger}
}

type gokit struct {
	log.Logger
}

// Helper to defer sprintf until it is needed.
type sprintf struct {
	format string
	args   []interface{}
}

func (s *sprintf) String() string {
	return fmt.Sprintf(s.format, s.args...)
}

// Helper to defer sprint until it is needed.
// Note we don't use Sprintln because the output is passed to go-kit as one value among many on a line
type sprint struct {
	args []interface{}
}

func (s *sprint) String() string {
	return fmt.Sprint(s.args...)
}

func (g gokit) Debugf(format string, args ...interface{}) {
	level.Debug(g.Logger).Log("msg", &sprintf{format: format, args: args})
}
func (g gokit) Debugln(args ...interface{}) {
	level.Debug(g.Logger).Log("msg", &sprint{args: args})
}

func (g gokit) Infof(format string, args ...interface{}) {
	level.Info(g.Logger).Log("msg", &sprintf{format: format, args: args})
}
func (g gokit) Infoln(args ...interface{}) {
	level.Info(g.Logger).Log("msg", &sprint{args: args})
}

func (g gokit) Warnf(format string, args ...interface{}) {
	level.Warn(g.Logger).Log("msg", &sprintf{format: format, args: args})
}
func (g gokit) Warnln(args ...interface{}) {
	level.Warn(g.Logger).Log("msg", &sprint{args: args})
}

func (g gokit) Errorf(format string, args ...interface{}) {
	level.Error(g.Logger).Log("msg", &sprintf{format: format, args: args})
}
func (g gokit) Errorln(args ...interface{}) {
	level.Error(g.Logger).Log("msg", &sprint{args: args})
}

func (g gokit) WithField(key string, value interface{}) Interface {
	return gokit{log.With(g.Logger, key, value)}
}

func (g gokit) WithFields(fields Fields) Interface {
	logger := g.Logger
	for k, v := range fields {
		logger = log.With(logger, k, v)
	}
	return gokit{logger}
}
