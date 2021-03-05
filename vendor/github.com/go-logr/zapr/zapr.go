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

// Copyright 2018 Solly Ross
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// package zapr defines an implementation of the github.com/go-logr/logr
// interfaces built on top of Zap (go.uber.org/zap).
//
// Usage
//
// A new logr.Logger can be constructed from an existing zap.Logger using
// the NewLogger function:
//
//  log := zapr.NewLogger(someZapLogger)
//
// Implementation Details
//
// For the most part, concepts in Zap correspond directly with those in
// logr.
//
// Unlike Zap, all fields *must* be in the form of sugared fields --
// it's illegal to pass a strongly-typed Zap field in a key position
// to any of the log methods.
//
// Levels in logr correspond to custom debug levels in Zap.  Any given level
// in logr is represents by its inverse in zap (`zapLevel = -1*logrLevel`).
// For example V(2) is equivalent to log level -2 in Zap, while V(1) is
// equivalent to Zap's DebugLevel.
package zapr

import (
	"github.com/go-logr/logr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NB: right now, we always use the equivalent of sugared logging.
// This is necessary, since logr doesn't define non-suggared types,
// and using zap-specific non-suggared types would make uses tied
// directly to Zap.

// zapLogger is a logr.Logger that uses Zap to log.  The level has already been
// converted to a Zap level, which is to say that `logrLevel = -1*zapLevel`.
type zapLogger struct {
	// NB: this looks very similar to zap.SugaredLogger, but
	// deals with our desire to have multiple verbosity levels.
	l   *zap.Logger
	lvl zapcore.Level
}

// handleFields converts a bunch of arbitrary key-value pairs into Zap fields.  It takes
// additional pre-converted Zap fields, for use with automatically attached fields, like
// `error`.
func handleFields(l *zap.Logger, args []interface{}, additional ...zap.Field) []zap.Field {
	// a slightly modified version of zap.SugaredLogger.sweetenFields
	if len(args) == 0 {
		// fast-return if we have no suggared fields.
		return additional
	}

	// unlike Zap, we can be pretty sure users aren't passing structured
	// fields (since logr has no concept of that), so guess that we need a
	// little less space.
	fields := make([]zap.Field, 0, len(args)/2+len(additional))
	for i := 0; i < len(args); {
		// check just in case for strongly-typed Zap fields, which is illegal (since
		// it breaks implementation agnosticism), so we can give a better error message.
		if _, ok := args[i].(zap.Field); ok {
			l.DPanic("strongly-typed Zap Field passed to logr", zap.Any("zap field", args[i]))
			break
		}

		// make sure this isn't a mismatched key
		if i == len(args)-1 {
			l.DPanic("odd number of arguments passed as key-value pairs for logging", zap.Any("ignored key", args[i]))
			break
		}

		// process a key-value pair,
		// ensuring that the key is a string
		key, val := args[i], args[i+1]
		keyStr, isString := key.(string)
		if !isString {
			// if the key isn't a string, DPanic and stop logging
			l.DPanic("non-string key argument passed to logging, ignoring all later arguments", zap.Any("invalid key", key))
			break
		}

		fields = append(fields, zap.Any(keyStr, val))
		i += 2
	}

	return append(fields, additional...)
}

func (zl *zapLogger) Enabled() bool {
	return zl.l.Core().Enabled(zl.lvl)
}

func (zl *zapLogger) Info(msg string, keysAndVals ...interface{}) {
	if checkedEntry := zl.l.Check(zl.lvl, msg); checkedEntry != nil {
		checkedEntry.Write(handleFields(zl.l, keysAndVals)...)
	}
}

func (zl *zapLogger) Error(err error, msg string, keysAndVals ...interface{}) {
	if checkedEntry := zl.l.Check(zap.ErrorLevel, msg); checkedEntry != nil {
		checkedEntry.Write(handleFields(zl.l, keysAndVals, zap.Error(err))...)
	}
}

func (zl *zapLogger) V(level int) logr.Logger {
	return &zapLogger{
		lvl: zl.lvl - zapcore.Level(level),
		l:   zl.l,
	}
}

func (zl *zapLogger) WithValues(keysAndValues ...interface{}) logr.Logger {
	newLogger := zl.l.With(handleFields(zl.l, keysAndValues)...)
	return newLoggerWithExtraSkip(newLogger, 0)
}

func (zl *zapLogger) WithName(name string) logr.Logger {
	newLogger := zl.l.Named(name)
	return newLoggerWithExtraSkip(newLogger, 0)
}

// newLoggerWithExtraSkip allows creation of loggers with variable levels of callstack skipping
func newLoggerWithExtraSkip(l *zap.Logger, callerSkip int) logr.Logger {
	log := l.WithOptions(zap.AddCallerSkip(callerSkip))
	return &zapLogger{
		l:   log,
		lvl: zap.InfoLevel,
	}
}

// NewLogger creates a new logr.Logger using the given Zap Logger to log.
func NewLogger(l *zap.Logger) logr.Logger {
	// creates a new logger skipping one level of callstack
	return newLoggerWithExtraSkip(l, 1)
}
