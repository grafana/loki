package queryrangebase

import (
	"context"
	"time"
)

type mockLimits struct {
	maxQueryLookback  time.Duration
	maxQueryLength    time.Duration
	maxCacheFreshness time.Duration
}

func (m mockLimits) MaxQueryLookback(context.Context, string) time.Duration {
	return m.maxQueryLookback
}

func (m mockLimits) MaxQueryLength(context.Context, string) time.Duration {
	return m.maxQueryLength
}

func (mockLimits) MaxQueryParallelism(context.Context, string) int {
	return 14 // Flag default.
}

func (m mockLimits) MaxCacheFreshness(context.Context, string) time.Duration {
	return m.maxCacheFreshness
}
