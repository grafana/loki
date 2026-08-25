package physical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestAggregationSplit(t *testing.T) {
	newOptimizer := func(plan *Plan) *Optimizer {
		return NewOptimizer(plan, []*Optimization{
			newOptimization("AggregationSplit", plan).withRules(&aggregationSplit{plan: plan}),
		})
	}

	t.Run("splits max", func(t *testing.T) {
		var plan Plan
		{
			rangeAgg := plan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeMax})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
		}

		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)

		var expectedPlan Plan
		{
			outerMax := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeMax})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			innerMax := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeMax})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: outerMax, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: innerMax}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: innerMax, Child: scan}))
		}

		require.Equal(t, PrintAsTree(&expectedPlan), PrintAsTree(&plan))
	})

	t.Run("splits min", func(t *testing.T) {
		var plan Plan
		{
			rangeAgg := plan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeMin})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
		}

		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)

		var expectedPlan Plan
		{
			outerMin := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeMin})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			innerMin := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeMin})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: outerMin, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: innerMin}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: innerMin, Child: scan}))
		}

		require.Equal(t, PrintAsTree(&expectedPlan), PrintAsTree(&plan))
	})

	t.Run("splits sum", func(t *testing.T) {
		var plan Plan
		{
			rangeAgg := plan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeSum})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
		}

		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)

		var expectedPlan Plan
		{
			outerSum := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeSum})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			innerSum := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeSum})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: outerSum, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: innerSum}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: innerSum, Child: scan}))
		}

		require.Equal(t, PrintAsTree(&expectedPlan), PrintAsTree(&plan))
	})

	t.Run("splits count into sum over count", func(t *testing.T) {
		var plan Plan
		{
			rangeAgg := plan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeCount})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
		}

		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)

		var expectedPlan Plan
		{
			outerSum := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeSum})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			innerCount := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeCount})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: outerSum, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: innerCount}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: innerCount, Child: scan}))
		}

		require.Equal(t, PrintAsTree(&expectedPlan), PrintAsTree(&plan))
	})

	t.Run("does not split unsupported operations", func(t *testing.T) {
		for _, op := range []types.RangeAggregationType{
			types.RangeAggregationTypeAvg,
			types.RangeAggregationTypeBytes,
		} {
			var plan Plan
			{
				rangeAgg := plan.graph.Add(&RangeAggregation{Operation: op})
				scan := plan.graph.Add(&DataObjScan{})
				require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
			}

			before := PrintAsTree(&plan)
			root, _ := plan.graph.Root()
			newOptimizer(&plan).Optimize(root)

			require.Equal(t, before, PrintAsTree(&plan), "operation %s should not be split", op)
		}
	})

	t.Run("does not split node when Parallelize is already parent", func(t *testing.T) {
		// ParallelPushdown has already pushed the RangeAgg inside a Parallelize.
		// Input: Parallelize -> RangeAgg -> Scan
		var plan Plan
		{
			parallelize := plan.graph.Add(&Parallelize{})
			agg := plan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeSum})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: agg}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: agg, Child: scan}))
		}
		before := PrintAsTree(&plan)
		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)
		require.Equal(t, before, PrintAsTree(&plan))
	})

	t.Run("does not split when step < range with without grouping (overlapping windows)", func(t *testing.T) {
		var plan Plan
		{
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      30 * time.Second,
				Range:     time.Minute,
				Grouping:  Grouping{Without: true},
			})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
		}

		before := PrintAsTree(&plan)
		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)

		require.Equal(t, before, PrintAsTree(&plan))
	})

	t.Run("does not split when step < range with by grouping having 5+ labels (overlapping windows)", func(t *testing.T) {
		var plan Plan
		{
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      30 * time.Second,
				Range:     time.Minute,
				Grouping: Grouping{Columns: []ColumnExpression{
					newColumnExpr("a", types.ColumnTypeLabel),
					newColumnExpr("b", types.ColumnTypeLabel),
					newColumnExpr("c", types.ColumnTypeLabel),
					newColumnExpr("d", types.ColumnTypeLabel),
					newColumnExpr("e", types.ColumnTypeLabel),
				}},
			})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
		}

		before := PrintAsTree(&plan)
		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)

		require.Equal(t, before, PrintAsTree(&plan))
	})

	t.Run("splits when step < range with by grouping (overlapping windows)", func(t *testing.T) {
		var plan Plan
		{
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      30 * time.Second,
				Range:     time.Minute,
			})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
		}

		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)

		var expectedPlan Plan
		{
			// Outer: Range is set to Step to capture exactly one inner result point.
			outerSum := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      30 * time.Second,
				Range:     30 * time.Second, // outer.Range = Step = 30s
			})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			// Inner: clone of original, Range unchanged.
			innerSum := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      30 * time.Second,
				Range:     time.Minute,
			})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: outerSum, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: innerSum}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: innerSum, Child: scan}))
		}

		require.Equal(t, PrintAsTree(&expectedPlan), PrintAsTree(&plan))
	})

	t.Run("splits when step == range (aligned windows)", func(t *testing.T) {
		var plan Plan
		{
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      time.Minute,
				Range:     time.Minute,
			})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
		}

		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)

		var expectedPlan Plan
		{
			// Outer: Range is set to Step (== Range here, so unchanged).
			outerSum := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      time.Minute,
				Range:     time.Minute, // outer.Range = Step = 1m
			})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			// Inner: clone of original, Range unchanged.
			innerSum := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      time.Minute,
				Range:     time.Minute,
			})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: outerSum, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: innerSum}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: innerSum, Child: scan}))
		}

		require.Equal(t, PrintAsTree(&expectedPlan), PrintAsTree(&plan))
	})

	t.Run("splits when step > range (gapped windows)", func(t *testing.T) {
		var plan Plan
		{
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      2 * time.Minute,
				Range:     time.Minute,
			})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
		}

		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)

		var expectedPlan Plan
		{
			// Outer: Range is widened to Step.
			outerSum := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      2 * time.Minute,
				Range:     2 * time.Minute, // outer.Range = Step = 2m
			})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			// Inner: clone of original, Range unchanged.
			innerSum := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Step:      2 * time.Minute,
				Range:     time.Minute,
			})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: outerSum, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: innerSum}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: innerSum, Child: scan}))
		}

		require.Equal(t, PrintAsTree(&expectedPlan), PrintAsTree(&plan))
	})

	t.Run("splits when Parallelize is already a direct child", func(t *testing.T) {
		// After ParallelPushdown shifts intermediate nodes (e.g. Compat) below
		// Parallelize, Parallelize may end up as a direct child of the RangeAgg
		// when ParallelPushdown cannot shift the RangeAgg itself.
		// AggregationSplit should reuse that existing Parallelize.
		// Input: RangeAgg(sum) -> Parallelize -> Scan  (simulates post-ParallelPushdown state)
		var plan Plan
		{
			agg := plan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeSum})
			parallelize := plan.graph.Add(&Parallelize{})
			scan := plan.graph.Add(&DataObjScan{})
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: agg, Child: parallelize}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: scan}))
		}

		root, _ := plan.graph.Root()
		newOptimizer(&plan).Optimize(root)

		var expectedPlan Plan
		{
			outerSum := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeSum})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			innerSum := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeSum})
			scan := expectedPlan.graph.Add(&DataObjScan{})
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: outerSum, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: innerSum}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: innerSum, Child: scan}))
		}

		require.Equal(t, PrintAsTree(&expectedPlan), PrintAsTree(&plan))
	})
}
