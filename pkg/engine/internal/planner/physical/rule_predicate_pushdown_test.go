package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestPredicatePushdown(t *testing.T) {
	plan := dummyPlan()
	optimizations := []*Optimization{
		newOptimization("predicate pushdown", plan).withRules(
			&predicatePushdown{plan},
		),
	}

	o := NewOptimizer(plan, optimizations)
	o.Optimize(plan.Roots()[0])
	actual := PrintAsTree(plan)

	optimized := &Plan{}
	scanSet := optimized.graph.Add(&ScanSet{
		Targets: []*ScanTarget{
			{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
		},

		Predicates: []Expression{
			&BinaryExpr{
				Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
				Right: NewLiteral(time1000),
				Op:    types.BinaryOpGt,
			},
		},
	})
	filter1 := optimized.graph.Add(&Filter{Predicates: []Expression{}})
	filter2 := optimized.graph.Add(&Filter{Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
			Right: NewLiteral("debug|info"),
			Op:    types.BinaryOpMatchRe,
		},
	}}) // ambiguous column predicates are not pushed down.
	filter3 := optimized.graph.Add(&Filter{Predicates: []Expression{}})

	_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter3, Child: filter2})
	_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
	_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scanSet})

	expected := PrintAsTree(optimized)
	require.Equal(t, expected, actual)
}

func TestPredicatePushdown_canApplyPredicate(t *testing.T) {
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
