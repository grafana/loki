package querier

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// makeEntry is a helper function to create a log entry with given timestamp and line
func makeEntry(ts time.Time, line string) logproto.Entry {
	return logproto.Entry{
		Timestamp: ts,
		Line:      line,
	}
}

func TestTopKIterator(t *testing.T) {
	tests := []struct {
		name      string
		k         int
		direction logproto.Direction
		input     []entryWithLabels
		want      []entryWithLabels
	}{
		{
			name:      "forward direction with k=2",
			k:         2,
			direction: logproto.FORWARD,
			input: []entryWithLabels{
				{Entry: makeEntry(time.Unix(1, 0), "line1"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(3, 0), "line3"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(2, 0), "line2"), Labels: "{app=\"app2\"}", StreamHash: 2},
				{Entry: makeEntry(time.Unix(4, 0), "line4"), Labels: "{app=\"app2\"}", StreamHash: 2},
			},
			want: []entryWithLabels{
				{Entry: makeEntry(time.Unix(1, 0), "line1"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(2, 0), "line2"), Labels: "{app=\"app2\"}", StreamHash: 2},
			},
		},
		{
			name:      "backward direction with k=3",
			k:         3,
			direction: logproto.BACKWARD,
			input: []entryWithLabels{
				{Entry: makeEntry(time.Unix(1, 0), "line1"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(4, 0), "line4"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(2, 0), "line2"), Labels: "{app=\"app2\"}", StreamHash: 2},
				{Entry: makeEntry(time.Unix(3, 0), "line3"), Labels: "{app=\"app2\"}", StreamHash: 2},
				{Entry: makeEntry(time.Unix(5, 0), "line5"), Labels: "{app=\"app2\"}", StreamHash: 2},
			},
			want: []entryWithLabels{
				{Entry: makeEntry(time.Unix(5, 0), "line5"), Labels: "{app=\"app2\"}", StreamHash: 2},
				{Entry: makeEntry(time.Unix(4, 0), "line4"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(3, 0), "line3"), Labels: "{app=\"app2\"}", StreamHash: 2},
			},
		},
		{
			name:      "k larger than available entries",
			k:         10,
			direction: logproto.FORWARD,
			input: []entryWithLabels{
				{Entry: makeEntry(time.Unix(1, 0), "line1"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(2, 0), "line2"), Labels: "{app=\"app1\"}", StreamHash: 1},
			},
			want: []entryWithLabels{
				{Entry: makeEntry(time.Unix(1, 0), "line1"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(2, 0), "line2"), Labels: "{app=\"app1\"}", StreamHash: 1},
			},
		},
		{
			name:      "mixed timestamps with k=4",
			k:         4,
			direction: logproto.FORWARD,
			input: []entryWithLabels{
				{Entry: makeEntry(time.Unix(1, 0), "line1"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(4, 0), "line4"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(5, 0), "line5"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(2, 0), "line2"), Labels: "{app=\"app2\"}", StreamHash: 2},
				{Entry: makeEntry(time.Unix(3, 0), "line3"), Labels: "{app=\"app2\"}", StreamHash: 2},
				{Entry: makeEntry(time.Unix(6, 0), "line6"), Labels: "{app=\"app2\"}", StreamHash: 2},
			},
			want: []entryWithLabels{
				{Entry: makeEntry(time.Unix(1, 0), "line1"), Labels: "{app=\"app1\"}", StreamHash: 1},
				{Entry: makeEntry(time.Unix(2, 0), "line2"), Labels: "{app=\"app2\"}", StreamHash: 2},
				{Entry: makeEntry(time.Unix(3, 0), "line3"), Labels: "{app=\"app2\"}", StreamHash: 2},
				{Entry: makeEntry(time.Unix(4, 0), "line4"), Labels: "{app=\"app1\"}", StreamHash: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create topk iterator
			top := &topk{
				k:       tt.k,
				minHeap: entryHeap{less: lessFn(tt.direction)},
			}

			// Add entries
			for _, e := range tt.input {
				top.Add(e)
			}

			// Collect results
			var got []entryWithLabels
			iter := top.Iterator()
			for iter.Next() {
				got = append(got, entryWithLabels{
					Entry:      iter.At(),
					Labels:     iter.Labels(),
					StreamHash: iter.StreamHash(),
				})
			}

			require.Equal(t, tt.want, got)
			require.NoError(t, iter.Err())
		})
	}
}
