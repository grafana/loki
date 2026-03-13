package physical

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
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
		require.Equal(t, TaskCacheTypeDataObjScan, cacheType)
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

	t.Run("PointersScan plan returns POINTERSSCAN cache type", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(&PointersScan{Location: "gs://bucket/meta"})
		plan := FromGraph(g)
		key, cacheType := TaskCacheKey(ctx, "t", plan)
		require.NotEmpty(t, key)
		require.Equal(t, TaskCacheTypePointersScan, cacheType)
	})
}

func TestWrapWithCacheIfSupported(t *testing.T) {
	t.Run("wraps plan with Cache as new root", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(&DataObjScan{Location: "loc1"})
		plan := FromGraph(g)

		node, ok, err := WrapWithCacheIfSupported(context.Background(), "tenant1", plan)
		require.NoError(t, err)
		require.True(t, ok)

		root, err := plan.Root()
		require.NoError(t, err)
		require.IsType(t, (*Cache)(nil), root)
		require.Equal(t, node, root)
	})
}
