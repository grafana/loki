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
)

// cacheKeyString is the string a hint cache entry is keyed on: the cache
// selector of the query, as ProvideHints uses it.
func cacheKeyString(expr syntax.Expr) string {
	return cacheKeyExpr(expr).String()
}

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
			name:  "log query is its own key",
			query: `{app="api"} |= "timeout"`,
			want:  `{app="api"} |= "timeout"`,
		},
		{
			name:  "whitespace is normalized",
			query: `{app="api"}   |=   "timeout"   |  json | level="error"`,
			want:  `{app="api"} |= "timeout" | json | level="error"`,
		},
		{
			name:  "range aggregation",
			query: `count_over_time({app="api"} |= "timeout" [5m])`,
			want:  `{app="api"} |= "timeout"`,
		},
		{
			name:  "offset and grouping are not part of the key",
			query: `sum by (pod) (rate({app="api"} |= "timeout" [5m] offset 10m))`,
			want:  `{app="api"} |= "timeout"`,
		},
		{
			name:  "range length is not part of the key",
			query: `rate({app="api"} |= "timeout" [1h])`,
			want:  `{app="api"} |= "timeout"`,
		},
		{
			name:  "pipeline stages stay in the key",
			query: `sum(count_over_time({app="api"} |= "timeout" | json | level="error" [1h]))`,
			want:  `{app="api"} |= "timeout" | json | level="error"`,
		},
		{
			name:  "unwrap and its post filters are not part of the key",
			query: `sum_over_time({app="api"} |= "timeout" | logfmt | unwrap duration(latency) | __error__="" [5m])`,
			want:  `{app="api"} |= "timeout" | logfmt`,
		},
		{
			name:  "unwrap with a post filter on a real label",
			query: `sum_over_time({app="api"} |= "timeout" | logfmt | unwrap latency | status="500" [5m])`,
			want:  `{app="api"} |= "timeout" | logfmt`,
		},
		{
			name:  "unwrap with a trailing grouping",
			query: `avg_over_time({app="api"} |= "timeout" | logfmt | unwrap bytes(size) [5m]) by (pod)`,
			want:  `{app="api"} |= "timeout" | logfmt`,
		},
		{
			name:  "nested vector aggregations and regex matchers",
			query: `topk(3, sum by (pod) (bytes_over_time({app="api", env=~"prod.*"} |~ "err.+" != "debug" [10m])))`,
			want:  `{app="api", env=~"prod.*"} |~ "err.+" != "debug"`,
		},
		{
			name:  "line_format stays in the key",
			query: `count_over_time({app="api"} |= "timeout" | line_format "{{.msg}}" [5m])`,
			want:  `{app="api"} |= "timeout" | line_format "{{.msg}}"`,
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

// A log query and every supported metric wrapper of it share one key.
func TestCacheKeyExpr_MetricSharesKeyWithLogQuery(t *testing.T) {
	logKey := cacheKeyString(mustParseExpr(t, `{app="api"} |= "timeout" | json`))

	for _, q := range []string{
		`count_over_time({app="api"} |= "timeout" | json [5m])`,
		`rate({app="api"} |= "timeout" | json [1m] offset 1h)`,
		`sum by (pod) (bytes_rate({app="api"} |= "timeout" | json [10m]))`,
		`max by (pod) (sum_over_time({app="api"} |= "timeout" | json | unwrap latency [5m]))`,
	} {
		require.Equal(t, logKey, cacheKeyString(mustParseExpr(t, q)), q)
	}
}

// Anything that can change which needles the delegate looks up must change the
// key, or a cache entry would be served to a query it was not computed for.
func TestCacheKeyExpr_DifferentSelectorsDoNotShareKey(t *testing.T) {
	base := `{app="api"} |= "timeout"`
	others := []string{
		`{app="web"} |= "timeout"`,
		`{app="api", env="prod"} |= "timeout"`,
		`{app="api"} |= "timeout2"`,
		`{app="api"} |= "timeout" | json`,
		`{app="api"} |= "timeout" | json | level="error"`,
		`{app="api"} !~ "timeout"`,
		`{app="api"} |= "timeout" | line_format "{{.msg}}"`,
	}

	baseKey := cacheKeyString(mustParseExpr(t, base))
	seen := map[string]string{base: baseKey}
	for _, q := range others {
		key := cacheKeyString(mustParseExpr(t, q))
		require.NotEqual(t, baseKey, key, q)
		for prevQuery, prevKey := range seen {
			require.NotEqual(t, prevKey, key, "%q collides with %q", q, prevQuery)
		}
		seen[q] = key
	}
}

// The cached value is a function of the key, so the delegate must compute it
// from the selector the key was built from, never from the metric wrapper.
func TestCachingHintProvider_DelegateReceivesSelector(t *testing.T) {
	delegate := &recordingHintProvider{}
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)
	provideHintsAt(t, provider, `sum by (pod) (rate({app="api"} |= "timeout" [5m] offset 10m))`, from, through)

	require.Equal(t, []string{`{app="api"} |= "timeout"`}, delegate.Exprs())
}

func TestCachingHintProvider_LogThenMetricSharesDayEntry(t *testing.T) {
	delegate := &stubHintProvider{hints: dayHints()}
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)

	logHints := provideHintsAt(t, provider, `{app="api"} |= "timeout"`, from, through)
	require.Equal(t, 1, delegate.Calls())

	metricHints := provideHintsAt(t, provider, `count_over_time({app="api"} |= "timeout" [5m])`, from, through)
	require.Equal(t, 1, delegate.Calls(), "metric query should be served from the log query's entry")
	require.Equal(t, logHints, metricHints)
}

func TestCachingHintProvider_MetricThenLogSharesDayEntry(t *testing.T) {
	delegate := &stubHintProvider{hints: dayHints()}
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)

	metricHints := provideHintsAt(t, provider, `rate({app="api"} |= "timeout" [5m] offset 10m)`, from, through)
	require.Equal(t, 1, delegate.Calls())

	logHints := provideHintsAt(t, provider, `{app="api"} |= "timeout"`, from, through)
	require.Equal(t, 1, delegate.Calls(), "log query should be served from the metric query's entry")
	require.Equal(t, metricHints, logHints)
}

// A metric query's lookback spills into the previous day. It caches both full
// days, and the log query over the same period then needs only the second.
func TestCachingHintProvider_MetricLookbackSpillsIntoPreviousDay(t *testing.T) {
	delegate := &stubHintProvider{hints: dayHints()}
	backend := newMockHintCacheBackend()
	provider := NewCachingHintProvider(delegate, backend, prometheus.NewRegistry())

	queryFrom := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	queryThrough := queryFrom.Add(time.Hour)
	lookbackFrom := queryFrom.Add(-5 * time.Minute) // the prefetch window for [5m]

	provideHintsAt(t, provider, `count_over_time({app="api"} |= "timeout" [5m])`, lookbackFrom, queryThrough)
	require.Equal(t, 2, delegate.Calls(), "one delegate call per missed day")
	require.Len(t, backend.data, 2)

	provideHintsAt(t, provider, `{app="api"} |= "timeout"`, queryFrom, queryThrough)
	require.Equal(t, 2, delegate.Calls(), "log query is fully served from the cached day")
}

// Reverse order: the log query caches only the day it touches, so the metric
// query that spills into the previous day fetches just that day.
func TestCachingHintProvider_LogThenMetricLookbackFetchesOnlyPreviousDay(t *testing.T) {
	delegate := &stubHintProvider{hints: dayHints()}
	backend := newMockHintCacheBackend()
	provider := NewCachingHintProvider(delegate, backend, prometheus.NewRegistry())

	queryFrom := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	queryThrough := queryFrom.Add(time.Hour)
	lookbackFrom := queryFrom.Add(-5 * time.Minute)

	provideHintsAt(t, provider, `{app="api"} |= "timeout"`, queryFrom, queryThrough)
	require.Equal(t, 1, delegate.Calls())
	require.Len(t, backend.data, 1)

	provideHintsAt(t, provider, `count_over_time({app="api"} |= "timeout" [5m])`, lookbackFrom, queryThrough)
	require.Equal(t, 2, delegate.Calls(), "only the previous day is missing")
	require.Len(t, backend.data, 2)
}

func TestCachingHintProvider_DifferentPipelinesDoNotShareEntry(t *testing.T) {
	delegate := &stubHintProvider{hints: dayHints()}
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)

	provideHintsAt(t, provider, `{app="api"} |= "timeout"`, from, through)
	provideHintsAt(t, provider, `count_over_time({app="api"} |= "timeout" | json | level="error" [5m])`, from, through)
	require.Equal(t, 2, delegate.Calls(), "a different pipeline has different needles and needs its own entry")
}

func TestCachingHintProvider_SelectorKeyStaysTenantScoped(t *testing.T) {
	delegate := &stubHintProvider{hints: dayHints()}
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)
	expr := mustParseExpr(t, `{app="api"} |= "timeout"`)

	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		_, _, err := provider.ProvideHints(context.Background(), tenant, expr,
			model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(through.UnixNano()))
		require.NoError(t, err)
	}
	require.Equal(t, 2, delegate.Calls())
}

// Concurrent lookups for a log query and its metric wrapper share one delegate
// call, because the singleflight key is built from the same selector.
func TestCachingHintProvider_SingleflightSharesLogAndMetricLookups(t *testing.T) {
	delegate := &stubHintProvider{hints: dayHints(), delay: 200 * time.Millisecond}
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)

	var wg sync.WaitGroup
	for _, q := range []string{
		`{app="api"} |= "timeout"`,
		`count_over_time({app="api"} |= "timeout" [5m])`,
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			provideHintsAt(t, provider, q, from, through)
		}()
	}
	wg.Wait()

	require.Equal(t, 1, delegate.Calls())
}

// dayHints is a delegate response with a range in each of two adjacent days.
func dayHints() *Hints {
	return &Hints{TimeRanges: []HintTimeRange{
		{
			Start: time.Date(2026, 3, 9, 23, 56, 0, 0, time.UTC),
			End:   time.Date(2026, 3, 9, 23, 58, 0, 0, time.UTC),
		},
		{
			Start: time.Date(2026, 3, 10, 0, 10, 0, 0, time.UTC),
			End:   time.Date(2026, 3, 10, 0, 20, 0, 0, time.UTC),
		},
	}}
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
