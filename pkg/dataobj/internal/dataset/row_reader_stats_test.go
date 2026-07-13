package dataset

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

type recordedPredicateBuild struct {
	logical string
	stats   ColumnReadPageStats
}

type recordingStatsTracker struct {
	calls []recordedPredicateBuild
}

func (r *recordingStatsTracker) OnColumnPredicateBuilt(col Column, stats ColumnReadPageStats) {
	r.calls = append(r.calls, recordedPredicateBuild{
		logical: col.ColumnDesc().Type.Logical,
		stats:   stats,
	})
}

// TestBuildColumnPredicateRanges_ReportsPageStats verifies that the stats
// tracker receives the column's total page count and only the pages that
// survive page-level pruning as relevant. Pages that lack min/max stats are
// always relevant, so they are counted even when the predicate would otherwise
// prune them.
func TestBuildColumnPredicateRanges_ReportsPageStats(t *testing.T) {
	ctx := context.Background()
	ds, cols := buildSingleColumnStatsDataset(t)

	tt := []struct {
		name         string
		predicate    Predicate
		wantRelevant uint64
	}{
		{
			// 50 falls inside page 0's [0,100] range; page 1 has no stats so it is
			// always relevant; page 2's [800,1000] range is pruned.
			name:         "matches a page with stats plus the no-stats page",
			predicate:    EqualPredicate{Column: cols[0], Value: Int64Value(50)},
			wantRelevant: 2,
		},
		{
			// 5000 is outside both pages with stats; only the no-stats page 1
			// remains relevant.
			name:         "matches only the no-stats page",
			predicate:    EqualPredicate{Column: cols[0], Value: Int64Value(5000)},
			wantRelevant: 1,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			tracker := &recordingStatsTracker{}
			r := NewRowReader(RowReaderOptions{
				Dataset:      ds,
				Columns:      cols,
				Predicates:   []Predicate{tc.predicate},
				StatsTracker: tracker,
			})
			defer r.Close()

			// initDownloader builds the predicate ranges once per predicate, which
			// is the same path the reader uses in production.
			require.NoError(t, r.initDownloader(ctx))

			require.Len(t, tracker.calls, 1, "tracker is notified once per column predicate")
			require.Equal(t, "number", tracker.calls[0].logical)
			require.Equal(t, uint64(3), tracker.calls[0].stats.Total, "total equals the column's page count")
			require.Equal(t, tc.wantRelevant, tracker.calls[0].stats.Relevant)
		})
	}
}

// TestBuildColumnPredicateRanges_NilStatsTracker verifies that range building
// tolerates the absence of a stats tracker.
func TestBuildColumnPredicateRanges_NilStatsTracker(t *testing.T) {
	ctx := context.Background()
	ds, cols := buildSingleColumnStatsDataset(t)

	r := NewRowReader(RowReaderOptions{
		Dataset:    ds,
		Columns:    cols,
		Predicates: []Predicate{EqualPredicate{Column: cols[0], Value: Int64Value(50)}},
		// StatsTracker intentionally left nil.
	})
	defer r.Close()

	require.NotPanics(t, func() {
		require.NoError(t, r.initDownloader(ctx))
	})
}

// buildSingleColumnStatsDataset builds a one-column dataset with three pages:
// two carry min/max stats and the middle page carries none.
func buildSingleColumnStatsDataset(t *testing.T) (Dataset, []Column) {
	t.Helper()

	dset := FromMemory([]*MemColumn{
		{
			Desc: ColumnDesc{
				Tag:        "value",
				Type:       ColumnType{Physical: datasetmd.PhysicalType_PHYSICAL_TYPE_INT64, Logical: "number"},
				RowsCount:  1000,
				PagesCount: 3,
			},
			Pages: []*MemPage{
				{
					Desc: PageDesc{
						RowCount: 250, // 0 - 249
						Stats: &datasetmd.Statistics{
							MinValue: encodeInt64Value(t, 0),
							MaxValue: encodeInt64Value(t, 100),
						},
					},
				},
				{
					Desc: PageDesc{
						RowCount: 500, // 250 - 749, no stats
					},
				},
				{
					Desc: PageDesc{
						RowCount: 250, // 750 - 999
						Stats: &datasetmd.Statistics{
							MinValue: encodeInt64Value(t, 800),
							MaxValue: encodeInt64Value(t, 1000),
						},
					},
				},
			},
		},
	})

	cols, err := result.Collect(dset.ListColumns(context.Background()))
	require.NoError(t, err)

	return dset, cols
}
