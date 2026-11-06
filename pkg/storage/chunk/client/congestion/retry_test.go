package congestion

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func TestLimitedRetrierErrors(t *testing.T) {
	errRetryable1 := errors.New("retryable 1")
	errRetryable2 := errors.New("retryable 2")
	errRetryable3 := errors.New("retryable 3")
	isRetryable := func(err error) bool { return !errors.Is(err, context.Canceled) }

	tests := []struct {
		name    string
		errs    []error // error returned by each attempt
		wantIs  error
		wantMsg string
	}{
		{
			name:    "retries exceeded includes the last error",
			errs:    []error{errRetryable1, errRetryable2, errRetryable3},
			wantIs:  RetriesExceeded,
			wantMsg: "retries exceeded after 3 attempts, caused by retryable 3",
		},
		{
			name:    "non-retryable error after a retry includes the previous error",
			errs:    []error{errRetryable1, context.Canceled},
			wantIs:  context.Canceled,
			wantMsg: "context canceled after 2 attempts, with previous error: retryable 1",
		},
		{
			name:    "non-retryable error on the first attempt is unchanged",
			errs:    []error{context.Canceled},
			wantIs:  context.Canceled,
			wantMsg: "context canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Retry: RetrierConfig{Strategy: "limited", Limit: 2}}
			metrics := NewMetrics(t.Name(), cfg, nil)
			t.Cleanup(metrics.Unregister)
			r := NewLimitedRetrier(cfg, metrics).withLogger(log.NewNopLogger())

			fn := func(attempt int) (io.ReadCloser, int64, error) {
				return nil, 0, tt.errs[attempt]
			}
			_, _, err := r.Do(fn, isRetryable, func() {}, func() {})

			require.ErrorIs(t, err, tt.wantIs)
			require.EqualError(t, err, tt.wantMsg)
			// The earlier errors are only in the message, so they can't change
			// how callers classify the returned error.
			for _, e := range []error{errRetryable1, errRetryable2, errRetryable3} {
				require.NotErrorIs(t, err, e)
			}
		})
	}
}
