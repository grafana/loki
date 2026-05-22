//go:build !js

package util //nolint:revive

import (
	"context"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/util/log"
)

// LogError logs any error returned by f; useful when deferring Close etc.
func LogError(message string, f func() error) {
	if err := f(); err != nil {
		level.Error(log.Logger).Log("message", message, "error", err)
	}
}

// LogErrorWithContext logs any error returned by f; useful when deferring Close etc.
func LogErrorWithContext(ctx context.Context, message string, f func() error) {
	if err := f(); err != nil {
		level.Error(log.WithContext(ctx, log.Logger)).Log("message", message, "error", err)
	}
}
