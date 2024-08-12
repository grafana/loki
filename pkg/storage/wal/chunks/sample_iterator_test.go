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

func TestNewSampleIterator(t *testing.T) {
	tests := []struct {
		name      string
		entries   []*logproto.Entry
		from      int64
		through   int64
		extractor log.StreamSampleExtractor
		expected  []logproto.Sample
		expectErr bool
	}{
		{
			name: "All samples within range",
			entries: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "1.0"},
				{Timestamp: time.Unix(0, 2), Line: "2.0"},
				{Timestamp: time.Unix(0, 3), Line: "3.0"},
			},
			from:      0,
			through:   4,
			extractor: mustNewExtractor(t, ""),
			expected: []logproto.Sample{
				{Timestamp: 1, Value: 1.0, Hash: 0},
				{Timestamp: 2, Value: 1.0, Hash: 0},
				{Timestamp: 3, Value: 1.0, Hash: 0},
			},
		},
		{
			name: "Partial range",
			entries: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "1.0"},
				{Timestamp: time.Unix(0, 2), Line: "2.0"},
				{Timestamp: time.Unix(0, 3), Line: "3.0"},
				{Timestamp: time.Unix(0, 4), Line: "4.0"},
			},
			from:      2,
			through:   4,
			extractor: mustNewExtractor(t, ""),
			expected: []logproto.Sample{
				{Timestamp: 2, Value: 1.0, Hash: 0},
				{Timestamp: 3, Value: 1.0, Hash: 0},
			},
		},
		{
			name: "Pipeline filter",
			entries: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "error: 1.0"},
				{Timestamp: time.Unix(0, 2), Line: "info: 2.0"},
				{Timestamp: time.Unix(0, 3), Line: "error: 3.0"},
				{Timestamp: time.Unix(0, 4), Line: "debug: 4.0"},
			},
			from:      1,
			through:   5,
			extractor: mustNewExtractor(t, `count_over_time({foo="bar"} |= "error"[1m])`),
			expected: []logproto.Sample{
				{Timestamp: 1, Value: 1.0, Hash: 0},
				{Timestamp: 3, Value: 1.0, Hash: 0},
			},
		},
		{
			name: "Pipeline filter with bytes_over_time",
			entries: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "error: 1.0"},
				{Timestamp: time.Unix(0, 2), Line: "info: 2.0"},
				{Timestamp: time.Unix(0, 3), Line: "error: 3.0"},
				{Timestamp: time.Unix(0, 4), Line: "debug: 4.0"},
			},
			from:      1,
			through:   5,
			extractor: mustNewExtractor(t, `bytes_over_time({foo="bar"} |= "error"[1m])`),
			expected: []logproto.Sample{
				{Timestamp: 1, Value: 10, Hash: 0},
				{Timestamp: 3, Value: 10, Hash: 0},
			},
		},
		{
			name: "No samples within range",
			entries: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "1.0"},
				{Timestamp: time.Unix(0, 2), Line: "2.0"},
			},
			from:      3,
			through:   5,
			extractor: mustNewExtractor(t, ""),
			expected:  nil,
		},
		{
			name:      "Empty chunk",
			entries:   []*logproto.Entry{},
			from:      0,
			through:   5,
			extractor: mustNewExtractor(t, ""),
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			// Write the chunk
			_, err := WriteChunk(&buf, tt.entries, EncodingSnappy)
			require.NoError(t, err, "WriteChunk failed")

			// Create the iterator
			iter, err := NewSampleIterator(buf.Bytes(), tt.extractor, tt.from, tt.through)
			if tt.expectErr {
				require.Error(t, err, "Expected an error but got none")
				return
			}
			require.NoError(t, err, "NewSampleIterator failed")
			defer iter.Close()

			// Read samples using the iterator
			var actualSamples []logproto.Sample
			for iter.Next() {
				actualSamples = append(actualSamples, iter.At())
			}
			require.NoError(t, iter.Err(), "Iterator encountered an error")

			// Compare actual samples with expected samples
			require.Equal(t, tt.expected, actualSamples, "Samples do not match expected values")

			// Check labels
			if len(actualSamples) > 0 {
				require.Equal(t, tt.extractor.BaseLabels().String(), iter.Labels(), "Unexpected labels")
			}

			// Check StreamHash
			if len(actualSamples) > 0 {
				require.Equal(t, tt.extractor.BaseLabels().Hash(), iter.StreamHash(), "Unexpected StreamHash")
			}
		})
	}
}

func TestNewSampleIteratorErrors(t *testing.T) {
	tests := []struct {
		name      string
		chunkData []byte
		extractor log.StreamSampleExtractor
		from      int64
		through   int64
	}{
		{
			name:      "Invalid chunk data",
			chunkData: []byte("invalid chunk data"),
			extractor: mustNewExtractor(t, ""),
			from:      0,
			through:   10,
		},
		{
			name:      "Nil extractor",
			chunkData: []byte{}, // valid empty chunk
			extractor: nil,
			from:      0,
			through:   10,
		},
		{
			name:      "Invalid time range",
			chunkData: []byte{}, // valid empty chunk
			extractor: mustNewExtractor(t, ""),
			from:      10,
			through:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSampleIterator(tt.chunkData, tt.extractor, tt.from, tt.through)
			require.Error(t, err, "Expected an error but got none")
		})
	}
}

func mustNewExtractor(t *testing.T, query string) log.StreamSampleExtractor {
	t.Helper()
	if query == `` {
		query = `count_over_time({foo="bar"}[1m])`
	}
	expr, err := syntax.ParseSampleExpr(query)
	require.NoError(t, err)

	extractor, err := expr.Extractor()
	require.NoError(t, err)

	return extractor.ForStream(labels.Labels{})
}
