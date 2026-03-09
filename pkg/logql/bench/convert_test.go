package bench

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

func TestConvertResult_Streams(t *testing.T) {
	now := time.Now()
	input := loghttp.Streams{
		{
			Labels: loghttp.LabelSet{"app": "foo", "env": "prod"},
			Entries: []loghttp.Entry{
				{Timestamp: now, Line: "log line 1"},
				{Timestamp: now.Add(time.Second), Line: "log line 2"},
			},
		},
	}

	result, err := ConvertResult(input)
	require.NoError(t, err)

	// Result should be of type logqlmodel.Streams — since we can't import logqlmodel
	// here directly, verify via the parser.Value interface
	require.NotNil(t, result)
	assert.Equal(t, "streams", string(result.Type()))
}

func TestConvertResult_Matrix(t *testing.T) {
	ts1 := model.Time(1000)
	ts2 := model.Time(2000)

	input := loghttp.Matrix{
		{
			Metric: model.Metric{
				"job":      "test",
				"instance": "localhost",
			},
			Values: []model.SamplePair{
				{Timestamp: ts1, Value: 1.5},
				{Timestamp: ts2, Value: 2.5},
			},
		},
	}

	result, err := ConvertResult(input)
	require.NoError(t, err)

	matrix, ok := result.(promql.Matrix)
	require.True(t, ok, "expected promql.Matrix but got %T", result)
	require.Len(t, matrix, 1)

	series := matrix[0]
	require.Len(t, series.Floats, 2)

	assert.Equal(t, int64(ts1), series.Floats[0].T)
	assert.Equal(t, float64(1.5), series.Floats[0].F)
	assert.Equal(t, int64(ts2), series.Floats[1].T)
	assert.Equal(t, float64(2.5), series.Floats[1].F)

	// Check labels
	assert.Equal(t, "test", series.Metric.Get("job"))
	assert.Equal(t, "localhost", series.Metric.Get("instance"))
}

func TestConvertResult_Matrix_MultiSeries(t *testing.T) {
	tests := []struct {
		name   string
		input  loghttp.Matrix
		wantNS int // number of series
	}{
		{
			name:   "empty matrix",
			input:  loghttp.Matrix{},
			wantNS: 0,
		},
		{
			name: "two series",
			input: loghttp.Matrix{
				{
					Metric: model.Metric{"job": "a"},
					Values: []model.SamplePair{{Timestamp: 1000, Value: 10}},
				},
				{
					Metric: model.Metric{"job": "b"},
					Values: []model.SamplePair{{Timestamp: 2000, Value: 20}},
				},
			},
			wantNS: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ConvertResult(tc.input)
			require.NoError(t, err)

			matrix, ok := result.(promql.Matrix)
			require.True(t, ok)
			assert.Len(t, matrix, tc.wantNS)
		})
	}
}

func TestConvertResult_Vector(t *testing.T) {
	ts := model.Time(5000)

	input := loghttp.Vector{
		{
			Metric:    model.Metric{"__name__": "up", "job": "prometheus"},
			Value:     1.0,
			Timestamp: ts,
		},
		{
			Metric:    model.Metric{"__name__": "up", "job": "node"},
			Value:     0.0,
			Timestamp: ts,
		},
	}

	result, err := ConvertResult(input)
	require.NoError(t, err)

	vector, ok := result.(promql.Vector)
	require.True(t, ok, "expected promql.Vector but got %T", result)
	require.Len(t, vector, 2)

	assert.Equal(t, int64(ts), vector[0].T)
	assert.Equal(t, float64(1.0), vector[0].F)
	assert.Equal(t, "prometheus", vector[0].Metric.Get("job"))

	assert.Equal(t, int64(ts), vector[1].T)
	assert.Equal(t, float64(0.0), vector[1].F)
	assert.Equal(t, "node", vector[1].Metric.Get("job"))
}

func TestConvertResult_Vector_Empty(t *testing.T) {
	input := loghttp.Vector{}

	result, err := ConvertResult(input)
	require.NoError(t, err)

	vector, ok := result.(promql.Vector)
	require.True(t, ok)
	assert.Empty(t, vector)
}

func TestConvertResult_Scalar(t *testing.T) {
	input := loghttp.Scalar{
		Value:     model.SampleValue(42.5),
		Timestamp: model.Time(9000),
	}

	result, err := ConvertResult(input)
	require.NoError(t, err)

	scalar, ok := result.(promql.Scalar)
	require.True(t, ok, "expected promql.Scalar but got %T", result)

	assert.Equal(t, int64(9000), scalar.T)
	assert.Equal(t, float64(42.5), scalar.V)
}

func TestConvertResult_UnknownType(t *testing.T) {
	// Use an anonymous type that implements loghttp.ResultValue
	type unknownResultType struct{}
	// We can't create an unknownResultType that implements loghttp.ResultValue
	// without defining Type() method. Instead, test using nil after casting.
	// We pass nil to see the error path for unknown types — but since nil
	// won't match any case, we verify the error is returned.
	//
	// Since loghttp.ResultValue is an interface, we can create a concrete
	// implementation inline.

	// Instead, test with a custom struct that happens to implement ResultValue
	// by testing that the function correctly returns an error for unsupported types.
	// We'll use a helper type in the test.
	result, err := ConvertResult(testUnknownResultValue{})
	assert.Nil(t, result)
	assert.ErrorContains(t, err, "unknown result type")
}

// testUnknownResultValue is a test helper that implements loghttp.ResultValue
// but is not one of the known types.
type testUnknownResultValue struct{}

func (testUnknownResultValue) Type() loghttp.ResultType { return "unknown" }

func TestConvertResult_Matrix_FPointTimestampConversion(t *testing.T) {
	// Verify timestamp conversion: model.Time is Unix ms as int64
	// promql.FPoint.T is also Unix ms as int64
	msTimestamp := model.Time(1700000000000) // some Unix ms timestamp

	input := loghttp.Matrix{
		{
			Metric: model.Metric{"job": "test"},
			Values: []model.SamplePair{
				{Timestamp: msTimestamp, Value: model.SampleValue(3.14)},
			},
		},
	}

	result, err := ConvertResult(input)
	require.NoError(t, err)

	matrix := result.(promql.Matrix)
	require.Len(t, matrix[0].Floats, 1)
	assert.Equal(t, int64(msTimestamp), matrix[0].Floats[0].T)
	assert.InDelta(t, 3.14, matrix[0].Floats[0].F, 1e-10)
}

func TestConvertResult_Vector_Timestamp(t *testing.T) {
	// Verify timestamp conversion for vector
	msTimestamp := model.Time(1700000000000)

	input := loghttp.Vector{
		{
			Metric:    model.Metric{"job": "test"},
			Value:     model.SampleValue(99.9),
			Timestamp: msTimestamp,
		},
	}

	result, err := ConvertResult(input)
	require.NoError(t, err)

	vector := result.(promql.Vector)
	require.Len(t, vector, 1)
	assert.Equal(t, int64(msTimestamp), vector[0].T)
	assert.InDelta(t, 99.9, vector[0].F, 1e-10)
}
