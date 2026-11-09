package hintprovider

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// These tests specify the hint cache key: the log selector (stream matchers
// plus pipeline) of the query, so a metric query shares entries with the log
// query it wraps. Range, offset, grouping, unwrap and outer aggregations are
// not part of the key.

func TestCacheKeyExpr(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "range, offset and grouping are not part of the key; the pipeline is",
			query: `sum by (pod) (rate({app="api"} |= "timeout" | json | level="critical" [5m] offset 10m))`,
			want:  `{app="api"} |= "timeout" | json | level="critical"`,
		},
		{
			// A filter before unwrap stays in the selector. One after it lives on
			// the unwrap, so it leaves the key.
			name:  "unwrap and its post filters are not part of the key",
			query: `sum_over_time({app="api"} |= "timeout" | logfmt | level="critical" | unwrap latency | status="errorcode" [5m])`,
			want:  `{app="api"} |= "timeout" | logfmt | level="critical"`,
		},
		{
			// No single selector. The middleware never sends this shape to the
			// cache, but the provider is generic, so it must not panic or collide
			// with another key: fall back to the whole expression.
			name:  "binary operation falls back to the whole expression",
			query: `count_over_time({app="api"} |= "x" [5m]) / count_over_time({app="api"} [5m])`,
			want:  mustParseExpr(t, `count_over_time({app="api"} |= "x" [5m]) / count_over_time({app="api"} [5m])`).String(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr := mustParseExpr(t, tc.query)
			require.Equal(t, tc.want, cacheKeyExpr(expr).String())

			// The delegate is asked about the key expression, so it must look up
			// the same needles as the original query. Otherwise moving to the
			// selector would silently change how well a metric query narrows.
			require.Equal(t, SupportedQuery(expr, 6), SupportedQuery(cacheKeyExpr(expr), 6))
		})
	}
}

// A metric query and the log query it wraps share one cache entry, whichever
// runs first. The metric window reaches into the previous day, so the metric
// query fills both days and the log query over the later day is a full hit.
func TestCachingHintProvider_MetricAndLogQueriesShareCacheEntries(t *testing.T) {
	const (
		logQuery    = `{app="api"} |= "timeout"`
		metricQuery = `sum by (pod) (count_over_time({app="api"} |= "timeout" [5m]))`
	)
	queryFrom := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	queryThrough := queryFrom.Add(time.Hour)
	lookbackFrom := queryFrom.Add(-5 * time.Minute)

	prevDay := HintTimeRange{
		Start: time.Date(2026, 3, 9, 23, 56, 0, 0, time.UTC),
		End:   time.Date(2026, 3, 9, 23, 58, 0, 0, time.UTC),
	}
	sameDay := HintTimeRange{
		Start: time.Date(2026, 3, 10, 0, 10, 0, 0, time.UTC),
		End:   time.Date(2026, 3, 10, 0, 20, 0, 0, time.UTC),
	}

	delegate := &stubHintProvider{hints: &Hints{TimeRanges: []HintTimeRange{prevDay, sameDay}}}
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

	got := provideHintsAt(t, provider, metricQuery, lookbackFrom, queryThrough)
	require.Equal(t, []HintTimeRange{prevDay, sameDay}, got)
	require.Equal(t, 2, delegate.Calls(), "one delegate call per missed day")

	got = provideHintsAt(t, provider, logQuery, queryFrom, queryThrough)
	require.Equal(t, []HintTimeRange{sameDay}, got)
	require.Equal(t, 2, delegate.Calls(), "the log query is served from the metric query's entry")
}

// The cached value is a function of the key, so the delegate must compute it
// from the expression the key was built from, never from the metric wrapper.
// ProvideHints rewrites the expression before it branches on the cache, so
// every path must hand the delegate the same thing.
func TestCachingHintProvider_DelegateReceivesKeyExpr(t *testing.T) {
	const (
		metricQuery = `sum by (pod) (rate({app="api"} |= "timeout" [5m] offset 10m))`
		want        = `{app="api"} |= "timeout"`
	)
	tests := []struct {
		name  string
		ctx   context.Context
		cache bool
	}{
		{"cache miss", context.Background(), true},
		{"skip cache", WithSkipCache(context.Background()), true},
		{"no cache", context.Background(), false},
	}

	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			delegate := &stubHintProvider{}
			var hintCache cache.Cache // stays a nil interface when there is no cache
			if tc.cache {
				hintCache = newMockHintCacheBackend()
			}
			provider := NewCachingHintProvider(delegate, hintCache, prometheus.NewRegistry())

			_, _, err := provider.ProvideHints(tc.ctx, "tenant-a", mustParseExpr(t, metricQuery),
				model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(through.UnixNano()))
			require.NoError(t, err)
			require.Equal(t, []string{want}, delegate.Exprs())
		})
	}
}

func provideHintsAt(t *testing.T, p *CachingHintProvider, query string, from, through time.Time) []HintTimeRange {
	t.Helper()
	hints, _, err := p.ProvideHints(
		context.Background(),
		"tenant-a",
		mustParseExpr(t, query),
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(through.UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	return hints.TimeRanges
}
