package bench

import (
	"testing"

	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// assertResultNotEmpty verifies that a query result is not empty
// This catches templating issues where queries may resolve to selectors that match no data
func assertResultNotEmpty(t *testing.T, data parser.Value, message string) {
	t.Helper()
	switch v := data.(type) {
	case promql.Vector:
		require.NotEmpty(t, v, message)
	case promql.Matrix:
		require.NotEmpty(t, v, message)
		// Also check that at least one series has data points
		hasData := false
		for _, series := range v {
			if len(series.Floats) > 0 || len(series.Histograms) > 0 {
				hasData = true
				break
			}
		}
		require.True(t, hasData, message+" - matrix has series but no data points")
	case promql.Scalar:
		// Scalars always have a value, so no need to check
	case logqlmodel.Streams:
		require.NotEmpty(t, v, message)
	default:
		t.Fatalf("unknown result type: %T", data)
	}
}

// assertDataEqualWithTolerance compares two parser.Value instances with floating point tolerance
func assertDataEqualWithTolerance(t *testing.T, expected, actual parser.Value, tolerance float64) {
	t.Helper()
	switch expectedType := expected.(type) {
	case promql.Vector:
		actualVector, ok := actual.(promql.Vector)
		require.True(t, ok, "expected Vector but got %T", actual)
		assertVectorEqualWithTolerance(t, expectedType, actualVector, tolerance)
	case promql.Matrix:
		actualMatrix, ok := actual.(promql.Matrix)
		require.True(t, ok, "expected Matrix but got %T", actual)
		assertMatrixEqualWithTolerance(t, expectedType, actualMatrix, tolerance)
	case promql.Scalar:
		actualScalar, ok := actual.(promql.Scalar)
		require.True(t, ok, "expected Scalar but got %T", actual)
		assertScalarEqualWithTolerance(t, expectedType, actualScalar, tolerance)
	default:
		assert.Equal(t, expected, actual)
	}
}

// assertVectorEqualWithTolerance compares two Vector instances with floating point tolerance
func assertVectorEqualWithTolerance(t *testing.T, expected, actual promql.Vector, tolerance float64) {
	t.Helper()
	require.Len(t, actual, len(expected))

	for i := range expected {
		e := expected[i]
		a := actual[i]
		require.Equal(t, e.Metric, a.Metric, "metric labels differ at index %d", i)
		require.Equal(t, e.T, a.T, "timestamp differs at index %d", i)

		if tolerance > 0 {
			require.InDelta(t, e.F, a.F, tolerance, "float value differs at index %d", i)
		} else {
			require.Equal(t, e.F, a.F, "float value differs at index %d", i)
		}
	}
}

// assertMatrixEqualWithTolerance compares two Matrix instances with floating point tolerance
func assertMatrixEqualWithTolerance(t *testing.T, expected, actual promql.Matrix, tolerance float64) {
	t.Helper()
	require.Equal(t, len(expected), len(actual), "number of entries differs")

	for i := range expected {
		e := expected[i]
		a := actual[i]
		require.Equal(t, e.Metric, a.Metric, "metric labels differ at series %d", i)
		require.Len(t, a.Floats, len(e.Floats), "float points count differs at series %d", i)

		for j := range e.Floats {
			ePoint := e.Floats[j]
			aPoint := a.Floats[j]
			require.Equal(t, ePoint.T, aPoint.T, "timestamp differs at series %d, point %d", i, j)

			if tolerance > 0 {
				require.InDelta(t, ePoint.F, aPoint.F, tolerance, "float value differs at series %d, point %d", i, j)
			} else {
				require.Equal(t, ePoint.F, aPoint.F, "float value differs at series %d, point %d", i, j)
			}
		}
	}
}

// assertScalarEqualWithTolerance compares two Scalar instances with floating point tolerance
func assertScalarEqualWithTolerance(t *testing.T, expected, actual promql.Scalar, tolerance float64) {
	t.Helper()
	require.Equal(t, expected.T, actual.T, "scalar timestamp differs")

	if tolerance > 0 {
		require.InDelta(t, expected.V, actual.V, tolerance, "scalar value differs")
	} else {
		require.Equal(t, expected.V, actual.V, "scalar value differs")
	}
}
