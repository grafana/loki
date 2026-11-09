package hintprovider

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// These tests specify the hint cache key: the log selector (stream matchers
// plus pipeline) of the query, so a metric query shares entries with the log
// query it wraps. Range, offset, grouping, unwrap and outer aggregations are
// not part of the key.

// cacheKeyString is the string a hint cache entry is keyed on, as
// ProvideHints computes it.
func cacheKeyString(expr syntax.Expr) string {
	return cacheKeyExpr(expr).String()
}

func TestCacheKeyExpr(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "log query is its own key",
			query: `{app="api"} |= "timeout"`,
			want:  `{app="api"} |= "timeout"`,
		},
		{
			name:  "offset and vector grouping are not part of the key",
			query: `sum by (pod) (rate({app="api"} |= "timeout" [5m] offset 10m))`,
			want:  `{app="api"} |= "timeout"`,
		},
		{
			name:  "pipeline stages stay in the key",
			query: `sum(count_over_time({app="api"} |= "timeout" | json | level="error" [1h]))`,
			want:  `{app="api"} |= "timeout" | json | level="error"`,
		},
		{
			name:  "unwrap and its post filters are not part of the key",
			query: `sum_over_time({app="api"} |= "timeout" | logfmt | unwrap duration(latency) | status="500" [5m])`,
			want:  `{app="api"} |= "timeout" | logfmt`,
		},
		{
			// No single selector. The middleware never sends this shape to the
			// cache, but the provider is generic, so it must not panic or
			// collide with another key: fall back to the whole expression.
			name:  "binary operation falls back to the whole expression",
			query: `count_over_time({app="api"} |= "x" [5m]) / count_over_time({app="api"} [5m])`,
			want:  `(count_over_time({app="api"} |= "x"[5m]) / count_over_time({app="api"}[5m]))`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, cacheKeyString(mustParseExpr(t, tc.query)))
		})
	}
}

// The delegate is asked about the selector, not the metric expression, so the
// needles it looks up must be the same for both. A label filter before unwrap
// stays in the selector. One after unwrap lives on UnwrapExpr and is dropped
// from the key. Neither may change the needles.
func TestCacheKeyExpr_PreservesNeedles(t *testing.T) {
	for _, q := range []string{
		`sum_over_time({app="api"} | logfmt | status="errorcode" | unwrap latency [5m])`,
		`sum_over_time({app="api"} |= "timeout" | logfmt | unwrap latency | status="errorcode" [5m])`,
	} {
		expr := mustParseExpr(t, q)
		require.Equal(t, SupportedQuery(expr, 6), SupportedQuery(cacheKeyExpr(expr), 6), q)
	}
}

// Entries are whole days. A metric query whose window reaches into the
// previous day fills both days under the log selector, and a later log query
// over the later day is served from cache.
func TestCachingHintProvider_MetricLookbackFillsDaysForLogQuery(t *testing.T) {
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
	backend := newMockHintCacheBackend()
	provider := NewCachingHintProvider(delegate, backend, prometheus.NewRegistry())

	got := provideHintsAt(t, provider, metricQuery, lookbackFrom, queryThrough)
	require.Equal(t, []HintTimeRange{prevDay, sameDay}, got)
	require.Equal(t, 2, delegate.Calls())
	require.Len(t, backend.data, 2)

	got = provideHintsAt(t, provider, logQuery, queryFrom, queryThrough)
	require.Equal(t, []HintTimeRange{sameDay}, got)
	require.Equal(t, 2, delegate.Calls())
	require.Len(t, backend.data, 2)
}

// The cached value is a function of the key, so the delegate must compute it
// from the expression the key was built from, never from the metric wrapper.
// ProvideHints rewrites the expression before it branches on the cache, so
// every path must hand the delegate the same thing.
func TestCachingHintProvider_DelegateReceivesKeyExpr(t *testing.T) {
	const binop = `count_over_time({app="api"} |= "x" [5m]) / count_over_time({app="api"} [5m])`

	tests := []struct {
		name  string
		ctx   context.Context
		cache bool
		query string
		want  string
	}{
		{"cache miss", context.Background(), true, `sum by (pod) (rate({app="api"} |= "timeout" [5m] offset 10m))`, `{app="api"} |= "timeout"`},
		{"skip cache", WithSkipCache(context.Background()), true, `count_over_time({app="api"} |= "timeout" [5m])`, `{app="api"} |= "timeout"`},
		{"no cache", context.Background(), false, `count_over_time({app="api"} |= "timeout" [5m])`, `{app="api"} |= "timeout"`},
		{"no single selector", context.Background(), true, binop, mustParseExpr(t, binop).String()},
	}

	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			delegate := &recordingHintProvider{}
			var hintCache cache.Cache // stays a nil interface when there is no cache
			if tc.cache {
				hintCache = newMockHintCacheBackend()
			}
			provider := NewCachingHintProvider(delegate, hintCache, prometheus.NewRegistry())

			_, _, err := provider.ProvideHints(tc.ctx, "tenant-a", mustParseExpr(t, tc.query),
				model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(through.UnixNano()))
			require.NoError(t, err)
			require.Equal(t, []string{tc.want}, delegate.Exprs())
		})
	}
}

// Concurrent lookups for a log query and its metric wrapper share one delegate
// call, because the singleflight key is built from the same expression as the
// cache key.
func TestCachingHintProvider_SingleflightSharesLogAndMetricLookups(t *testing.T) {
	delegate := &releasedHintProvider{
		started: make(chan struct{}),
		release: make(chan struct{}),
		hints:   &Hints{TimeRanges: []HintTimeRange{{Start: time.Date(2026, 3, 10, 0, 10, 0, 0, time.UTC), End: time.Date(2026, 3, 10, 0, 20, 0, 0, time.UTC)}}},
	}
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

	from := model.TimeFromUnixNano(time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC).UnixNano())
	through := model.TimeFromUnixNano(time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC).UnixNano())
	logExpr := mustParseExpr(t, `{app="api"} |= "timeout"`)
	metricExpr := mustParseExpr(t, `count_over_time({app="api"} |= "timeout" [5m])`)

	errs := make(chan error, 2)
	lookup := func(expr syntax.Expr) {
		go func() {
			_, _, err := provider.ProvideHints(context.Background(), "tenant-a", expr, from, through)
			errs <- err
		}()
	}

	lookup(logExpr)
	<-delegate.started
	lookup(metricExpr)
	// Give the second lookup time to join the first before the delegate returns.
	time.Sleep(100 * time.Millisecond)
	close(delegate.release)

	for range 2 {
		require.NoError(t, <-errs)
	}
	require.Equal(t, int64(1), delegate.calls.Load())
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

// recordingHintProvider records the expressions it is asked about.
type recordingHintProvider struct {
	mu    sync.Mutex
	exprs []string
}

func (r *recordingHintProvider) ProvideHints(
	_ context.Context,
	_ string,
	expr syntax.Expr,
	_, _ model.Time,
) (*Hints, *QueryStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exprs = append(r.exprs, expr.String())
	return &Hints{}, NewQueryStats(), nil
}

func (r *recordingHintProvider) Exprs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.exprs...)
}
