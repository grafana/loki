package engine

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestLogQLShapeOf(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  logqlShape
	}{
		{
			name:  "bare stream selector",
			query: `{app="test"}`,
			want: logqlShape{
				lengthChars:    len(`{app="test"}`),
				labelMatchers:  1,
				pipelineStages: 0,
				hasRegex:       false,
			},
		},
		{
			name:  "multiple matchers with regex matcher",
			query: `{app="test", env=~"prod|dev"}`,
			want: logqlShape{
				lengthChars:   len(`{app="test", env=~"prod|dev"}`),
				labelMatchers: 2,
				hasRegex:      true,
			},
		},
		{
			name:  "pipeline stages without regex",
			query: `{app="test"} |= "error" | json | level="warn"`,
			want: logqlShape{
				lengthChars:    len(`{app="test"} |= "error" | json | level="warn"`),
				labelMatchers:  1,
				pipelineStages: 3,
				hasRegex:       false,
			},
		},
		{
			name:  "regex line filter",
			query: `{app="test"} |~ "err.*"`,
			want: logqlShape{
				lengthChars:    len(`{app="test"} |~ "err.*"`),
				labelMatchers:  1,
				pipelineStages: 1,
				hasRegex:       true,
			},
		},
		{
			name:  "metric query unwraps selector",
			query: `sum(rate({app="test"} |= "x" [5m]))`,
			want: logqlShape{
				lengthChars:    len(`sum(rate({app="test"} |= "x" [5m]))`),
				labelMatchers:  1,
				pipelineStages: 1,
				hasRegex:       false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tt.query)
			require.NoError(t, err)

			got := logqlShapeOf(tt.query, expr)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLogQLShapeOf_NilExpr(t *testing.T) {
	got := logqlShapeOf("", nil)
	require.Equal(t, logqlShape{}, got)
}

func TestPhysicalPlanShapeOf(t *testing.T) {
	// Build the following plan:
	//
	//	limit (root)
	//	  merge            (fanout 2)
	//	    parallelize
	//	      scan1
	//	    parallelize
	//	      scan2
	plan := &physical.Plan{}
	g := plan.Graph()

	limit := g.Add(physical.Node(&physical.Limit{NodeID: ulid.Make()}))
	merge := g.Add(physical.Node(&physical.Merge{NodeID: ulid.Make()}))
	par1 := g.Add(physical.Node(&physical.Parallelize{NodeID: ulid.Make()}))
	par2 := g.Add(physical.Node(&physical.Parallelize{NodeID: ulid.Make()}))
	scan1 := g.Add(physical.Node(&physical.DataObjScan{NodeID: ulid.Make()}))
	scan2 := g.Add(physical.Node(&physical.DataObjScan{NodeID: ulid.Make()}))

	for _, e := range []dag.Edge[physical.Node]{
		{Parent: limit, Child: merge},
		{Parent: merge, Child: par1},
		{Parent: merge, Child: par2},
		{Parent: par1, Child: scan1},
		{Parent: par2, Child: scan2},
	} {
		require.NoError(t, g.AddEdge(e))
	}

	got := physicalPlanShapeOf(plan)
	require.Equal(t, physicalShape{
		depth:                 4, // limit -> merge -> parallelize -> scan
		nodeCount:             6,
		maxFanout:             2, // merge has two children
		partitionableSubtrees: 2, // two Parallelize nodes
	}, got)
}

func TestPhysicalPlanShapeOf_Nil(t *testing.T) {
	require.Equal(t, physicalShape{}, physicalPlanShapeOf(nil))
}

func TestEncodeFirings(t *testing.T) {
	t.Run("logical", func(t *testing.T) {
		firings := []logical.PassFiring{
			{Name: "SimplifyRegex", Applicable: true, Succeeded: true},
		}
		require.Equal(t, `[{"name":"SimplifyRegex","applicable":true,"succeeded":true}]`, encodeLogicalPasses(firings))
	})

	t.Run("physical", func(t *testing.T) {
		rules := map[string]bool{
			"PredicatePushdown": false,
		}
		require.Equal(t, `[{"name":"PredicatePushdown","applicable":false}]`, encodePhysicalRules(rules))
	})

	t.Run("empty", func(t *testing.T) {
		require.Equal(t, `[]`, encodeLogicalPasses(nil))
	})
}
