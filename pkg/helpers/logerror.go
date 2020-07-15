package helpers

import (
	"context"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
)

// LogError logs any error returned by f; useful when deferring Close etc.
func LogError(message string, f func() error) {
	if err := f(); err != nil {
		level.Error(util.Logger).Log("message", message, "error", err)
	}
}

// LogError logs any error returned by f; useful when deferring Close etc.
func LogErrorWithContext(ctx context.Context, message string, f func() error) {
	if err := f(); err != nil {
		level.Error(util.WithContext(ctx, util.Logger)).Log("message", message, "error", err)
	}
}
