package querylimits

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/validation"
)

type mockTenantLimits struct {
	limits map[string]*validation.Limits
}

func newMockTenantLimits(limits map[string]*validation.Limits) *mockTenantLimits {
	return &mockTenantLimits{
		limits: limits,
	}
}

func (l *mockTenantLimits) TenantLimits(tenantID string) *validation.Limits {
	return l.limits[tenantID]
}

func (l *mockTenantLimits) AllByUserID() map[string]*validation.Limits { return l.limits }

// end copy pasta

func TestLimiter_Defaults(t *testing.T) {
	dur := model.Duration(30 * time.Second)
	entries := 10
	// some fake tenant
	tLimits := make(map[string]*validation.Limits)
	tLimits["fake"] = &validation.Limits{
		QueryTimeout:            dur,
		MaxQueryLookback:        dur,
		MaxQueryLength:          dur,
		MaxEntriesLimitPerQuery: entries,
	}

	overrides, _ := validation.NewOverrides(validation.Limits{}, newMockTenantLimits(tLimits))
	l := NewLimiter(log.NewNopLogger(), overrides)

	expectedLimits := QueryLimits{
		MaxQueryLength:          &dur,
		MaxQueryLookback:        &dur,
		MaxEntriesLimitPerQuery: &entries,
		QueryTimeout:            &dur,
	}

	ctx := context.Background()
	queryLookback := l.MaxQueryLookback(ctx, "fake")
	require.Equal(t, time.Duration(*expectedLimits.MaxQueryLookback), queryLookback)
	queryLength := l.MaxQueryLength(ctx, "fake")
	require.Equal(t, time.Duration(*expectedLimits.MaxQueryLength), queryLength)
	maxEntries := l.MaxEntriesLimitPerQuery(ctx, "fake")
	require.Equal(t, *expectedLimits.MaxEntriesLimitPerQuery, maxEntries)
	queryTimeout := l.QueryTimeout(ctx, "fake")
	require.Equal(t, time.Duration(*expectedLimits.QueryTimeout), queryTimeout)

	//var limits QueryLimits

	timeout2 := model.Duration(29 * time.Second)
	expectedLimits2 := QueryLimits{QueryTimeout: &timeout2}
	//expectedLimits2.QueryTimeout = &timeout2
	{
		ctx2 := InjectQueryLimitsContext(context.Background(), expectedLimits2)
		queryLookback := l.MaxQueryLookback(ctx2, "fake")
		require.Equal(t, time.Duration(*expectedLimits.MaxQueryLookback), queryLookback)
		queryLength := l.MaxQueryLength(ctx2, "fake")
		require.Equal(t, time.Duration(*expectedLimits.MaxQueryLength), queryLength)
		maxEntries := l.MaxEntriesLimitPerQuery(ctx2, "fake")
		require.Equal(t, *expectedLimits.MaxEntriesLimitPerQuery, maxEntries)
		queryTimeout := l.QueryTimeout(ctx2, "fake")
		require.Equal(t, time.Duration(*expectedLimits2.QueryTimeout), queryTimeout)
	}

}

func TestLimiter_RejectHighLimits(t *testing.T) {
	dur := model.Duration(30 * time.Second)
	entries := 10

	// some fake tenant
	tLimits := make(map[string]*validation.Limits)
	tLimits["fake"] = &validation.Limits{
		MaxQueryLookback:        dur,
		MaxQueryLength:          dur,
		MaxEntriesLimitPerQuery: entries,
		QueryTimeout:            dur,
	}

	overrides, _ := validation.NewOverrides(validation.Limits{}, newMockTenantLimits(tLimits))
	l := NewLimiter(log.NewNopLogger(), overrides)
	limits := QueryLimits{
		MaxQueryLength:          &dur,
		MaxQueryLookback:        &dur,
		MaxEntriesLimitPerQuery: &entries,
		QueryTimeout:            &dur,
	}
	//expectedLimits := QueryLimits{
	//	MaxQueryLength:          model.Duration(30 * time.Second),
	//	MaxQueryLookback:        model.Duration(30 * time.Second),
	//	MaxEntriesLimitPerQuery: 10,
	//	QueryTimeout:            model.Duration(30 * time.Second),
	//}

	ctx := InjectQueryLimitsContext(context.Background(), limits)
	require.Equal(t, time.Duration(*limits.MaxQueryLookback), l.MaxQueryLookback(ctx, "fake"))
	require.Equal(t, time.Duration(*limits.MaxQueryLength), l.MaxQueryLength(ctx, "fake"))
	require.Equal(t, *limits.MaxEntriesLimitPerQuery, l.MaxEntriesLimitPerQuery(ctx, "fake"))
	require.Equal(t, time.Duration(*limits.QueryTimeout), l.QueryTimeout(ctx, "fake"))
}

func TestLimiter_AcceptLowerLimits(t *testing.T) {
	dur := model.Duration(30 * time.Second)
	entries := 10

	// some fake tenant
	tLimits := make(map[string]*validation.Limits)
	tLimits["fake"] = &validation.Limits{
		MaxQueryLookback:        dur,
		MaxQueryLength:          dur,
		MaxEntriesLimitPerQuery: entries,
		QueryTimeout:            dur,
	}

	overrides, _ := validation.NewOverrides(validation.Limits{}, newMockTenantLimits(tLimits))
	l := NewLimiter(log.NewNopLogger(), overrides)
	qlEntries := 9
	limits := QueryLimits{
		MaxQueryLength:          &dur,
		MaxQueryLookback:        &dur,
		MaxEntriesLimitPerQuery: &qlEntries,
		QueryTimeout:            &dur,
	}

	ctx := InjectQueryLimitsContext(context.Background(), limits)
	require.Equal(t, time.Duration(*limits.MaxQueryLookback), l.MaxQueryLookback(ctx, "fake"))
	require.Equal(t, time.Duration(*limits.MaxQueryLength), l.MaxQueryLength(ctx, "fake"))
	require.Equal(t, *limits.MaxEntriesLimitPerQuery, l.MaxEntriesLimitPerQuery(ctx, "fake"))
	require.Equal(t, time.Duration(*limits.QueryTimeout), l.QueryTimeout(ctx, "fake"))
}
