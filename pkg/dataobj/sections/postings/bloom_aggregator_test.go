package postings

import (
	"testing"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/stretchr/testify/require"
)

func TestBloomAggregator_TimeRange(t *testing.T) {
	a := newBloomAggregator()

	gotMin, gotMax := a.TimeRange()
	require.True(t, gotMin.IsZero(), "empty aggregator min must be zero")
	require.True(t, gotMax.IsZero(), "empty aggregator max must be zero")

	// Prepared but never observed: must NOT contribute a range.
	a.PrepareColumn("/a", 0, "svc", 16, 0)
	gotMin, gotMax = a.TimeRange()
	require.True(t, gotMin.IsZero(), "prepared-but-unobserved min must be zero")
	require.True(t, gotMax.IsZero(), "prepared-but-unobserved max must be zero")

	base := time.Unix(2000, 0).UTC()
	require.NoError(t, a.Observe(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "v1", StreamID: 1, Timestamp: base}))
	require.NoError(t, a.Observe(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "v2", StreamID: 2, Timestamp: base.Add(-300 * time.Second)}))

	gotMin, gotMax = a.TimeRange()
	require.Equal(t, base.Add(-300*time.Second), gotMin)
	require.Equal(t, base, gotMax)

	a.Reset()
	gotMin, gotMax = a.TimeRange()
	require.True(t, gotMin.IsZero(), "after Reset min must be zero")
	require.True(t, gotMax.IsZero(), "after Reset max must be zero")
}

func TestBloomAggregator_Observe(t *testing.T) {
	values := []string{"a", "b", "a", "c", "b", "a"}
	observe := func(t *testing.T, estimatedCardinality uint) *bloomPostingEntry {
		t.Helper()
		a := newBloomAggregator()
		a.PrepareColumn("/a", 0, "svc", estimatedCardinality, 0)
		for i, value := range values {
			require.NoError(t, a.Observe(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: value, StreamID: int64(i + 1), Timestamp: time.Unix(1, 0)}))
		}
		return a.entries[bloomPostingKey{objectPath: "/a", sectionIndex: 0, columnName: "svc"}]
	}
	distinctFilter := func(estimatedCardinality uint) *bloom.BloomFilter {
		filter := bloom.NewWithEstimates(estimatedCardinality, 1.0/128.0)
		for _, value := range []string{"a", "b", "c"} {
			filter.Add([]byte(value))
		}
		return filter
	}

	t.Run("builds the same filter as adding each distinct value once when values repeat", func(t *testing.T) {
		entry := observe(t, 3)
		require.NotNil(t, entry.added, "a low-cardinality column must track added values")
		require.True(t, entry.bloomFilter.Equal(distinctFilter(3)))
	})

	t.Run("adds every value to the filter of a high-cardinality column", func(t *testing.T) {
		entry := observe(t, maxAddedValues+1)
		require.Nil(t, entry.added, "a high-cardinality column must not track added values")
		require.True(t, entry.bloomFilter.Equal(distinctFilter(maxAddedValues+1)))
	})
}

func TestBloomAggregator_PrepareColumn(t *testing.T) {
	t.Run("sizes the bloom filter of a column with no estimated values for one value", func(t *testing.T) {
		a := newBloomAggregator()
		a.PrepareColumn("/a", 0, "svc", 0, 0)
		require.NoError(t, a.Observe(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "", StreamID: 1, Timestamp: time.Unix(1, 0)}))

		entry := a.entries[bloomPostingKey{objectPath: "/a", sectionIndex: 0, columnName: "svc"}]
		require.Equal(t, bloom.NewWithEstimates(1, 1.0/128.0).K(), entry.bloomFilter.K())
	})
}

func TestBloomAggregator_TimeRange_ObserveAtUnixEpoch(t *testing.T) {
	a := newBloomAggregator()
	epoch := time.Unix(0, 0).UTC()

	a.PrepareColumn("/a", 0, "svc", 16, 0)
	require.NoError(t, a.Observe(BloomObservation{ObjectPath: "/a", SectionIndex: 0, ColumnName: "svc", Value: "v", StreamID: 1, Timestamp: epoch}))

	gotMin, gotMax := a.TimeRange()
	// A genuine observation at the Unix epoch is recorded, not mistaken for
	// "no observations".
	require.False(t, gotMin.IsZero(), "epoch observation must not read as empty")
	require.Equal(t, epoch, gotMin)
	require.Equal(t, epoch, gotMax)
}
