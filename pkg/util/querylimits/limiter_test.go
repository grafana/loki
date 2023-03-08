package querylimits

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/validation"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
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
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
	}

	overrides, _ := validation.NewOverrides(validation.Limits{}, newMockTenantLimits(tLimits))
	l := NewLimiter(overrides)

	expectedLimits := QueryLimits{
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
	}
	ctx := context.Background()
	queryLookback := l.MaxQueryLookback(ctx, "fake")
	require.Equal(t, time.Duration(expectedLimits.MaxQueryLookback), queryLookback)
	queryLength := l.MaxQueryLength(ctx, "fake")
	require.Equal(t, time.Duration(expectedLimits.MaxQueryLength), queryLength)
	maxEntries := l.MaxEntriesLimitPerQuery(ctx, "fake")
	require.Equal(t, expectedLimits.MaxEntriesLimitPerQuery, maxEntries)

	var limits QueryLimits

	expectedLimits2 := QueryLimits{
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
	}
	{
		ctx2 := InjectQueryLimitsContext(context.Background(), limits)
		queryLookback := l.MaxQueryLookback(ctx2, "fake")
		require.Equal(t, time.Duration(expectedLimits2.MaxQueryLookback), queryLookback)
		queryLength := l.MaxQueryLength(ctx2, "fake")
		require.Equal(t, time.Duration(expectedLimits2.MaxQueryLength), queryLength)
		maxEntries := l.MaxEntriesLimitPerQuery(ctx2, "fake")
		require.Equal(t, expectedLimits2.MaxEntriesLimitPerQuery, maxEntries)
	}

}

func TestLimiter_RejectHighLimits(t *testing.T) {
	// some fake tenant
	tLimits := make(map[string]*validation.Limits)
	tLimits["fake"] = &validation.Limits{
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
	}

	overrides, _ := validation.NewOverrides(validation.Limits{}, newMockTenantLimits(tLimits))
	l := NewLimiter(overrides)
	limits := QueryLimits{
		MaxQueryLength:          model.Duration(2 * 24 * time.Hour),
		MaxQueryLookback:        model.Duration(14 * 24 * time.Hour),
		MaxEntriesLimitPerQuery: 100,
	}
	expectedLimits := QueryLimits{
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
	}

	ctx := InjectQueryLimitsContext(context.Background(), limits)
	require.Equal(t, time.Duration(expectedLimits.MaxQueryLookback), l.MaxQueryLookback(ctx, "fake"))
	require.Equal(t, time.Duration(expectedLimits.MaxQueryLength), l.MaxQueryLength(ctx, "fake"))
	require.Equal(t, expectedLimits.MaxEntriesLimitPerQuery, l.MaxEntriesLimitPerQuery(ctx, "fake"))
}

func TestLimiter_AcceptLowerLimits(t *testing.T) {
	// some fake tenant
	tLimits := make(map[string]*validation.Limits)
	tLimits["fake"] = &validation.Limits{
		MaxQueryLookback:        model.Duration(30 * time.Second),
		MaxQueryLength:          model.Duration(30 * time.Second),
		MaxEntriesLimitPerQuery: 10,
	}

	overrides, _ := validation.NewOverrides(validation.Limits{}, newMockTenantLimits(tLimits))
	l := NewLimiter(overrides)
	limits := QueryLimits{
		MaxQueryLength:          model.Duration(29 * time.Second),
		MaxQueryLookback:        model.Duration(29 * time.Second),
		MaxEntriesLimitPerQuery: 9,
	}

	ctx := InjectQueryLimitsContext(context.Background(), limits)
	require.Equal(t, time.Duration(limits.MaxQueryLookback), l.MaxQueryLookback(ctx, "fake"))
	require.Equal(t, time.Duration(limits.MaxQueryLength), l.MaxQueryLength(ctx, "fake"))
	require.Equal(t, limits.MaxEntriesLimitPerQuery, l.MaxEntriesLimitPerQuery(ctx, "fake"))
}
