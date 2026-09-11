package queryfrontend

import (
	"context"
	"errors"
	"flag"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.yaml.in/yaml/v3"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/querylimits"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
)

func defaultShardPlanningTestConfig() MiddlewareConfig {
	return MiddlewareConfig{
		RequireOptInHeader:    true,
		NgramLength:           3,
		MinQueryBytesForIndex: 0,
		HintTimeout:           time.Second,
		QuerySplitDuration:    time.Hour,
		ShardPlanning: ShardPlanningConfig{
			Enabled:               true,
			MinTimeReductionRatio: 0.75,
		},
	}
}

func TestShardPlanningConfigValidationAndFlags(t *testing.T) {
	cfg := MiddlewareConfig{}
	require.NoError(t, cfg.Validate())
	require.True(t, cfg.ShardPlanning.Enabled)
	require.Equal(t, 0.75, cfg.ShardPlanning.MinTimeReductionRatio)

	flagCfg := MiddlewareConfig{}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flagCfg.RegisterFlagsWithPrefix("logline-query-frontend", fs)
	require.NotNil(t, fs.Lookup("logline-query-frontend.shard-planning.enabled"))
	require.NotNil(t, fs.Lookup("logline-query-frontend.shard-planning.min-time-reduction-ratio"))
	require.True(t, flagCfg.ShardPlanning.Enabled)
	require.Equal(t, 0.75, flagCfg.ShardPlanning.MinTimeReductionRatio)

	invalid := defaultShardPlanningTestConfig()
	invalid.ShardPlanning.MinTimeReductionRatio = -0.1
	require.ErrorContains(t, invalid.Validate(), "min_time_reduction_ratio")
}

func TestShardPlanningConfigYAMLDefaultsAndOptOut(t *testing.T) {
	var cfg MiddlewareConfig
	require.NoError(t, yaml.Unmarshal([]byte("{}"), &cfg))
	require.NoError(t, cfg.Validate())
	require.True(t, cfg.ShardPlanning.Enabled)
	require.Equal(t, 0.75, cfg.ShardPlanning.MinTimeReductionRatio)

	cfg = MiddlewareConfig{}
	require.NoError(t, yaml.Unmarshal([]byte("shard_planning:\n  enabled: false\n"), &cfg))
	require.NoError(t, cfg.Validate())
	require.False(t, cfg.ShardPlanning.Enabled)
	require.Equal(t, 0.75, cfg.ShardPlanning.MinTimeReductionRatio)
}

func TestShardPlanning_FirstQueryWinsReturnsUnchangedResponse(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
			{Start: now.Add(-40 * time.Minute), End: now.Add(-35 * time.Minute)},
		}},
		delay: 100 * time.Millisecond,
	}

	want := streamResponseWithEntries(logproto.Entry{Timestamp: now.Add(-10 * time.Minute), Line: "first"})
	var calls atomic.Int64
	next := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		calls.Add(1)
		require.Nil(t, querylimits.ExtractQueryLimitsFromContext(ctx))
		return want, nil
	})

	prefetchMW := NewLoglinePrefetchMiddleware(hp, defaultShardPlanningTestConfig(), nil, newTestMetrics(), nil)
	handler := prefetchMW.Wrap(next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	resp, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Same(t, want, resp)
	require.Equal(t, int64(1), calls.Load())
}

func TestShardPlanning_NarrowSingleHintRerunsWithQueryLimitsOverride(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	reqStart := now.Add(-1 * time.Hour)
	reqEnd := now
	hintRange := hintprovider.HintTimeRange{Start: now.Add(-35 * time.Minute), End: now.Add(-30 * time.Minute)}
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{hintRange}},
		delay: 100 * time.Millisecond,
	}

	filterMW := NewLoglineFilterMiddleware(time.Second, newTestMetrics(), nil)
	var gotOuterStart, gotOuterEnd time.Time
	var gotInnerStart, gotInnerEnd time.Time
	var gotStrategy string
	var sawQueryLimits bool
	firstCanceled := make(chan struct{})

	querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		gotInnerStart = lokiReq.StartTs
		gotInnerEnd = lokiReq.EndTs
		return emptyStreamResponse(), nil
	})

	var calls atomic.Int64
	next := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		c := calls.Add(1)
		if c == 1 {
			<-ctx.Done()
			close(firstCanceled)
			return nil, ctx.Err()
		}

		lokiReq := req.(*queryrange.LokiRequest)
		gotOuterStart = lokiReq.StartTs
		gotOuterEnd = lokiReq.EndTs
		limits := querylimits.ExtractQueryLimitsFromContext(ctx)
		sawQueryLimits = limits != nil
		if limits != nil {
			gotStrategy = limits.TSDBShardingStrategy
		}
		return filterMW.Wrap(querier).Do(ctx, req)
	})

	prefetchMW := NewLoglinePrefetchMiddleware(hp, defaultShardPlanningTestConfig(), nil, newTestMetrics(), nil)
	handler := prefetchMW.Wrap(next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, reqStart, reqEnd)

	_, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		select {
		case <-firstCanceled:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, int64(2), calls.Load())
	require.Equal(t, 1, hp.Calls(), "rerun must reuse the existing hint prefetch")
	require.Equal(t, shardPlanningStrategyPowerOfTwo, gotStrategy)
	require.True(t, sawQueryLimits)
	require.Equal(t, reqStart, gotOuterStart, "rerun should preserve the original request start")
	require.Equal(t, reqEnd, gotOuterEnd, "rerun should preserve the original request end")
	require.Equal(t, hintRange.Start, gotInnerStart, "filter middleware should do the actual narrowing")
	require.Equal(t, hintRange.End, gotInnerEnd)
}

func TestShardPlanning_ZeroOverlapsRerunsAndFilterReturnsEmptyResponse(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
			{Start: now.Add(-3 * time.Hour), End: now.Add(-2 * time.Hour)},
		}},
		delay: 100 * time.Millisecond,
	}

	filterMW := NewLoglineFilterMiddleware(time.Second, newTestMetrics(), nil)
	querierCalls := 0
	querier := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		querierCalls++
		return emptyStreamResponse(), nil
	})

	var calls atomic.Int64
	next := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		c := calls.Add(1)
		if c == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return filterMW.Wrap(querier).Do(ctx, req)
	})

	prefetchMW := NewLoglinePrefetchMiddleware(hp, defaultShardPlanningTestConfig(), nil, newTestMetrics(), nil)
	handler := prefetchMW.Wrap(next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	resp, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Equal(t, int64(2), calls.Load())
	require.Equal(t, 0, querierCalls, "empty result should still be produced by the filter middleware")
	lokiResp := resp.(*queryrange.LokiResponse)
	require.Empty(t, lokiResp.Data.Result)
}

func TestShardPlanning_MultipleDisjointNarrowHintsRerunWithinThresholds(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	r1 := hintprovider.HintTimeRange{Start: now.Add(-50 * time.Minute), End: now.Add(-48 * time.Minute)}
	r2 := hintprovider.HintTimeRange{Start: now.Add(-20 * time.Minute), End: now.Add(-18 * time.Minute)}
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{r1, r2}},
		delay: 100 * time.Millisecond,
	}

	filterMW := NewLoglineFilterMiddleware(time.Second, newTestMetrics(), nil)
	var mu sync.Mutex
	var gotStarts []time.Time
	querier := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		lokiReq := req.(*queryrange.LokiRequest)
		mu.Lock()
		gotStarts = append(gotStarts, lokiReq.StartTs)
		mu.Unlock()
		return emptyStreamResponse(), nil
	})

	var calls atomic.Int64
	next := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		c := calls.Add(1)
		if c == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		limits := querylimits.ExtractQueryLimitsFromContext(ctx)
		require.NotNil(t, limits)
		require.Equal(t, shardPlanningStrategyPowerOfTwo, limits.TSDBShardingStrategy)
		return filterMW.Wrap(querier).Do(ctx, req)
	})

	prefetchMW := NewLoglinePrefetchMiddleware(hp, defaultShardPlanningTestConfig(), nil, newTestMetrics(), nil)
	handler := prefetchMW.Wrap(next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Equal(t, int64(2), calls.Load())
	require.ElementsMatch(t, []time.Time{r1.Start, r2.Start}, gotStarts, "28m gap exceeds the k-envelope cut, so each hint is its own group")
}

func TestShardPlanningDecision_UsesEnvelopeDuration(t *testing.T) {
	start := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	h := &loglinePrefetchHandler{
		querySplitDuration: time.Hour,
		shardPlanning: ShardPlanningConfig{
			Enabled:               true,
			MinTimeReductionRatio: 0.75,
		},
	}

	t.Run("bookend hints on a 15m range fail after k=1 union", func(t *testing.T) {
		end := start.Add(15 * time.Minute)
		result := &hintPrefetchResult{
			ranges: []hintprovider.HintTimeRange{
				{Start: start.Add(time.Minute), End: start.Add(2 * time.Minute)},
				{Start: end.Add(-2 * time.Minute), End: end.Add(-time.Minute)},
			},
			ingesterCutoff: end,
		}
		// Raw hints cover 2m (ratio 0.867). One envelope covers 12m (ratio 0.2).
		got := h.shardPlanningDecision(result, start, end)
		require.False(t, got.eligible)
		require.Equal(t, "time_reduction_too_small", got.reason)
	})

	t.Run("distant clusters on a 1h range stay eligible", func(t *testing.T) {
		end := start.Add(time.Hour)
		result := &hintPrefetchResult{
			ranges: []hintprovider.HintTimeRange{
				{Start: start.Add(5 * time.Minute), End: start.Add(6 * time.Minute)},
				{Start: start.Add(50 * time.Minute), End: start.Add(51 * time.Minute)},
			},
			ingesterCutoff: end,
		}
		got := h.shardPlanningDecision(result, start, end)
		require.True(t, got.eligible)
		require.Equal(t, "eligible", got.reason)
	})

	t.Run("sparse hourly hints on an 8h range stay eligible after per-split budgets", func(t *testing.T) {
		end := start.Add(8 * time.Hour)
		var ranges []hintprovider.HintTimeRange
		for i := 0; i < 16; i++ {
			hintStart := start.Add(time.Duration(i) * 30 * time.Minute)
			ranges = append(ranges, hintprovider.HintTimeRange{
				Start: hintStart,
				End:   hintStart.Add(time.Minute),
			})
		}
		result := &hintPrefetchResult{ranges: ranges, ingesterCutoff: end}
		// Unsplit k=8 fills eight 29m gaps (~4.1h, ratio 0.48). After 1h
		// splits, each slice keeps both 1m hints (16m, ratio 0.97).
		require.Greater(t, intervalEnvelopeDuration(ranges, start, end), 4*time.Hour)
		require.Equal(t, 16*time.Minute, envelopeQueriedDuration(ranges, start, end, time.Hour))
		got := h.shardPlanningDecision(result, start, end)
		require.True(t, got.eligible)
		require.Equal(t, "eligible", got.reason)
	})

	t.Run("unsplit configured interval keeps the 8h sparse case ineligible", func(t *testing.T) {
		end := start.Add(8 * time.Hour)
		var ranges []hintprovider.HintTimeRange
		for i := 0; i < 16; i++ {
			hintStart := start.Add(time.Duration(i) * 30 * time.Minute)
			ranges = append(ranges, hintprovider.HintTimeRange{
				Start: hintStart,
				End:   hintStart.Add(time.Minute),
			})
		}
		unsplit := &loglinePrefetchHandler{shardPlanning: h.shardPlanning}
		got := unsplit.shardPlanningDecision(&hintPrefetchResult{ranges: ranges, ingesterCutoff: end}, start, end)
		require.False(t, got.eligible)
		require.Equal(t, "time_reduction_too_small", got.reason)
	})
}

func TestShardPlanning_BroadOrUnsafeHintsFallBackToFirstQuery(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hour := now.Truncate(time.Hour)
	baseCfg := defaultShardPlanningTestConfig()
	firstResp := streamResponseWithEntries(logproto.Entry{Timestamp: now.Add(-10 * time.Minute), Line: "first"})

	tests := []struct {
		name string
		cfg  MiddlewareConfig
		hp   *mockHintProvider
		req  *queryrange.LokiRequest
	}{
		{
			name: "time reduction too small falls back",
			cfg:  baseCfg,
			hp: &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-50 * time.Minute), End: now.Add(-20 * time.Minute)},
			}}},
			req: newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now),
		},
		{
			name: "envelope fill drops reduction below threshold",
			cfg:  baseCfg,
			hp: &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
				{Start: hour.Add(-14 * time.Minute), End: hour.Add(-13 * time.Minute)},
				{Start: hour.Add(-2 * time.Minute), End: hour.Add(-time.Minute)},
			}}},
			// Stay inside one SplitByInterval hour so k=1 unions the bookends.
			req: newTestLokiRequest(`{job="test"} |= "error"`, hour.Add(-15*time.Minute), hour),
		},
		{
			name: "hint provider error falls back",
			cfg:  baseCfg,
			hp:   &mockHintProvider{err: errors.New("hint backend down")},
			req:  newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now),
		},
		{
			name: "unsupported query falls back",
			cfg:  baseCfg,
			hp:   &mockHintProvider{err: hintprovider.ErrUnsupported},
			req:  newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now),
		},
		{
			name: "passthrough range falls back",
			cfg:  baseCfg,
			hp: &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
				{Start: time.Time{}, End: now.Add(-30 * time.Minute), Source: hintprovider.HintSourcePreMinDate},
			}}},
			req: newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now),
		},
		{
			name: "ingester window request falls back",
			cfg: func() MiddlewareConfig {
				cfg := baseCfg
				cfg.QueryIngestersWithin = 3 * time.Hour
				return cfg
			}(),
			hp:  &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: nil}},
			req: newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-30*time.Minute), now),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			next := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				calls.Add(1)
				require.Nil(t, querylimits.ExtractQueryLimitsFromContext(ctx))
				time.Sleep(20 * time.Millisecond)
				return firstResp, nil
			})

			prefetchMW := NewLoglinePrefetchMiddleware(tc.hp, tc.cfg, nil, newTestMetrics(), nil)
			handler := prefetchMW.Wrap(next)
			resp, err := handler.Do(testTenantContextWithLive(), tc.req)
			require.NoError(t, err)
			require.Same(t, firstResp, resp)
			require.Equal(t, int64(1), calls.Load())
		})
	}
}

func TestShardPlanning_DryRunModeNeverReruns(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	cfg := defaultShardPlanningTestConfig()
	cfg.DryRun = true
	hp := &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
		{Start: now.Add(-35 * time.Minute), End: now.Add(-30 * time.Minute)},
	}}}

	var calls atomic.Int64
	next := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		calls.Add(1)
		require.Nil(t, querylimits.ExtractQueryLimitsFromContext(ctx))
		time.Sleep(20 * time.Millisecond)
		return streamResponseWithEntries(logproto.Entry{Timestamp: now.Add(-33 * time.Minute), Line: "error"}), nil
	})

	prefetchMW := NewLoglinePrefetchMiddleware(hp, cfg, nil, newTestMetrics(), nil)
	handler := prefetchMW.Wrap(next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(testTenantContextWithDryRun(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1), calls.Load())
	require.Equal(t, 1, hp.Calls())
}

func TestShardPlanning_RerunGuardPreventsRecursion(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
		{Start: now.Add(-35 * time.Minute), End: now.Add(-30 * time.Minute)},
	}}}

	var calls atomic.Int64
	next := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		calls.Add(1)
		return emptyStreamResponse(), nil
	})

	prefetchMW := NewLoglinePrefetchMiddleware(hp, defaultShardPlanningTestConfig(), nil, newTestMetrics(), nil)
	handler := prefetchMW.Wrap(next)
	ctx := withShardPlanningRerunGuard(testTenantContextWithLive())
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, int64(1), calls.Load())
	require.Equal(t, 0, hp.Calls())
}

func TestShardPlanning_PreservesExistingQueryLimitsOnRerun(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{TimeRanges: []hintprovider.HintTimeRange{
			{Start: now.Add(-35 * time.Minute), End: now.Add(-30 * time.Minute)},
		}},
		delay: 100 * time.Millisecond,
	}

	var calls atomic.Int64
	var gotLimits *querylimits.QueryLimits
	var gotStrategy string
	next := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		c := calls.Add(1)
		if c == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		gotLimits = querylimits.ExtractQueryLimitsFromContext(ctx)
		if gotLimits != nil {
			gotStrategy = gotLimits.TSDBShardingStrategy
		}
		return emptyStreamResponse(), nil
	})

	prefetchMW := NewLoglinePrefetchMiddleware(hp, defaultShardPlanningTestConfig(), nil, newTestMetrics(), nil)
	handler := prefetchMW.Wrap(next)
	req := newTestLokiRequest(`{job="test"} |= "error"`, now.Add(-1*time.Hour), now)

	ctx := querylimits.InjectQueryLimitsIntoContext(testTenantContextWithLive(), querylimits.QueryLimits{MaxEntriesLimitPerQuery: 123})
	_, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, gotLimits)
	require.Equal(t, 123, gotLimits.MaxEntriesLimitPerQuery)
	require.Equal(t, shardPlanningStrategyPowerOfTwo, gotStrategy)
}

func TestShardPlanningMetricDoesNotRequireRegisterer(t *testing.T) {
	metrics := NewMetrics(prometheus.NewRegistry())
	require.NotNil(t, metrics.shardPlanningTotal)
}
