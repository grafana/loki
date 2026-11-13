package tsdb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRenameWithRetry(t *testing.T) {
	errDenied := errors.New("access is denied")
	errOther := errors.New("no such file")
	retriable := func(err error) bool { return errors.Is(err, errDenied) }

	for _, tc := range []struct {
		name      string
		failures  []error // returned by successive rename calls, then success
		attempts  int
		wantCalls int
		wantErr   error
	}{
		{name: "success first try", attempts: 3, wantCalls: 1},
		{name: "transient then success", failures: []error{errDenied, errDenied}, attempts: 3, wantCalls: 3},
		{name: "transient exhausts attempts", failures: []error{errDenied, errDenied, errDenied, errDenied}, attempts: 3, wantCalls: 3, wantErr: errDenied},
		{name: "non transient not retried", failures: []error{errOther}, attempts: 3, wantCalls: 1, wantErr: errOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			rename := func(_, _ string) error {
				defer func() { calls++ }()
				if calls < len(tc.failures) {
					return tc.failures[calls]
				}
				return nil
			}
			err := renameWithRetry(context.Background(), rename, retriable, tc.attempts, time.Millisecond, "a.staging", "b.tsdb")
			require.Equal(t, tc.wantCalls, calls)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
			require.ErrorContains(t, err, "a.staging")
		})
	}
}

func TestRenameWithRetryContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := renameWithRetry(ctx, func(_, _ string) error { calls++; return errors.New("denied") },
		func(error) bool { return true }, 5, time.Hour, "a", "b")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
}
