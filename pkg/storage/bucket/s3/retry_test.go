package s3

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"
)

func TestIsRetryableErr(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "slow down",
			err:       minio.ErrorResponse{Code: errCodeSlowDown, StatusCode: http.StatusServiceUnavailable},
			retryable: true,
		},
		{
			name:      "too many requests",
			err:       minio.ErrorResponse{Code: errCodeTooManyRequests, StatusCode: http.StatusTooManyRequests},
			retryable: true,
		},
		{
			name:      "unknown server error",
			err:       minio.ErrorResponse{StatusCode: http.StatusBadGateway},
			retryable: true,
		},
		{
			name:      "wrapped error",
			err:       fmt.Errorf("get object: %w", minio.ErrorResponse{Code: errCodeSlowDown, StatusCode: http.StatusServiceUnavailable}),
			retryable: true,
		},
		{
			name:      "end of file",
			err:       io.EOF,
			retryable: true,
		},
		{
			name:      "connection reset",
			err:       syscall.ECONNRESET,
			retryable: true,
		},
		{
			name:      "timeout",
			err:       timeoutError{},
			retryable: true,
		},
		{
			name:      "object not found",
			err:       minio.ErrorResponse{Code: minio.NoSuchKey, StatusCode: http.StatusNotFound},
			retryable: false,
		},
		{
			name:      "access denied",
			err:       minio.ErrorResponse{Code: minio.AccessDenied, StatusCode: http.StatusForbidden},
			retryable: false,
		},
		{
			name:      "connection refused",
			err:       syscall.ECONNREFUSED,
			retryable: false,
		},
		{
			name:      "closed connection",
			err:       net.ErrClosed,
			retryable: false,
		},
		{
			name:      "context canceled",
			err:       context.Canceled,
			retryable: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.retryable, IsRetryableErr(test.err))
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
