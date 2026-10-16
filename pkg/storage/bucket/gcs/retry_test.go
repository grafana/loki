package gcs

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/api/googleapi"
)

func TestIsRetryableErr(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "request timeout",
			err:       &googleapi.Error{Code: http.StatusRequestTimeout},
			retryable: true,
		},
		{
			name:      "too many requests",
			err:       &googleapi.Error{Code: http.StatusTooManyRequests},
			retryable: true,
		},
		{
			name:      "server error",
			err:       &googleapi.Error{Code: http.StatusServiceUnavailable},
			retryable: true,
		},
		{
			name:      "wrapped error",
			err:       fmt.Errorf("get object: %w", &googleapi.Error{Code: http.StatusServiceUnavailable}),
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
			name:      "not found",
			err:       &googleapi.Error{Code: http.StatusNotFound},
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
