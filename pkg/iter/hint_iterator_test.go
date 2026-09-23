package iter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
