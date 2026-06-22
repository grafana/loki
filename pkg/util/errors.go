package util //nolint:revive

// Forwarder to avoid editing dozens of files that use these names.

import (
	"context"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/util/errors"
	"github.com/grafana/loki/v3/pkg/util/log"
)

// LogError logs any error returned by f; useful when deferring Close etc.
func LogError(message string, f func() error) {
	errors.LogError(log.Logger, message, f)
}

// LogError logs any error returned by f; useful when deferring Close etc.
func LogErrorWithContext(ctx context.Context, message string, f func() error) {
	if err := f(); err != nil {
		level.Error(log.WithContext(ctx, log.Logger)).Log("message", message, "error", err)
	}
}

type MultiError = errors.MultiError
type GroupedErrors = errors.GroupedErrors

// IsConnCanceled returns true, if error is from a closed gRPC connection.
func IsConnCanceled(err error) bool {
	return errors.IsConnCanceled(err)
}
