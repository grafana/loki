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
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 1, Time: At(base)})
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 2, Time: At(base.Add(-500 * time.Second))})
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "env", LabelValue: "y", StreamID: 3, Time: At(base.Add(500 * time.Second))})

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

	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 1, Time: At(epoch)})

	gotMin, gotMax := a.TimeRange()
	// A genuine observation at the Unix epoch is recorded, not mistaken for
	// "no observations".
	require.False(t, gotMin.IsZero(), "epoch observation must not read as empty")
	require.Equal(t, epoch, gotMin)
	require.Equal(t, epoch, gotMax)
}

func TestLabelAggregator_ShardBucketRange(t *testing.T) {
	a := newLabelAggregator()
	ts := time.Unix(1, 0).UTC()
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 1, Time: At(ts), ShardBucket: 4})
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 2, Time: At(ts), ShardBucket: 1})
	a.Observe(LabelObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "app", LabelValue: "x", StreamID: 3, Time: At(ts), ShardBucket: 9})

	entries := a.Entries()
	require.Len(t, entries, 1)
	require.Equal(t, uint32(1), entries[0].MinShardBucket)
	require.Equal(t, uint32(9), entries[0].MaxShardBucket)
}

func TestLabelAggregator_Observe(t *testing.T) {
	t.Run("records both ends of the observed time range", func(t *testing.T) {
		a := newLabelAggregator()
		start := time.Unix(10, 0).UTC()
		end := time.Unix(30, 0).UTC()

		a.Observe(LabelObservation{ObjectPath: "/a", ColumnName: "app", LabelValue: "x", StreamID: 1, Time: At(end).Extend(start), UncompressedSize: 7})

		entries := a.Entries()
		require.Len(t, entries, 1)
		require.Equal(t, start.UnixNano(), entries[0].MinTimestamp)
		require.Equal(t, end.UnixNano(), entries[0].MaxTimestamp)
		require.Equal(t, int64(7), entries[0].UncompressedSize)

		gotMin, gotMax := a.TimeRange()
		require.Equal(t, start, gotMin)
		require.Equal(t, end, gotMax)
	})

	t.Run("merges overlapping time ranges into their union", func(t *testing.T) {
		a := newLabelAggregator()
		t10, t20, t30, t40 := time.Unix(10, 0).UTC(), time.Unix(20, 0).UTC(), time.Unix(30, 0).UTC(), time.Unix(40, 0).UTC()

		a.Observe(LabelObservation{ObjectPath: "/a", ColumnName: "app", LabelValue: "x", StreamID: 1, Time: At(t20).Extend(t40), UncompressedSize: 3})
		a.Observe(LabelObservation{ObjectPath: "/a", ColumnName: "app", LabelValue: "x", StreamID: 2, Time: At(t10).Extend(t30), UncompressedSize: 4})

		entries := a.Entries()
		require.Len(t, entries, 1)
		require.Equal(t, t10.UnixNano(), entries[0].MinTimestamp)
		require.Equal(t, t40.UnixNano(), entries[0].MaxTimestamp)
		require.Equal(t, int64(7), entries[0].UncompressedSize)
	})
}
