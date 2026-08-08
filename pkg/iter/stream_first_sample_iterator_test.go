package iter

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// TestNewStreamFirstMergeSampleIterator_OrderingAndDedup verifies stream-first ordering
// (streamHash ASC, timestamp ASC) and that replicas (same streamHash+timestamp+hash) collapse.
func TestNewStreamFirstMergeSampleIterator_OrderingAndDedup(t *testing.T) {
	// Distinct per-stream values surface any sample attributed to the wrong stream.
	a := mkStreamSeries(`{s="a"}`, 30, mkSample(1, 1), mkSample(2, 2), mkSample(3, 3))
	b := mkStreamSeries(`{s="b"}`, 10, mkSample(1, 4), mkSample(2, 5), mkSample(3, 6))
	c := mkStreamSeries(`{s="c"}`, 20, mkSample(1, 7), mkSample(2, 8), mkSample(3, 9))

	it := NewStreamFirstMergeSampleIterator(context.Background(), []SampleIterator{
		NewSeriesIterator(a), NewSeriesIterator(b), NewSeriesIterator(c),
		// replicas: same content, must be deduplicated away.
		NewSeriesIterator(a), NewSeriesIterator(b), NewSeriesIterator(c),
	})

	want := []sampleWithLabels{
		{Sample: mkSample(1, 4), labels: `{s="b"}`, streamHash: 10}, {Sample: mkSample(2, 5), labels: `{s="b"}`, streamHash: 10}, {Sample: mkSample(3, 6), labels: `{s="b"}`, streamHash: 10},
		{Sample: mkSample(1, 7), labels: `{s="c"}`, streamHash: 20}, {Sample: mkSample(2, 8), labels: `{s="c"}`, streamHash: 20}, {Sample: mkSample(3, 9), labels: `{s="c"}`, streamHash: 20},
		{Sample: mkSample(1, 1), labels: `{s="a"}`, streamHash: 30}, {Sample: mkSample(2, 2), labels: `{s="a"}`, streamHash: 30}, {Sample: mkSample(3, 3), labels: `{s="a"}`, streamHash: 30},
	}
	require.Equal(t, want, collectSamplesWithLabels(t, it))
}

// TestNewStreamFirstMergeSampleIterator_DistinctHashesKept verifies that two samples in the same
// stream at the same timestamp but with different Sample.Hash are NOT deduplicated.
func TestNewStreamFirstMergeSampleIterator_DistinctHashesKept(t *testing.T) {
	// Same (streamHash, timestamp) but distinct Sample.Hash: both must survive dedup.
	s1 := mkStreamSeries(`{s="a"}`, 5, logproto.Sample{Timestamp: 1, Hash: 100, Value: 1})
	s2 := mkStreamSeries(`{s="a"}`, 5, logproto.Sample{Timestamp: 1, Hash: 200, Value: 2})

	it := NewStreamFirstMergeSampleIterator(context.Background(), []SampleIterator{
		NewSeriesIterator(s1), NewSeriesIterator(s2),
	})

	want := []sampleWithLabels{
		{Sample: logproto.Sample{Timestamp: 1, Hash: 100, Value: 1}, labels: `{s="a"}`, streamHash: 5},
		{Sample: logproto.Sample{Timestamp: 1, Hash: 200, Value: 2}, labels: `{s="a"}`, streamHash: 5},
	}
	require.ElementsMatch(t, want, collectSamplesWithLabels(t, it))
}

// TestStreamFirstMergeMatchesTimestampFirstMergeDedup verifies that the stream-first merge produces
// the same deduplicated set of samples as the timestamp-first merge (only the order differs).
func TestStreamFirstMergeMatchesTimestampFirstMergeDedup(t *testing.T) {
	build := func() []SampleIterator {
		a := mkStreamSeries(`{s="a"}`, 30, mkSample(1, 11), mkSample(2, 12), mkSample(5, 15))
		b := mkStreamSeries(`{s="b"}`, 10, mkSample(2, 22), mkSample(3, 23), mkSample(4, 24))
		c := mkStreamSeries(`{s="c"}`, 20, mkSample(1, 31), mkSample(4, 34))
		return []SampleIterator{
			NewSeriesIterator(a), NewSeriesIterator(b), NewSeriesIterator(c),
			NewSeriesIterator(a), NewSeriesIterator(c), // partial replicas
		}
	}

	// Sort on (streamHash, ts) so the two merges' differing orders compare equal.
	sortByStream := func(s []sampleWithLabels) []sampleWithLabels {
		sort.Slice(s, func(i, j int) bool {
			if s[i].streamHash != s[j].streamHash {
				return s[i].streamHash < s[j].streamHash
			}
			return s[i].Timestamp < s[j].Timestamp
		})
		return s
	}

	timestampMergeResult := sortByStream(collectSamplesWithLabels(t, NewTimestampFirstMergeSampleIterator(context.Background(), build())))
	streamMergeResult := sortByStream(collectSamplesWithLabels(t, NewStreamFirstMergeSampleIterator(context.Background(), build())))
	require.Equal(t, timestampMergeResult, streamMergeResult)
}

// TestNewStreamFirstMergeSampleIterator_HashCollisionKeepsStreamsGrouped verifies that when two
// distinct streams (different labels) collide on the same streamHash, the merge keeps each stream's
// samples contiguous — ordering by labels before timestamp — instead of interleaving them by time.
func TestNewStreamFirstMergeSampleIterator_HashCollisionKeepsStreamsGrouped(t *testing.T) {
	// Injecting the collision directly (rather than hunting a real StableHash collision) is enough:
	// the heap orders on the iterator's StreamHash(), which we set equal for two different labels.
	const collidingHash = 42
	a := mkStreamSeries(`{s="a"}`, collidingHash, mkSample(1, 1), mkSample(2, 2), mkSample(3, 3))
	b := mkStreamSeries(`{s="b"}`, collidingHash, mkSample(1, 4), mkSample(2, 5), mkSample(3, 6))

	it := NewStreamFirstMergeSampleIterator(context.Background(), []SampleIterator{
		NewSeriesIterator(a), NewSeriesIterator(b),
	})

	// Each stream stays contiguous (not interleaved by timestamp), and no sample is dropped.
	want := []sampleWithLabels{
		{Sample: mkSample(1, 1), labels: `{s="a"}`, streamHash: collidingHash}, {Sample: mkSample(2, 2), labels: `{s="a"}`, streamHash: collidingHash}, {Sample: mkSample(3, 3), labels: `{s="a"}`, streamHash: collidingHash},
		{Sample: mkSample(1, 4), labels: `{s="b"}`, streamHash: collidingHash}, {Sample: mkSample(2, 5), labels: `{s="b"}`, streamHash: collidingHash}, {Sample: mkSample(3, 6), labels: `{s="b"}`, streamHash: collidingHash},
	}
	require.Equal(t, want, collectSamplesWithLabels(t, it))
}

// TestNewStreamFirstMergeSampleIterator_HashCollisionNotDeduped verifies that two distinct streams
// (different labels) colliding on streamHash are not deduplicated against each other even when
// their samples also share the same Sample.Hash at the same timestamp.
func TestNewStreamFirstMergeSampleIterator_HashCollisionNotDeduped(t *testing.T) {
	const (
		collidingHash    = 42
		sharedSampleHash = 7
	)
	// Same streamHash, same Sample.Hash, same timestamp — differing only in labels.
	a := mkStreamSeries(`{s="a"}`, collidingHash, logproto.Sample{Timestamp: 1, Hash: sharedSampleHash, Value: 1})
	b := mkStreamSeries(`{s="b"}`, collidingHash, logproto.Sample{Timestamp: 1, Hash: sharedSampleHash, Value: 2})

	it := NewStreamFirstMergeSampleIterator(context.Background(), []SampleIterator{
		NewSeriesIterator(a), NewSeriesIterator(b),
	})

	// Both survive: distinct streams are never merged, regardless of Sample.Hash.
	want := []sampleWithLabels{
		{Sample: logproto.Sample{Timestamp: 1, Hash: sharedSampleHash, Value: 1}, labels: `{s="a"}`, streamHash: collidingHash},
		{Sample: logproto.Sample{Timestamp: 1, Hash: sharedSampleHash, Value: 2}, labels: `{s="b"}`, streamHash: collidingHash},
	}
	require.Equal(t, want, collectSamplesWithLabels(t, it))
}

func mkStreamSeries(labels string, hash uint64, samples ...logproto.Sample) logproto.Series {
	return logproto.Series{Labels: labels, StreamHash: hash, Samples: samples}
}

// mkSample builds a sample at ts carrying a distinct value, with Hash derived from the value so
// identical copies (replicas) still collapse while samples from different streams stay
// distinguishable — surfacing any sample attributed to the wrong stream.
func mkSample(ts int64, value float64) logproto.Sample {
	return logproto.Sample{Timestamp: ts, Hash: uint64(value), Value: value}
}

// collectSamplesWithLabels drains it into the (sample, labels, streamHash) triples it produced.
func collectSamplesWithLabels(t *testing.T, it SampleIterator) []sampleWithLabels {
	t.Helper()
	var got []sampleWithLabels
	for it.Next() {
		got = append(got, sampleWithLabels{Sample: it.At(), labels: it.Labels(), streamHash: it.StreamHash()})
	}
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())
	return got
}
