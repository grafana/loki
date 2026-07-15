package postings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLabelAggregator_TimeRange(t *testing.T) {
	a := newLabelAggregator()

	gotMin, gotMax := a.TimeRange()
	require.True(t, gotMin.IsZero(), "empty aggregator min must be zero")
	require.True(t, gotMax.IsZero(), "empty aggregator max must be zero")
	// Empty must be the zero time.Time, NOT the Unix epoch (guards against the
	// store-int64-and-convert anti-pattern).
	require.NotEqual(t, time.Unix(0, 0).UTC(), gotMin, "empty min must not be the Unix epoch")

	base := time.Unix(1000, 0).UTC()
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 1, Timestamp: base})
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 2, Timestamp: base.Add(-500 * time.Second)})
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "env", LabelValue: "y", StreamID: 3, Timestamp: base.Add(500 * time.Second)})

	gotMin, gotMax = a.TimeRange()
	require.Equal(t, base.Add(-500*time.Second), gotMin)
	require.Equal(t, base.Add(500*time.Second), gotMax)

	a.Reset()
	gotMin, gotMax = a.TimeRange()
	require.True(t, gotMin.IsZero(), "after Reset min must be zero")
	require.True(t, gotMax.IsZero(), "after Reset max must be zero")
}

func TestLabelAggregator_TimeRange_ObserveAtUnixEpoch(t *testing.T) {
	a := newLabelAggregator()
	epoch := time.Unix(0, 0).UTC()

	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 1, Timestamp: epoch})

	gotMin, gotMax := a.TimeRange()
	// A genuine observation at the Unix epoch is recorded, not mistaken for
	// "no observations".
	require.False(t, gotMin.IsZero(), "epoch observation must not read as empty")
	require.Equal(t, epoch, gotMin)
	require.Equal(t, epoch, gotMax)
}
