package queryrange

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

func Test_PreservesCachingOptions(t *testing.T) {
	splitter := newDefaultSplitter(fakeLimits{splitDuration: map[string]time.Duration{"1": time.Hour}}, nil)
	end := time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)
	req := &LokiRequest{
		StartTs: end.Add(-3 * time.Hour),
		EndTs:   end,
		CachingOptions: resultscache.CachingOptions{
			Disabled: true,
		},
		HintRanges: []logproto.HintTimeRange{
			{Start: end.Add(-150 * time.Minute), End: end.Add(-90 * time.Minute)},
			{Start: end.Add(-30 * time.Minute), End: end.Add(30 * time.Minute)},
		},
	}
	splitReqs, enabled := splitter.split(time.Now().UTC(), []string{"1"}, req)
	require.True(t, enabled)
	require.Len(t, splitReqs, 3)
	for _, splitReq := range splitReqs {
		require.Equal(t, splitReq.GetCachingOptions(), req.GetCachingOptions())
	}
	require.Equal(t, []logproto.HintTimeRange{{
		Start: end.Add(-150 * time.Minute),
		End:   end.Add(-2 * time.Hour),
	}}, splitReqs[0].(*LokiRequest).HintRanges)
	require.Equal(t, []logproto.HintTimeRange{{
		Start: end.Add(-2 * time.Hour),
		End:   end.Add(-90 * time.Minute),
	}}, splitReqs[1].(*LokiRequest).HintRanges)
	require.Equal(t, []logproto.HintTimeRange{{
		Start: end.Add(-30 * time.Minute),
		End:   end,
	}}, splitReqs[2].(*LokiRequest).HintRanges)
}

func TestDefaultSplitter_split(t *testing.T) {
	var (
		start        = time.Date(2026, 3, 30, 0, 0, 0, 0, time.UTC)
		intervalsSet = fakeLimits{
			splitDuration:         map[string]time.Duration{"1": time.Hour, "2": 30 * time.Minute, "3": 0},
			metadataSplitDuration: map[string]time.Duration{"1": 6 * time.Hour, "2": 2 * time.Hour},
		}
		limits = fakeLimits{splitDuration: map[string]time.Duration{"1": time.Hour}}
	)

	numberOfSplits := func(t *testing.T, tenantIDs []string, req queryrangebase.Request) int {
		t.Helper()
		splits, enabled := newDefaultSplitter(intervalsSet, nil).split(start.Add(24*time.Hour), tenantIDs, req)
		require.True(t, enabled)
		return len(splits)
	}

	t.Run("returns enabled false when the tenant turns splitting off", func(t *testing.T) {
		splitter := newDefaultSplitter(fakeLimits{}, nil)

		splits, enabled := splitter.split(time.Now().UTC(), []string{"1"}, &LokiRequest{})

		require.False(t, enabled)
		require.Empty(t, splits)
	})

	t.Run("returns enabled true and no splits for a request type it does not split", func(t *testing.T) {
		splitter := newDefaultSplitter(limits, nil)

		splits, enabled := splitter.split(time.Now().UTC(), []string{"1"}, &LokiInstantRequest{})

		require.True(t, enabled)
		require.Empty(t, splits)
	})

	t.Run("cuts a log request at the query split duration of the tenant", func(t *testing.T) {
		req := &LokiRequest{StartTs: start, EndTs: start.Add(6 * time.Hour)}

		require.Equal(t, 6, numberOfSplits(t, []string{"1"}, req))
	})

	t.Run("cuts a log request at the smallest positive query split duration of the tenants", func(t *testing.T) {
		req := &LokiRequest{StartTs: start, EndTs: start.Add(6 * time.Hour)}

		require.Equal(t, 12, numberOfSplits(t, []string{"1", "2", "3"}, req))
	})

	t.Run("cuts a series request at the metadata split duration of the tenant", func(t *testing.T) {
		req := &LokiSeriesRequest{StartTs: start, EndTs: start.Add(12 * time.Hour)}

		require.Equal(t, 2, numberOfSplits(t, []string{"1"}, req))
	})

	t.Run("cuts a label request at the largest metadata split duration of the tenants", func(t *testing.T) {
		req := NewLabelRequest(start, start.Add(12*time.Hour), "", "", "/loki/api/v1/labels")

		require.Equal(t, 2, numberOfSplits(t, []string{"1", "2"}, req))
	})

	t.Run("returns enabled false for a log request when the tenant sets no split duration", func(t *testing.T) {
		splitter := newDefaultSplitter(fakeLimits{splitDuration: map[string]time.Duration{"3": 0}}, nil)

		splits, enabled := splitter.split(time.Now().UTC(), []string{"3"}, &LokiRequest{})

		require.False(t, enabled)
		require.Empty(t, splits)
	})

	t.Run("returns enabled false for a series request when the tenant sets no metadata split duration", func(t *testing.T) {
		splitter := newDefaultSplitter(fakeLimits{splitDuration: map[string]time.Duration{"1": time.Hour}}, nil)

		splits, enabled := splitter.split(time.Now().UTC(), []string{"1"}, &LokiSeriesRequest{})

		require.False(t, enabled)
		require.Empty(t, splits)
	})
}

func TestMetricQuerySplitter_split(t *testing.T) {
	const query = `sum(count_over_time({app="a"}[1m]))`
	var (
		start    = time.Date(2026, 10, 7, 13, 0, 0, 0, time.UTC)
		end      = time.Date(2026, 10, 9, 7, 30, 0, 0, time.UTC)
		execTime = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
		tenants  = []string{"1"}
		limits   = fakeLimits{splitDuration: map[string]time.Duration{"1": time.Hour}}
		cfg      = Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag, now: func() time.Time { return dataObjRoutingNow }}
	)

	split := func(t *testing.T, splitter *metricQuerySplitter, req *LokiRequest) []queryrangebase.Request {
		t.Helper()
		splits, enabled := splitter.split(dataObjRoutingNow, tenants, req)
		require.True(t, enabled)
		return splits
	}

	t.Run("cuts a query at the query split duration of the tenant", func(t *testing.T) {
		splits, enabled := newMetricQuerySplitter(Config{}, limits, nil).split(execTime, tenants, newRangeRequest(query, start, end))

		require.True(t, enabled)
		require.Len(t, splits, 43)
	})

	t.Run("widens the interval to a long range vector and cuts at multiples of the widened interval", func(t *testing.T) {
		splitter := newMetricQuerySplitter(Config{}, limits, nil)

		splits, enabled := splitter.split(execTime, tenants, newRangeRequest(`sum(count_over_time({app="a"}[30h]))`, start, end))

		interval := 30 * time.Hour
		boundary := time.Unix(0, (start.UnixNano()/int64(interval)+1)*int64(interval)).UTC()
		require.True(t, enabled)
		require.Len(t, splits, 3)
		require.Equal(t, boundary.Add(-time.Minute), splits[0].GetEnd().UTC())
		require.Equal(t, boundary, splits[1].GetStart().UTC())
		require.Equal(t, boundary.Add(interval-time.Minute), splits[1].GetEnd().UTC())
	})

	t.Run("returns enabled false when the tenant turns splitting off", func(t *testing.T) {
		splitter := newMetricQuerySplitter(Config{}, fakeLimits{splitDuration: map[string]time.Duration{"1": 0}}, nil)

		splits, enabled := splitter.split(execTime, tenants, newRangeRequest(query, start, end))

		require.False(t, enabled)
		require.Empty(t, splits)
	})

	t.Run("returns enabled false for a request without a plan when the tenant turns splitting off", func(t *testing.T) {
		splitter := newMetricQuerySplitter(Config{}, fakeLimits{splitDuration: map[string]time.Duration{"1": 0}}, nil)
		req := newRangeRequest(query, start, end)
		req.Plan = nil

		splits, enabled := splitter.split(execTime, tenants, req)

		require.False(t, enabled)
		require.Empty(t, splits)
	})

	t.Run("cuts a query that prefers data objects at the UTC window boundaries", func(t *testing.T) {
		splitter := newMetricQuerySplitter(cfg, newSplitTestLimits("1"), nil)

		splits := split(t, splitter, newRangeRequest(streamFirstQuery, start, end))

		var got [][2]time.Time
		for _, s := range splits {
			got = append(got, [2]time.Time{s.GetStart().UTC(), s.GetEnd().UTC()})
		}
		require.Equal(t, [][2]time.Time{
			{time.Date(2026, 10, 7, 13, 0, 0, 0, time.UTC), time.Date(2026, 10, 7, 23, 59, 0, 0, time.UTC)},
			{time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 8, 11, 59, 0, 0, time.UTC)},
			{time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), time.Date(2026, 10, 8, 23, 59, 0, 0, time.UTC)},
			{time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 9, 7, 30, 0, 0, time.UTC)},
		}, got)
	})

	t.Run("cuts a query that does not prefer data objects like the chunk logic", func(t *testing.T) {
		limits := newSplitTestLimits("1")
		req := newRangeRequest(streamFirstQuery, dataObjRoutingNow.Add(-150*time.Minute), dataObjRoutingNow.Add(-30*time.Minute))

		withDataObjs := split(t, newMetricQuerySplitter(cfg, limits, nil), req)
		withoutDataObjs := split(t, newMetricQuerySplitter(Config{}, limits, nil), req)

		require.Equal(t, withoutDataObjs, withDataObjs)
		require.Greater(t, len(withDataObjs), 1)
	})

	t.Run("cuts a query with one stream-first leg at the UTC window boundaries", func(t *testing.T) {
		splitter := newMetricQuerySplitter(cfg, newSplitTestLimits("1"), nil)
		query := streamFirstQuery + ` / count(count_over_time({app="b"}[1m]))`

		splits := split(t, splitter, newRangeRequest(query, start, end))

		require.Len(t, splits, 4)
	})

	t.Run("keeps every split aligned to the step", func(t *testing.T) {
		splitter := newMetricQuerySplitter(cfg, newSplitTestLimits("1"), nil)

		splits := split(t, splitter, newRangeRequest(streamFirstQuery, start.Add(17*time.Second), end))

		for _, s := range splits {
			require.Zero(t, s.GetStart().UnixMilli()%60000)
			require.Zero(t, s.GetEnd().UnixMilli()%60000)
		}
	})

	t.Run("widens to a long range vector and cuts at multiples of the rounded interval", func(t *testing.T) {
		splitter := newMetricQuerySplitter(cfg, newSplitTestLimits("1"), nil)
		query := `sum(count_over_time({app="a"}[30h]))`

		splits := split(t, splitter, newRangeRequest(query, start, end))

		interval := int64(36 * time.Hour)
		boundary := time.Unix(0, (start.UnixNano()/interval+1)*interval).UTC()
		require.Len(t, splits, 2)
		require.Equal(t, boundary.Add(-time.Minute), splits[0].GetEnd().UTC())
		require.Equal(t, boundary, splits[1].GetStart().UTC())
	})

	t.Run("cuts the ingester window with the ingester interval", func(t *testing.T) {
		limits := newSplitTestLimits("1")
		limits.ingesterSplitDuration = map[string]time.Duration{"1": 15 * time.Minute}
		splitter := newMetricQuerySplitter(cfg, limits, ingesterQueryOpts{queryIngestersWithin: 3 * time.Hour})
		ingesterWindowStart := dataObjRoutingNow.Add(-3 * time.Hour)

		splits := split(t, splitter, newRangeRequest(streamFirstQuery, dataObjRoutingNow.Add(-30*time.Hour), dataObjRoutingNow.Add(-time.Hour)))

		var sawLongSplit bool
		for _, s := range splits {
			length := s.GetEnd().Sub(s.GetStart())
			if !s.GetStart().Before(ingesterWindowStart) {
				require.LessOrEqual(t, length, 15*time.Minute)
			} else if length >= 11*time.Hour {
				sawLongSplit = true
			}
		}
		require.True(t, sawLongSplit)
	})

	t.Run("returns enabled false when the tenant turns splitting off for a query that prefers data objects", func(t *testing.T) {
		limits := newSplitTestLimits("1")
		limits.splitDuration["1"] = 0
		splitter := newMetricQuerySplitter(cfg, limits, nil)

		splits, enabled := splitter.split(dataObjRoutingNow, tenants, newRangeRequest(streamFirstQuery, start, end))

		require.False(t, enabled)
		require.Empty(t, splits)
	})
}

func TestSplitByRange_policy(t *testing.T) {
	tenants := []string{"1"}
	limits := newSplitTestLimits("1")
	cfg := Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag, now: func() time.Time { return dataObjRoutingNow }}
	noop := queryrangebase.HandlerFunc(func(context.Context, queryrangebase.Request) (queryrangebase.Response, error) {
		return nil, nil
	})

	t.Run("returns the data-object policy for a query that prefers data objects", func(t *testing.T) {
		splitter := NewSplitByRangeMiddleware(log.NewNopLogger(), cfg, testEngineOpts, limits, false, nilShardingMetrics).Wrap(noop).(*splitByRange)

		got := splitter.policy(tenants, newInstantRequest(`sum(count_over_time({app="a"}[24h]))`, dataObjRoutingNow.Add(-6*time.Hour)))

		require.Same(t, splitter.dataObjPolicy, got)
	})

	t.Run("returns the chunk policy for a query that does not prefer data objects", func(t *testing.T) {
		splitter := NewSplitByRangeMiddleware(log.NewNopLogger(), cfg, testEngineOpts, limits, false, nilShardingMetrics).Wrap(noop).(*splitByRange)

		got := splitter.policy(tenants, newInstantRequest(`sum(count_over_time({app="a"}[1h]))`, dataObjRoutingNow))

		require.Equal(t, splitter.chunkPolicy, got)
	})

	t.Run("returns the chunk policy when the config turns data objects off", func(t *testing.T) {
		splitter := NewSplitByRangeMiddleware(log.NewNopLogger(), Config{}, testEngineOpts, limits, false, nilShardingMetrics).Wrap(noop).(*splitByRange)

		got := splitter.policy(tenants, newInstantRequest(`sum(count_over_time({app="a"}[24h]))`, dataObjRoutingNow.Add(-6*time.Hour)))

		require.Equal(t, splitter.chunkPolicy, got)
	})

	t.Run("returns the chunk policy for a request that is not an instant request", func(t *testing.T) {
		splitter := NewSplitByRangeMiddleware(log.NewNopLogger(), cfg, testEngineOpts, limits, false, nilShardingMetrics).Wrap(noop).(*splitByRange)

		got := splitter.policy(tenants, &LokiRequest{})

		require.Equal(t, splitter.chunkPolicy, got)
	})

	t.Run("returns the chunk policy for an instant request with a query that does not parse", func(t *testing.T) {
		splitter := NewSplitByRangeMiddleware(log.NewNopLogger(), cfg, testEngineOpts, limits, false, nilShardingMetrics).Wrap(noop).(*splitByRange)

		got := splitter.policy(tenants, &LokiInstantRequest{Query: `sum(`, TimeTs: dataObjRoutingNow.Add(-6 * time.Hour)})

		require.Equal(t, splitter.chunkPolicy, got)
	})
}
