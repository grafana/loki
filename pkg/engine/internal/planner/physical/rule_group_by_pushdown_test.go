package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestGroupByPushdown(t *testing.T) {
	type testCase struct {
		name              string
		rangeAggOperation types.RangeAggregationType
		vectorAgg         *VectorAggregation
		expectedRangeAgg  *RangeAggregation
	}
	testCases := []testCase{
		{
			name:              "pushdown to RangeAggregation with empty group by",
			rangeAggOperation: types.RangeAggregationTypeCount,
			vectorAgg: &VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
				Grouping: Grouping{
					Columns: []ColumnExpression{
						&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
						&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
					},
				},
			},
			expectedRangeAgg: &RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
				Grouping: Grouping{
					Columns: []ColumnExpression{
						&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
						&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
					},
					Without: false,
				},
			},
		},
		{
			name:              "pushdown to RangeAggregation with empty group by",
			rangeAggOperation: types.RangeAggregationTypeCount,
			vectorAgg: &VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
				Grouping: Grouping{
					Columns: []ColumnExpression{},
					Without: false,
				},
			},
			expectedRangeAgg: &RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
				Grouping: Grouping{
					Columns: []ColumnExpression{},
					Without: false,
				},
			},
		},
		{
			name:              "pushdown to RangeAggregation as 'without'",
			rangeAggOperation: types.RangeAggregationTypeCount,
			vectorAgg: &VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
				Grouping: Grouping{
					Columns: []ColumnExpression{
						&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
					},
					Without: true,
				},
			},
			expectedRangeAgg: &RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
			},
		},
		{
			name:              "MAX->SUM is not allowed",
			rangeAggOperation: types.RangeAggregationTypeSum,
			vectorAgg: &VectorAggregation{
				Operation: types.VectorAggregationTypeMax,
				Grouping: Grouping{
					Columns: []ColumnExpression{
						&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
					},
				},
			},
			expectedRangeAgg: &RangeAggregation{
				Operation: types.RangeAggregationTypeSum,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
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
					Operation: testCase.rangeAggOperation,
				})
				vectorAgg := plan.graph.Add(testCase.vectorAgg)

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
				rangeAgg := expectedPlan.graph.Add(testCase.expectedRangeAgg)
				vectorAgg := expectedPlan.graph.Add(testCase.vectorAgg)

				_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
				_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scanSet})
			}

			actual := PrintAsTree(plan)
			expected := PrintAsTree(expectedPlan)
			require.Equal(t, expected, actual)
		})
	}
}
