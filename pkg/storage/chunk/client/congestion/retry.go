package congestion

import (
	"errors"
	"io"
)

var RetriesExceeded = errors.New("retries exceeded")

type NoopRetryStrategy struct{}

func (n NoopRetryStrategy) Do(fn DoRequestFunc, _ IsRetryableErrFunc, _ func(), _ func()) (io.ReadCloser, int64, error) {
	// don't retry, just execute the given function once
	return fn(0)
}

// LimitedRetryStrategy executes the initial request plus a configurable limit of subsequent retries.
// limit=0 is equivalent to NoopRetryStrategy
type LimitedRetryStrategy struct {
	limit int
}

func NewLimitedRetryStrategy(limit int) *LimitedRetryStrategy {
	return &LimitedRetryStrategy{limit: limit}
}

func (l LimitedRetryStrategy) Do(fn DoRequestFunc, isRetryable IsRetryableErrFunc, onSuccess func(), onError func()) (io.ReadCloser, int64, error) {
	for i := 0; i <= l.limit; i++ {
		rc, sz, err := fn(i)

		if err != nil {
			onError()

			if !isRetryable(err) {
				return rc, sz, err
			}
		} else {
			onSuccess()
		}
	}

	return nil, 0, RetriesExceeded
}
