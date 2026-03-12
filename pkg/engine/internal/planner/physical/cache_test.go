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

		key := TaskCacheKey(ctx, "tenant1", plan)
		require.NotEmpty(t, key)
		require.Contains(t, key, "tenant1")
	})

	t.Run("same plan produces identical key", func(t *testing.T) {
		build := func() *Plan {
			var g dag.Graph[Node]
			g.Add(scan())
			return FromGraph(g)
		}
		require.Equal(t, TaskCacheKey(ctx, "t", build()), TaskCacheKey(ctx, "t", build()))
	})

	t.Run("different tenant produces different key", func(t *testing.T) {
		var g1, g2 dag.Graph[Node]
		g1.Add(scan())
		g2.Add(scan())
		require.NotEqual(t, TaskCacheKey(ctx, "tenantA", FromGraph(g1)), TaskCacheKey(ctx, "tenantB", FromGraph(g2)))
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
		require.NotEmpty(t, TaskCacheKey(ctx, "t", plan))
	})

	t.Run("VectorAggregation without scan is not cacheable", func(t *testing.T) {
		// VectorAgg-only tasks read from streams, not directly from scan nodes.
		var g dag.Graph[Node]
		g.Add(&VectorAggregation{Operation: types.VectorAggregationTypeSum})
		plan := FromGraph(g)
		require.Empty(t, TaskCacheKey(ctx, "t", plan))
	})

	t.Run("plan with Merge but no scan is not cacheable", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(&Merge{})
		plan := FromGraph(g)
		require.Empty(t, TaskCacheKey(ctx, "t", plan))
	})

	t.Run("plan with Batching and DataObjScan is cacheable", func(t *testing.T) {
		var g dag.Graph[Node]
		s := g.Add(scan())
		b := g.Add(&Batching{BatchSize: 100})
		_ = g.AddEdge(dag.Edge[Node]{Parent: b, Child: s})
		plan := FromGraph(g)
		require.NotEmpty(t, TaskCacheKey(ctx, "t", plan))
	})

	t.Run("plan with ScanSet returns empty key (planning artifact)", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(&ScanSet{})
		plan := FromGraph(g)
		require.Empty(t, TaskCacheKey(ctx, "t", plan))
	})

	t.Run("plan with multiple roots returns empty key", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(scan())
		g.Add(scan())
		plan := FromGraph(g)
		require.Empty(t, TaskCacheKey(ctx, "t", plan))
	})
}

func TestWrapWithCache(t *testing.T) {
	t.Run("wraps plan with Cache as new root", func(t *testing.T) {
		var g dag.Graph[Node]
		g.Add(&DataObjScan{Location: "loc1"})
		plan := FromGraph(g)

		wrapped, err := WrapWithCache(plan, "mykey")
		require.NoError(t, err)

		root, err := wrapped.Root()
		require.NoError(t, err)
		require.IsType(t, (*Cache)(nil), root)
		require.Equal(t, "mykey", root.(*Cache).Key)
	})
}
