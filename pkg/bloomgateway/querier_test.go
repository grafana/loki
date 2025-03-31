package bloomgateway

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

type noopClient struct {
	err       error // error to return
	callCount int
}

var _ Client = &noopClient{}

// FilterChunks implements Client.
func (c *noopClient) FilterChunks(_ context.Context, _ string, _ bloomshipper.Interval, blocks []blockWithSeries, _ plan.QueryPlan) (result []*logproto.GroupedChunkRefs, err error) {
	for _, block := range blocks {
		result = append(result, block.series...)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Fingerprint < result[j].Fingerprint
	})

	c.callCount++
	return result, c.err
}

func (c *noopClient) PrefetchBloomBlocks(_ context.Context, _ []bloomshipper.BlockRef) error {
	return nil
}

type mockBlockResolver struct{}

// Resolve implements BlockResolver.
func (*mockBlockResolver) Resolve(_ context.Context, tenant string, interval bloomshipper.Interval, series []*logproto.GroupedChunkRefs) ([]blockWithSeries, []*logproto.GroupedChunkRefs, error) {
	day := truncateDay(interval.Start)
	first, last := getFirstLast(series)
	block := bloomshipper.BlockRef{
		Ref: bloomshipper.Ref{
			TenantID:       tenant,
			TableName:      "table",
			Bounds:         v1.NewBounds(model.Fingerprint(first.Fingerprint), model.Fingerprint(last.Fingerprint)),
			StartTimestamp: day,
			EndTimestamp:   day.Add(Day),
			Checksum:       0,
		},
	}
	return []blockWithSeries{{block: block, series: series}}, nil, nil
}

var _ BlockResolver = &mockBlockResolver{}

func TestBloomQuerier(t *testing.T) {
	logger := log.NewNopLogger()
	limits := newLimits()
	cfg := QuerierConfig{}
	resolver := &mockBlockResolver{}
	tenant := "fake"

	t.Run("client not called when filters are empty", func(t *testing.T) {
		c := &noopClient{}
		bq := NewQuerier(c, cfg, limits, resolver, nil, logger)

		ctx := context.Background()
		through := model.Now()
		from := through.Add(-12 * time.Hour)
		chunkRefs := []*logproto.ChunkRef{
			{Fingerprint: 3000, UserID: tenant, Checksum: 1},
			{Fingerprint: 1000, UserID: tenant, Checksum: 2},
			{Fingerprint: 2000, UserID: tenant, Checksum: 3},
		}
		expr, err := syntax.ParseExpr(`{foo="bar"}`)
		require.NoError(t, err)
		res, _, err := bq.FilterChunkRefs(ctx, tenant, from, through, nil, chunkRefs, plan.QueryPlan{AST: expr})
		require.NoError(t, err)
		require.Equal(t, chunkRefs, res)
		require.Equal(t, 0, c.callCount)
	})

	t.Run("client not called when chunkRefs are empty", func(t *testing.T) {
		c := &noopClient{}
		bq := NewQuerier(c, cfg, limits, resolver, nil, logger)

		ctx := context.Background()
		through := model.Now()
		from := through.Add(-12 * time.Hour)
		chunkRefs := []*logproto.ChunkRef{}
		expr, err := syntax.ParseExpr(`{foo="bar"} | trace_id="exists"`)
		require.NoError(t, err)
		res, _, err := bq.FilterChunkRefs(ctx, tenant, from, through, nil, chunkRefs, plan.QueryPlan{AST: expr})
		require.NoError(t, err)
		require.Equal(t, chunkRefs, res)
		require.Equal(t, 0, c.callCount)
	})

	t.Run("querier propagates error from client", func(t *testing.T) {
		c := &noopClient{err: errors.New("something went wrong")}
		bq := NewQuerier(c, cfg, limits, resolver, nil, logger)

		ctx := context.Background()
		through := model.Now()
		from := through.Add(-12 * time.Hour)
		chunkRefs := []*logproto.ChunkRef{
			{Fingerprint: 3000, UserID: tenant, From: from, Through: through, Checksum: 1},
			{Fingerprint: 1000, UserID: tenant, From: from, Through: through, Checksum: 2},
			{Fingerprint: 2000, UserID: tenant, From: from, Through: through, Checksum: 3},
		}
		expr, err := syntax.ParseExpr(`{foo="bar"} | trace_id="exists"`)
		require.NoError(t, err)
		res, _, err := bq.FilterChunkRefs(ctx, tenant, from, through, nil, chunkRefs, plan.QueryPlan{AST: expr})
		require.Error(t, err)
		require.Nil(t, res)
	})

	t.Run("client called once for each day of the interval", func(t *testing.T) {
		c := &noopClient{}
		bq := NewQuerier(c, cfg, limits, resolver, nil, logger)

		ctx := context.Background()
		from := mktime("2024-04-16 22:00")
		through := mktime("2024-04-17 02:00")
		chunkRefs := []*logproto.ChunkRef{
			{Fingerprint: 1000, UserID: tenant, From: mktime("2024-04-16 22:30"), Through: mktime("2024-04-16 23:30"), Checksum: 1}, // day 1
			{Fingerprint: 2000, UserID: tenant, From: mktime("2024-04-16 23:30"), Through: mktime("2024-04-17 00:30"), Checksum: 2}, // day 1
			{Fingerprint: 3000, UserID: tenant, From: mktime("2024-04-17 00:30"), Through: mktime("2024-04-17 01:30"), Checksum: 3}, // day 2
		}
		expr, err := syntax.ParseExpr(`{foo="bar"} | trace_id="exists"`)
		require.NoError(t, err)
		res, _, err := bq.FilterChunkRefs(ctx, tenant, from, through, nil, chunkRefs, plan.QueryPlan{AST: expr})
		require.NoError(t, err)
		require.Equal(t, chunkRefs, res)
		require.Equal(t, 2, c.callCount)
	})

}

func TestGroupChunkRefs(t *testing.T) {
	series := []labels.Labels{
		labels.FromStrings("app", "1"),
		labels.FromStrings("app", "2"),
		labels.FromStrings("app", "3"),
	}
	seriesMap := make(map[uint64]labels.Labels)
	for _, s := range series {
		seriesMap[s.Hash()] = s
	}

	chunkRefs := []*logproto.ChunkRef{
		{Fingerprint: series[0].Hash(), UserID: "tenant", From: mktime("2024-04-20 00:00"), Through: mktime("2024-04-20 00:59")},
		{Fingerprint: series[0].Hash(), UserID: "tenant", From: mktime("2024-04-20 01:00"), Through: mktime("2024-04-20 01:59")},
		{Fingerprint: series[1].Hash(), UserID: "tenant", From: mktime("2024-04-20 00:00"), Through: mktime("2024-04-20 00:59")},
		{Fingerprint: series[1].Hash(), UserID: "tenant", From: mktime("2024-04-20 01:00"), Through: mktime("2024-04-20 01:59")},
		{Fingerprint: series[2].Hash(), UserID: "tenant", From: mktime("2024-04-20 00:00"), Through: mktime("2024-04-20 00:59")},
		{Fingerprint: series[2].Hash(), UserID: "tenant", From: mktime("2024-04-20 01:00"), Through: mktime("2024-04-20 01:59")},
	}

	result := groupChunkRefs(seriesMap, chunkRefs, nil)
	require.Equal(t, []*logproto.GroupedChunkRefs{
		{Fingerprint: series[0].Hash(), Tenant: "tenant", Refs: []*logproto.ShortRef{
			{From: mktime("2024-04-20 00:00"), Through: mktime("2024-04-20 00:59")},
			{From: mktime("2024-04-20 01:00"), Through: mktime("2024-04-20 01:59")},
		}, Labels: &logproto.IndexSeries{
			Labels: logproto.FromLabelsToLabelAdapters(series[0]),
		}},
		{Fingerprint: series[1].Hash(), Tenant: "tenant", Refs: []*logproto.ShortRef{
			{From: mktime("2024-04-20 00:00"), Through: mktime("2024-04-20 00:59")},
			{From: mktime("2024-04-20 01:00"), Through: mktime("2024-04-20 01:59")},
		}, Labels: &logproto.IndexSeries{
			Labels: logproto.FromLabelsToLabelAdapters(series[1]),
		}},
		{Fingerprint: series[2].Hash(), Tenant: "tenant", Refs: []*logproto.ShortRef{
			{From: mktime("2024-04-20 00:00"), Through: mktime("2024-04-20 00:59")},
			{From: mktime("2024-04-20 01:00"), Through: mktime("2024-04-20 01:59")},
		}, Labels: &logproto.IndexSeries{
			Labels: logproto.FromLabelsToLabelAdapters(series[2]),
		}},
	}, result)
}

func BenchmarkGroupChunkRefs(b *testing.B) {
	b.StopTimer()

	n := 1000  // num series
	m := 10000 // num chunks per series
	chunkRefs := make([]*logproto.ChunkRef, 0, n*m)
	series := make(map[uint64]labels.Labels, n)

	for i := 0; i < n; i++ {
		s := labels.FromStrings("app", fmt.Sprintf("%d", i))
		sFP := s.Hash()
		series[sFP] = s
		for j := 0; j < m; j++ {
			chunkRefs = append(chunkRefs, &logproto.ChunkRef{
				Fingerprint: sFP,
				UserID:      "tenant",
				From:        mktime("2024-04-20 00:00"),
				Through:     mktime("2024-04-20 00:59"),
				Checksum:    uint32((i << 20) + j),
			})
		}
	}

	rand.Shuffle(n*m, func(i, j int) {
		chunkRefs[i], chunkRefs[j] = chunkRefs[j], chunkRefs[i]
	})

	b.ReportAllocs()
	b.StartTimer()

	groups := make([]*logproto.GroupedChunkRefs, 0, n)
	groupChunkRefs(series, chunkRefs, groups)
}
