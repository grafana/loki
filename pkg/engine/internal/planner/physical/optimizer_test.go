package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

var time1000 = types.Timestamp(1000000000)

func dummyPlan() *Plan {
	plan := &Plan{}

	scanSet := plan.graph.Add(&ScanSet{
		Targets: []*ScanTarget{
			{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
		},
	})
	filter1 := plan.graph.Add(&Filter{Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("timestamp", types.ColumnTypeBuiltin),
			Right: NewLiteral(time1000),
			Op:    types.BinaryOpGt,
		},
	}})
	filter2 := plan.graph.Add(&Filter{Predicates: []Expression{
		&BinaryExpr{
			Left:  newColumnExpr("level", types.ColumnTypeAmbiguous),
			Right: NewLiteral("debug|info"),
			Op:    types.BinaryOpMatchRe,
		},
	}})
	filter3 := plan.graph.Add(&Filter{Predicates: []Expression{}})

	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter3, Child: filter2})
	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: filter1})
	_ = plan.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scanSet})

	return plan
}

// countingRule fires (returns true) a fixed number of times before reporting no
// further changes, mimicking a rule that reaches a fixed point.
type countingRule struct{ fires int }

func (r *countingRule) apply(Node) bool {
	if r.fires <= 0 {
		return false
	}
	r.fires--
	return true
}

func TestOptimizer_Optimize_RuleFirings(t *testing.T) {
	plan := dummyPlan()
	root := plan.Roots()[0]

	optimizer := NewOptimizer(plan, []*Optimization{
		newOptimization("Applied", plan).withRules(&countingRule{fires: 2}),
		newOptimization("NotApplied", plan).withRules(&countingRule{fires: 0}),
	})

	firings := optimizer.Optimize(root)
	require.Equal(t, map[string]bool{
		"Applied":    true,
		"NotApplied": false,
	}, firings)
}
