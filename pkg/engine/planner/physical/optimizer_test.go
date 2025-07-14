package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestCanApplyPredicate(t *testing.T) {
	tests := []struct {
		predicate Expression
		want      bool
	}{
		{
			predicate: NewLiteral(int64(123)),
			want:      true,
		},
		{
			predicate: newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			want:      true,
		},
		{
			predicate: newColumnExpr("foo", types.ColumnTypeLabel),
			want:      false,
		},
		{
			predicate: &BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(datatype.Timestamp(3600000)),
				Op:    types.BinaryOpGt,
			},
			want: true,
		},
		{
			predicate: &BinaryExpr{
				Left:  newColumnExpr("foo", types.ColumnTypeLabel),
				Right: NewLiteral("bar"),
				Op:    types.BinaryOpEq,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.predicate.String(), func(t *testing.T) {
			got := canApplyPredicate(tt.predicate)
			require.Equal(t, tt.want, got)
		})
	}
}

var (
	time1000 = datatype.Timestamp(1000000000)
	time2000 = datatype.Timestamp(2000000000)
)

func dummyPlan() *Plan {
	plan := &Plan{}
	scan1 := plan.addNode(&DataObjScan{id: "scan1"})
	scan2 := plan.addNode(&DataObjScan{id: "scan2"})
	merge := plan.addNode(&SortMerge{id: "merge"})
	filter1 := plan.addNode(&Filter{id: "filter1", Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time1000),
			Op:    types.BinaryOpGt,
		},
	}})
	filter2 := plan.addNode(&Filter{id: "filter2", Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time2000),
			Op:    types.BinaryOpLte,
		},
	}})
	filter3 := plan.addNode(&Filter{id: "filter3", Predicates: []Expression{}})

	_ = plan.addEdge(Edge{Parent: filter3, Child: filter2})
	_ = plan.addEdge(Edge{Parent: filter2, Child: filter1})
	_ = plan.addEdge(Edge{Parent: filter1, Child: merge})
	_ = plan.addEdge(Edge{Parent: merge, Child: scan1})
	_ = plan.addEdge(Edge{Parent: merge, Child: scan2})

	return plan
}

func TestOptimizer(t *testing.T) {
	t.Run("noop", func(t *testing.T) {
		plan := dummyPlan()
		optimizations := []*optimization{
			newOptimization("noop", plan),
		}

		original := PrintAsTree(plan)
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		optimized := PrintAsTree(plan)
		require.Equal(t, original, optimized)
	})

	t.Run("filter predicate pushdown", func(t *testing.T) {
		plan := dummyPlan()
		optimizations := []*optimization{
			newOptimization("predicate pushdown", plan).withRules(
				&predicatePushdown{plan},
			),
		}

		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])
		actual := PrintAsTree(plan)

		optimized := &Plan{}
		scan1 := optimized.addNode(&DataObjScan{id: "scan1", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time2000),
				Op:    types.BinaryOpLte,
			},
		}})
		scan2 := optimized.addNode(&DataObjScan{id: "scan2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time2000),
				Op:    types.BinaryOpLte,
			},
		}})
		merge := optimized.addNode(&SortMerge{id: "merge"})
		filter1 := optimized.addNode(&Filter{id: "filter1", Predicates: []Expression{}})
		filter2 := optimized.addNode(&Filter{id: "filter2", Predicates: []Expression{}})
		filter3 := optimized.addNode(&Filter{id: "filter3", Predicates: []Expression{}})

		_ = optimized.addEdge(Edge{Parent: filter3, Child: filter2})
		_ = optimized.addEdge(Edge{Parent: filter2, Child: filter1})
		_ = optimized.addEdge(Edge{Parent: filter1, Child: merge})
		_ = optimized.addEdge(Edge{Parent: merge, Child: scan1})
		_ = optimized.addEdge(Edge{Parent: merge, Child: scan2})

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual)
	})

	t.Run("filter remove", func(t *testing.T) {
		plan := dummyPlan()
		optimizations := []*optimization{
			newOptimization("noop filter", plan).withRules(
				&removeNoopFilter{plan},
			),
		}

		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])
		actual := PrintAsTree(plan)

		optimized := &Plan{}
		scan1 := optimized.addNode(&DataObjScan{id: "scan1", Predicates: []Expression{}})
		scan2 := optimized.addNode(&DataObjScan{id: "scan2", Predicates: []Expression{}})
		merge := optimized.addNode(&SortMerge{id: "merge"})
		filter1 := optimized.addNode(&Filter{id: "filter1", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		filter2 := optimized.addNode(&Filter{id: "filter2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time2000),
				Op:    types.BinaryOpLte,
			},
		}})

		_ = optimized.addEdge(Edge{Parent: filter2, Child: filter1})
		_ = optimized.addEdge(Edge{Parent: filter1, Child: merge})
		_ = optimized.addEdge(Edge{Parent: merge, Child: scan1})
		_ = optimized.addEdge(Edge{Parent: merge, Child: scan2})

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual)
	})

	t.Run("groupby pushdown", func(t *testing.T) {
		groupBy := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
			&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
		}

		// generate plan for sum by(service, instance) (count_over_time{...}[])
		plan := &Plan{}
		{
			scan1 := plan.addNode(&DataObjScan{id: "scan1"})
			rangeAgg := plan.addNode(&RangeAggregation{
				id:        "count_over_time",
				Operation: types.RangeAggregationTypeCount,
			})
			vectorAgg := plan.addNode(&VectorAggregation{
				id:        "sum_of",
				Operation: types.VectorAggregationTypeSum,
				GroupBy:   groupBy,
			})

			_ = plan.addEdge(Edge{Parent: vectorAgg, Child: rangeAgg})
			_ = plan.addEdge(Edge{Parent: rangeAgg, Child: scan1})
		}

		// apply optimisation
		optimizations := []*optimization{
			newOptimization("group by pushdown", plan).withRules(
				&groupByPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			scan1 := expectedPlan.addNode(&DataObjScan{id: "scan1"})
			rangeAgg := expectedPlan.addNode(&RangeAggregation{
				id:          "count_over_time",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: groupBy,
			})
			vectorAgg := expectedPlan.addNode(&VectorAggregation{
				id:        "sum_of",
				Operation: types.VectorAggregationTypeSum,
				GroupBy:   groupBy,
			})

			_ = expectedPlan.addEdge(Edge{Parent: vectorAgg, Child: rangeAgg})
			_ = expectedPlan.addEdge(Edge{Parent: rangeAgg, Child: scan1})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown", func(t *testing.T) {
		partitionBy := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
			&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
		}

		plan := &Plan{}
		{
			scan1 := plan.addNode(&DataObjScan{
				id: "scan1",
			})
			scan2 := plan.addNode(&DataObjScan{
				id: "scan2",
			})
			rangeAgg := plan.addNode(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = plan.addEdge(Edge{Parent: rangeAgg, Child: scan1})
			_ = plan.addEdge(Edge{Parent: rangeAgg, Child: scan2})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			projected := append(partitionBy, &ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}})
			scan1 := expectedPlan.addNode(&DataObjScan{
				id:          "scan1",
				Projections: projected,
			})
			scan2 := expectedPlan.addNode(&DataObjScan{
				id:          "scan2",
				Projections: projected,
			})

			rangeAgg := expectedPlan.addNode(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.addEdge(Edge{Parent: rangeAgg, Child: scan1})
			_ = expectedPlan.addEdge(Edge{Parent: rangeAgg, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})
}
