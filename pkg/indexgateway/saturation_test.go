package indexgateway

import (
	"context"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsSaturatedError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "saturated error",
			err:      newSaturatedError("cpu"),
			expected: true,
		},
		{
			name:     "wrapped saturated error",
			err:      errors.Wrap(newSaturatedError("cpu"), "get shards"),
			expected: true,
		},
		{
			name:     "saturated error with stack",
			err:      errors.WithStack(newSaturatedError("memory")),
			expected: true,
		},
		{
			name:     "plain error",
			err:      errors.New("mock error"),
			expected: false,
		},
		{
			name:     "same status code, different message",
			err:      status.Error(saturationStatusCode, "connection refused"),
			expected: false,
		},
		{
			name:     "different status code, same message",
			err:      status.Error(codes.ResourceExhausted, saturatedErrMessage),
			expected: false,
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, IsSaturatedError(tc.err))
		})
	}
}
