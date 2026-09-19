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
	indexes []metastore.IndexEntry
}

func (f fakeMetastoreIndexes) GetIndexes(_ context.Context, _ metastore.GetIndexesRequest) (metastore.GetIndexesResponse, error) {
	return metastore.GetIndexesResponse{Indexes: f.indexes}, nil
}

func TestPointersScan_Clone_AllowsNilSelector(t *testing.T) {
	scan := &PointersScan{
		NodeID:   ulid.Make(),
		Location: DataObjLocation("index/0"),
	}

	cloned := scan.Clone().(*PointersScan)
	require.Nil(t, cloned.Selector)
}

func TestMetastorePlanner_Plan_ClampsScanTimeRange(t *testing.T) {
	// Queries typically span multiple hours while individual index objects cover
	// shorter windows. The common case is that the index range sits fully inside
	// the query range and the scan uses the index boundaries unchanged.
	// Clamping only kicks in for the boundary objects whose range extends outside
	// the query window.
	queryStart := time.Unix(0, 0).UTC()
	queryEnd := queryStart.Add(6 * time.Hour)

	tests := []struct {
		name          string
		indexStart    time.Time
		indexEnd      time.Time
		wantScanStart time.Time
		wantScanEnd   time.Time
	}{
		{
			name:          "index within query range (common case, no clamping)",
			indexStart:    queryStart.Add(1 * time.Hour),
			indexEnd:      queryStart.Add(2 * time.Hour),
			wantScanStart: queryStart.Add(1 * time.Hour),
			wantScanEnd:   queryStart.Add(2 * time.Hour),
		},
		{
			name:          "index starts before query start (start clamped to query start)",
			indexStart:    queryStart.Add(-1 * time.Hour),
			indexEnd:      queryStart.Add(1 * time.Hour),
			wantScanStart: queryStart,
			wantScanEnd:   queryStart.Add(1 * time.Hour),
		},
		{
			name:          "index ends after query end (end clamped to query end)",
			indexStart:    queryEnd.Add(-1 * time.Hour),
			indexEnd:      queryEnd.Add(1 * time.Hour),
			wantScanStart: queryEnd.Add(-1 * time.Hour),
			wantScanEnd:   queryEnd,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ms := fakeMetastoreIndexes{
				indexes: []metastore.IndexEntry{
					{Path: "index/0", Start: tc.indexStart, End: tc.indexEnd},
				},
			}

			p := NewMetastorePlanner(ms, 100)
			plan, err := p.Plan(context.Background(), nil, nil, queryStart, queryEnd)
			require.NoError(t, err)

			root, err := plan.Root()
			require.NoError(t, err)

			// Unwrap Batching → Merge → Parallelize → ScanSet
			merge := plan.Children(root)
			require.Len(t, merge, 1)
			parallelize := plan.Children(merge[0])
			require.Len(t, parallelize, 1)
			scanSetNodes := plan.Children(parallelize[0])
			require.Len(t, scanSetNodes, 1)
			set := scanSetNodes[0].(*ScanSet)
			require.Len(t, set.Targets, 1)

			scan := set.Targets[0].Pointers
			require.Equal(t, tc.wantScanStart, scan.Start)
			require.Equal(t, tc.wantScanEnd, scan.End)
		})
	}
}

func TestMetastorePlanner_Plan_UsesMergeRootAndPointersTargets(t *testing.T) {
	start := time.Unix(10, 0)
	end := start.Add(time.Hour)

	ms := fakeMetastoreIndexes{
		indexes: []metastore.IndexEntry{
			{Path: "index/0", Start: start, End: end},
			{Path: "index/1", Start: start, End: end},
		},
	}

	p := NewMetastorePlanner(ms, 100)
	plan, err := p.Plan(context.Background(), nil, nil, start, end)
	require.NoError(t, err)

	root, err := plan.Root()
	require.NoError(t, err)
	require.IsType(t, &Batching{}, root)

	batchChildren := plan.Children(root)
	require.Len(t, batchChildren, 1)
	require.IsType(t, &Merge{}, batchChildren[0])

	children := plan.Children(batchChildren[0])
	require.Len(t, children, 1)
	require.IsType(t, &Parallelize{}, children[0])

	parallel := children[0]
	parallelChildren := plan.Children(parallel)
	require.Len(t, parallelChildren, 1)
	require.IsType(t, &ScanSet{}, parallelChildren[0])

	set := parallelChildren[0].(*ScanSet)
	require.Len(t, set.Targets, len(ms.indexes))

	for i, target := range set.Targets {
		require.Equal(t, ScanTypePointers, target.Type)
		require.NotNil(t, target.Pointers)
		require.Equal(t, DataObjLocation(ms.indexes[i].Path), target.Pointers.Location)
		require.Equal(t, start, target.Pointers.Start)
		require.Equal(t, end, target.Pointers.End)
	}
}
