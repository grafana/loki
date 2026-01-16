package physical

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

type fakeMetastoreIndexes struct {
	metastore.Metastore
	indexPaths []string
}

func (f fakeMetastoreIndexes) GetIndexes(_ context.Context, _ metastore.GetIndexesRequest) (metastore.GetIndexesResponse, error) {
	return metastore.GetIndexesResponse{IndexesPaths: f.indexPaths}, nil
}

func TestPointersScan_Clone_AllowsNilSelector(t *testing.T) {
	scan := &PointersScan{
		NodeID:   ulid.Make(),
		Location: DataObjLocation("index/0"),
	}

	cloned := scan.Clone().(*PointersScan)
	require.Nil(t, cloned.Selector)
}

func TestMetastorePlanner_Plan_UsesMergeRootAndPointersTargets(t *testing.T) {
	ms := fakeMetastoreIndexes{
		indexPaths: []string{"index/0", "index/1"},
	}

	start := time.Unix(10, 0)
	end := start.Add(time.Hour)

	p := NewMetastorePlanner(ms)
	plan, err := p.Plan(context.Background(), nil, nil, start, end)
	require.NoError(t, err)

	root, err := plan.Root()
	require.NoError(t, err)
	require.IsType(t, &Merge{}, root)

	children := plan.Children(root)
	require.Len(t, children, 1)
	require.IsType(t, &Parallelize{}, children[0])

	parallel := children[0]
	parallelChildren := plan.Children(parallel)
	require.Len(t, parallelChildren, 1)
	require.IsType(t, &ScanSet{}, parallelChildren[0])

	set := parallelChildren[0].(*ScanSet)
	require.Len(t, set.Targets, len(ms.indexPaths))

	for i, target := range set.Targets {
		require.Equal(t, ScanTypePointers, target.Type)
		require.NotNil(t, target.Pointers)
		require.Equal(t, DataObjLocation(ms.indexPaths[i]), target.Pointers.Location)
		require.Equal(t, start, target.Pointers.Start)
		require.Equal(t, end, target.Pointers.End)
	}
}
