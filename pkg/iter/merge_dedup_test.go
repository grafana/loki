package iter

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// Lines that automatic stream sharding sent to more than one shard arrive
// within a single iterator when only the store or only the ingesters are
// queried, because those paths sort-merge the shard streams before the merge
// iterator runs. The merge iterators must deduplicate them there too.

func TestMergeEntryIteratorDedupesWithinSingleIterator(t *testing.T) {
	stream := func(entries ...logproto.Entry) EntryIterator {
		return NewStreamIterator(logproto.Stream{Labels: `{app="foo"}`, Hash: 42, Entries: entries})
	}
	dup := logproto.Entry{Timestamp: time.Unix(0, 1), Line: "dup"}
	other := logproto.Entry{Timestamp: time.Unix(0, 1), Line: "other"}
	later := logproto.Entry{Timestamp: time.Unix(0, 2), Line: "later"}

	for _, tc := range []struct {
		name    string
		entries []logproto.Entry
		want    []string
	}{
		{"adjacent duplicate", []logproto.Entry{dup, dup, later}, []string{"dup", "later"}},
		{"duplicate split by another line at the same timestamp", []logproto.Entry{dup, other, dup}, []string{"dup", "other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := NewMergeEntryIterator(context.Background(), []EntryIterator{stream(tc.entries...)}, logproto.FORWARD)
			var got []string
			for it.Next() {
				got = append(got, it.At().Line)
			}
			require.NoError(t, it.Err())
			require.Equal(t, tc.want, got)
		})
	}
}

func TestMergeSampleIteratorDedupesWithinSingleIterator(t *testing.T) {
	series := func(samples ...logproto.Sample) SampleIterator {
		return NewSeriesIterator(logproto.Series{Labels: `{app="foo"}`, StreamHash: 42, Samples: samples})
	}
	dup := logproto.Sample{Timestamp: 1, Value: 1, Hash: 99}
	other := logproto.Sample{Timestamp: 1, Value: 1, Hash: 77}
	later := logproto.Sample{Timestamp: 2, Value: 1, Hash: 55}

	for _, tc := range []struct {
		name string
		its  []SampleIterator
		want []uint64
	}{
		{"adjacent duplicate", []SampleIterator{series(dup, dup, later)}, []uint64{99, 55}},
		{"duplicate split by another sample at the same timestamp", []SampleIterator{series(dup, other, dup)}, []uint64{99, 77}},
		// A zero hash means the duplicate state is unknown, so nothing is dropped.
		{"zero hash is not deduplicated", []SampleIterator{series(
			logproto.Sample{Timestamp: 1, Value: 1, Hash: 0},
			logproto.Sample{Timestamp: 1, Value: 1, Hash: 0},
		)}, []uint64{0, 0}},
		// Once all but one iterator are exhausted, the merge drains the last one
		// through a fast path which must keep deduplicating.
		{"duplicates while draining the last iterator", []SampleIterator{
			series(dup),
			series(dup, later, later),
		}, []uint64{99, 55}},
		// A duplicate inside one iterator must also be dropped while other
		// sources are still active, not only against the other iterators.
		{"within-iterator duplicate while another source is active", []SampleIterator{
			series(dup, dup),
			series(dup),
		}, []uint64{99}},
		{"within-iterator duplicate next to a distinct sample while another source is active", []SampleIterator{
			series(dup, other, dup),
			series(dup),
		}, []uint64{99, 77}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := NewMergeSampleIterator(context.Background(), tc.its)
			var got []uint64
			for it.Next() {
				got = append(got, it.At().Hash)
			}
			require.NoError(t, it.Err())
			require.Equal(t, tc.want, got)
		})
	}
}
