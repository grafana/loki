package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestGroupByPushdown(t *testing.T) {
	t.Run("pushdown to RangeAggregation", func(t *testing.T) {
		grouping := Grouping{
			Columns: []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
			},
			Without: false,
		}

		// generate plan for sum by(service, instance) (count_over_time{...}[])
		plan := &Plan{}
		{
			scanSet := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},

				Predicates: []Expression{},
			})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
			})
			vectorAgg := plan.graph.Add(&VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
				Grouping:  grouping,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scanSet})
		}

		// apply optimisation
		optimizations := []*Optimization{
			newOptimization("groupBy pushdown", plan).withRules(
				&groupByPushdown{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			scanSet := expectedPlan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				Predicates: []Expression{},
			})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
				Grouping:  grouping,
			})
			vectorAgg := expectedPlan.graph.Add(&VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
				Grouping:  grouping,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scanSet})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("MAX->SUM is not allowed", func(t *testing.T) {
		grouping := Grouping{
			Columns: []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
			},
			Without: false,
		}

		// generate plan for max by(service) (sum_over_time{...}[])
		plan := &Plan{}
		{
			scanSet := plan.graph.Add(&ScanSet{
				Targets: []*ScanTarget{
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
					{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				},
				Predicates: []Expression{},
			})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
			})
			vectorAgg := plan.graph.Add(&VectorAggregation{
				Operation: types.VectorAggregationTypeMax,
				Grouping:  grouping,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scanSet})
		}

		orig := PrintAsTree(plan)

		// apply optimisation
		optimizations := []*Optimization{
			newOptimization("projection pushdown", plan).withRules(
				&groupByPushdown{plan: plan},
			),
		}
		o := NewOptimizer(plan, optimizations)
		o.Optimize(plan.Roots()[0])

		actual := PrintAsTree(plan)
		require.Equal(t, orig, actual)
	})
}
