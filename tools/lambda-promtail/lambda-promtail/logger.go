package main

import (
	"os"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

func NewLogger(logLevel string) *log.Logger {
	logger := log.NewLogfmtLogger(os.Stderr)
	logger = level.NewFilter(logger, level.Allow(level.ParseDefault(logLevel, level.DebugValue())))
	logger = log.With(logger, "caller", log.DefaultCaller)
	return &logger
}
