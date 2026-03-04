package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

// --------------------------------------------------------
// parseStreamsResponse tests
// --------------------------------------------------------

func TestParseStreamsResponse(t *testing.T) {
	t.Run("valid streams response", func(t *testing.T) {
		raw := []byte(`{"status":"success","data":{"resultType":"streams","result":[{"stream":{"app":"foo"},"values":[["1000000000","line1"],["2000000000","line2"]]}]}}`)
		parsed, err := parseStreamsResponse(raw)
		require.NoError(t, err)
		require.Len(t, parsed.Streams, 1)
		assert.Equal(t, "foo", string(parsed.Streams[0].Labels["app"]))
		assert.Len(t, parsed.Streams[0].Entries, 2)
	})

	t.Run("empty body", func(t *testing.T) {
		_, err := parseStreamsResponse(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty response body")
	})

	t.Run("error status", func(t *testing.T) {
		raw := []byte(`{"status":"error","error":"query timed out"}`)
		_, err := parseStreamsResponse(raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "error")
		assert.Contains(t, err.Error(), "query timed out")
	})

	t.Run("wrong result type", func(t *testing.T) {
		raw := []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`)
		_, err := parseStreamsResponse(raw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected streams, got matrix")
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := parseStreamsResponse([]byte(`{invalid`))
		assert.Error(t, err)
	})

	t.Run("empty streams is valid", func(t *testing.T) {
		raw := []byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`)
		parsed, err := parseStreamsResponse(raw)
		require.NoError(t, err)
		assert.Empty(t, parsed.Streams)
	})
}

// --------------------------------------------------------
// streamsByLabels tests
// --------------------------------------------------------

func TestStreamsByLabels(t *testing.T) {
	streams := loghttp.Streams{
		{Labels: loghttp.LabelSet{"app": "foo", "pod": "p1"}, Entries: []loghttp.Entry{{Line: "a"}}},
		{Labels: loghttp.LabelSet{"app": "bar"}, Entries: []loghttp.Entry{{Line: "b"}}},
	}
	m := streamsByLabels(streams)
	assert.Len(t, m, 2)

	for _, s := range streams {
		got, ok := m[s.Labels.String()]
		require.True(t, ok)
		assert.Equal(t, s.Labels, got.Labels)
	}
}

// --------------------------------------------------------
// findDivergenceRange tests
// --------------------------------------------------------

func TestFindDivergenceRange(t *testing.T) {
	t.Run("extra entries at end", func(t *testing.T) {
		wider := []loghttp.Entry{
			{Timestamp: time.Unix(1, 0), Line: "a"},
			{Timestamp: time.Unix(2, 0), Line: "b"},
			{Timestamp: time.Unix(3, 0), Line: "c"},
		}
		narrower := []loghttp.Entry{
			{Timestamp: time.Unix(1, 0), Line: "a"},
		}
		start, end := findDivergenceRange(wider, narrower)
		assert.Equal(t, time.Unix(2, 0), start)
		assert.Equal(t, time.Unix(3, 0), end)
	})

	t.Run("extra entry in the middle", func(t *testing.T) {
		wider := []loghttp.Entry{
			{Timestamp: time.Unix(1, 0)},
			{Timestamp: time.Unix(5, 0)},
			{Timestamp: time.Unix(10, 0)},
		}
		narrower := []loghttp.Entry{
			{Timestamp: time.Unix(1, 0)},
			{Timestamp: time.Unix(10, 0)},
		}
		start, end := findDivergenceRange(wider, narrower)
		assert.Equal(t, time.Unix(5, 0), start)
		assert.Equal(t, time.Unix(5, 0), end)
	})

	t.Run("no divergence - same timestamps", func(t *testing.T) {
		entries := []loghttp.Entry{{Timestamp: time.Unix(1, 0)}}
		start, end := findDivergenceRange(entries, entries)
		assert.True(t, start.IsZero())
		assert.True(t, end.IsZero())
	})

	t.Run("empty narrower - all entries diverge", func(t *testing.T) {
		wider := []loghttp.Entry{
			{Timestamp: time.Unix(5, 0)},
			{Timestamp: time.Unix(2, 0)},
			{Timestamp: time.Unix(8, 0)},
		}
		start, end := findDivergenceRange(wider, nil)
		assert.Equal(t, time.Unix(2, 0), start)
		assert.Equal(t, time.Unix(8, 0), end)
	})
}

// --------------------------------------------------------
// StreamAnalyzer.Analyze tests
// --------------------------------------------------------

func makeStreamsJSON(streams []streamSpec) []byte {
	type resultStream struct {
		Stream map[string]string `json:"stream"`
		Values [][]string        `json:"values"`
	}
	var result []resultStream
	for _, s := range streams {
		rs := resultStream{Stream: s.labels}
		for _, e := range s.entries {
			rs.Values = append(rs.Values, []string{
				e.ts, e.line,
			})
		}
		result = append(result, rs)
	}
	data := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "streams",
			"result":     result,
		},
	}
	b, _ := json.Marshal(data)
	return b
}

type streamSpec struct {
	labels  map[string]string
	entries []entrySpec
}

type entrySpec struct {
	ts   string
	line string
}

func TestStreamAnalyzer_MissingStreams(t *testing.T) {
	analyzer := &StreamAnalyzer{}
	entry := &MismatchEntry{Query: `{app="foo"}`, StartTime: time.Unix(0, 0), EndTime: time.Unix(100, 0)}

	cellA := makeStreamsJSON([]streamSpec{
		{labels: map[string]string{"app": "foo"}, entries: []entrySpec{{"1000000000", "line1"}}},
		{labels: map[string]string{"app": "bar"}, entries: []entrySpec{{"2000000000", "line2"}}},
	})
	cellB := makeStreamsJSON([]streamSpec{
		{labels: map[string]string{"app": "foo"}, entries: []entrySpec{{"1000000000", "line1"}}},
	})

	results, err := analyzer.Analyze(context.Background(), entry, cellA, cellB)
	require.NoError(t, err)

	var missingTypes []string
	for _, r := range results {
		missingTypes = append(missingTypes, r.DetectedMismatchType)
	}
	assert.Contains(t, missingTypes, "stream_missing_in_cell_b")
}

func TestStreamAnalyzer_EntryCountMismatch(t *testing.T) {
	analyzer := &StreamAnalyzer{}
	entry := &MismatchEntry{Query: `{app="foo"}`, StartTime: time.Unix(0, 0), EndTime: time.Unix(100, 0)}

	cellA := makeStreamsJSON([]streamSpec{
		{labels: map[string]string{"app": "foo"}, entries: []entrySpec{
			{"1000000000", "line1"},
			{"2000000000", "line2"},
			{"3000000000", "line3"},
		}},
	})
	cellB := makeStreamsJSON([]streamSpec{
		{labels: map[string]string{"app": "foo"}, entries: []entrySpec{
			{"1000000000", "line1"},
		}},
	})

	results, err := analyzer.Analyze(context.Background(), entry, cellA, cellB)
	require.NoError(t, err)

	var found bool
	for _, r := range results {
		if r.DetectedMismatchType == "stream_entry_count" {
			found = true
			assert.Contains(t, r.Details, "3 entries")
			assert.Contains(t, r.Details, "1 entries")
		}
	}
	assert.True(t, found, "expected stream_entry_count result")
}

func TestStreamAnalyzer_LineMismatch(t *testing.T) {
	analyzer := &StreamAnalyzer{}
	entry := &MismatchEntry{Query: `{app="foo"}`, StartTime: time.Unix(0, 0), EndTime: time.Unix(100, 0)}

	cellA := makeStreamsJSON([]streamSpec{
		{labels: map[string]string{"app": "foo"}, entries: []entrySpec{
			{"1000000000", "line-A"},
			{"2000000000", "same"},
		}},
	})
	cellB := makeStreamsJSON([]streamSpec{
		{labels: map[string]string{"app": "foo"}, entries: []entrySpec{
			{"1000000000", "line-B"},
			{"2000000000", "same"},
		}},
	})

	results, err := analyzer.Analyze(context.Background(), entry, cellA, cellB)
	require.NoError(t, err)

	var found bool
	for _, r := range results {
		if r.DetectedMismatchType == "stream_line_mismatch" {
			found = true
			assert.Contains(t, r.Details, "position 0")
		}
	}
	assert.True(t, found, "expected stream_line_mismatch result")
}

func TestStreamAnalyzer_IdenticalStreams(t *testing.T) {
	analyzer := &StreamAnalyzer{}
	entry := &MismatchEntry{Query: `{app="foo"}`, StartTime: time.Unix(0, 0), EndTime: time.Unix(100, 0)}

	payload := makeStreamsJSON([]streamSpec{
		{labels: map[string]string{"app": "foo"}, entries: []entrySpec{
			{"1000000000", "line1"},
			{"2000000000", "line2"},
		}},
	})

	results, err := analyzer.Analyze(context.Background(), entry, payload, payload)
	require.NoError(t, err)
	assert.Empty(t, results, "identical streams should produce no results")
}

func TestStreamAnalyzer_ParseError(t *testing.T) {
	analyzer := &StreamAnalyzer{}
	entry := &MismatchEntry{Query: `{app="foo"}`}

	results, err := analyzer.Analyze(context.Background(), entry, []byte(`{invalid`), []byte(`{invalid`))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "parse_error", results[0].DetectedMismatchType)
}
