package testutil

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/util/validation"
)

// MockLimits is a mock implementation of limits.Limits interface that can be used in tests
type MockLimits struct {
	MaxQueryLookbackVal           time.Duration
	MaxQueryLengthVal             time.Duration
	MaxQueryTimeoutVal            time.Duration
	MaxQueryRangeVal              time.Duration
	MaxQuerySeriesVal             int
	MaxConcurrentTailRequestsVal  int
	MaxEntriesLimitPerQueryVal    int
	MaxStreamsMatchersPerQueryVal int
	EnableMultiVariantQueriesVal  bool
}

func (m *MockLimits) EnableMultiVariantQueries(_ string) bool {
	return m.EnableMultiVariantQueriesVal
}

func (m *MockLimits) MaxQueryLookback(_ context.Context, _ string) time.Duration {
	return m.MaxQueryLookbackVal
}

func (m *MockLimits) MaxQueryLength(_ context.Context, _ string) time.Duration {
	return m.MaxQueryLengthVal
}

func (m *MockLimits) QueryTimeout(_ context.Context, _ string) time.Duration {
	return m.MaxQueryTimeoutVal
}

func (m *MockLimits) MaxQueryRange(_ context.Context, _ string) time.Duration {
	return m.MaxQueryRangeVal
}

func (m *MockLimits) MaxQuerySeries(_ context.Context, _ string) int {
	return m.MaxQuerySeriesVal
}

func (m *MockLimits) MaxConcurrentTailRequests(_ context.Context, _ string) int {
	return m.MaxConcurrentTailRequestsVal
}

func (m *MockLimits) MaxEntriesLimitPerQuery(_ context.Context, _ string) int {
	return m.MaxEntriesLimitPerQueryVal
}

func (m *MockLimits) MaxStreamsMatchersPerQuery(_ context.Context, _ string) int {
	return m.MaxStreamsMatchersPerQueryVal
}

func (m *MockLimits) BlockedQueries(_ context.Context, _ string) []*validation.BlockedQuery {
	return nil
}
