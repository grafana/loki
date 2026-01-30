package physical

import (
	"context"
	"fmt"
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

	p := NewMetastorePlanner(ms, 0)
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

func TestMetastorePlanner_Plan_PointersScansPerInnerMerge(t *testing.T) {
	start := time.Unix(10, 0)
	end := start.Add(time.Hour)

	makePaths := func(n int) []string {
		paths := make([]string, n)
		for i := 0; i < n; i++ {
			paths[i] = fmt.Sprintf("index/%d", i)
		}
		return paths
	}

	for _, tc := range []struct {
		name                       string
		paths                      []string
		pointersScansPerInnerMerge int
		expectSingleLevel          bool
		expectScanSetTargetCount   int   // for single-level: targets in the one ScanSet
		expectTargetsPerInner      []int // for two-level: targets per inner Merge's ScanSet
	}{
		{
			name:                       "fan-in 0 single level",
			paths:                      makePaths(2),
			pointersScansPerInnerMerge: 0,
			expectSingleLevel:         true,
			expectScanSetTargetCount:   2,
		},
		{
			name:                       "fan-in 50 with 100 indexes two level",
			paths:                      makePaths(100),
			pointersScansPerInnerMerge: 50,
			expectTargetsPerInner:      []int{50, 50},
		},
		{
			name:                       "fan-in 50 with 25 indexes one inner merge",
			paths:                      makePaths(25),
			pointersScansPerInnerMerge: 50,
			expectTargetsPerInner:      []int{25},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ms := fakeMetastoreIndexes{indexPaths: tc.paths}
			p := NewMetastorePlanner(ms, tc.pointersScansPerInnerMerge)
			plan, err := p.Plan(context.Background(), nil, nil, start, end)
			require.NoError(t, err)

			root, err := plan.Root()
			require.NoError(t, err)
			require.IsType(t, &Merge{}, root)

			children := plan.Children(root)
			if tc.expectSingleLevel {
				require.Len(t, children, 1)
				require.IsType(t, &Parallelize{}, children[0])
				parallelChildren := plan.Children(children[0])
				require.Len(t, parallelChildren, 1)
				set := parallelChildren[0].(*ScanSet)
				require.Len(t, set.Targets, tc.expectScanSetTargetCount)
			} else {
				require.Len(t, children, len(tc.expectTargetsPerInner))
				for i, child := range children {
					require.IsType(t, &Merge{}, child)
					innerMerge := child.(*Merge)
					innerChildren := plan.Children(innerMerge)
					require.Len(t, innerChildren, 1)
					require.IsType(t, &Parallelize{}, innerChildren[0])
					parallelChildren := plan.Children(innerChildren[0])
					require.Len(t, parallelChildren, 1)
					set := parallelChildren[0].(*ScanSet)
					require.Len(t, set.Targets, tc.expectTargetsPerInner[i])
				}
			}
		})
	}
}
