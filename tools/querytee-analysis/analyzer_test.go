package main

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

// --------------------------------------------------------
// detectResultType tests
// --------------------------------------------------------

func TestDetectResultType(t *testing.T) {
	t.Run("streams", func(t *testing.T) {
		raw := []byte(`{"data":{"resultType":"streams"}}`)
		assert.Equal(t, "streams", detectResultType(raw))
	})

	t.Run("matrix", func(t *testing.T) {
		raw := []byte(`{"data":{"resultType":"matrix"}}`)
		assert.Equal(t, "matrix", detectResultType(raw))
	})

	t.Run("vector", func(t *testing.T) {
		raw := []byte(`{"data":{"resultType":"vector"}}`)
		assert.Equal(t, "vector", detectResultType(raw))
	})

	t.Run("scalar", func(t *testing.T) {
		raw := []byte(`{"data":{"resultType":"scalar"}}`)
		assert.Equal(t, "scalar", detectResultType(raw))
	})

	t.Run("empty response", func(t *testing.T) {
		assert.Equal(t, "", detectResultType(nil))
	})

	t.Run("invalid json", func(t *testing.T) {
		assert.Equal(t, "", detectResultType([]byte(`{bad`)))
	})

	t.Run("first non-empty wins", func(t *testing.T) {
		raw := []byte(`{"data":{"resultType":"vector"}}`)
		assert.Equal(t, "vector", detectResultType(nil, raw))
	})
}

// --------------------------------------------------------
// AnalysisCoordinator tests (with mock analyzers)
// --------------------------------------------------------

type mockAnalyzer struct {
	name       string
	canHandle  bool
	resultsFn  func() []*AnalysisResult
	analyzeErr error
}

func newMockAnalyzer(name string, canHandle bool, results ...*AnalysisResult) *mockAnalyzer {
	return &mockAnalyzer{
		name:      name,
		canHandle: canHandle,
		resultsFn: func() []*AnalysisResult {
			var cloned []*AnalysisResult
			for _, r := range results {
				c := *r
				cloned = append(cloned, &c)
			}
			return cloned
		},
	}
}

func (m *mockAnalyzer) Name() string                               { return m.name }
func (m *mockAnalyzer) CanAnalyze(_ *MismatchEntry, _ string) bool { return m.canHandle }
func (m *mockAnalyzer) Analyze(_ context.Context, _ *MismatchEntry, _, _ []byte) ([]*AnalysisResult, error) {
	if m.resultsFn != nil {
		return m.resultsFn(), m.analyzeErr
	}
	return nil, m.analyzeErr
}

func TestCoordinator_NilClients_SkipsFailures(t *testing.T) {
	coord := NewAnalysisCoordinator(nil, nil, nil, 1, false)
	entries := []*MismatchEntry{{CorrelationID: "test"}}
	results := coord.AnalyzeAll(context.Background(), entries, 0)

	assert.Empty(t, results, "nil clients cause query failures which should be skipped")
}

func TestCoordinator_BasicMode_StopsAfterPhase2(t *testing.T) {
	a := newMockAnalyzer("deep", true, &AnalysisResult{DetectedMismatchType: "should_not_appear"})

	coord := NewAnalysisCoordinator(nil, nil, nil, 1, false, a)
	entries := []*MismatchEntry{{CorrelationID: "test"}}
	results := coord.AnalyzeAll(context.Background(), entries, 0)

	assert.Empty(t, results, "nil clients cause failures, entries should be skipped")
}

func TestCoordinator_SetsEntryOnResults(t *testing.T) {
	coord := NewAnalysisCoordinator(nil, nil, nil, 1, false)
	entry := &MismatchEntry{CorrelationID: "entry-1"}
	results := coord.AnalyzeAll(context.Background(), []*MismatchEntry{entry}, 0)

	assert.Empty(t, results, "nil clients cause failures, entries should be skipped")
}

func TestCoordinator_LimitZero_ProcessesAll(t *testing.T) {
	coord := NewAnalysisCoordinator(nil, nil, nil, 4, false)
	entries := make([]*MismatchEntry, 10)
	for i := range entries {
		entries[i] = &MismatchEntry{CorrelationID: string(rune('A' + i))}
	}

	results := coord.AnalyzeAll(context.Background(), entries, 0)
	assert.Empty(t, results, "nil clients cause failures, all entries skipped")
}

// --------------------------------------------------------
// extractVector tests
// --------------------------------------------------------

func TestExtractVector(t *testing.T) {
	t.Run("nil data", func(t *testing.T) {
		_, err := extractVector(nil)
		assert.Error(t, err)
	})

	t.Run("non-vector type", func(t *testing.T) {
		data := &loghttp.QueryResponseData{
			ResultType: loghttp.ResultTypeStream,
			Result:     loghttp.Streams{},
		}
		_, err := extractVector(data)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected vector")
	})

	t.Run("valid vector", func(t *testing.T) {
		data := &loghttp.QueryResponseData{
			ResultType: loghttp.ResultTypeVector,
			Result: loghttp.Vector{
				{Metric: model.Metric{"app": "foo"}, Value: 42},
			},
		}
		vec, err := extractVector(data)
		require.NoError(t, err)
		require.Len(t, vec, 1)
		assert.Equal(t, model.SampleValue(42), vec[0].Value)
	})
}

// --------------------------------------------------------
// sumVector tests
// --------------------------------------------------------

func TestSumVector(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		assert.Equal(t, int64(0), sumVector(nil))
	})

	t.Run("single", func(t *testing.T) {
		vec := loghttp.Vector{{Value: 100}}
		assert.Equal(t, int64(100), sumVector(vec))
	})

	t.Run("multiple", func(t *testing.T) {
		vec := loghttp.Vector{
			{Value: 100},
			{Value: 200},
			{Value: 50},
		}
		assert.Equal(t, int64(350), sumVector(vec))
	})
}

// --------------------------------------------------------
// summarizeCountVectors tests
// --------------------------------------------------------

func TestSummarizeCountVectors_AllMatch(t *testing.T) {
	vecA := loghttp.Vector{
		{Metric: model.Metric{"app": "foo"}, Value: 100},
		{Metric: model.Metric{"app": "bar"}, Value: 200},
	}
	vecB := loghttp.Vector{
		{Metric: model.Metric{"app": "foo"}, Value: 100},
		{Metric: model.Metric{"app": "bar"}, Value: 200},
	}

	s := summarizeCountVectors(vecA, vecB)
	assert.False(t, s.hasDiscrepancies())
	assert.Equal(t, 2, s.seriesMatch)
	assert.Equal(t, 0, s.seriesCountMismatch)
	assert.Equal(t, 0, s.seriesMissingCellA)
	assert.Equal(t, 0, s.seriesMissingCellB)
	assert.Equal(t, int64(300), s.totalA)
	assert.Equal(t, int64(300), s.totalB)
	assert.Contains(t, s.details, "all 2 series match")
	assert.NotContains(t, s.details, "totalA")
}

func TestSummarizeCountVectors_MissingInCellB(t *testing.T) {
	vecA := loghttp.Vector{
		{Metric: model.Metric{"app": "foo"}, Value: 100},
		{Metric: model.Metric{"app": "bar"}, Value: 200},
	}
	vecB := loghttp.Vector{
		{Metric: model.Metric{"app": "foo"}, Value: 100},
	}

	s := summarizeCountVectors(vecA, vecB)
	assert.True(t, s.hasDiscrepancies())
	assert.Equal(t, 1, s.seriesMatch)
	assert.Equal(t, 0, s.seriesCountMismatch)
	assert.Equal(t, 0, s.seriesMissingCellA)
	assert.Equal(t, 1, s.seriesMissingCellB)
}

func TestSummarizeCountVectors_MissingInCellA(t *testing.T) {
	vecA := loghttp.Vector{
		{Metric: model.Metric{"app": "foo"}, Value: 100},
	}
	vecB := loghttp.Vector{
		{Metric: model.Metric{"app": "foo"}, Value: 100},
		{Metric: model.Metric{"app": "baz"}, Value: 50},
	}

	s := summarizeCountVectors(vecA, vecB)
	assert.True(t, s.hasDiscrepancies())
	assert.Equal(t, 1, s.seriesMatch)
	assert.Equal(t, 1, s.seriesMissingCellA)
	assert.Equal(t, 0, s.seriesMissingCellB)
}

func TestSummarizeCountVectors_CountMismatch(t *testing.T) {
	vecA := loghttp.Vector{
		{Metric: model.Metric{"app": "foo"}, Value: 100},
		{Metric: model.Metric{"app": "bar"}, Value: 200},
	}
	vecB := loghttp.Vector{
		{Metric: model.Metric{"app": "foo"}, Value: 100},
		{Metric: model.Metric{"app": "bar"}, Value: 150},
	}

	s := summarizeCountVectors(vecA, vecB)
	assert.True(t, s.hasDiscrepancies())
	assert.Equal(t, 1, s.seriesMatch)
	assert.Equal(t, 1, s.seriesCountMismatch)
	assert.Equal(t, int64(300), s.totalA)
	assert.Equal(t, int64(250), s.totalB)
}

func TestSummarizeCountVectors_MultipleDiscrepancies(t *testing.T) {
	vecA := loghttp.Vector{
		{Metric: model.Metric{"level": "info"}, Value: 100},
		{Metric: model.Metric{"level": "error"}, Value: 50},
		{Metric: model.Metric{"level": "warn"}, Value: 30},
	}
	vecB := loghttp.Vector{
		{Metric: model.Metric{"level": "info"}, Value: 100},
		{Metric: model.Metric{"level": "error"}, Value: 40},
		{Metric: model.Metric{"level": "debug"}, Value: 10},
	}

	s := summarizeCountVectors(vecA, vecB)
	assert.True(t, s.hasDiscrepancies())
	assert.Equal(t, 1, s.seriesMatch)
	assert.Equal(t, 1, s.seriesCountMismatch)
	assert.Equal(t, 1, s.seriesMissingCellA)
	assert.Equal(t, 1, s.seriesMissingCellB)
	assert.Contains(t, s.details, "4 series total")
	assert.Contains(t, s.details, "1 match")
	assert.Contains(t, s.details, "1 count_mismatch")
	assert.Contains(t, s.details, "1 missing_in_cell_a")
	assert.Contains(t, s.details, "1 missing_in_cell_b")
}

func TestSummarizeCountVectors_Empty(t *testing.T) {
	s := summarizeCountVectors(nil, nil)
	assert.False(t, s.hasDiscrepancies())
	assert.Equal(t, 0, s.seriesMatch)
}

func TestSummarizeCountVectors_SingleSeries(t *testing.T) {
	vecA := loghttp.Vector{{Metric: model.Metric{}, Value: 500}}
	vecB := loghttp.Vector{{Metric: model.Metric{}, Value: 500}}

	s := summarizeCountVectors(vecA, vecB)
	assert.False(t, s.hasDiscrepancies())
	assert.Equal(t, 1, s.seriesMatch)
	assert.Equal(t, int64(500), s.totalA)
}

// --------------------------------------------------------
// filterByCorrelationIDs tests
// --------------------------------------------------------

func TestFilterByCorrelationIDs(t *testing.T) {
	entries := []*MismatchEntry{
		{CorrelationID: "aaa"},
		{CorrelationID: "bbb"},
		{CorrelationID: "ccc"},
		{CorrelationID: "ddd"},
	}

	ids := map[string]struct{}{"aaa": {}, "ccc": {}}
	filtered := filterByCorrelationIDs(entries, ids)

	require.Len(t, filtered, 2)
	assert.Equal(t, "aaa", filtered[0].CorrelationID)
	assert.Equal(t, "ccc", filtered[1].CorrelationID)
}

func TestFilterByCorrelationIDs_NoMatch(t *testing.T) {
	entries := []*MismatchEntry{{CorrelationID: "aaa"}}
	ids := map[string]struct{}{"zzz": {}}
	filtered := filterByCorrelationIDs(entries, ids)
	assert.Empty(t, filtered)
}
