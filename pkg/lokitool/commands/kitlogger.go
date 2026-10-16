package commands

import (
	"fmt"

	kitlog "github.com/go-kit/log"
	kitlevel "github.com/go-kit/log/level"
	log "github.com/sirupsen/logrus"
)

// kitLogger forwards log lines of go-kit loggers to logrus.
//
// Code that is shared with Loki logs through go-kit. The level and msg keys
// become the level and message of the logrus entry, all other keys become
// fields. Lines without a level are logged at info level.
type kitLogger struct {
	logger *log.Logger
}

var _ kitlog.Logger = kitLogger{}

func newKitLogger() kitlog.Logger {
	return kitLogger{logger: log.StandardLogger()}
}

// Log implements the go-kit Logger interface.
func (l kitLogger) Log(keyvals ...any) error {
	level := log.InfoLevel
	msg := ""
	fields := make(log.Fields, len(keyvals)/2)

	for i := 0; i < len(keyvals); i += 2 {
		key := fmt.Sprint(keyvals[i])
		var value any = "(MISSING)"
		if i+1 < len(keyvals) {
			value = keyvals[i+1]
		}

		switch key {
		case "level":
			level = toLogrusLevel(value)
		case "msg":
			msg = fmt.Sprint(value)
		default:
			fields[key] = value
		}
	}

	l.logger.WithFields(fields).Log(level, msg)
	return nil
}

func toLogrusLevel(value any) log.Level {
	switch value {
	case kitlevel.DebugValue():
		return log.DebugLevel
	case kitlevel.WarnValue():
		return log.WarnLevel
	case kitlevel.ErrorValue():
		return log.ErrorLevel
	default:
		return log.InfoLevel
	}
}
