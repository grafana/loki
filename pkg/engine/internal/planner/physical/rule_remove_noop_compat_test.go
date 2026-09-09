package physical

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

func TestRemoveNoopCompat(t *testing.T) {
	start := time.Unix(0, 0)
	end := start.Add(time.Hour)

	tests := []struct {
		name          string
		query         string
		formatOp      types.VariadicOp
		wantCompatOps []types.VariadicOp
	}{
		{
			name:          "line format",
			query:         `{app="loki"} | json | line_format "{{.level}}"`,
			formatOp:      types.VariadicOpParseLinefmt,
			wantCompatOps: []types.VariadicOp{types.VariadicOpParseJSON},
		},
		{
			name:     "label format",
			query:    `{app="loki"} | json | label_format severity=level`,
			formatOp: types.VariadicOpParseLabelfmt,
			wantCompatOps: []types.VariadicOp{
				types.VariadicOpParseJSON,
				types.VariadicOpParseLabelfmt,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := logql.NewLiteralParams(
				tt.query,
				start, end,
				0, 0,
				logproto.BACKWARD,
				100,
				[]string{"0_of_1"},
				nil,
			)
			require.NoError(t, err)
			logicalPlan, err := logical.BuildPlan(context.Background(), params)
			require.NoError(t, err)

			planner := NewPlanner(NewContext(start, end), &catalog{
				sectionDescriptors: []*metastore.DataobjSectionDescriptor{
					{Start: start, End: end},
				},
			})
			plan, err := planner.Build(logicalPlan)
			require.NoError(t, err)
			requireCompatProjections(t, plan, 3, types.VariadicOpParseJSON, tt.formatOp)

			plan, err = planner.Optimize(plan)
			require.NoError(t, err)
			requireCompatProjections(t, plan, len(tt.wantCompatOps)+1, tt.wantCompatOps...)
		})
	}
}

func requireCompatProjections(t *testing.T, plan *Plan, wantCount int, wantOps ...types.VariadicOp) {
	t.Helper()

	root, err := plan.Root()
	require.NoError(t, err)
	compatNodes := findMatchingNodes(plan, root, func(node Node) bool {
		_, ok := node.(*ColumnCompat)
		return ok
	})
	require.Len(t, compatNodes, wantCount)

	var compatOps []types.VariadicOp
	for _, node := range compatNodes {
		children := plan.Children(node)
		if len(children) != 1 {
			continue
		}
		projection, ok := children[0].(*Projection)
		if !ok || len(projection.Expressions) != 1 {
			continue
		}
		expr, ok := projection.Expressions[0].(*VariadicExpr)
		if ok {
			compatOps = append(compatOps, expr.Op)
		}
	}
	require.ElementsMatch(t, wantOps, compatOps)
}
