package util

// Wrapper for the error from a canceled context, to provide better information
// in logs than "context canceled".

import (
	"context"
	"fmt"
)

type causeError struct {
	err   error
	cause error
}

func CauseError(ctx context.Context) error {
	cause := context.Cause(ctx)
	if cause == ctx.Err() {
		return ctx.Err() // Context has no distinct cause; just return the error.
	}
	return &causeError{err: ctx.Err(), cause: cause}
}

func (err *causeError) Error() string {
	return fmt.Sprintf("%s: %s", err.err.Error(), err.cause.Error())
}

// Unwrap returns both, so that usages like `errors.Is(err, context.Canceled)` work.
func (err *causeError) Unwrap() []error {
	return []error{err.err, err.cause}
}
