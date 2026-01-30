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

	p := physical.NewMetastorePlanner(ms, 0)
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

func TestPlanWorkflow_MetastorePlan_TwoLevelMerge_AllMergesInRootTask(t *testing.T) {
	ms := fakeMetastoreIndexes{
		indexPaths: []string{"index/0", "index/1", "index/2", "index/3"},
	}
	start := time.Unix(10, 0)
	end := start.Add(time.Hour)

	p := physical.NewMetastorePlanner(ms, 2)
	plan, err := p.Plan(context.Background(), nil, nil, start, end)
	require.NoError(t, err)

	graph, err := planWorkflow("tenant", plan)
	require.NoError(t, err)

	rootTask, err := graph.Root()
	require.NoError(t, err)

	rootNode, err := rootTask.Fragment.Root()
	require.NoError(t, err)
	require.IsType(t, &physical.Merge{}, rootNode, "fragment root should be top Merge")

	// Fragment should contain top Merge and both inner Merge nodes (3 Merge nodes total).
	mergeCount := 0
	for n := range rootTask.Fragment.Graph().Nodes() {
		if n.Type() == physical.NodeTypeMerge {
			mergeCount++
		}
	}
	require.Equal(t, 3, mergeCount, "fragment should have 3 Merge nodes (top + 2 inner)")

	// Root task has 4 children (4 PointersScan tasks).
	children := graph.Children(rootTask)
	require.Len(t, children, 4, "root task should have 4 PointersScan child tasks")

	// Each inner Merge node in the fragment should have 2 sources (streams from 2 PointersScan tasks).
	innerMerges := rootTask.Fragment.Children(rootNode)
	require.Len(t, innerMerges, 2, "top Merge should have 2 inner Merge children in fragment")
	for _, innerMerge := range innerMerges {
		streams, ok := rootTask.Sources[innerMerge]
		require.True(t, ok, "inner Merge should have entry in Sources")
		require.Len(t, streams, 2, "each inner Merge should have 2 source streams")
	}
}
