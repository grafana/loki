package chunks

import (
	"bytes"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestNewEntryIterator(t *testing.T) {
	tests := []struct {
		name      string
		entries   []*logproto.Entry
		direction logproto.Direction
		from      int64
		through   int64
		pipeline  log.StreamPipeline
		expected  []*logproto.Entry
	}{
		{
			name: "Forward direction, all entries within range",
			entries: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "line 1"},
				{Timestamp: time.Unix(0, 2), Line: "line 2"},
				{Timestamp: time.Unix(0, 3), Line: "line 3"},
			},
			direction: logproto.FORWARD,
			from:      0,
			through:   4,
			pipeline:  noopStreamPipeline(),
			expected: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "line 1"},
				{Timestamp: time.Unix(0, 2), Line: "line 2"},
				{Timestamp: time.Unix(0, 3), Line: "line 3"},
			},
		},
		{
			name: "Backward direction, all entries within range",
			entries: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "line 1"},
				{Timestamp: time.Unix(0, 2), Line: "line 2"},
				{Timestamp: time.Unix(0, 3), Line: "line 3"},
			},
			direction: logproto.BACKWARD,
			from:      0,
			through:   4,
			pipeline:  noopStreamPipeline(),
			expected: []*logproto.Entry{
				{Timestamp: time.Unix(0, 3), Line: "line 3"},
				{Timestamp: time.Unix(0, 2), Line: "line 2"},
				{Timestamp: time.Unix(0, 1), Line: "line 1"},
			},
		},
		{
			name: "Forward direction, partial range",
			entries: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "line 1"},
				{Timestamp: time.Unix(0, 2), Line: "line 2"},
				{Timestamp: time.Unix(0, 3), Line: "line 3"},
				{Timestamp: time.Unix(0, 4), Line: "line 4"},
			},
			direction: logproto.FORWARD,
			from:      2,
			through:   4,
			pipeline:  noopStreamPipeline(),
			expected: []*logproto.Entry{
				{Timestamp: time.Unix(0, 2), Line: "line 2"},
				{Timestamp: time.Unix(0, 3), Line: "line 3"},
			},
		},
		{
			name: "Forward direction with logql pipeline filter",
			entries: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1).UTC(), Line: "error: something went wrong"},
				{Timestamp: time.Unix(0, 2).UTC(), Line: "info: operation successful"},
				{Timestamp: time.Unix(0, 3).UTC(), Line: "error: another error occurred"},
				{Timestamp: time.Unix(0, 4).UTC(), Line: "debug: checking status"},
			},
			direction: logproto.FORWARD,
			from:      1,
			through:   5,
			pipeline:  mustNewPipeline(t, `{foo="bar"} | line_format "foo {{ __line__ }}" |= "error"`),
			expected: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "foo error: something went wrong"},
				{Timestamp: time.Unix(0, 3), Line: "foo error: another error occurred"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			// Write the chunk
			_, err := WriteChunk(&buf, tt.entries, EncodingSnappy)
			require.NoError(t, err, "WriteChunk failed")

			// Create the iterator
			iter, err := NewEntryIterator(buf.Bytes(), tt.pipeline, tt.direction, tt.from, tt.through)
			require.NoError(t, err, "NewEntryIterator failed")
			defer iter.Close()

			// Read entries using the iterator
			var actualEntries []*logproto.Entry
			for iter.Next() {
				entry := iter.At()
				actualEntries = append(actualEntries, &logproto.Entry{
					Timestamp: entry.Timestamp,
					Line:      entry.Line,
				})
			}
			require.NoError(t, iter.Err(), "Iterator encountered an error")

			// Compare actual entries with expected entries
			require.Equal(t, tt.expected, actualEntries, "Entries do not match expected values")
		})
	}
}

// mustNewPipeline creates a new pipeline or fails the test
func mustNewPipeline(t *testing.T, query string) log.StreamPipeline {
	t.Helper()
	if query == "" {
		return log.NewNoopPipeline().ForStream(labels.Labels{})
	}
	expr, err := syntax.ParseLogSelector(query, true)
	require.NoError(t, err)

	pipeline, err := expr.Pipeline()
	require.NoError(t, err)

	return pipeline.ForStream(labels.Labels{})
}

func noopStreamPipeline() log.StreamPipeline {
	return log.NewNoopPipeline().ForStream(labels.Labels{})
}
