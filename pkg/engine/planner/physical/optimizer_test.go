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
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
			want: false,
		},
		{
			predicate: &BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeMetadata),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
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

var time1000 = datatype.Timestamp(1000000000)

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
			Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
			Right: NewLiteral("debug|info"),
			Op:    types.BinaryOpMatchRe,
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
		}})
		scan2 := optimized.addNode(&DataObjScan{id: "scan2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		merge := optimized.addNode(&SortMerge{id: "merge"})
		filter1 := optimized.addNode(&Filter{id: "filter1", Predicates: []Expression{}})
		filter2 := optimized.addNode(&Filter{id: "filter2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
		}})
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
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
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

	t.Run("predicate column projection pushdown with existing projections", func(t *testing.T) {
		// Predicate columns should be projected when there are existing projections (metric query)
		partitionBy := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
		}

		filterPredicates := []Expression{
			&BinaryExpr{
				Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				Right: NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
			&BinaryExpr{
				Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				Right: NewLiteral(".*exception.*"),
				Op:    types.BinaryOpMatchRe,
			},
		}

		plan := &Plan{}
		{
			scan1 := plan.addNode(&DataObjScan{id: "scan1"})
			scan2 := plan.addNode(&DataObjScan{id: "scan2"})
			filter := plan.addNode(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			rangeAgg := plan.addNode(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = plan.addEdge(Edge{Parent: rangeAgg, Child: filter})
			_ = plan.addEdge(Edge{Parent: filter, Child: scan1})
			_ = plan.addEdge(Edge{Parent: filter, Child: scan2})
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
			expectedProjections := []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
			}

			scan1 := expectedPlan.addNode(&DataObjScan{
				id:          "scan1",
				Projections: expectedProjections,
			})
			scan2 := expectedPlan.addNode(&DataObjScan{
				id:          "scan2",
				Projections: expectedProjections,
			})
			filter := expectedPlan.addNode(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			rangeAgg := expectedPlan.addNode(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.addEdge(Edge{Parent: rangeAgg, Child: filter})
			_ = expectedPlan.addEdge(Edge{Parent: filter, Child: scan1})
			_ = expectedPlan.addEdge(Edge{Parent: filter, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("predicate column projection pushdown without existing projections", func(t *testing.T) {
		// Predicate columns should NOT be projected when there are no existing projections (log query)
		filterPredicates := []Expression{
			&BinaryExpr{
				Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				Right: NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
			&BinaryExpr{
				Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				Right: NewLiteral(".*exception.*"),
				Op:    types.BinaryOpMatchRe,
			},
		}

		plan := &Plan{}
		{
			scan1 := plan.addNode(&DataObjScan{id: "scan1"})
			scan2 := plan.addNode(&DataObjScan{id: "scan2"})
			filter := plan.addNode(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			limit := plan.addNode(&Limit{id: "limit1", Fetch: 100})

			_ = plan.addEdge(Edge{Parent: limit, Child: filter})
			_ = plan.addEdge(Edge{Parent: filter, Child: scan1})
			_ = plan.addEdge(Edge{Parent: filter, Child: scan2})
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
			scan1 := expectedPlan.addNode(&DataObjScan{id: "scan1"})
			scan2 := expectedPlan.addNode(&DataObjScan{id: "scan2"})
			filter := expectedPlan.addNode(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			limit := expectedPlan.addNode(&Limit{id: "limit1", Fetch: 100})

			_ = expectedPlan.addEdge(Edge{Parent: limit, Child: filter})
			_ = expectedPlan.addEdge(Edge{Parent: filter, Child: scan1})
			_ = expectedPlan.addEdge(Edge{Parent: filter, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("limit pushdown with filter should not propagate limit to child nodes", func(t *testing.T) {
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
			scan1 := plan.addNode(&DataObjScan{id: "scan1"})
			scan2 := plan.addNode(&DataObjScan{id: "scan2"})
			filter := plan.addNode(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			limit := plan.addNode(&Limit{id: "limit1", Fetch: 100})

			_ = plan.addEdge(Edge{Parent: limit, Child: filter})
			_ = plan.addEdge(Edge{Parent: filter, Child: scan1})
			_ = plan.addEdge(Edge{Parent: filter, Child: scan2})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("limit pushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			scan1 := expectedPlan.addNode(&DataObjScan{id: "scan1"})
			scan2 := expectedPlan.addNode(&DataObjScan{id: "scan2"})
			filter := expectedPlan.addNode(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			limit := expectedPlan.addNode(&Limit{id: "limit1", Fetch: 100})

			_ = expectedPlan.addEdge(Edge{Parent: limit, Child: filter})
			_ = expectedPlan.addEdge(Edge{Parent: filter, Child: scan1})
			_ = expectedPlan.addEdge(Edge{Parent: filter, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("limit pushdown without filter should propagate limit to child nodes", func(t *testing.T) {
		plan := &Plan{}
		{
			scan1 := plan.addNode(&DataObjScan{id: "scan1"})
			scan2 := plan.addNode(&DataObjScan{id: "scan2"})
			limit := plan.addNode(&Limit{id: "limit1", Fetch: 100})

			_ = plan.addEdge(Edge{Parent: limit, Child: scan1})
			_ = plan.addEdge(Edge{Parent: limit, Child: scan2})
		}

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("limit pushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			scan1 := expectedPlan.addNode(&DataObjScan{id: "scan1", Limit: 100})
			scan2 := expectedPlan.addNode(&DataObjScan{id: "scan2", Limit: 100})
			limit := expectedPlan.addNode(&Limit{id: "limit1", Fetch: 100})

			_ = expectedPlan.addEdge(Edge{Parent: limit, Child: scan1})
			_ = expectedPlan.addEdge(Edge{Parent: limit, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("filter pushdown simple", func(t *testing.T) {
		// Filter -> Merge
		// Merge -> Scan1
		// Merge -> Scan2
		plan := &Plan{}
		scan1 := plan.addNode(&DataObjScan{id: "scan1", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		scan2 := plan.addNode(&DataObjScan{id: "scan2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		merge := plan.addNode(&SortMerge{id: "merge"})
		filter := plan.addNode(&Filter{id: "filter", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
		}})

		_ = plan.addEdge(Edge{Parent: filter, Child: merge})
		_ = plan.addEdge(Edge{Parent: merge, Child: scan1})
		_ = plan.addEdge(Edge{Parent: merge, Child: scan2})

		optimizations := []*optimization{
			newOptimization("predicate pushdown", plan).withRules(
				&predicatePushdown{plan},
			),
			newOptimization("filter node pushdown", plan).withRules(
				&filterNodePushdown{plan: plan, addedNodes: make(map[Node]struct{})},
			),
		}

		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])
		actual := PrintAsTree(plan)

		t.Logf("Optimized Plan:\n%s", actual)

		// Merge -> Filter1
		// Merge -> Filter2
		// Filter1 -> Scan1
		// Filter2 -> Scan2
		optimized := &Plan{}
		optimized.addNode(scan1)
		optimized.addNode(scan2)
		optimized.addNode(merge)

		filter1 := *(filter.(*Filter))
		filter2 := *(filter.(*Filter))

		optimized.addNode(&filter1)
		optimized.addNode(&filter2)

		_ = optimized.addEdge(Edge{Parent: merge, Child: &filter1})
		_ = optimized.addEdge(Edge{Parent: merge, Child: &filter2})
		_ = optimized.addEdge(Edge{Parent: &filter1, Child: scan1})
		_ = optimized.addEdge(Edge{Parent: &filter2, Child: scan2})

		t.Logf("Expected Plan:\n%s", PrintAsTree(optimized))

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual)
	})

	t.Run("filter pushdown with mix of SortMerge and Merge", func(t *testing.T) {
		// Filter -> Merge
		// Merge -> Scan1
		// Merge -> SortMerge
		// SortMerge -> Scan2
		// SortMerge -> Scan3
		plan := &Plan{}
		scan1 := plan.addNode(&DataObjScan{id: "scan1"})
		scan2 := plan.addNode(&DataObjScan{id: "scan2"})
		scan3 := plan.addNode(&DataObjScan{id: "scan3"})
		sortMerge := plan.addNode(&SortMerge{id: "sortMerge"})
		merge := plan.addNode(&Merge{id: "merge"})
		filter := plan.addNode(&Filter{id: "filter", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
		}})

		_ = plan.addEdge(Edge{Parent: filter, Child: merge})
		_ = plan.addEdge(Edge{Parent: merge, Child: sortMerge})
		_ = plan.addEdge(Edge{Parent: merge, Child: scan1})
		_ = plan.addEdge(Edge{Parent: sortMerge, Child: scan2})
		_ = plan.addEdge(Edge{Parent: sortMerge, Child: scan3})

		optimizations := []*optimization{
			newOptimization("filter node pushdown", plan).withRules(
				&filterNodePushdown{plan: plan, addedNodes: make(map[Node]struct{})},
			),
		}

		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])
		actual := PrintAsTree(plan)

		t.Logf("Optimized Plan:\n%s", actual)

		// Merge -> Filter1 -> Scan1
		// Merge -> SortMerge -> Filter2 -> Scan2
		// Merge -> SortMerge -> Filter3 -> Scan3
		optimized := &Plan{}
		optimized.addNode(scan1)
		optimized.addNode(scan2)
		optimized.addNode(scan3)
		optimized.addNode(sortMerge)
		optimized.addNode(merge)

		filter1 := &Filter{
			id:         "filter",
			Predicates: filter.(*Filter).Predicates,
		}
		filter2 := &Filter{
			id:         "filter",
			Predicates: filter.(*Filter).Predicates,
		}
		filter3 := &Filter{
			id:         "filter",
			Predicates: filter.(*Filter).Predicates,
		}

		optimized.addNode(filter1)
		optimized.addNode(filter2)
		optimized.addNode(filter3)

		_ = optimized.addEdge(Edge{Parent: merge, Child: sortMerge})
		_ = optimized.addEdge(Edge{Parent: merge, Child: filter1})
		_ = optimized.addEdge(Edge{Parent: sortMerge, Child: filter2})
		_ = optimized.addEdge(Edge{Parent: sortMerge, Child: filter3})
		_ = optimized.addEdge(Edge{Parent: filter1, Child: scan1})
		_ = optimized.addEdge(Edge{Parent: filter2, Child: scan2})
		_ = optimized.addEdge(Edge{Parent: filter3, Child: scan3})

		t.Logf("Expected Plan:\n%s", PrintAsTree(optimized))

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual)
	})

}
