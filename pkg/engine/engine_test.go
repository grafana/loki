package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

func TestPrepareCatalogWithMetaqueries(t *testing.T) {
	t.Parallel()

	start := time.Unix(0, 0)
	end := start.Add(time.Minute)

	params, err := logql.NewLiteralParams("{foo=\"bar\"}", start, end, 0, 0, logproto.BACKWARD, 0, nil, nil)
	require.NoError(t, err)

	makeTable := buildTestMakeTable()
	plan := &logical.Plan{
		Instructions: []logical.Instruction{
			makeTable,
			&logical.Return{Value: makeTable},
		},
	}

	stub := &metaqueryRunnerStub{
		sections: []*metastore.DataobjSectionDescriptor{
			{
				SectionKey: metastore.SectionKey{ObjectPath: "obj", SectionIdx: 1},
				StreamIDs:  []int64{1},
				Start:      start,
				End:        end,
			},
		},
	}

	e := &Engine{
		metaqueriesEnabled: true,
		metaqueryRunner:    stub,
	}

	catalog, _, requestCount, err := e.prepareCatalogWithMetaqueries(context.Background(), params, plan)
	require.NoError(t, err)
	require.Equal(t, 1, requestCount)
	require.Len(t, stub.requests, 1)

	// Running the planner with the prepared catalog should succeed because the metaqueries were resolved.
	planner := physical.NewPlanner(physical.NewContext(start, end), catalog)
	_, err = planner.Build(plan)
	require.NoError(t, err)
}

func buildTestMakeTable() *logical.MakeTable {
	selector := &logical.BinOp{
		Left:  logical.NewColumnRef("__name__", types.ColumnTypeLabel),
		Right: logical.NewLiteral("value"),
		Op:    types.BinaryOpEq,
	}
	return &logical.MakeTable{
		Selector: selector,
		Shard:    logical.NewShard(0, 1),
	}
}

type metaqueryRunnerStub struct {
	sections []*metastore.DataobjSectionDescriptor
	requests []physical.MetaqueryRequest
}

func (m *metaqueryRunnerStub) Run(_ context.Context, req physical.MetaqueryRequest) (physical.MetaqueryResponse, error) {
	m.requests = append(m.requests, req)
	return physical.MetaqueryResponse{Kind: physical.MetaqueryRequestKindSections, Sections: m.sections}, nil
}
