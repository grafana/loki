package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

type fakeMetastoreIndexes struct {
	metastore.Metastore
	indexPaths []string
}

func (f fakeMetastoreIndexes) GetIndexes(_ context.Context, _ metastore.GetIndexesRequest) (metastore.GetIndexesResponse, error) {
	return metastore.GetIndexesResponse{IndexesPaths: f.indexPaths}, nil
}

func TestPlanWorkflow_MetastorePlan_UsesMergeRootAndPointersPartitions(t *testing.T) {
	ms := fakeMetastoreIndexes{
		indexPaths: []string{"index/0", "index/1", "index/2"},
	}

	start := time.Unix(10, 0)
	end := start.Add(time.Hour)

	p := physical.NewMetastorePlanner(ms)
	plan, err := p.Plan(context.Background(), nil, nil, start, end)
	require.NoError(t, err)

	graph, err := planWorkflow("tenant", plan)
	require.NoError(t, err)

	rootTask, err := graph.Root()
	require.NoError(t, err)

	rootNode, err := rootTask.Fragment.Root()
	require.NoError(t, err)
	require.IsType(t, &physical.Merge{}, rootNode)

	require.Len(t, rootTask.Sources[rootNode], len(ms.indexPaths))

	children := graph.Children(rootTask)
	require.Len(t, children, len(ms.indexPaths))

	gotLocations := make(map[physical.DataObjLocation]struct{}, len(children))
	for _, child := range children {
		childRoot, err := child.Fragment.Root()
		require.NoError(t, err)
		require.IsType(t, &physical.PointersScan{}, childRoot)

		gotLocations[childRoot.(*physical.PointersScan).Location] = struct{}{}
	}

	for _, indexPath := range ms.indexPaths {
		_, ok := gotLocations[physical.DataObjLocation(indexPath)]
		require.True(t, ok, "missing partition for %q", indexPath)
	}
}
