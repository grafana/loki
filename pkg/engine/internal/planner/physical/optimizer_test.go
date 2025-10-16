package physical

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
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
				Right: NewLiteral(types.Timestamp(3600000)),
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

var time1000 = types.Timestamp(1000000000)

func dummyPlan() *Plan {
	plan := &Plan{}
	scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
	scan2 := plan.graph.Add(&DataObjScan{id: "scan2"})
	merge := plan.graph.Add(&SortMerge{id: "merge"})
	filter1 := plan.graph.Add(&Filter{id: "filter1", Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time1000),
			Op:    types.BinaryOpGt,
		},
	}})
	filter2 := plan.graph.Add(&Filter{id: "filter2", Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
			Right: NewLiteral("debug|info"),
			Op:    types.BinaryOpMatchRe,
		},
	}})
	filter3 := plan.graph.Add(&Filter{id: "filter3", Predicates: []Expression{}})

	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter3, Child: filter2})
	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: merge})
	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: scan1})
	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: scan2})

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
		scan1 := optimized.graph.Add(&DataObjScan{id: "scan1", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		scan2 := optimized.graph.Add(&DataObjScan{id: "scan2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		merge := optimized.graph.Add(&SortMerge{id: "merge"})
		filter1 := optimized.graph.Add(&Filter{id: "filter1", Predicates: []Expression{}})
		filter2 := optimized.graph.Add(&Filter{id: "filter2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
		}})
		filter3 := optimized.graph.Add(&Filter{id: "filter3", Predicates: []Expression{}})

		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter3, Child: filter2})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: merge})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: scan1})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: scan2})

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
		scan1 := optimized.graph.Add(&DataObjScan{id: "scan1", Predicates: []Expression{}})
		scan2 := optimized.graph.Add(&DataObjScan{id: "scan2", Predicates: []Expression{}})
		merge := optimized.graph.Add(&SortMerge{id: "merge"})
		filter1 := optimized.graph.Add(&Filter{id: "filter1", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		filter2 := optimized.graph.Add(&Filter{id: "filter2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
		}})

		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: merge})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: scan1})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: scan2})

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown handles groupby for SUM->COUNT", func(t *testing.T) {
		groupBy := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
			&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
		}

		// generate plan for sum by(service, instance) (count_over_time{...}[])
		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				id:        "count_over_time",
				Operation: types.RangeAggregationTypeCount,
			})
			vectorAgg := plan.graph.Add(&VectorAggregation{
				id:        "sum_of",
				Operation: types.VectorAggregationTypeSum,
				GroupBy:   groupBy,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan1})
		}

		// apply optimisation
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			// pushed down from group and partition by, with range aggregations adding timestamp
			expectedProjections := []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}},
			}

			scan1 := expectedPlan.graph.Add(&DataObjScan{id: "scan1", Projections: expectedProjections})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				id:          "count_over_time",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: groupBy,
			})
			vectorAgg := expectedPlan.graph.Add(&VectorAggregation{
				id:        "sum_of",
				Operation: types.VectorAggregationTypeSum,
				GroupBy:   groupBy,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan1})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown does not handle groupby for MAX->SUM", func(t *testing.T) {
		groupBy := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
		}

		partitionBy := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
		}

		// generate plan for max by(service) (sum_over_time{...}[])
		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				id:          "sum_over_time",
				Operation:   types.RangeAggregationTypeSum,
				PartitionBy: partitionBy,
			})
			vectorAgg := plan.graph.Add(&VectorAggregation{
				id:        "max_of",
				Operation: types.VectorAggregationTypeMax,
				GroupBy:   groupBy,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan1})
		}

		// apply optimisation
		optimizations := []*optimization{
			newOptimization("projection pushdown", plan).withRules(
				&projectionPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		expectedPlan := &Plan{}
		{
			// groupby was not pushed down
			expectedProjections := []ColumnExpression{
				&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}},
			}

			scan1 := expectedPlan.graph.Add(&DataObjScan{id: "scan1", Projections: expectedProjections})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				id:          "sum_over_time",
				Operation:   types.RangeAggregationTypeSum,
				PartitionBy: partitionBy,
			})
			vectorAgg := expectedPlan.graph.Add(&VectorAggregation{
				id:        "max_of",
				Operation: types.VectorAggregationTypeMax,
				GroupBy:   groupBy,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: vectorAgg, Child: rangeAgg})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan1})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("projection pushdown handles partition by", func(t *testing.T) {
		partitionBy := []ColumnExpression{
			&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
			&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
		}

		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&DataObjScan{
				id: "scan1",
			})
			scan2 := plan.graph.Add(&DataObjScan{
				id: "scan2",
			})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan2})
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
			scan1 := expectedPlan.graph.Add(&DataObjScan{
				id:          "scan1",
				Projections: projected,
			})
			scan2 := expectedPlan.graph.Add(&DataObjScan{
				id:          "scan2",
				Projections: projected,
			})

			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: scan2})
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
			scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
			scan2 := plan.graph.Add(&DataObjScan{id: "scan2"})
			filter := plan.graph.Add(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			rangeAgg := plan.graph.Add(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: filter})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan2})
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
				&ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "message", Type: types.ColumnTypeBuiltin}},
				&ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}},
				&ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}},
			}

			scan1 := expectedPlan.graph.Add(&DataObjScan{
				id:          "scan1",
				Projections: expectedProjections,
			})
			scan2 := expectedPlan.graph.Add(&DataObjScan{
				id:          "scan2",
				Projections: expectedProjections,
			})
			filter := expectedPlan.graph.Add(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			rangeAgg := expectedPlan.graph.Add(&RangeAggregation{
				id:          "range1",
				Operation:   types.RangeAggregationTypeCount,
				PartitionBy: partitionBy,
			})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: rangeAgg, Child: filter})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan2})
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
			scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
			scan2 := plan.graph.Add(&DataObjScan{id: "scan2"})
			filter := plan.graph.Add(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			limit := plan.graph.Add(&Limit{id: "limit1", Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan2})
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
			scan1 := expectedPlan.graph.Add(&DataObjScan{id: "scan1"})
			scan2 := expectedPlan.graph.Add(&DataObjScan{id: "scan2"})
			filter := expectedPlan.graph.Add(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			limit := expectedPlan.graph.Add(&Limit{id: "limit1", Fetch: 100})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scan2})
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
			scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
			scan2 := plan.graph.Add(&DataObjScan{id: "scan2"})
			topK1 := plan.graph.Add(&TopK{id: "topK1", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			topK2 := plan.graph.Add(&TopK{id: "topK2", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			filter := plan.graph.Add(&Filter{
				id:         "filter1",
				Predicates: filterPredicates,
			})
			limit := plan.graph.Add(&Limit{id: "limit1", Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: topK1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: topK2})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK2, Child: scan2})
		}
		orig := PrintAsTree(plan)

		// apply optimisations
		optimizations := []*optimization{
			newOptimization("limit pushdown", plan).withRules(
				&limitPushdown{plan: plan},
			),
		}
		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])

		actual := PrintAsTree(plan)
		require.Equal(t, orig, actual)
	})

	t.Run("limit pushdown without filter should propagate limit to child nodes", func(t *testing.T) {
		plan := &Plan{}
		{
			scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
			scan2 := plan.graph.Add(&DataObjScan{id: "scan2"})
			topK1 := plan.graph.Add(&TopK{id: "topK1", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			topK2 := plan.graph.Add(&TopK{id: "topK2", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin)})
			limit := plan.graph.Add(&Limit{id: "limit1", Fetch: 100})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scan1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topK2, Child: scan2})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK1})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK2})
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
			scan1 := expectedPlan.graph.Add(&DataObjScan{id: "scan1"})
			scan2 := expectedPlan.graph.Add(&DataObjScan{id: "scan2"})
			topK1 := expectedPlan.graph.Add(&TopK{id: "topK1", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin), K: 100})
			topK2 := expectedPlan.graph.Add(&TopK{id: "topK2", SortBy: newColumnExpr("timestamp", types.ColumnTypeBuiltin), K: 100})
			limit := expectedPlan.graph.Add(&Limit{id: "limit1", Fetch: 100})

			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: topK2})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: topK1, Child: scan1})
			_ = expectedPlan.graph.AddEdge(dag.Edge[Node]{Parent: topK2, Child: scan2})
		}

		actual := PrintAsTree(plan)
		expected := PrintAsTree(expectedPlan)
		require.Equal(t, expected, actual)
	})

	t.Run("cleanup no-op merge nodes", func(t *testing.T) {
		plan := func() *Plan {
			plan := &Plan{}
			limit := plan.graph.Add(&Limit{id: "limit"})
			merge := plan.graph.Add(&Merge{id: "merge"})
			sortmerge := plan.graph.Add(&Merge{id: "sortmerge"})
			scan := plan.graph.Add(&DataObjScan{id: "scan"})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: merge})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: sortmerge})
			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: sortmerge, Child: scan})
			return plan
		}()

		optimizations := []*optimization{
			newOptimization("cleanup", plan).withRules(
				&removeNoopMerge{plan},
			),
		}

		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])
		actual := PrintAsTree(plan)

		optimized := func() *Plan {
			plan := &Plan{}
			limit := plan.graph.Add(&Limit{id: "limit"})
			scan := plan.graph.Add(&DataObjScan{id: "scan"})

			_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: scan})
			return plan
		}()

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, actual, fmt.Sprintf("Expected:\n%s\nActual:\n%s\n", expected, actual))
	})

	// both predicate pushdown and limits pushdown should work together
	t.Run("predicate and limits pushdown", func(t *testing.T) {
		plan := &Plan{}
		scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
		scan2 := plan.graph.Add(&DataObjScan{id: "scan2"})
		sortMerge := plan.graph.Add(&SortMerge{id: "sortMerge"})
		filter := plan.graph.Add(&Filter{id: "filter", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		}})
		limit := plan.graph.Add(&Limit{id: "limit", Fetch: 100})

		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: sortMerge})
		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: sortMerge, Child: scan1})
		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: sortMerge, Child: scan2})

		planner := NewPlanner(NewContext(time.Unix(0, 0), time.Unix(3600, 0)), &catalog{})
		actual, err := planner.Optimize(plan)
		require.NoError(t, err)

		optimized := &Plan{}
		{
			scan1 := optimized.graph.Add(&DataObjScan{id: "scan1",
				Predicates: []Expression{
					&BinaryExpr{
						Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
						Right: NewLiteral(time1000),
						Op:    types.BinaryOpGt,
					},
				}})
			scan2 := optimized.graph.Add(&DataObjScan{id: "scan2",
				Predicates: []Expression{
					&BinaryExpr{
						Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
						Right: NewLiteral(time1000),
						Op:    types.BinaryOpGt,
					},
				}})
			merge := optimized.graph.Add(&SortMerge{id: "merge"})
			limit := optimized.graph.Add(&Limit{id: "limit1", Fetch: 100})

			_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: merge})
			_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: scan1})
			_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: scan2})
		}

		expected := PrintAsTree(optimized)
		require.Equal(t, expected, PrintAsTree(actual))
	})

	t.Run("filter node pushdown", func(t *testing.T) {
		// Filter -> Merge
		// Merge -> TopK1 -> Scan1
		// Merge -> TopK2
		// TopK2 -> Scan2
		// TopK2 -> Scan3
		plan := &Plan{}
		scan1 := plan.graph.Add(&DataObjScan{id: "scan1"})
		scan2 := plan.graph.Add(&DataObjScan{id: "scan2"})
		scan3 := plan.graph.Add(&DataObjScan{id: "scan3"})
		topk1 := plan.graph.Add(&SortMerge{id: "topK1"})
		topk2 := plan.graph.Add(&SortMerge{id: "topK2"})
		merge := plan.graph.Add(&Merge{id: "merge"})
		filter := plan.graph.Add(&Filter{id: "filter", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
				Right: NewLiteral("debug|info"),
				Op:    types.BinaryOpMatchRe,
			},
		}})

		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: merge})
		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: topk1})
		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: topk2})
		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topk1, Child: scan1})
		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topk2, Child: scan2})
		_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: topk2, Child: scan3})

		optimizations := []*optimization{
			newOptimization("filter node pushdown", plan).withRules(
				newFilterNodePushdown(plan),
			),
		}

		o := newOptimizer(plan, optimizations)
		o.optimize(plan.Roots()[0])
		actual := PrintAsTree(plan)

		// Merge -> TopK1 -> Filter1 -> Scan1
		// Merge -> TopK2 -> Filter2 -> Scan2
		// Merge -> TopK2 -> Filter3 -> Scan3
		optimized := &Plan{}
		optimized.graph.Add(scan1)
		optimized.graph.Add(scan2)
		optimized.graph.Add(scan3)
		optimized.graph.Add(topk1)
		optimized.graph.Add(topk2)
		optimized.graph.Add(merge)

		filter1 := *(filter.(*Filter))
		filter2 := *(filter.(*Filter))
		filter3 := *(filter.(*Filter))
		optimized.graph.Add(&filter1)
		optimized.graph.Add(&filter2)
		optimized.graph.Add(&filter3)

		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: topk1})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: topk2})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: topk1, Child: &filter1})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: topk2, Child: &filter2})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: topk2, Child: &filter3})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: &filter1, Child: scan1})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: &filter2, Child: scan2})
		_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: &filter3, Child: scan3})
		expected := PrintAsTree(optimized)

		require.Equal(t, expected, actual)
	})
}

func TestProjectionPushdown_PushesRequestedKeysToParseNodes(t *testing.T) {
	tests := []struct {
		name                           string
		buildLogical                   func() logical.Value
		expectedParseKeysRequested     []string
		expectedDataObjScanProjections []string
	}{
		{
			name: "ParseNode remains empty when no operations need parsed fields",
			buildLogical: func() logical.Value {
				// Create a simple log query with no filters that need parsed fields
				// {app="test"} | logfmt
				selectorPredicate := &logical.BinOp{
					Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
					Right: logical.NewLiteral("test"),
					Op:    types.BinaryOpEq,
				}
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector:   selectorPredicate,
					Predicates: []logical.Value{selectorPredicate},
					Shard:      logical.NewShard(0, 1),
				})

				// Add parse but no filters requiring parsed fields
				builder = builder.Parse(logical.ParserLogfmt)
				return builder.Value()
			},
		},
		{
			name: "ParseNode skips label and builtin columns, only collects ambiguous",
			buildLogical: func() logical.Value {
				// {app="test"} | logfmt | app="frontend" | level="error"
				// This is a log query (no RangeAggregation) so should parse all keys
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})

				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter on label column (should be skipped)
				labelFilter := &logical.BinOp{
					Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
					Right: logical.NewLiteral("frontend"),
					Op:    types.BinaryOpEq,
				}
				builder = builder.Select(labelFilter)

				// Add filter on ambiguous column (should be collected)
				ambiguousFilter := &logical.BinOp{
					Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("error"),
					Op:    types.BinaryOpEq,
				}
				builder = builder.Select(ambiguousFilter)

				return builder.Value()
			},
		},
		{
			name: "RangeAggregation with PartitionBy on ambiguous columns",
			buildLogical: func() logical.Value {
				// count_over_time({app="test"} | logfmt [5m]) by (duration, service)
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})

				builder = builder.Parse(logical.ParserLogfmt)

				// Range aggregation with PartitionBy
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "duration", Type: types.ColumnTypeAmbiguous}},
						{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeLabel}}, // Label should be skipped
					},
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute,
					5*time.Minute,
				)

				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"duration"}, // Only ambiguous column from PartitionBy
			expectedDataObjScanProjections: []string{"message", "service", "timestamp"},
		},
		{
			name: "log query with logfmt and filter on ambiguous column",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents:
				// {app="test"} | logfmt | level="error"
				// This is a log query (no RangeAggregation) so should parse all keys
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter with ambiguous column
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("error"),
					Op:    types.BinaryOpEq,
				}
				builder = builder.Select(filterExpr)
				return builder.Value()
			},
			expectedParseKeysRequested: nil, // Log queries should parse all keys
		},
		{
			name: "metric query with logfmt and groupby on ambiguous column",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents:
				// sum by(status) (count_over_time({app="test"} | logfmt [5m]))
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Range aggregation
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{}, // no partition by
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute, // step
					5*time.Minute, // range interval
				)

				// Vector aggregation with groupby on ambiguous column
				builder = builder.VectorAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "status", Type: types.ColumnTypeAmbiguous}},
					},
					types.VectorAggregationTypeSum,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"status"},
			expectedDataObjScanProjections: []string{"message", "timestamp"},
		},
		{
			name: "metric query with multiple ambiguous columns",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents:
				// sum by(status,code) (count_over_time({app="test"} | logfmt | duration > 100 [5m]))
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter with ambiguous column
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("duration", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral(int64(100)),
					Op:    types.BinaryOpGt,
				}
				builder = builder.Select(filterExpr)

				// Range aggregation
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{}, // no partition by
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute, // step
					5*time.Minute, // range interval
				)

				// Vector aggregation with groupby on ambiguous columns
				builder = builder.VectorAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "status", Type: types.ColumnTypeAmbiguous}},
						{Ref: types.ColumnRef{Column: "code", Type: types.ColumnTypeAmbiguous}},
					},
					types.VectorAggregationTypeSum,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"code", "duration", "status"}, // sorted alphabetically
			expectedDataObjScanProjections: []string{"message", "timestamp"},
		},
		{
			name: "log query should request all keys even with filters",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents a log query:
				// {app="test"} | logfmt | level="error" | limit 100
				// This is a log query (no range aggregation) so should parse all keys
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector: &logical.BinOp{
						Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
						Right: logical.NewLiteral("test"),
						Op:    types.BinaryOpEq,
					},
					Shard: logical.NewShard(0, 1),
				})

				// Add parse without specifying RequestedKeys
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter on ambiguous column
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("error"),
					Op:    types.BinaryOpEq,
				}
				builder = builder.Select(filterExpr)

				// Add a limit (typical for log queries)
				builder = builder.Limit(0, 100)

				return builder.Value()
			},
		},
		{
			name: "ParseNodes consume ambiguous projections, they are not pushed down to DataObjScans",
			buildLogical: func() logical.Value {
				// Create a logical plan that represents:
				// sum by(app) (count_over_time({app="test"} | logfmt | level="error" [5m]) by (status, app))
				selectorPredicate := &logical.BinOp{
					Left:  logical.NewColumnRef("app", types.ColumnTypeLabel),
					Right: logical.NewLiteral("test"),
					Op:    types.BinaryOpEq,
				}
				builder := logical.NewBuilder(&logical.MakeTable{
					Selector:   selectorPredicate,
					Predicates: []logical.Value{selectorPredicate},
					Shard:      logical.NewShard(0, 1), // noShard
				})

				// Don't set RequestedKeys here - optimization should determine them
				builder = builder.Parse(logical.ParserLogfmt)

				// Add filter with ambiguous column (different from grouping field)
				filterExpr := &logical.BinOp{
					Left:  logical.NewColumnRef("level", types.ColumnTypeAmbiguous),
					Right: logical.NewLiteral("error"),
					Op:    types.BinaryOpEq,
				}
				builder = builder.Select(filterExpr)

				// Range aggregation
				builder = builder.RangeAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "status", Type: types.ColumnTypeAmbiguous}},
						{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
					}, // no partition by
					types.RangeAggregationTypeCount,
					time.Unix(0, 0),
					time.Unix(3600, 0),
					5*time.Minute, // step
					5*time.Minute, // range interval
				)

				// Vector aggregation with single groupby on parsed field (different from filter field)
				builder = builder.VectorAggregation(
					[]logical.ColumnRef{
						{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
					},
					types.VectorAggregationTypeSum,
				)
				return builder.Value()
			},
			expectedParseKeysRequested:     []string{"level", "status"},
			expectedDataObjScanProjections: []string{"app", "message", "timestamp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build logical plan
			logicalValue := tt.buildLogical()
			builder := logical.NewBuilder(logicalValue)
			logicalPlan, err := builder.ToPlan()
			require.NoError(t, err)

			// Create physical planner with test catalog
			catalog := &catalog{}
			for i := 0; i < 10; i++ {
				catalog.sectionDescriptors = append(catalog.sectionDescriptors, &metastore.DataobjSectionDescriptor{
					SectionKey: metastore.SectionKey{ObjectPath: "/test/object", SectionIdx: int64(i)},
					StreamIDs:  []int64{1, 2},
					Start:      time.Unix(0, 0),
					End:        time.Unix(3600, 0),
				})
			}
			ctx := NewContext(time.Unix(0, 0), time.Unix(3600, 0))
			planner := NewPlanner(ctx, catalog)

			// Build physical plan
			physicalPlan, err := planner.Build(logicalPlan)
			require.NoError(t, err)

			// Optimize the plan - this should apply parseKeysPushdown
			optimizedPlan, err := planner.Optimize(physicalPlan)
			require.NoError(t, err)

			// Check that ParseNode and DataObjScan get the correct projections
			var parseNode *ParseNode
			projections := map[string]struct{}{}
			for node := range optimizedPlan.graph.Nodes() {
				if pn, ok := node.(*ParseNode); ok {
					parseNode = pn
					continue
				}
				if pn, ok := node.(*DataObjScan); ok {
					for _, colExpr := range pn.Projections {
						expr := colExpr.(*ColumnExpr)
						projections[expr.Ref.Column] = struct{}{}
					}
				}
			}

			var projectionArr []string
			for column := range projections {
				projectionArr = append(projectionArr, column)
			}
			sort.Strings(projectionArr)

			require.NotNil(t, parseNode, "ParseNode not found in plan")
			require.Equal(t, tt.expectedParseKeysRequested, parseNode.RequestedKeys)
			require.Equal(t, tt.expectedDataObjScanProjections, projectionArr)
		})
	}
}
