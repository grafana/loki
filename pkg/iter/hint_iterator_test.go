package iter

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestNewHintTimeRanges(t *testing.T) {
	base := time.Unix(100, 0)
	hints := []logproto.HintTimeRange{
		{Start: base.Add(4 * time.Millisecond), End: base.Add(6 * time.Millisecond)},
		{Start: base.Add(-time.Millisecond), End: base.Add(2 * time.Millisecond)},
		{Start: base.Add(2 * time.Millisecond), End: base.Add(4 * time.Millisecond)},
		{Start: base.Add(8 * time.Millisecond), End: base.Add(9 * time.Millisecond)},
		{Start: base.Add(3 * time.Millisecond), End: base.Add(3 * time.Millisecond)},
	}
	original := append([]logproto.HintTimeRange(nil), hints...)

	got := NewHintTimeRanges(hints, base, base.Add(6*time.Millisecond))

	require.Equal(t, HintTimeRanges{
		enabled: true,
		ranges: []hintTimeRange{{
			start: base.UnixNano(),
			end:   base.Add(6 * time.Millisecond).UnixNano(),
		}},
	}, got)
	require.Equal(t, original, hints)

	from, through, ok := got.Bounds()
	require.True(t, ok)
	require.Equal(t, base, from)
	require.Equal(t, base.Add(6*time.Millisecond), through)
}

func TestNewHintTimeRangesClipsBeforeUnixNanoConversion(t *testing.T) {
	from := time.Unix(100, 0)
	through := from.Add(time.Hour)

	ranges := NewHintTimeRanges([]logproto.HintTimeRange{{
		Start: time.Time{},
		End:   time.Date(3000, time.January, 1, 0, 0, 0, 0, time.UTC),
	}}, from, through)

	gotFrom, gotThrough, ok := ranges.Bounds()
	require.True(t, ok)
	require.Equal(t, from, gotFrom)
	require.Equal(t, through, gotThrough)
}

func TestHintTimeRangesOverlapSemantics(t *testing.T) {
	base := time.Unix(100, 0)
	ranges := NewHintTimeRanges(
		[]logproto.HintTimeRange{{
			Start: base.Add(time.Millisecond),
			End:   base.Add(2 * time.Millisecond),
		}},
		base,
		base.Add(3*time.Millisecond),
	)

	require.True(t, ranges.Overlaps(base, base.Add(time.Millisecond+time.Nanosecond)))
	require.False(t, ranges.Overlaps(base, base.Add(time.Millisecond)))
	require.True(t, ranges.OverlapsClosed(base, base.Add(time.Millisecond)))
	require.False(t, ranges.OverlapsClosed(base.Add(2*time.Millisecond), base.Add(3*time.Millisecond)))
}

func TestHintTimeRangesForFingerprint(t *testing.T) {
	base := time.Unix(100, 0)
	at := func(ms int) time.Time { return base.Add(time.Duration(ms) * time.Millisecond) }
	// inShard returns a fingerprint in document shard s at 2 bits, above the
	// fingerprints the ingester remaps into.
	inShard := func(s uint64) uint64 { return s<<62 | 1<<32 }
	ranges := NewHintTimeRanges([]logproto.HintTimeRange{
		{Start: at(0), End: at(1)},
		{Start: at(2), End: at(3), DocumentShard: 1, DocumentShardBits: 1},
		{Start: at(4), End: at(5), DocumentShard: 2, DocumentShardBits: 2},
		{Start: at(6), End: at(7), DocumentShard: 4, DocumentShardBits: 2}, // too wide for its bits: every stream
	}, base, at(8))

	for _, tc := range []struct {
		name string
		fp   uint64
		want []int
	}{
		{name: "wildcard ranges only", fp: inShard(0), want: []int{0, 6}},
		{name: "coarser and exact shard", fp: inShard(2), want: []int{0, 2, 4, 6}},
		{name: "coarser shard", fp: inShard(3), want: []int{0, 2, 6}},
		{name: "remapped fingerprint gets every shard", fp: logline.MaxMappedFingerprint, want: []int{0, 2, 4, 6}},
		{name: "first fingerprint above the remapped ones", fp: logline.MaxMappedFingerprint + 1, want: []int{0, 6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := ranges.ForFingerprint(tc.fp)
			var got []int
			for ms := range 8 {
				if view.contains(at(ms).UnixNano()) {
					got = append(got, ms)
				}
			}
			require.Equal(t, tc.want, got)
		})
	}

	require.True(t, ranges.Overlaps(at(4), at(5)), "the time union covers every shard")

	unsharded := NewHintTimeRanges([]logproto.HintTimeRange{{Start: at(0), End: at(1)}}, base, at(8))
	require.Equal(t, unsharded, unsharded.ForFingerprint(inShard(3)))
}

func TestHintTimeRangesTreatInvalidShardBitsAsEveryStream(t *testing.T) {
	base := time.Unix(100, 0)
	// fp is not in document shard 0 at any width.
	fp := uint64(1<<63 | 1<<32)
	for _, tc := range []struct {
		bits    uint32
		covered bool
	}{
		{bits: logline.MaxDocumentShardBits, covered: false},
		{bits: logline.MaxDocumentShardBits + 1, covered: true},
		{bits: 32, covered: true},
		{bits: math.MaxUint32, covered: true},
	} {
		t.Run(fmt.Sprint(tc.bits), func(t *testing.T) {
			ranges := NewHintTimeRanges([]logproto.HintTimeRange{{Start: base, End: base.Add(time.Millisecond), DocumentShardBits: tc.bits}}, base, base.Add(time.Millisecond))
			require.Equal(t, tc.covered, ranges.ForFingerprint(fp).contains(base.UnixNano()))
		})
	}
}

func TestNewHintTimeRangeSet(t *testing.T) {
	base := time.Unix(100, 0)
	from, through := base, base.Add(10*time.Millisecond)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }

	for _, tc := range []struct {
		desc        string
		hints       *logproto.HintTimeRanges
		wantEnabled bool
		wantOverlap bool
	}{
		{desc: "nil set passes everything", hints: nil, wantEnabled: false, wantOverlap: true},
		{desc: "empty set matches nothing", hints: &logproto.HintTimeRanges{}, wantEnabled: true, wantOverlap: false},
		{
			desc:        "set outside the bounds matches nothing",
			hints:       &logproto.HintTimeRanges{Ranges: []logproto.HintTimeRange{{Start: ms(20), End: ms(30)}}},
			wantEnabled: true,
			wantOverlap: false,
		},
		{
			desc:        "set with a range matches it",
			hints:       &logproto.HintTimeRanges{Ranges: []logproto.HintTimeRange{{Start: ms(2), End: ms(4)}}},
			wantEnabled: true,
			wantOverlap: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ranges := NewHintTimeRangeSet(tc.hints, from, through)
			require.Equal(t, tc.wantEnabled, ranges.Enabled())
			require.Equal(t, tc.wantOverlap, ranges.OverlapsClosed(from, through))
		})
	}
}

func TestHintTimeRangesCovers(t *testing.T) {
	base := time.Unix(100, 0)
	ms := func(n int) time.Time { return base.Add(time.Duration(n) * time.Millisecond) }
	ranges := NewHintTimeRanges([]logproto.HintTimeRange{
		{Start: ms(1), End: ms(3)},
		{Start: ms(3), End: ms(4)}, // merged with the previous range
		{Start: ms(6), End: ms(8)},
	}, base, ms(10))

	for _, tc := range []struct {
		desc          string
		from, through time.Time
		want          bool
		wantClosed    bool
	}{
		{desc: "inside a range", from: ms(1), through: ms(2), want: true, wantClosed: true},
		{desc: "across merged ranges", from: ms(2), through: ms(3).Add(999 * time.Microsecond), want: true, wantClosed: true},
		{desc: "through equals the exclusive end", from: ms(6), through: ms(8), want: true, wantClosed: false},
		{desc: "starts before a range", from: base, through: ms(2), want: false, wantClosed: false},
		{desc: "spans a gap", from: ms(3), through: ms(6), want: false, wantClosed: false},
		{desc: "outside every range", from: ms(4), through: ms(5), want: false, wantClosed: false},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.want, ranges.Covers(tc.from, tc.through))
			require.Equal(t, tc.wantClosed, ranges.CoversClosed(tc.from, tc.through))
		})
	}

	require.True(t, HintTimeRanges{}.Covers(base, ms(10)), "no hints covers everything")
	require.True(t, HintTimeRanges{}.CoversClosed(base, ms(10)), "no hints covers everything")
	empty := NewHintTimeRangeSet(&logproto.HintTimeRanges{}, base, ms(10))
	require.False(t, empty.Covers(ms(1), ms(2)), "an empty set covers nothing")

	sharded := []logproto.HintTimeRange{
		{Start: base, End: ms(10), DocumentShard: 0, DocumentShardBits: 1},
		{Start: base, End: ms(5), DocumentShard: 1, DocumentShardBits: 1},
	}
	partial := NewHintTimeRanges(sharded, base, ms(10))
	require.False(t, partial.Covers(base, ms(10)), "the time union covers the bounds, but not for document shard 1")
	require.True(t, partial.ForFingerprint(1<<32).Covers(base, ms(10)), "document shard 0 covers the bounds")
	every := NewHintTimeRanges(append(sharded, logproto.HintTimeRange{Start: ms(5), End: ms(10)}), base, ms(10))
	require.True(t, every.Covers(base, ms(10)), "every document shard covers the bounds")
}

func TestHintIterators(t *testing.T) {
	base := time.Unix(100, 0)
	allOffsets := []time.Duration{0, time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond, 5 * time.Millisecond}

	tests := []struct {
		name     string
		hints    []logproto.HintTimeRange
		start    time.Time
		end      time.Time
		expected []time.Duration
	}{
		{
			name:     "nil hints preserve current behavior",
			start:    base,
			end:      base.Add(6 * time.Millisecond),
			expected: allOffsets,
		},
		{
			name:     "empty hints preserve current behavior",
			hints:    []logproto.HintTimeRange{},
			start:    base,
			end:      base.Add(6 * time.Millisecond),
			expected: allOffsets,
		},
		{
			name: "disjoint hints return nothing",
			hints: []logproto.HintTimeRange{{
				Start: base.Add(10 * time.Millisecond),
				End:   base.Add(11 * time.Millisecond),
			}},
			start: base,
			end:   base.Add(6 * time.Millisecond),
		},
		{
			name: "ranges are clipped and half open",
			hints: []logproto.HintTimeRange{
				{Start: base.Add(-time.Millisecond), End: base.Add(2 * time.Millisecond)},
				{Start: base.Add(5 * time.Millisecond), End: base.Add(8 * time.Millisecond)},
			},
			start:    base.Add(time.Millisecond),
			end:      base.Add(5 * time.Millisecond),
			expected: []time.Duration{time.Millisecond},
		},
		{
			name: "multiple ranges form a union",
			hints: []logproto.HintTimeRange{
				{Start: base.Add(4 * time.Millisecond), End: base.Add(6 * time.Millisecond)},
				{Start: base.Add(time.Millisecond), End: base.Add(2 * time.Millisecond)},
				{Start: base.Add(3 * time.Millisecond), End: base.Add(5 * time.Millisecond)},
			},
			start: base,
			end:   base.Add(6 * time.Millisecond),
			expected: []time.Duration{
				time.Millisecond,
				3 * time.Millisecond,
				4 * time.Millisecond,
				5 * time.Millisecond,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ranges := NewHintTimeRanges(tc.hints, tc.start, tc.end)

			entries := make([]logproto.Entry, 0, len(allOffsets))
			samples := make([]logproto.Sample, 0, len(allOffsets))
			for _, offset := range allOffsets {
				entries = append(entries, logproto.Entry{
					Timestamp: base.Add(offset),
					Line:      offset.String(),
				})
				samples = append(samples, logproto.Sample{
					Timestamp: base.Add(offset).UnixNano(),
					Value:     float64(offset),
				})
			}

			entryIt := NewHintEntryIterator(NewStreamIterator(logproto.Stream{
				Labels:  `{foo="bar"}`,
				Hash:    123,
				Entries: entries,
			}), ranges)
			var gotEntries []time.Duration
			for entryIt.Next() {
				gotEntries = append(gotEntries, entryIt.At().Timestamp.Sub(base))
			}
			require.NoError(t, entryIt.Err())
			if ranges.Enabled() && len(tc.expected) > 0 {
				require.Equal(t, tc.expected[len(tc.expected)-1], entryIt.At().Timestamp.Sub(base), "At must retain the last accepted entry after exhaustion")
				require.Equal(t, `{foo="bar"}`, entryIt.Labels())
				require.Equal(t, uint64(123), entryIt.StreamHash())
			}
			require.NoError(t, entryIt.Close())

			sampleIt := NewHintSampleIterator(NewSeriesIterator(logproto.Series{
				Labels:     `{foo="bar"}`,
				StreamHash: 123,
				Samples:    samples,
			}), ranges)
			var gotSamples []time.Duration
			for sampleIt.Next() {
				gotSamples = append(gotSamples, time.Unix(0, sampleIt.At().Timestamp).Sub(base))
			}
			require.NoError(t, sampleIt.Err())
			if ranges.Enabled() && len(tc.expected) > 0 {
				require.Equal(t, tc.expected[len(tc.expected)-1], time.Unix(0, sampleIt.At().Timestamp).Sub(base), "At must retain the last accepted sample after exhaustion")
				require.Equal(t, `{foo="bar"}`, sampleIt.Labels())
				require.Equal(t, uint64(123), sampleIt.StreamHash())
			}
			require.NoError(t, sampleIt.Close())

			require.Equal(t, tc.expected, gotEntries)
			require.Equal(t, tc.expected, gotSamples)
		})
	}
}
