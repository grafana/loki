package sloggokit

import (
	"log/slog"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// levelLoggerCache holds pre-built leveled loggers so that Handle() can
// retrieve an existing leveled logger rather than creating a new one each
// time.
type levelLoggerCache struct {
	debugLogger log.Logger
	infoLogger  log.Logger
	warnLogger  log.Logger
	errorLogger log.Logger
}

func newLevelCache(logger log.Logger) *levelLoggerCache {
	return &levelLoggerCache{
		debugLogger: level.Debug(logger),
		infoLogger:  level.Info(logger),
		warnLogger:  level.Warn(logger),
		errorLogger: level.Error(logger),
	}
}

func (c *levelLoggerCache) get(lvl slog.Level) log.Logger {
	switch {
	case lvl >= slog.LevelError:
		return c.errorLogger
	case lvl >= slog.LevelWarn:
		return c.warnLogger
	case lvl >= slog.LevelInfo:
		return c.infoLogger
	default:
		return c.debugLogger
	}
}
