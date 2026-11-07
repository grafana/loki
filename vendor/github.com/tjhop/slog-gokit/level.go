package sloggokit

import (
	"log/slog"

	"github.com/go-kit/log/level"
)

// go-kit/log hoists these into package level variables to pre-pay boxing cost
// once, same that we do for slog keys in the handler:
// https://github.com/go-kit/log/blob/v0.2.1/level/level.go#L229-L239
//
// Re-hoist them here for the same reasons so they can be appended right into
// the log's kv pairs.
var (
	levelKey        any = level.Key()
	debugLevelValue any = level.DebugValue()
	infoLevelValue  any = level.InfoValue()
	warnLevelValue  any = level.WarnValue()
	errorLevelValue any = level.ErrorValue()
)

// gokitLevelValue maps an slog.Level to the boxed go-kit level value.
// Unnamed levels bucket to the highest named level at or below them.
func gokitLevelValue(lvl slog.Level) any {
	switch {
	case lvl >= slog.LevelError:
		return errorLevelValue
	case lvl >= slog.LevelWarn:
		return warnLevelValue
	case lvl >= slog.LevelInfo:
		return infoLevelValue
	default:
		return debugLevelValue
	}
}
