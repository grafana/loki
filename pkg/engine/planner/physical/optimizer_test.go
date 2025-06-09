package physical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestCanApplyPredicate(t *testing.T) {
	tests := []struct {
		predicate Expression
		want      bool
	}{
		{
			predicate: NewLiteral(123),
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
				Right: NewLiteral(time.Now()),
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

func dummyPlan() *Plan {
	plan := &Plan{}
	scan1 := plan.addNode(&DataObjScan{id: "scan1"})
	scan2 := plan.addNode(&DataObjScan{id: "scan2"})
	merge := plan.addNode(&SortMerge{id: "merge"})
	filter1 := plan.addNode(&Filter{id: "filter1", Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time.Unix(0, 1000000000)),
			Op:    types.BinaryOpGt,
		},
	}})
	filter2 := plan.addNode(&Filter{id: "filter2", Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time.Unix(0, 2000000000)),
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
				Right: NewLiteral(time.Unix(0, 1000000000)),
				Op:    types.BinaryOpGt,
			},
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time.Unix(0, 2000000000)),
				Op:    types.BinaryOpLte,
			},
		}})
		scan2 := optimized.addNode(&DataObjScan{id: "scan2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time.Unix(0, 1000000000)),
				Op:    types.BinaryOpGt,
			},
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time.Unix(0, 2000000000)),
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
				Right: NewLiteral(time.Unix(0, 1000000000)),
				Op:    types.BinaryOpGt,
			},
		}})
		filter2 := optimized.addNode(&Filter{id: "filter2", Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time.Unix(0, 2000000000)),
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
}
