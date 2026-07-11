package metastore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func columnWithLogical(logical string) dataset.Column {
	return &dataset.MemColumn{
		Desc: dataset.ColumnDesc{Type: dataset.ColumnType{Logical: logical}},
	}
}

// TestSectionStatsTracker_OnColumnPredicateBuilt verifies that only the
// "column_name" column is tracked, that its total page count is treated as
// idempotent, and that relevant pages accumulate across predicate builds.
func TestSectionStatsTracker_OnColumnPredicateBuilt(t *testing.T) {
	nameCol := columnWithLogical(postings.ColumnTypeColumnName.String())
	otherCol := columnWithLogical(postings.ColumnTypeLabelValue.String())

	st := &sectionStatsTracker{}

	st.OnColumnPredicateBuilt(nameCol, dataset.ColumnReadPageStats{Total: 5, Relevant: 2})
	st.OnColumnPredicateBuilt(nameCol, dataset.ColumnReadPageStats{Total: 5, Relevant: 3})

	require.Equal(t, uint64(5), st.columnNamePages.Total, "total is set per build, not summed")
	require.Equal(t, uint64(5), st.columnNamePages.Relevant, "relevant accumulates across builds")

	// A non-column_name column must not affect the counters.
	st.OnColumnPredicateBuilt(otherCol, dataset.ColumnReadPageStats{Total: 99, Relevant: 99})

	require.Equal(t, uint64(5), st.columnNamePages.Total)
	require.Equal(t, uint64(5), st.columnNamePages.Relevant)
}

// TestStatsTracker_Report verifies that Report sums page counts across sections
// and records label and bloom reads into their own separate statistics.
func TestStatsTracker_Report(t *testing.T) {
	nameCol := columnWithLogical(postings.ColumnTypeColumnName.String())

	st := newStatsTracker(2)
	st.SectionLabelTracker(0).OnColumnPredicateBuilt(nameCol, dataset.ColumnReadPageStats{Total: 10, Relevant: 3})
	st.SectionLabelTracker(1).OnColumnPredicateBuilt(nameCol, dataset.ColumnReadPageStats{Total: 20, Relevant: 5})
	st.SectionBloomTracker(0).OnColumnPredicateBuilt(nameCol, dataset.ColumnReadPageStats{Total: 7, Relevant: 1})

	ctx, capture := xcap.NewCapture(context.Background(), nil)
	ctx, _ = xcap.StartRegion(ctx, "test")

	st.Report(ctx)

	require.Equal(t, int64(30), xcap.Value[int64](capture, StatPostingsLabelColumnNameTotalPages))
	require.Equal(t, int64(8), xcap.Value[int64](capture, StatPostingsLabelColumnNameRelevantPages))
	require.Equal(t, int64(7), xcap.Value[int64](capture, StatPostingsBloomColumnNameTotalPages),
		"bloom totals are independent of label totals")
	require.Equal(t, int64(1), xcap.Value[int64](capture, StatPostingsBloomColumnNameRelevantPages))
}

// TestStatsTracker_Report_NoRegion verifies Report is a no-op (rather than a
// panic) when the context carries no xcap region.
func TestStatsTracker_Report_NoRegion(t *testing.T) {
	st := newStatsTracker(0)
	require.NotPanics(t, func() {
		st.Report(context.Background())
	})
}
