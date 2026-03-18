package physical

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestTaskCacheKey(t *testing.T) {
	ctx := context.Background()

	scan := func() *DataObjScan {
		return &DataObjScan{
			Location:     "gs://bucket/obj.parquet",
			Section:      1,
			StreamIDs:    []int64{10, 20},
			MaxTimeRange: TimeRange{Start: time.Unix(0, 0).UTC(), End: time.Unix(100, 0).UTC()},
		}
	}

	t.Run("single DataObjScan is cacheable", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(scan())
		plan := FromGraph(g)

		key, cacheType := TaskCacheKey(ctx, "tenant1", plan)
		require.NotEmpty(t, key)
		require.Contains(t, key, "tenant1")
		require.Equal(t, TaskCacheLogsScan, cacheType)
	})

	t.Run("same plan produces identical key", func(t *testing.T) {
		build := func() *Plan {
			var g dag.Graph[Node]
			g.Add(scan())
			return FromGraph(g)
		}
		key1, _ := TaskCacheKey(ctx, "t", build())
		key2, _ := TaskCacheKey(ctx, "t", build())
		require.Equal(t, key1, key2)
	})

	t.Run("different tenant produces different key", func(t *testing.T) {
		var g1, g2 dag.Graph[Node]
		g1.Add(scan())
		g2.Add(scan())
		keyA, _ := TaskCacheKey(ctx, "tenantA", FromGraph(g1))
		keyB, _ := TaskCacheKey(ctx, "tenantB", FromGraph(g2))
		require.NotEqual(t, keyA, keyB)
	})

	t.Run("RangeAggregation over DataObjScan is cacheable", func(t *testing.T) {
		var g dag.Graph[Node]
		s := g.Add(scan())
		agg := g.Add(&RangeAggregation{
			Operation: types.RangeAggregationTypeCount,
			Start:     time.Unix(0, 0).UTC(),
			End:       time.Unix(100, 0).UTC(),
		})
		_ = g.AddEdge(dag.Edge[Node]{Parent: agg, Child: s})
		plan := FromGraph(g)
		key, _ := TaskCacheKey(ctx, "t", plan)
		require.NotEmpty(t, key)
	})

	t.Run("VectorAggregation without scan is not cacheable", func(t *testing.T) {
		// VectorAgg-only tasks read from streams, not directly from scan nodes.
		var g dag.Graph[Node]
		g.Add(&VectorAggregation{Operation: types.VectorAggregationTypeSum})
		plan := FromGraph(g)
		key, _ := TaskCacheKey(ctx, "t", plan)
		require.Empty(t, key)
	})

	t.Run("plan with Merge but no scan is not cacheable", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(&Merge{})
		plan := FromGraph(g)
		key, _ := TaskCacheKey(ctx, "t", plan)
		require.Empty(t, key)
	})

	t.Run("plan with Batching and DataObjScan is cacheable", func(t *testing.T) {
		var g dag.Graph[Node]
		s := g.Add(scan())
		b := g.Add(&Batching{BatchSize: 100})
		_ = g.AddEdge(dag.Edge[Node]{Parent: b, Child: s})
		plan := FromGraph(g)
		key, _ := TaskCacheKey(ctx, "t", plan)
		require.NotEmpty(t, key)
	})

	t.Run("plan with ScanSet returns empty key (planning artifact)", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(&ScanSet{})
		plan := FromGraph(g)
		key, _ := TaskCacheKey(ctx, "t", plan)
		require.Empty(t, key)
	})

	t.Run("plan with multiple roots returns empty key", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(scan())
		g.Add(scan())
		plan := FromGraph(g)
		key, _ := TaskCacheKey(ctx, "t", plan)
		require.Empty(t, key)
	})

	t.Run("DataObjScan + RangeAggregation returns TaskCacheLogsScanRangeAggr", func(t *testing.T) {
		var g dag.Graph[Node]
		s := g.Add(scan())
		agg := g.Add(&RangeAggregation{
			Operation: types.RangeAggregationTypeCount,
			Start:     time.Unix(0, 0).UTC(),
			End:       time.Unix(100, 0).UTC(),
		})
		_ = g.AddEdge(dag.Edge[Node]{Parent: agg, Child: s})
		plan := FromGraph(g)
		key, cacheType := TaskCacheKey(ctx, "t", plan)
		require.NotEmpty(t, key)
		require.Equal(t, TaskCacheLogsScanRangeAggr, cacheType)
	})

	t.Run("PointersScan plan returns POINTERSSCAN cache type", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(&PointersScan{Location: "gs://bucket/meta"})
		plan := FromGraph(g)
		key, cacheType := TaskCacheKey(ctx, "t", plan)
		require.NotEmpty(t, key)
		require.Equal(t, TaskCacheMetastore, cacheType)
	})
}

func TestDataObjScanCacheKeyClampsTimestampPredicates(t *testing.T) {
	ctx := t.Context()
	start := time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC)
	end := time.Date(2026, 3, 14, 16, 48, 0, 0, time.UTC)
	early := types.Timestamp(time.Date(2026, 3, 11, 12, 41, 44, 719000000, time.UTC).UnixNano())
	late := types.Timestamp(time.Date(2026, 3, 18, 9, 41, 44, 976217699, time.UTC).UnixNano())
	col := newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin)

	scan := &DataObjScan{
		Location:  "objects/fc/obj",
		Section:   57,
		StreamIDs: []int64{155, 377},
		Predicates: []Expression{
			&BinaryExpr{Left: col, Right: NewLiteral(early), Op: types.BinaryOpGte},
			&BinaryExpr{Left: col, Right: NewLiteral(late), Op: types.BinaryOpLt},
		},
		MaxTimeRange: TimeRange{Start: start, End: end},
	}
	key := scan.CacheKey(ctx)
	require.Contains(t, key, util.FormatTimeRFC3339Nano(start))
	require.Contains(t, key, util.FormatTimeRFC3339Nano(end))
	require.NotContains(t, key, util.FormatTimeRFC3339Nano(time.Unix(0, int64(early))))
	require.NotContains(t, key, util.FormatTimeRFC3339Nano(time.Unix(0, int64(late))))
}

func TestWrapWithCacheIfSupported(t *testing.T) {
	t.Run("wraps plan with Cache as new root", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(&DataObjScan{Location: "loc1"})
		plan := FromGraph(g)

		node, ok, err := WrapWithCacheIfSupported(context.Background(), "tenant1", plan, 0)
		require.NoError(t, err)
		require.True(t, ok)

		root, err := plan.Root()
		require.NoError(t, err)
		require.IsType(t, (*Cache)(nil), root)
		require.Equal(t, node, root)
	})
}
