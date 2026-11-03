package util //nolint:revive

// Forwarder to avoid editing dozens of files that use these names.

import (
	"context"
	"io"

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

// UnwrapMultiError returns es as a single error, unwrapped to the bare error when there is
// exactly one. A caller that does errors.As or a type switch on the result can then still
// reach it, which MultiError alone would hide.
func UnwrapMultiError(es MultiError) error {
	if len(es) == 1 {
		return es[0]
	}
	return es.Err()
}

// IsConnCanceled returns true, if error is from a closed gRPC connection.
func IsConnCanceled(err error) bool {
	return errors.IsConnCanceled(err)
}

func CloseAndHandleError(closer io.Closer, returnErr *error) {
	closeErr := closer.Close()
	if *returnErr == nil {
		*returnErr = closeErr
	}
}
