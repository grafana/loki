package postings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLabelAggregator_TimeRange(t *testing.T) {
	a := newLabelAggregator()

	min, max := a.TimeRange()
	require.True(t, min.IsZero(), "empty aggregator min must be zero")
	require.True(t, max.IsZero(), "empty aggregator max must be zero")
	// Empty must be the zero time.Time, NOT the Unix epoch (guards against the
	// store-int64-and-convert anti-pattern).
	require.NotEqual(t, time.Unix(0, 0).UTC(), min, "empty min must not be the Unix epoch")

	base := time.Unix(1000, 0).UTC()
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 1, Timestamp: base})
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 2, Timestamp: base.Add(-500 * time.Second)})
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "env", LabelValue: "y", StreamID: 3, Timestamp: base.Add(500 * time.Second)})

	min, max = a.TimeRange()
	require.Equal(t, base.Add(-500*time.Second), min)
	require.Equal(t, base.Add(500*time.Second), max)

	a.Reset()
	min, max = a.TimeRange()
	require.True(t, min.IsZero(), "after Reset min must be zero")
	require.True(t, max.IsZero(), "after Reset max must be zero")
}

func TestLabelAggregator_TimeRange_ObserveAtUnixEpoch(t *testing.T) {
	a := newLabelAggregator()
	epoch := time.Unix(0, 0).UTC()

	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 1, Timestamp: epoch})

	min, max := a.TimeRange()
	// A genuine observation at the Unix epoch is recorded, not mistaken for
	// "no observations".
	require.False(t, min.IsZero(), "epoch observation must not read as empty")
	require.Equal(t, epoch, min)
	require.Equal(t, epoch, max)
}
