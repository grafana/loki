package workflow

import (
	"context"
	"fmt"
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
		require.IsType(t, &physical.PointersScan{}, childRoot, "partition fragment root should be PointersScan when not batching")

		gotLocations[childRoot.(*physical.PointersScan).Location] = struct{}{}
	}

	for _, indexPath := range ms.indexPaths {
		_, ok := gotLocations[physical.DataObjLocation(indexPath)]
		require.True(t, ok, "missing partition for %q", indexPath)
	}
}

func TestPlanWorkflow_MetastorePlan_BatchesPointersScans(t *testing.T) {
	paths := make([]string, 250)
	for i := range paths {
		paths[i] = fmt.Sprintf("index/%d", i)
	}
	ms := fakeMetastoreIndexes{indexPaths: paths}
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
	require.IsType(t, &physical.Merge{}, rootNode)

	children := graph.Children(rootTask)
	require.Len(t, children, 3, "250 paths with batch 100 should produce 3 tasks (100, 100, 50)")

	expectedSizes := []int{100, 100, 50}
	for i, child := range children {
		childRoot, err := child.Fragment.Root()
		require.NoError(t, err)
		scanSet, ok := childRoot.(*physical.ScanSet)
		require.True(t, ok, "child %d: partition fragment root should be ScanSet when batching, got %T", i, childRoot)
		require.Len(t, scanSet.Targets, expectedSizes[i], "child %d: expected %d PointersScan targets", i, expectedSizes[i])
		require.Equal(t, 0, scanSet.ShardBatchSize, "batched shard should have ShardBatchSize 0")
	}
}
