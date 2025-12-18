package wire

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestError_Is(t *testing.T) {
	tt := []struct {
		name   string
		left   error
		right  error
		expect bool
	}{
		{
			name:   "same code and message",
			left:   Errorf(100, "hello, world"),
			right:  Errorf(100, "hello, world"),
			expect: true,
		},

		{
			// NOTE(rfratto): errors.Is will only unwrap the left-hand side of
			// the operation.
			name:   "same code and message (wrapped)",
			left:   fmt.Errorf("wrapped: %w", Errorf(100, "hello, world")),
			right:  Errorf(100, "hello, world"),
			expect: true,
		},

		{
			name:   "different code, same message",
			left:   Errorf(100, "hello, world"),
			right:  Errorf(200, "hello, world"),
			expect: false,
		},

		{
			name:   "different code, different message",
			left:   Errorf(100, "hello, world"),
			right:  Errorf(200, "goodbye, world"),
			expect: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expect, errors.Is(tc.left, tc.right), "expected errors.Is to return %t", tc.expect)
		})
	}
}
