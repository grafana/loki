package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

type fakeMetastoreIndexes struct {
	metastore.Metastore
	indexes []metastore.IndexEntry
}

func (f fakeMetastoreIndexes) GetIndexes(_ context.Context, _ metastore.GetIndexesRequest) (metastore.GetIndexesResponse, error) {
	return metastore.GetIndexesResponse{Indexes: f.indexes}, nil
}

func TestPlanWorkflow_MetastorePlan_UsesMergeRootAndPointersPartitions(t *testing.T) {
	start := time.Unix(10, 0)
	end := start.Add(time.Hour)

	ms := fakeMetastoreIndexes{
		indexes: []metastore.IndexEntry{
			{Path: "index/0", Start: start, End: end},
			{Path: "index/1", Start: start, End: end},
			{Path: "index/2", Start: start, End: end},
		},
	}

	p := physical.NewMetastorePlanner(ms, 100)
	plan, err := p.Plan(context.Background(), nil, nil, start, end)
	require.NoError(t, err)

	graph, err := planWorkflow("tenant", plan, cacheParams{enabled: true, taskCacheMaxSizeBytes: 1 * 1024 * 1024}, log.NewNopLogger())
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

	require.Len(t, rootTask.Sources[mergeNode[0]], len(ms.indexes))

	children := graph.Children(rootTask)
	require.Len(t, children, len(ms.indexes))

	gotLocations := make(map[physical.DataObjLocation]struct{}, len(children))
	for _, child := range children {
		childRoot, err := child.Fragment.Root()
		require.NoError(t, err)

		// Cache wraps the Batching node when caching is enabled.
		require.IsType(t, &physical.Cache{}, childRoot)
		batchingNode := child.Fragment.Children(childRoot)
		require.Len(t, batchingNode, 1)
		require.IsType(t, &physical.Batching{}, batchingNode[0])

		// PointersScan is the child of Batching.
		batchingChildren := child.Fragment.Children(batchingNode[0])
		require.Len(t, batchingChildren, 1)
		require.IsType(t, &physical.PointersScan{}, batchingChildren[0])

		gotLocations[batchingChildren[0].(*physical.PointersScan).Location] = struct{}{}
	}

	for _, entry := range ms.indexes {
		_, ok := gotLocations[physical.DataObjLocation(entry.Path)]
		require.True(t, ok, "missing partition for %q", entry.Path)
	}
}
