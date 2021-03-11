package helpers

import (
	"context"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/kit/log/level"
)

// LogError logs any error returned by f; useful when deferring Close etc.
func LogError(message string, f func() error) {
	if err := f(); err != nil {
		level.Error(util_log.Logger).Log("message", message, "error", err)
	}
}

// LogError logs any error returned by f; useful when deferring Close etc.
func LogErrorWithContext(ctx context.Context, message string, f func() error) {
	if err := f(); err != nil {
		level.Error(util_log.WithContext(ctx, util_log.Logger)).Log("message", message, "error", err)
	}
}
