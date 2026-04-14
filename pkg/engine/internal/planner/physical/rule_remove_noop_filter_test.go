package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestRemoveNoopFilter(t *testing.T) {
	plan := dummyPlan()
	optimizations := []*Optimization{
		newOptimization("noop filter", plan).withRules(
			&removeNoopFilter{plan},
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

		Predicates: []Expression{},
	})
	filter1 := optimized.graph.Add(&Filter{Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time1000),
			Op:    types.BinaryOpGt,
		},
	}})
	filter2 := optimized.graph.Add(&Filter{Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
			Right: NewLiteral("debug|info"),
			Op:    types.BinaryOpMatchRe,
		},
	}})

	_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
	_ = optimized.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scanSet})

	expected := PrintAsTree(optimized)
	require.Equal(t, expected, actual)
}
