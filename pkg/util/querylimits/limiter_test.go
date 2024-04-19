package querylimits

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/validation"
)

type mockTenantLimits struct {
	limits map[string]*validation.Limits
}

func newMockTenantLimits(limits map[string]*validation.Limits) *mockTenantLimits {
	return &mockTenantLimits{
		limits: limits,
	}
}

func (l *mockTenantLimits) TenantLimits(userID string) *validation.Limits {
	return l.limits[userID]
}

func (l *mockTenantLimits) AllByUserID() map[string]*validation.Limits { return l.limits }

// end copy pasta

func TestLimiter_Defaults(t *testing.T) {
	// some fake tenant
	tLimits := make(map[string]*validation.Limits)
	tLimits["fake"] = &validation.Limits{
		QueryTimeout:            model.Duration(30 * time.Second),
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxQueryRange:           model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
		RequiredLabels:          []string{"foo", "bar"},
		RequiredNumberLabels:    10,
		MaxQueryBytesRead:       10,
		MaxQuerierBytesRead:     10,
	}

	overrides, _ := validation.NewOverrides(validation.Limits{}, newMockTenantLimits(tLimits))
	l := NewLimiter(log.NewNopLogger(), overrides)

	expectedLimits := QueryLimits{
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxQueryRange:           model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
		QueryTimeout:            model.Duration(30 * time.Second),
		MaxQueryBytesRead:       10,
		RequiredLabels:          []string{"foo", "bar"},
		RequiredNumberLabels:    10,
	}
	ctx := context.Background()
	queryLookback := l.MaxQueryLookback(ctx, "fake")
	require.Equal(t, time.Duration(expectedLimits.MaxQueryLookback), queryLookback)
	queryLength := l.MaxQueryLength(ctx, "fake")
	require.Equal(t, time.Duration(expectedLimits.MaxQueryLength), queryLength)
	maxEntries := l.MaxEntriesLimitPerQuery(ctx, "fake")
	require.Equal(t, expectedLimits.MaxEntriesLimitPerQuery, maxEntries)
	queryTimeout := l.QueryTimeout(ctx, "fake")
	require.Equal(t, time.Duration(expectedLimits.QueryTimeout), queryTimeout)
	maxQueryBytesRead := l.MaxQueryBytesRead(ctx, "fake")
	require.Equal(t, expectedLimits.MaxQueryBytesRead.Val(), maxQueryBytesRead)
	requiredNumberLabels := l.RequiredNumberLabels(ctx, "fake")
	require.Equal(t, expectedLimits.RequiredNumberLabels, requiredNumberLabels)
	maxQueryRange := l.MaxQueryRange(ctx, "fake")
	require.Equal(t, time.Duration(expectedLimits.MaxQueryRange), maxQueryRange)

	// Deserialized with defaults
	limits, err := UnmarshalQueryLimits([]byte(`{}`))
	require.NoError(t, err)

	expectedLimits2 := QueryLimits{
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxQueryRange:           model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
		QueryTimeout:            model.Duration(29 * time.Second),
		RequiredLabels:          []string{"foo", "bar"},
		RequiredNumberLabels:    10,
		MaxQueryBytesRead:       10,
	}
	{
		ctx2 := InjectQueryLimitsContext(context.Background(), *limits)
		queryLookback := l.MaxQueryLookback(ctx2, "fake")
		require.Equal(t, time.Duration(expectedLimits2.MaxQueryLookback), queryLookback)
		queryLength := l.MaxQueryLength(ctx2, "fake")
		require.Equal(t, time.Duration(expectedLimits2.MaxQueryLength), queryLength)
		maxEntries := l.MaxEntriesLimitPerQuery(ctx2, "fake")
		require.Equal(t, expectedLimits2.MaxEntriesLimitPerQuery, maxEntries)
		queryTimeout := l.QueryTimeout(ctx2, "fake")
		require.Equal(t, time.Duration(expectedLimits.QueryTimeout), queryTimeout)
		maxQueryBytesRead := l.MaxQueryBytesRead(ctx2, "fake")
		require.Equal(t, expectedLimits2.MaxQueryBytesRead.Val(), maxQueryBytesRead)
		requiredNumberLabels := l.RequiredNumberLabels(ctx2, "fake")
		require.Equal(t, expectedLimits2.RequiredNumberLabels, requiredNumberLabels)
		maxQueryRange := l.MaxQueryRange(ctx, "fake")
		require.Equal(t, time.Duration(expectedLimits2.MaxQueryRange), maxQueryRange)
	}

}

func TestLimiter_RejectHighLimits(t *testing.T) {
	// some fake tenant
	tLimits := make(map[string]*validation.Limits)
	tLimits["fake"] = &validation.Limits{
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxQueryRange:           model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
		QueryTimeout:            model.Duration(30 * time.Second),
		RequiredNumberLabels:    10,
		MaxQueryBytesRead:       10,
		MaxQuerierBytesRead:     10,
	}

	overrides, _ := validation.NewOverrides(validation.Limits{}, newMockTenantLimits(tLimits))
	l := NewLimiter(log.NewNopLogger(), overrides)
	limits := QueryLimits{
		MaxQueryLength:          model.Duration(2 * 24 * time.Hour),
		MaxQueryLookback:        model.Duration(14 * 24 * time.Hour),
		MaxEntriesLimitPerQuery: 100,
		QueryTimeout:            model.Duration(100 * time.Second),
		RequiredNumberLabels:    100,
		MaxQueryBytesRead:       100,
	}
	expectedLimits := QueryLimits{
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
		QueryTimeout:            model.Duration(30 * time.Second),
		MaxQueryBytesRead:       10,
		// In this case, the higher it is, the more restrictive.
		// Therefore, we return the query-time limit as it's more restrictive than the original.
		RequiredNumberLabels: 100,
	}

	ctx := InjectQueryLimitsContext(context.Background(), limits)
	require.Equal(t, time.Duration(expectedLimits.MaxQueryLookback), l.MaxQueryLookback(ctx, "fake"))
	require.Equal(t, time.Duration(expectedLimits.MaxQueryLength), l.MaxQueryLength(ctx, "fake"))
	require.Equal(t, expectedLimits.MaxEntriesLimitPerQuery, l.MaxEntriesLimitPerQuery(ctx, "fake"))
	require.Equal(t, time.Duration(expectedLimits.QueryTimeout), l.QueryTimeout(ctx, "fake"))
	require.Equal(t, expectedLimits.RequiredNumberLabels, l.RequiredNumberLabels(ctx, "fake"))
	require.Equal(t, expectedLimits.MaxQueryBytesRead.Val(), l.MaxQueryBytesRead(ctx, "fake"))
}

func TestLimiter_AcceptLowerLimits(t *testing.T) {
	// some fake tenant
	tLimits := make(map[string]*validation.Limits)
	tLimits["fake"] = &validation.Limits{
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxQueryRange:           model.Duration(2 * 24 * time.Hour),
		MaxEntriesLimitPerQuery: 10,
		QueryTimeout:            model.Duration(30 * time.Second),
		RequiredNumberLabels:    10,
		MaxQueryBytesRead:       10,
		MaxQuerierBytesRead:     10,
	}

	overrides, _ := validation.NewOverrides(validation.Limits{}, newMockTenantLimits(tLimits))
	l := NewLimiter(log.NewNopLogger(), overrides)
	limits := QueryLimits{
		MaxQueryLength:          model.Duration(29 * time.Second),
		MaxQueryLookback:        model.Duration(29 * time.Second),
		MaxQueryRange:           model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 9,
		QueryTimeout:            model.Duration(29 * time.Second),
		MaxQueryBytesRead:       9,
		// In this case, the higher it is, the more restrictive.
		// Therefore, we return the original limit as it's more restrictive than the query-time limit.
		RequiredNumberLabels: 10,
	}

	ctx := InjectQueryLimitsContext(context.Background(), limits)
	require.Equal(t, time.Duration(limits.MaxQueryLookback), l.MaxQueryLookback(ctx, "fake"))
	require.Equal(t, time.Duration(limits.MaxQueryLength), l.MaxQueryLength(ctx, "fake"))
	require.Equal(t, limits.MaxEntriesLimitPerQuery, l.MaxEntriesLimitPerQuery(ctx, "fake"))
	require.Equal(t, time.Duration(limits.QueryTimeout), l.QueryTimeout(ctx, "fake"))
	require.Equal(t, limits.MaxQueryBytesRead.Val(), l.MaxQueryBytesRead(ctx, "fake"))
	require.Equal(t, limits.RequiredNumberLabels, l.RequiredNumberLabels(ctx, "fake"))
	require.Equal(t, time.Duration(limits.MaxQueryRange), l.MaxQueryRange(ctx, "fake"))
}

func TestLimiter_MergeLimits(t *testing.T) {
	// some fake tenant
	tLimits := make(map[string]*validation.Limits)
	tLimits["fake"] = &validation.Limits{
		RequiredLabels: []string{"one", "two"},
	}

	overrides, _ := validation.NewOverrides(validation.Limits{}, newMockTenantLimits(tLimits))
	l := NewLimiter(log.NewNopLogger(), overrides)
	limits := QueryLimits{
		RequiredLabels: []string{"one", "three"},
	}

	require.ElementsMatch(t, []string{"one", "two"}, l.RequiredLabels(context.Background(), "fake"))

	ctx := InjectQueryLimitsContext(context.Background(), limits)

	require.ElementsMatch(t, []string{"one", "two", "three"}, l.RequiredLabels(ctx, "fake"))
}
