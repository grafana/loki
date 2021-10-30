package util

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/log/level"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LogError logs any error returned by f; useful when deferring Close etc.
func LogError(message string, f func() error) {
	if err := f(); err != nil {
		level.Error(log.Logger).Log("message", message, "error", err)
	}
}

// LogError logs any error returned by f; useful when deferring Close etc.
func LogErrorWithContext(ctx context.Context, message string, f func() error) {
	if err := f(); err != nil {
		level.Error(log.WithContext(ctx, log.Logger)).Log("message", message, "error", err)
	}
}

// The MultiError type implements the error interface, and contains the
// Errors used to construct it.
type MultiError []error

// Returns a concatenated string of the contained errors
func (es MultiError) Error() string {
	var buf bytes.Buffer

	if len(es) > 1 {
		_, _ = fmt.Fprintf(&buf, "%d errors: ", len(es))
	}

	for i, err := range es {
		if i != 0 {
			buf.WriteString("; ")
		}
		buf.WriteString(err.Error())
	}

	return buf.String()
}

// Add adds the error to the error list if it is not nil.
func (es *MultiError) Add(err error) {
	if err == nil {
		return
	}
	if merr, ok := err.(MultiError); ok {
		*es = append(*es, merr...)
	} else {
		*es = append(*es, err)
	}
}

// Err returns the error list as an error or nil if it is empty.
func (es MultiError) Err() error {
	if len(es) == 0 {
		return nil
	}
	return es
}

// Is tells if all errors are the same as the target error.
func (es MultiError) Is(target error) bool {
	for _, err := range es {
		if !errors.Is(err, target) {
			return false
		}
	}
	return true
}

// IsCancel tells if all errors are either context.Canceled or grpc codes.Canceled.
func (es MultiError) IsCancel() bool {
	if len(es) == 0 {
		return false
	}
	for _, err := range es {
		if errors.Is(err, context.Canceled) {
			continue
		}
		if IsConnCanceled(err) {
			continue
		}
		return false
	}
	return true
}

// IsDeadlineExceeded tells if all errors are either context.DeadlineExceeded or grpc codes.DeadlineExceeded.
func (es MultiError) IsDeadlineExceeded() bool {
	if len(es) == 0 {
		return false
	}
	for _, err := range es {
		if errors.Is(err, context.DeadlineExceeded) {
			continue
		}
		s, ok := status.FromError(err)
		if ok && s.Code() == codes.DeadlineExceeded {
			continue
		}
		return false
	}
	return true
}

// IsConnCanceled returns true, if error is from a closed gRPC connection.
// copied from https://github.com/etcd-io/etcd/blob/7f47de84146bdc9225d2080ec8678ca8189a2d2b/clientv3/client.go#L646
func IsConnCanceled(err error) bool {
	if err == nil {
		return false
	}

	// >= gRPC v1.23.x
	s, ok := status.FromError(err)
	if ok {
		// connection is canceled or server has already closed the connection
		return s.Code() == codes.Canceled || s.Message() == "transport is closing"
	}

	return false
}
