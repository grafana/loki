package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestParallelPushdown(t *testing.T) {
	t.Run("canPushdown", func(t *testing.T) {
		tt := []struct {
			name     string
			children []Node
			expected bool
		}{
			{
				name:     "no children",
				children: nil,
				expected: false,
			},
			{
				name:     "one child (not Parallelize)",
				children: []Node{&DataObjScan{}},
				expected: false,
			},
			{
				name:     "one child (Parallelize)",
				children: []Node{&Parallelize{}},
				expected: true,
			},
			{
				name:     "multiple children (all Parallelize)",
				children: []Node{&Parallelize{}, &Parallelize{}},
				expected: true,
			},
			{
				name:     "multiple children (not all Parallelize)",
				children: []Node{&Parallelize{}, &DataObjScan{}},
				expected: false,
			},
		}

		for _, tc := range tt {
			t.Run(tc.name, func(t *testing.T) {
				var plan Plan
				parent := plan.graph.Add(&Filter{})

				for _, child := range tc.children {
					plan.graph.Add(child)
					require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parent, Child: child}))
				}

				pass := parallelPushdown{plan: &plan}
				require.Equal(t, tc.expected, pass.canPushdown(parent))
			})
		}
	})

	t.Run("Shifts Filter", func(t *testing.T) {
		var plan Plan
		{
			vectorAgg := plan.graph.Add(&VectorAggregation{})
			rangeAgg := plan.graph.Add(&RangeAggregation{})
			filter := plan.graph.Add(&Filter{})
			parallelize := plan.graph.Add(&Parallelize{})
			scan := plan.graph.Add(&DataObjScan{})

			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: filter}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: parallelize}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: scan}))
		}

		opt := NewOptimizer(&plan, []*Optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.graph.Root()
		opt.Optimize(root)

		var expectedPlan Plan
		{
			vectorAgg := expectedPlan.graph.Add(&VectorAggregation{})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			filter := expectedPlan.graph.Add(&Filter{})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: filter}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})

	t.Run("Splits TopK", func(t *testing.T) {
		var plan Plan
		{
			limit := plan.graph.Add(&Limit{})
			topk := plan.graph.Add(&TopK{SortBy: &ColumnExpr{}})
			parallelize := plan.graph.Add(&Parallelize{})
			scan := plan.graph.Add(&DataObjScan{})

			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topk}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: topk, Child: parallelize}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: scan}))
		}

		opt := NewOptimizer(&plan, []*Optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.graph.Root()

		// Since [optimization.optimize] does up to three passes,
		// parallelPushdown must ignore a node after it's already been
		// processed. Otherwise, it will cause TopK to be sharded three times,
		// ending up with this plan:
		//
		//   TopK
		//     Parallelize
		//       TopK # Shard from first iteration
		//         TopK # Shard from second iteration
		//           TopK # Shard from third iteration
		//             DataObjScan
		opt.Optimize(root)

		var expectedPlan Plan
		{
			limit := expectedPlan.graph.Add(&Limit{})
			globalTopK := expectedPlan.graph.Add(&TopK{SortBy: &ColumnExpr{}})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			localTopK := expectedPlan.graph.Add(&TopK{SortBy: &ColumnExpr{}})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: globalTopK}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: globalTopK, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: localTopK}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: localTopK, Child: scan}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})

	t.Run("Shifts RangeAggregation with valid VectorAggregation parent", func(t *testing.T) {
		// Input plan: VectorAgg -> RangeAgg -> Parallelize -> Scan
		// Expected output: VectorAgg -> Parallelize -> RangeAgg -> Scan
		var plan Plan
		{
			vecAgg := plan.graph.Add(&VectorAggregation{Operation: types.VectorAggregationTypeSum})
			rangeAgg := plan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeSum})
			parallelize := plan.graph.Add(&Parallelize{})
			scan := plan.graph.Add(&DataObjScan{})

			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: vecAgg, Child: rangeAgg}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: parallelize}))
			require.NoError(t, plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: scan}))
		}

		opt := NewOptimizer(&plan, []*Optimization{
			newOptimization("ParallelPushdown", &plan).withRules(&parallelPushdown{plan: &plan}),
		})
		root, _ := plan.graph.Root()
		opt.Optimize(root)

		var expectedPlan Plan
		{
			vecAgg := expectedPlan.graph.Add(&VectorAggregation{Operation: types.VectorAggregationTypeSum})
			parallelize := expectedPlan.graph.Add(&Parallelize{})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{Operation: types.RangeAggregationTypeSum})
			scan := expectedPlan.graph.Add(&DataObjScan{})

			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: vecAgg, Child: parallelize}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: rangeAgg}))
			require.NoError(t, expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan}))
		}

		expected := PrintAsTree(&expectedPlan)
		require.Equal(t, expected, PrintAsTree(&plan))
	})
}

// TestParallelPushdown_canShardAggregation tests the canShardAggregation function directly
// to verify which vector/range aggregation combinations are safe to parallelize.
func TestParallelPushdown_canShardAggregation(t *testing.T) {
	t.Run("operation combinations", func(t *testing.T) {
		tests := []struct {
			name     string
			vecOp    types.VectorAggregationType
			rangeOp  types.RangeAggregationType
			expected bool
		}{
			// Valid distributive combinations
			{"sum over sum", types.VectorAggregationTypeSum, types.RangeAggregationTypeSum, true},
			{"sum over count", types.VectorAggregationTypeSum, types.RangeAggregationTypeCount, true},
			{"max over max", types.VectorAggregationTypeMax, types.RangeAggregationTypeMax, true},
			{"min over min", types.VectorAggregationTypeMin, types.RangeAggregationTypeMin, true},

			// Invalid combinations - operation mismatch
			{"sum over max", types.VectorAggregationTypeSum, types.RangeAggregationTypeMax, false},
			{"sum over min", types.VectorAggregationTypeSum, types.RangeAggregationTypeMin, false},
			{"max over sum", types.VectorAggregationTypeMax, types.RangeAggregationTypeSum, false},
			{"max over min", types.VectorAggregationTypeMax, types.RangeAggregationTypeMin, false},
			{"min over sum", types.VectorAggregationTypeMin, types.RangeAggregationTypeSum, false},
			{"min over max", types.VectorAggregationTypeMin, types.RangeAggregationTypeMax, false},

			// Non-distributive vector aggregations
			{"avg over sum", types.VectorAggregationTypeAvg, types.RangeAggregationTypeSum, false},
			{"avg over count", types.VectorAggregationTypeAvg, types.RangeAggregationTypeCount, false},
			{"count over count", types.VectorAggregationTypeCount, types.RangeAggregationTypeCount, false},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				vec := &VectorAggregation{Operation: tc.vecOp}
				rng := &RangeAggregation{Operation: tc.rangeOp}
				require.Equal(t, tc.expected, canShardAggregation(vec, rng))
			})
		}
	})

	t.Run("grouping compatibility", func(t *testing.T) {
		t.Run("rejects without grouping", func(t *testing.T) {
			vec := &VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
				Grouping:  Grouping{Without: true},
			}
			rng := &RangeAggregation{Operation: types.RangeAggregationTypeSum}
			require.False(t, canShardAggregation(vec, rng))
		})

		t.Run("rejects mismatched grouping columns", func(t *testing.T) {
			vec := &VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
				Grouping: Grouping{
					Columns: []ColumnExpression{&ColumnExpr{Ref: types.ColumnRef{Column: "foo"}}},
				},
			}
			rng := &RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Grouping: Grouping{
					Columns: []ColumnExpression{&ColumnExpr{Ref: types.ColumnRef{Column: "bar"}}},
				},
			}
			require.False(t, canShardAggregation(vec, rng))
		})

		t.Run("accepts matching grouping columns", func(t *testing.T) {
			vec := &VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
				Grouping: Grouping{
					Columns: []ColumnExpression{&ColumnExpr{Ref: types.ColumnRef{Column: "foo"}}},
				},
			}
			rng := &RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
				Grouping: Grouping{
					Columns: []ColumnExpression{&ColumnExpr{Ref: types.ColumnRef{Column: "foo"}}},
				},
			}
			require.True(t, canShardAggregation(vec, rng))
		})
	})
}
