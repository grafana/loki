package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --------------------------------------------------------
// parseMetricResponse tests
// --------------------------------------------------------

func TestParseMetricResponse_Matrix(t *testing.T) {
	raw := []byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"app":"foo"},"values":[[1234567890,"100"],[1234567900,"200"]]}]}}`)
	parsed, err := parseMetricResponse(raw)
	require.NoError(t, err)
	assert.Equal(t, "matrix", parsed.ResultType)
	require.Len(t, parsed.Matrix, 1)
	assert.Len(t, parsed.Matrix[0].Values, 2)
	assert.Equal(t, model.SampleValue(100), parsed.Matrix[0].Values[0].Value)
}

func TestParseMetricResponse_Vector(t *testing.T) {
	raw := []byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"app":"foo"},"value":[1234567890,"42"]}]}}`)
	parsed, err := parseMetricResponse(raw)
	require.NoError(t, err)
	assert.Equal(t, "vector", parsed.ResultType)
	require.Len(t, parsed.Vector, 1)
	assert.Equal(t, model.SampleValue(42), parsed.Vector[0].Value)
}

func TestParseMetricResponse_Scalar(t *testing.T) {
	raw := []byte(`{"status":"success","data":{"resultType":"scalar","result":[1234567890,"42"]}}`)
	parsed, err := parseMetricResponse(raw)
	require.NoError(t, err)
	assert.Equal(t, "scalar", parsed.ResultType)
	assert.NotNil(t, parsed.Scalar)
	assert.Equal(t, model.SampleValue(42), parsed.Scalar.Value)
}

func TestParseMetricResponse_EmptyBody(t *testing.T) {
	_, err := parseMetricResponse(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty response body")
}

func TestParseMetricResponse_ErrorStatus(t *testing.T) {
	raw := []byte(`{"status":"error","errorType":"timeout","error":"query timeout"}`)
	_, err := parseMetricResponse(raw)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error")
}

func TestParseMetricResponse_InvalidJSON(t *testing.T) {
	_, err := parseMetricResponse([]byte(`{invalid`))
	assert.Error(t, err)
}

func TestParseMetricResponse_UnsupportedType(t *testing.T) {
	raw := []byte(`{"status":"success","data":{"resultType":"unknown","result":[]}}`)
	_, err := parseMetricResponse(raw)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported metric type")
}

// --------------------------------------------------------
// metricLabelsToMap tests
// --------------------------------------------------------

func TestMetricLabelsToMap(t *testing.T) {
	t.Run("skips __name__", func(t *testing.T) {
		m := metricLabelsToMap(model.Metric{
			"__name__": "http_requests_total",
			"app":      "foo",
			"status":   "200",
		})
		assert.Equal(t, map[string]string{"app": "foo", "status": "200"}, m)
	})

	t.Run("empty metric", func(t *testing.T) {
		m := metricLabelsToMap(model.Metric{})
		assert.Empty(t, m)
	})

	t.Run("only __name__", func(t *testing.T) {
		m := metricLabelsToMap(model.Metric{"__name__": "foo"})
		assert.Empty(t, m)
	})
}

// --------------------------------------------------------
// matrixByFingerprint / vectorByFingerprint tests
// --------------------------------------------------------

func TestMatrixByFingerprint(t *testing.T) {
	m := model.Matrix{
		{Metric: model.Metric{"app": "a"}, Values: []model.SamplePair{{Value: 1}}},
		{Metric: model.Metric{"app": "b"}, Values: []model.SamplePair{{Value: 2}}},
	}
	result := matrixByFingerprint(m)
	assert.Len(t, result, 2)

	for _, s := range m {
		fp := s.Metric.Fingerprint()
		assert.Equal(t, s, result[fp])
	}
}

func TestVectorByFingerprint(t *testing.T) {
	v := model.Vector{
		{Metric: model.Metric{"app": "a"}, Value: 1, Timestamp: 100},
		{Metric: model.Metric{"app": "b"}, Value: 2, Timestamp: 200},
	}
	result := vectorByFingerprint(v)
	assert.Len(t, result, 2)

	for _, s := range v {
		fp := s.Metric.Fingerprint()
		assert.Equal(t, s, result[fp])
	}
}

// --------------------------------------------------------
// MetricAnalyzer.Analyze tests
// --------------------------------------------------------

func makeVectorJSON(samples []vectorSample) []byte {
	type metricSample struct {
		Metric map[string]string `json:"metric"`
		Value  []interface{}     `json:"value"`
	}
	var result []metricSample
	for _, s := range samples {
		result = append(result, metricSample{
			Metric: s.labels,
			Value:  []interface{}{s.timestamp, s.value},
		})
	}
	data := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "vector",
			"result":     result,
		},
	}
	b, _ := json.Marshal(data)
	return b
}

type vectorSample struct {
	labels    map[string]string
	timestamp int64
	value     string
}

func makeMatrixJSON(series []matrixSeries) []byte {
	type metricSeries struct {
		Metric map[string]string `json:"metric"`
		Values [][]interface{}   `json:"values"`
	}
	var result []metricSeries
	for _, s := range series {
		ms := metricSeries{Metric: s.labels}
		for _, v := range s.values {
			ms.Values = append(ms.Values, []interface{}{v.timestamp, v.value})
		}
		result = append(result, ms)
	}
	data := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result":     result,
		},
	}
	b, _ := json.Marshal(data)
	return b
}

type matrixSeries struct {
	labels map[string]string
	values []matrixValue
}

type matrixValue struct {
	timestamp int64
	value     string
}

func makeScalarJSON(timestamp int64, value string) []byte {
	data := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "scalar",
			"result":     []interface{}{timestamp, value},
		},
	}
	b, _ := json.Marshal(data)
	return b
}

func TestMetricAnalyzer_VectorMissingMetric(t *testing.T) {
	analyzer := &MetricAnalyzer{}
	entry := &MismatchEntry{
		Query:     `count_over_time({app="foo"}[5m])`,
		StartTime: time.Unix(0, 0),
		EndTime:   time.Unix(100, 0),
	}

	cellA := makeVectorJSON([]vectorSample{
		{labels: map[string]string{"app": "foo"}, timestamp: 1000, value: "10"},
		{labels: map[string]string{"app": "bar"}, timestamp: 1000, value: "20"},
	})
	cellB := makeVectorJSON([]vectorSample{
		{labels: map[string]string{"app": "foo"}, timestamp: 1000, value: "10"},
	})

	results, err := analyzer.Analyze(context.Background(), entry, cellA, cellB)
	require.NoError(t, err)

	var found bool
	for _, r := range results {
		if r.DetectedMismatchType == "metric_missing_in_cell_b" {
			found = true
			assert.Contains(t, r.MismatchedItem, "bar")
		}
	}
	assert.True(t, found, "expected metric_missing_in_cell_b")
}

func TestMetricAnalyzer_VectorValueMismatch(t *testing.T) {
	analyzer := &MetricAnalyzer{}
	entry := &MismatchEntry{
		Query:     `count_over_time({app="foo"}[5m])`,
		StartTime: time.Unix(0, 0),
		EndTime:   time.Unix(100, 0),
	}

	cellA := makeVectorJSON([]vectorSample{
		{labels: map[string]string{"app": "foo"}, timestamp: 1000, value: "100"},
	})
	cellB := makeVectorJSON([]vectorSample{
		{labels: map[string]string{"app": "foo"}, timestamp: 1000, value: "200"},
	})

	results, err := analyzer.Analyze(context.Background(), entry, cellA, cellB)
	require.NoError(t, err)

	var found bool
	for _, r := range results {
		if r.DetectedMismatchType == "sample_value_mismatch" {
			found = true
			assert.Contains(t, r.Details, "delta=100")
		}
	}
	assert.True(t, found, "expected sample_value_mismatch")
}

func TestMetricAnalyzer_MatrixSampleCountMismatch(t *testing.T) {
	analyzer := &MetricAnalyzer{}
	entry := &MismatchEntry{
		Query:     `rate({app="foo"}[5m])`,
		StartTime: time.Unix(0, 0),
		EndTime:   time.Unix(100, 0),
	}

	cellA := makeMatrixJSON([]matrixSeries{
		{labels: map[string]string{"app": "foo"}, values: []matrixValue{
			{timestamp: 1000, value: "1"},
			{timestamp: 2000, value: "2"},
			{timestamp: 3000, value: "3"},
		}},
	})
	cellB := makeMatrixJSON([]matrixSeries{
		{labels: map[string]string{"app": "foo"}, values: []matrixValue{
			{timestamp: 1000, value: "1"},
		}},
	})

	results, err := analyzer.Analyze(context.Background(), entry, cellA, cellB)
	require.NoError(t, err)

	var found bool
	for _, r := range results {
		if r.DetectedMismatchType == "sample_count_mismatch" {
			found = true
			assert.Contains(t, r.Details, "3 samples")
			assert.Contains(t, r.Details, "1 samples")
		}
	}
	assert.True(t, found, "expected sample_count_mismatch")
}

func TestMetricAnalyzer_MatrixValueMismatch(t *testing.T) {
	analyzer := &MetricAnalyzer{}
	entry := &MismatchEntry{
		Query:     `rate({app="foo"}[5m])`,
		StartTime: time.Unix(0, 0),
		EndTime:   time.Unix(100, 0),
	}

	cellA := makeMatrixJSON([]matrixSeries{
		{labels: map[string]string{"app": "foo"}, values: []matrixValue{
			{timestamp: 1000, value: "100"},
			{timestamp: 2000, value: "200"},
		}},
	})
	cellB := makeMatrixJSON([]matrixSeries{
		{labels: map[string]string{"app": "foo"}, values: []matrixValue{
			{timestamp: 1000, value: "100"},
			{timestamp: 2000, value: "999"},
		}},
	})

	results, err := analyzer.Analyze(context.Background(), entry, cellA, cellB)
	require.NoError(t, err)

	var found bool
	for _, r := range results {
		if r.DetectedMismatchType == "sample_value_mismatch" {
			found = true
			assert.Contains(t, r.Details, "delta=")
		}
	}
	assert.True(t, found, "expected sample_value_mismatch")
}

func TestMetricAnalyzer_ScalarMismatch(t *testing.T) {
	analyzer := &MetricAnalyzer{}
	entry := &MismatchEntry{
		Query:     `count(rate({app="foo"}[5m]))`,
		StartTime: time.Unix(0, 0),
		EndTime:   time.Unix(100, 0),
	}

	cellA := makeScalarJSON(1000, "42")
	cellB := makeScalarJSON(1000, "99")

	results, err := analyzer.Analyze(context.Background(), entry, cellA, cellB)
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, "scalar_value_mismatch", results[0].DetectedMismatchType)
	assert.Contains(t, results[0].Details, "delta=57")
}

func TestMetricAnalyzer_ScalarIdentical(t *testing.T) {
	analyzer := &MetricAnalyzer{}
	entry := &MismatchEntry{
		Query:     `count(rate({app="foo"}[5m]))`,
		StartTime: time.Unix(0, 0),
		EndTime:   time.Unix(100, 0),
	}

	payload := makeScalarJSON(1000, "42")

	results, err := analyzer.Analyze(context.Background(), entry, payload, payload)
	require.NoError(t, err)
	assert.Empty(t, results, "identical scalars should produce no results")
}

func TestMetricAnalyzer_IdenticalVectors(t *testing.T) {
	analyzer := &MetricAnalyzer{}
	entry := &MismatchEntry{
		Query:     `count_over_time({app="foo"}[5m])`,
		StartTime: time.Unix(0, 0),
		EndTime:   time.Unix(100, 0),
	}

	payload := makeVectorJSON([]vectorSample{
		{labels: map[string]string{"app": "foo"}, timestamp: 1000, value: "42"},
	})

	results, err := analyzer.Analyze(context.Background(), entry, payload, payload)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMetricAnalyzer_DifferentResultTypes(t *testing.T) {
	analyzer := &MetricAnalyzer{}
	entry := &MismatchEntry{Query: `rate({app="foo"}[5m])`}

	cellA := makeVectorJSON([]vectorSample{
		{labels: map[string]string{"app": "foo"}, timestamp: 1000, value: "1"},
	})
	cellB := makeMatrixJSON([]matrixSeries{
		{labels: map[string]string{"app": "foo"}, values: []matrixValue{{timestamp: 1000, value: "1"}}},
	})

	results, err := analyzer.Analyze(context.Background(), entry, cellA, cellB)
	require.NoError(t, err)
	assert.Empty(t, results, "mismatched result types should be handled by ResultTypeMismatchAnalyzer, not MetricAnalyzer")
}

func TestMetricAnalyzer_ParseError(t *testing.T) {
	analyzer := &MetricAnalyzer{}
	entry := &MismatchEntry{Query: `{app="foo"}`}

	results, err := analyzer.Analyze(context.Background(), entry, []byte(`{invalid`), []byte(`{invalid`))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "parse_error", results[0].DetectedMismatchType)
}
