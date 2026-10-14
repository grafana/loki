package queryrange

import (
	"testing"
	"time"

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
	)

	t.Run("cuts a query at the query split duration of the tenant", func(t *testing.T) {
		splits, enabled := newMetricQuerySplitter(limits, nil).split(execTime, tenants, newRangeRequest(query, start, end))

		require.True(t, enabled)
		require.Len(t, splits, 43)
	})

	t.Run("widens the interval to a long range vector and cuts at multiples of the widened interval", func(t *testing.T) {
		splitter := newMetricQuerySplitter(limits, nil)

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
		splitter := newMetricQuerySplitter(fakeLimits{splitDuration: map[string]time.Duration{"1": 0}}, nil)

		splits, enabled := splitter.split(execTime, tenants, newRangeRequest(query, start, end))

		require.False(t, enabled)
		require.Empty(t, splits)
	})

	t.Run("returns enabled false for a request without a plan when the tenant turns splitting off", func(t *testing.T) {
		splitter := newMetricQuerySplitter(fakeLimits{splitDuration: map[string]time.Duration{"1": 0}}, nil)
		req := newRangeRequest(query, start, end)
		req.Plan = nil

		splits, enabled := splitter.split(execTime, tenants, req)

		require.False(t, enabled)
		require.Empty(t, splits)
	})
}
