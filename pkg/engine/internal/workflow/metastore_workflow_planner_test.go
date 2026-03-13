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

	p := physical.NewMetastorePlanner(ms, 100)
	plan, err := p.Plan(context.Background(), nil, nil, start, end)
	require.NoError(t, err)

	graph, err := planWorkflow("tenant", plan)
	require.NoError(t, err)

	rootTask, err := graph.Root()
	require.NoError(t, err)

	rootNode, err := rootTask.Fragment.Root()
	require.NoError(t, err)
	require.IsType(t, &physical.Batching{}, rootNode)

	// Merge is the child of Batching and is the direct parent of Parallelize,
	// so sources are keyed by the Merge node.
	mergeNode := rootTask.Fragment.Children(rootNode)
	require.Len(t, mergeNode, 1)
	require.IsType(t, &physical.Merge{}, mergeNode[0])

	require.Len(t, rootTask.Sources[mergeNode[0]], len(ms.indexPaths))

	children := graph.Children(rootTask)
	require.Len(t, children, len(ms.indexPaths))

	gotLocations := make(map[physical.DataObjLocation]struct{}, len(children))
	for _, child := range children {
		childRoot, err := child.Fragment.Root()
		require.NoError(t, err)
		require.IsType(t, &physical.Batching{}, childRoot)

		// PointersScan is the child of the wrapping Batching node.
		batchingChildren := child.Fragment.Children(childRoot)
		require.Len(t, batchingChildren, 1)
		require.IsType(t, &physical.PointersScan{}, batchingChildren[0])

		gotLocations[batchingChildren[0].(*physical.PointersScan).Location] = struct{}{}
	}

	for _, indexPath := range ms.indexPaths {
		_, ok := gotLocations[physical.DataObjLocation(indexPath)]
		require.True(t, ok, "missing partition for %q", indexPath)
	}
}
