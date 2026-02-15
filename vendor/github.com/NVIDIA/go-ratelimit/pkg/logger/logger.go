/*
 * Copyright (c) 2025, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package logger

import (
	"log"
	"os"

	"github.com/NVIDIA/go-ratelimit/pkg/config"

	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log *otelzap.Logger

func InitLogger(config config.Config) {
	var console_log_level zapcore.Level
	var file_log_level zapcore.Level

	switch config.Logging.ConsoleLevel {
	case "debug":
		console_log_level = zapcore.DebugLevel
	case "info":
		console_log_level = zapcore.InfoLevel
	case "warn":
		console_log_level = zapcore.WarnLevel
	case "error":
		console_log_level = zapcore.ErrorLevel
	default:
		console_log_level = zapcore.InfoLevel
	}

	switch config.Logging.ConsoleLevel {
	case "debug":
		file_log_level = zapcore.DebugLevel
	case "info":
		file_log_level = zapcore.InfoLevel
	case "warn":
		file_log_level = zapcore.WarnLevel
	case "error":
		file_log_level = zapcore.ErrorLevel
	default:
		console_log_level = zapcore.InfoLevel
	}
	// Open a log file
	logFile, err := os.OpenFile(config.Logging.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log %s file: %v", config.Logging.File, err.Error())
	}

	// Console encoder (human-readable)
	consoleEncoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())

	// File encoder (JSON format)
	fileEncoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())

	// Create cores for both file and console
	consoleCore := zapcore.NewCore(consoleEncoder, zapcore.Lock(os.Stdout), console_log_level)
	fileCore := zapcore.NewCore(fileEncoder, zapcore.Lock(logFile), file_log_level)

	// Combine cores
	core := zapcore.NewTee(consoleCore, fileCore)

	// Create the Zap logger
	zapLogger := zap.New(core, zap.AddCaller())

	// Create an otelzap logger
	Log = otelzap.New(zapLogger)

	// Sync logs on exit
	defer zapLogger.Sync()

	// Replace global logger
	undo := otelzap.ReplaceGlobals(Log)
	defer undo()
}

// CustomLogger is a logger that writes to a channel
type CustomLogger struct {
	logChan chan string
}

// NewCustomLogger creates a new CustomLogger that writes to the given channel
func NewCustomLogger(logChan chan string) *CustomLogger {
	return &CustomLogger{
		logChan: logChan,
	}
}

// Log writes a log message to the channel
func (l *CustomLogger) Log(msg string) {
	// Log to console
	Log.Info(msg)
	// Try to send to channel
	select {
	case l.logChan <- msg:
	default:
		// Channel is full, drop the message
	}
}
