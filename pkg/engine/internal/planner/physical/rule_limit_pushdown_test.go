package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestLimitPushdown(t *testing.T) {
	t.Run("pushdown limit to target nodes", func(t *testing.T) {
		plan := &Plan{}
		{
			scanset := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
			})
			topK1 := plan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			topK2 := plan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			limit := plan.graph.Add(&Limit{Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scanset})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK2})
		}

		// apply optimisations
		optimizations := []*Optimization{
			newOptimization("limit pushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			scanset := expectedPlan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
			})
			topK1 := expectedPlan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin), K: 100})
			topK2 := expectedPlan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin), K: 100})
			limit := expectedPlan.graph.Add(&Limit{Fetch: 100})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK2})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scanset})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("pushdown blocked by filter nodes", func(t *testing.T) {
		// Limit should not be propagated to child nodes when there are filters
		filterPredicates := []Expression{
			&BinaryExpr{
				Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				Right: NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
		}

		plan := &Plan{}
		{
			scanset := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
			})
			topK1 := plan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			topK2 := plan.graph.Add(&TopK{SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			filter := plan.graph.Add(&Filter{
				Predicates: filterPredicates,
			})
			limit := plan.graph.Add(&Limit{Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: topK1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: topK2})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scanset})
		}
		orig := PrintAsTree(plan)

		// apply optimisations
		optimizations := []*Optimization{
			newOptimization("limit pushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		actual := PrintAsTree(plan)
		require.Equal(t, orig, actual)
	})
}
