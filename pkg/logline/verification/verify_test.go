package verification

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
)

var base = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

func TestRangeCoversTimestamp(t *testing.T) {
	r := hintprovider.HintTimeRange{Start: base, End: base.Add(10 * time.Minute)}

	require.True(t, RangeCoversTimestamp(r, base), "start is inclusive")
	require.True(t, RangeCoversTimestamp(r, base.Add(5*time.Minute)), "middle is covered")
	require.True(t, RangeCoversTimestamp(r, base.Add(10*time.Minute)), "end boundary is covered")
	require.False(t, RangeCoversTimestamp(r, base.Add(-1*time.Minute)), "before start")
	require.False(t, RangeCoversTimestamp(r, base.Add(11*time.Minute)), "after end")
}

func TestTimestampCovered(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute)},
		{Start: base.Add(20 * time.Minute), End: base.Add(30 * time.Minute)},
	}

	require.True(t, TimestampCovered(ranges, base.Add(5*time.Minute)))
	require.True(t, TimestampCovered(ranges, base.Add(25*time.Minute)))
	require.False(t, TimestampCovered(ranges, base.Add(15*time.Minute)), "gap between ranges")
	require.False(t, TimestampCovered(nil, base), "nil ranges")
}

func TestVerifyEntries_AllCovered(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute)},
	}
	timestamps := []time.Time{base.Add(2 * time.Minute), base.Add(5 * time.Minute)}
	r := VerifyEntries(ranges, timestamps)

	require.Equal(t, 2, r.TotalEntries)
	require.Equal(t, 2, r.CoveredEntries)
	require.Equal(t, 0, r.FalseNegatives)
	require.True(t, r.Correct())
	require.Empty(t, r.FalseNegativeTimestamps)
}

func TestVerifyEntries_WithFalseNegatives(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute)},
	}
	uncovered := base.Add(20 * time.Minute)
	timestamps := []time.Time{base.Add(5 * time.Minute), uncovered}
	r := VerifyEntries(ranges, timestamps)

	require.Equal(t, 2, r.TotalEntries)
	require.Equal(t, 1, r.CoveredEntries)
	require.Equal(t, 1, r.FalseNegatives)
	require.False(t, r.Correct())
	require.Equal(t, []time.Time{uncovered}, r.FalseNegativeTimestamps)
}

func TestVerifyEntries_EmptyEntries(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute)},
	}
	r := VerifyEntries(ranges, nil)

	require.Equal(t, 0, r.TotalEntries)
	require.Equal(t, 0, r.FalseNegatives)
	require.True(t, r.Correct())
}

func TestCountFalsePositives(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute)},
		{Start: base.Add(20 * time.Minute), End: base.Add(30 * time.Minute)},
	}
	timestamps := []time.Time{base.Add(5 * time.Minute)}
	fp := CountFalsePositives(ranges, timestamps)
	require.Equal(t, 1, fp, "second range has no entries")
}

// shard3Fingerprint is a stream fingerprint in document shard 3 of 32.
const shard3Fingerprint = uint64(3) << 59

func TestRangeCoversEntry(t *testing.T) {
	allStreams := hintprovider.HintTimeRange{Start: base, End: base.Add(10 * time.Minute)}
	shard3 := hintprovider.HintTimeRange{Start: base, End: base.Add(10 * time.Minute), DocumentShard: 3, DocumentShardBits: 5}
	entry := Entry{Timestamp: base.Add(5 * time.Minute), Fingerprint: shard3Fingerprint}

	require.True(t, RangeCoversEntry(allStreams, entry), "zero shard bits cover every stream")
	require.True(t, RangeCoversEntry(shard3, entry), "same document shard")
	require.False(t, RangeCoversEntry(shard3, Entry{Timestamp: entry.Timestamp, Fingerprint: uint64(4) << 59}), "other document shard")
	require.False(t, RangeCoversEntry(shard3, Entry{Timestamp: base.Add(11 * time.Minute), Fingerprint: shard3Fingerprint}), "after end")
}

func TestVerifyStreamEntries(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: base, End: base.Add(10 * time.Minute), DocumentShard: 3, DocumentShardBits: 5},
		{Start: base.Add(20 * time.Minute), End: base.Add(30 * time.Minute), DocumentShard: 4, DocumentShardBits: 5},
	}
	wrongShard := base.Add(25 * time.Minute)
	entries := []Entry{
		{Timestamp: base.Add(5 * time.Minute), Fingerprint: shard3Fingerprint},
		{Timestamp: wrongShard, Fingerprint: shard3Fingerprint},
	}
	r := VerifyStreamEntries(ranges, entries)

	require.Equal(t, 2, r.HintRanges)
	require.Equal(t, 2, r.TotalEntries)
	require.Equal(t, 1, r.CoveredEntries)
	require.Equal(t, 1, r.FalseNegatives)
	require.Equal(t, []time.Time{wrongShard}, r.FalseNegativeTimestamps)
	require.Equal(t, 1, CountStreamFalsePositives(ranges, entries), "the shard 4 range covers no entry's stream")
}
