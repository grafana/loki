// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package global // import "go.opentelemetry.io/otel/log/internal/global"

import (
	"errors"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/internal/global"
	"go.opentelemetry.io/otel/log"
)

var (
	globalLoggerProvider = defaultLoggerProvider()

	delegateLoggerOnce sync.Once
)

func defaultLoggerProvider() *atomic.Value {
	v := &atomic.Value{}
	v.Store(loggerProviderHolder{provider: &loggerProvider{}})
	return v
}

type loggerProviderHolder struct {
	provider log.LoggerProvider
}

// GetLoggerProvider returns the global LoggerProvider.
func GetLoggerProvider() log.LoggerProvider {
	return globalLoggerProvider.Load().(loggerProviderHolder).provider
}

// SetLoggerProvider sets the global LoggerProvider.
func SetLoggerProvider(provider log.LoggerProvider) {
	current := GetLoggerProvider()
	if _, cOk := current.(*loggerProvider); cOk {
		if _, mpOk := provider.(*loggerProvider); mpOk && current == provider {
			err := errors.New("invalid delegation: LoggerProvider self-delegation")
			global.Error(err, "No delegate will be configured")
			return
		}
	}

	delegateLoggerOnce.Do(func() {
		if def, ok := current.(*loggerProvider); ok {
			def.setDelegate(provider)
		}
	})
	globalLoggerProvider.Store(loggerProviderHolder{provider: provider})
}
