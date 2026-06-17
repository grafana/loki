package postings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBloomAggregator_TimeRange(t *testing.T) {
	a := newBloomAggregator()

	min, max := a.TimeRange()
	require.True(t, min.IsZero(), "empty aggregator min must be zero")
	require.True(t, max.IsZero(), "empty aggregator max must be zero")

	// Prepared but never observed: must NOT contribute a range.
	a.PrepareColumn("/a", 0, "svc", 16)
	min, max = a.TimeRange()
	require.True(t, min.IsZero(), "prepared-but-unobserved min must be zero")
	require.True(t, max.IsZero(), "prepared-but-unobserved max must be zero")

	base := time.Unix(2000, 0).UTC()
	require.NoError(t, a.Observe(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "v1", StreamID: 1, Timestamp: base}))
	require.NoError(t, a.Observe(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "v2", StreamID: 2, Timestamp: base.Add(-300 * time.Second)}))

	min, max = a.TimeRange()
	require.Equal(t, base.Add(-300*time.Second), min)
	require.Equal(t, base, max)

	a.Reset()
	min, max = a.TimeRange()
	require.True(t, min.IsZero(), "after Reset min must be zero")
	require.True(t, max.IsZero(), "after Reset max must be zero")
}

func TestBloomAggregator_TimeRange_ObserveAtUnixEpoch(t *testing.T) {
	a := newBloomAggregator()
	epoch := time.Unix(0, 0).UTC()

	a.PrepareColumn("/a", 0, "svc", 16)
	require.NoError(t, a.Observe(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "v", StreamID: 1, Timestamp: epoch}))

	min, max := a.TimeRange()
	// A genuine observation at the Unix epoch is recorded, not mistaken for
	// "no observations".
	require.False(t, min.IsZero(), "epoch observation must not read as empty")
	require.Equal(t, epoch, min)
	require.Equal(t, epoch, max)
}
