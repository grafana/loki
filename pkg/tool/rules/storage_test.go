package rules

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestParseStreamLabels(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    map[string]string
		expectError bool
	}{
		{
			name:  "simple label set",
			input: `{job="test"}`,
			expected: map[string]string{
				"job": "test",
			},
			expectError: false,
		},
		{
			name:  "multiple labels",
			input: `{job="test", instance="localhost", level="error"}`,
			expected: map[string]string{
				"job":      "test",
				"instance": "localhost",
				"level":    "error",
			},
			expectError: false,
		},
		{
			name:        "metric with name - should error (not valid LogQL)",
			input:       `up{job="test"}`,
			expected:    nil,
			expectError: true,
		},
		{
			name:        "metric name only - should error (not valid LogQL)",
			input:       `http_requests_total{}`,
			expected:    nil,
			expectError: true,
		},
		{
			name:        "empty label set",
			input:       `{}`,
			expected:    map[string]string{},
			expectError: false,
		},
		{
			name:        "regex matcher - should error (only equality supported)",
			input:       `{job=~"test.*"}`,
			expected:    nil,
			expectError: true,
		},
		{
			name:        "not equal matcher - should error (only equality supported)",
			input:       `{job!="test"}`,
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lbls, err := parseStreamLabels(tt.input)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				// Convert labels to map for easier comparison
				labelMap := make(map[string]string)
				lbls.Range(func(lbl labels.Label) {
					labelMap[lbl.Name] = lbl.Value
				})
				require.Equal(t, tt.expected, labelMap)
			}
		})
	}
}

func TestGetStreamsByLabels(t *testing.T) {
	ts := newTestStorage()

	inputStreams := []stream{
		{
			Labels: `{job="test", level="error"}`,
			Lines:  []string{"error 1", "error 2", "error 3"},
		},
		{
			Labels: `{job="test", level="info"}`,
			Lines:  []string{"info 1", "info 2", "info 3"},
		},
		{
			Labels: `{job="web", level="error"}`,
			Lines:  []string{"log line 1", "log line 2", "log line 3"},
		},
	}

	err := ts.parseAndLoadStreams(inputStreams, model.Duration(1*time.Minute))
	require.NoError(t, err)

	tests := []struct {
		name          string
		matchers      []*labels.Matcher
		expectedCount int
	}{
		{
			name: "match by job",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "job", "test"),
			},
			expectedCount: 2,
		},
		{
			name: "match by level",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "level", "error"),
			},
			expectedCount: 2,
		},
		{
			name: "match by multiple labels",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "job", "test"),
				labels.MustNewMatcher(labels.MatchEqual, "level", "error"),
			},
			expectedCount: 1,
		},
		{
			name: "no match",
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "job", "nonexistent"),
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := ts.GetStreamsByLabels(tt.matchers)
			require.Equal(t, tt.expectedCount, len(matched))
		})
	}
}

func TestGetEntriesInRange(t *testing.T) {
	ts := newTestStorage()

	inputStreams := []stream{
		{
			Labels: `{job="test"}`,
			Lines:  []string{"log 1", "log 2", "log 3", "log 4", "log 5", "log 6", "log 7", "log 8", "log 9", "log 10"}, // 10 entries at 1-minute intervals
		},
	}

	err := ts.parseAndLoadStreams(inputStreams, model.Duration(1*time.Minute))
	require.NoError(t, err)

	baseTime := time.Unix(0, 0).UTC()

	tests := []struct {
		name          string
		start         time.Time
		end           time.Time
		expectedCount int
	}{
		{
			name:          "entire range",
			start:         baseTime,
			end:           baseTime.Add(10 * time.Minute),
			expectedCount: 10,
		},
		{
			name:          "first half",
			start:         baseTime,
			end:           baseTime.Add(5 * time.Minute),
			expectedCount: 6, // Inclusive of both start and end
		},
		{
			name:          "middle range",
			start:         baseTime.Add(3 * time.Minute),
			end:           baseTime.Add(7 * time.Minute),
			expectedCount: 5,
		},
		{
			name:          "single point",
			start:         baseTime.Add(5 * time.Minute),
			end:           baseTime.Add(5 * time.Minute),
			expectedCount: 1,
		},
		{
			name:          "no entries in range",
			start:         baseTime.Add(20 * time.Minute),
			end:           baseTime.Add(30 * time.Minute),
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := ts.GetEntriesInRange(tt.start, tt.end)
			require.Equal(t, tt.expectedCount, len(entries))

			// Verify entries are sorted by timestamp
			for i := 1; i < len(entries); i++ {
				require.True(t, entries[i-1].Timestamp.Before(entries[i].Timestamp) ||
					entries[i-1].Timestamp.Equal(entries[i].Timestamp))
			}
		})
	}
}

func TestTestStorageQuerier(t *testing.T) {
	ts := newTestStorage()

	inputStreams := []stream{
		{
			Labels: `{job="test", instance="host1"}`,
			Lines:  []string{"log line 1", "log line 2", "log line 3"},
		},
	}

	err := ts.parseAndLoadStreams(inputStreams, model.Duration(1*time.Minute))
	require.NoError(t, err)

	querier := ts.Querier()
	require.NotNil(t, querier)
}

// Tests for Phase 1: Lines support

func TestParseStream_Lines(t *testing.T) {
	ts := newTestStorage()
	interval := model.Duration(1 * time.Minute)

	s := stream{
		Labels: `{job="app"}`,
		Lines: []string{
			`{"level":"info","duration":100}`,
			`{"level":"error","duration":500}`,
			`{"invalid json`,
		},
	}

	parsedStream, err := ts.parseStream(s, interval)
	require.NoError(t, err)
	require.Equal(t, `{job="app"}`, parsedStream.Labels)
	require.Len(t, parsedStream.Entries, 3)

	// Verify timestamps
	baseTime := time.Unix(0, 0).UTC()
	require.Equal(t, baseTime, parsedStream.Entries[0].Timestamp)
	require.Equal(t, baseTime.Add(1*time.Minute), parsedStream.Entries[1].Timestamp)
	require.Equal(t, baseTime.Add(2*time.Minute), parsedStream.Entries[2].Timestamp)

	// Verify log lines match exactly what was provided
	require.Equal(t, `{"level":"info","duration":100}`, parsedStream.Entries[0].Line)
	require.Equal(t, `{"level":"error","duration":500}`, parsedStream.Entries[1].Line)
	require.Equal(t, `{"invalid json`, parsedStream.Entries[2].Line)

	// Verify no structured metadata
	require.Nil(t, parsedStream.Entries[0].StructuredMetadata)
	require.Nil(t, parsedStream.Entries[1].StructuredMetadata)
	require.Nil(t, parsedStream.Entries[2].StructuredMetadata)
}

func TestParseStream_LinesWithSpecialCharacters(t *testing.T) {
	ts := newTestStorage()
	interval := model.Duration(1 * time.Minute)

	s := stream{
		Labels: `{job="app"}`,
		Lines: []string{
			`line with "quotes"`,
			`line with 'apostrophes'`,
			`line with \backslash`,
			`line with newline\n`,
			`line with tab\t`,
		},
	}

	parsedStream, err := ts.parseStream(s, interval)
	require.NoError(t, err)
	require.Len(t, parsedStream.Entries, 5)

	// Verify lines are preserved exactly as provided
	require.Equal(t, `line with "quotes"`, parsedStream.Entries[0].Line)
	require.Equal(t, `line with 'apostrophes'`, parsedStream.Entries[1].Line)
	require.Equal(t, `line with \backslash`, parsedStream.Entries[2].Line)
	require.Equal(t, `line with newline\n`, parsedStream.Entries[3].Line)
	require.Equal(t, `line with tab\t`, parsedStream.Entries[4].Line)
}

// Tests for validation

func TestStreamValidate(t *testing.T) {
	tests := []struct {
		name        string
		stream      stream
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid lines",
			stream: stream{
				Labels: `{job="test"}`,
				Lines:  []string{"line1", "line2"},
			},
			expectError: false,
		},
		{
			name: "valid lines with multiple entries",
			stream: stream{
				Labels: `{job="test"}`,
				Lines:  []string{"log line 1", "log line 2", "log line 3"},
			},
			expectError: false,
		},
		{
			name: "no lines",
			stream: stream{
				Labels: `{job="test"}`,
			},
			expectError: true,
			errorMsg:    "must have 'lines'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.stream.Validate()
			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestParseAndLoadStreams_Validation(t *testing.T) {
	ts := newTestStorage()
	interval := model.Duration(1 * time.Minute)

	// Test that validation is called during loading
	inputStreams := []stream{
		{
			Labels: `{job="test"}`,
			// Missing Lines field - should fail validation
		},
	}

	err := ts.parseAndLoadStreams(inputStreams, interval)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must have 'lines'")
}

func TestParseStream_StructuredMetadata(t *testing.T) {
	storage := newTestStorage()

	stream := stream{
		Labels: `{job="test"}`,
		StructuredMetadata: map[string]string{
			"trace_id": "abc123",
			"user_id":  "user-456",
		},
		Lines: []string{
			"log line 1",
			"log line 2",
		},
	}

	result, err := storage.parseStream(stream, model.Duration(1*time.Minute))
	require.NoError(t, err)

	// Verify labels
	require.Equal(t, `{job="test"}`, result.Labels)

	// Verify entries
	require.Len(t, result.Entries, 2)

	// Verify structured metadata is attached to all entries
	for _, entry := range result.Entries {
		require.Len(t, entry.StructuredMetadata, 2)

		// Check sorted order and values
		require.Equal(t, "trace_id", entry.StructuredMetadata[0].Name)
		require.Equal(t, "abc123", entry.StructuredMetadata[0].Value)
		require.Equal(t, "user_id", entry.StructuredMetadata[1].Name)
		require.Equal(t, "user-456", entry.StructuredMetadata[1].Value)
	}
}

func TestParseStream_NoStructuredMetadata(t *testing.T) {
	storage := newTestStorage()

	stream := stream{
		Labels: `{job="test"}`,
		Lines: []string{
			"log line 1",
		},
	}

	result, err := storage.parseStream(stream, model.Duration(1*time.Minute))
	require.NoError(t, err)

	// Verify no structured metadata
	require.Len(t, result.Entries, 1)
	require.Empty(t, result.Entries[0].StructuredMetadata)
}
