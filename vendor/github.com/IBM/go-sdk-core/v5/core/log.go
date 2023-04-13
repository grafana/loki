package core

// (C) Copyright IBM Corp. 2020, 2021.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"log"
	"os"
	"sync"
)

// LogLevel defines a type for logging levels
type LogLevel int

// Log level constants
const (
	LevelNone LogLevel = iota
	LevelError
	LevelWarn
	LevelInfo
	LevelDebug
)

// Logger is the logging interface implemented and used by the Go core library.
// Users of the library can supply their own implementation by calling SetLogger().
type Logger interface {
	Log(level LogLevel, format string, inserts ...interface{})
	Error(format string, inserts ...interface{})
	Warn(format string, inserts ...interface{})
	Info(format string, inserts ...interface{})
	Debug(format string, inserts ...interface{})

	SetLogLevel(level LogLevel)
	GetLogLevel() LogLevel
	IsLogLevelEnabled(level LogLevel) bool
}

// SDKLoggerImpl is the Go core's implementation of the Logger interface.
// This logger contains two instances of Go's log.Logger interface which are
// used to perform message logging.
// "infoLogger" is used to log info/warn/debug messages.
// If specified as nil, then a default log.Logger instance that uses stdout will be created
// and used for "infoLogger".
// "errorLogger" is used to log error messages.
// If specified as nil, then a default log.Logger instance that uses stderr will be created
// and used for "errorLogger".
type SDKLoggerImpl struct {

	// The current log level configured in this logger.
	// Only messages with a log level that is <= 'logLevel' will be displayed.
	logLevel LogLevel

	// The underlying log.Logger instances used to log info/warn/debug messages.
	infoLogger *log.Logger

	// The underlying log.Logger instances used to log error messages.
	errorLogger *log.Logger

	// These are used to initialize the loggers above.
	infoInit  sync.Once
	errorInit sync.Once
}

// SetLogLevel sets level to be the current logging level
func (l *SDKLoggerImpl) SetLogLevel(level LogLevel) {
	l.logLevel = level
}

// GetLogLevel sets level to be the current logging level
func (l *SDKLoggerImpl) GetLogLevel() LogLevel {
	return l.logLevel
}

// IsLogLevelEnabled returns true iff the logger's current logging level
// indicates that 'level' is enabled.
func (l *SDKLoggerImpl) IsLogLevelEnabled(level LogLevel) bool {
	return l.logLevel >= level
}

// infoLog returns the underlying log.Logger instance used for info/warn/debug logging.
func (l *SDKLoggerImpl) infoLog() *log.Logger {
	l.infoInit.Do(func() {
		if l.infoLogger == nil {
			l.infoLogger = log.New(os.Stdout, "", log.LstdFlags)
		}
	})

	return l.infoLogger
}

// errorLog returns the underlying log.Logger instance used for error logging.
func (l *SDKLoggerImpl) errorLog() *log.Logger {
	l.errorInit.Do(func() {
		if l.errorLogger == nil {
			l.errorLogger = log.New(os.Stderr, "", log.LstdFlags)
		}
	})

	return l.errorLogger
}

// Log will log the specified message on the appropriate log.Logger instance if "level" is currently enabled.
func (l *SDKLoggerImpl) Log(level LogLevel, format string, inserts ...interface{}) {
	if l.IsLogLevelEnabled(level) {
		var goLogger *log.Logger
		switch level {
		case LevelError:
			goLogger = l.errorLog()
		default:
			goLogger = l.infoLog()
		}
		goLogger.Printf(format, inserts...)
	}
}

// Error logs a message at level "Error"
func (l *SDKLoggerImpl) Error(format string, inserts ...interface{}) {
	l.Log(LevelError, "[Error] "+format, inserts...)
}

// Warn logs a message at level "Warn"
func (l *SDKLoggerImpl) Warn(format string, inserts ...interface{}) {
	l.Log(LevelWarn, "[Warn] "+format, inserts...)
}

// Info logs a message at level "Info"
func (l *SDKLoggerImpl) Info(format string, inserts ...interface{}) {
	l.Log(LevelInfo, "[Info] "+format, inserts...)
}

// Debug logs a message at level "Debug"
func (l *SDKLoggerImpl) Debug(format string, inserts ...interface{}) {
	l.Log(LevelDebug, "[Debug] "+format, inserts...)
}

// NewLogger constructs an SDKLoggerImpl instance with the specified logging level
// enabled.
// The "infoLogger" parameter is the log.Logger instance to be used to log
// info/warn/debug messages.  If specified as nil, then a default log.Logger instance
// that writes messages to "stdout" will be used.
// The "errorLogger" parameter is the log.Logger instance to be used to log
// error messages.  If specified as nil, then a default log.Logger instance
// that writes messages to "stderr" will be used.
func NewLogger(level LogLevel, infoLogger *log.Logger, errorLogger *log.Logger) *SDKLoggerImpl {
	return &SDKLoggerImpl{
		logLevel:    level,
		infoLogger:  infoLogger,
		errorLogger: errorLogger,
	}
}

// sdkLogger holds the Logger implementation used by the Go core library.
var sdkLogger Logger = NewLogger(LevelError, nil, nil)

// SetLogger sets the specified Logger instance as the logger to be used by the Go core library.
func SetLogger(logger Logger) {
	sdkLogger = logger
}

// GetLogger returns the Logger instance currently used by the Go core.
func GetLogger() Logger {
	return sdkLogger
}

// SetLoggingLevel will enable the specified logging level in the Go core library.
func SetLoggingLevel(level LogLevel) {
	GetLogger().SetLogLevel(level)
}
