//go:build js

// Package log provides the global logger for Loki.
// On js/wasm builds the logger is a no-op; all server-side logging infrastructure
// (dskit/server, dskit/tracing, Prometheus counters) is omitted to keep the WASM
// binary lean. Use the standard go-kit/log interface for structured logging in
// code that must compile on js/wasm.
package log

import (
	"context"

	kitlog "github.com/go-kit/log"
	dslog "github.com/grafana/dskit/log"
)

// Logger is a shared go-kit logger. On js/wasm it is a no-op logger; InitLogger
// is not available on this platform.
var Logger = kitlog.NewNopLogger()

// logLevel is the current log level, used by slogadapter.go.
// On js/wasm it defaults to "info" and cannot be changed at runtime.
var logLevel = func() dslog.Level {
	var l dslog.Level
	_ = l.Set("info")
	return l
}()

// WithContext returns the logger unchanged on js/wasm (no tracing context to extract).
func WithContext(_ context.Context, l kitlog.Logger) kitlog.Logger {
	return l
}

// WithUserID returns a Logger annotated with the given user ID.
func WithUserID(userID string, l kitlog.Logger) kitlog.Logger {
	return kitlog.With(l, "org_id", userID)
}

// Flush is a no-op on js/wasm.
func Flush() error { return nil }

// CheckFatal is a no-op on js/wasm (no exit mechanism).
func CheckFatal(_ string, err error, _ kitlog.Logger) {
	if err != nil {
		_ = Logger.Log("msg", "fatal error (ignored on js/wasm)", "err", err)
	}
}

