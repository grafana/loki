package sketch

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDDSketchQuantile_Quantile(t *testing.T) {
	t.Run("should linearly interpolate values", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			values []float64
			q      float64
			exact  float64
		}{
			{name: "two values, median", values: []float64{2, 4}, q: 0.5, exact: 3},
			{name: "four values, median", values: []float64{2, 4, 10, 20}, q: 0.5, exact: 7},
			{name: "four values, q=0.25", values: []float64{2, 4, 10, 20}, q: 0.25, exact: 3.5},
			{name: "four values, q=0.75", values: []float64{2, 4, 10, 20}, q: 0.75, exact: 12.5},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s := newDDSketchWithValues(t, tc.values...)

				actual, err := s.Quantile(tc.q)
				require.NoError(t, err)
				require.InEpsilon(t, tc.exact, actual, 0.02)
			})
		}
	})

	// Exercises interpolation between order statistics that fall on either side of
	// the sketch's separately tracked negative, zero, and positive stores.
	t.Run("should linearly interpolate across sign boundary", func(t *testing.T) {
		for _, tc := range []struct {
			name           string
			values         []float64
			q              float64
			exact          float64
			toleratedDelta float64
		}{
			// Rank lands exactly on the two zero-valued order statistics: interpolating
			// between them must still yield exactly 0, not a rounded approximation.
			{name: "interpolates within the zero bucket", values: []float64{-10, -5, 0, 0, 3, 8}, q: 0.5, exact: 0, toleratedDelta: 0},
			// Rank lands between a negative and a positive order statistic, with no
			// zero values in between.
			{name: "interpolates negative to positive", values: []float64{-4, 6}, q: 0.5, exact: 1, toleratedDelta: 0.1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s := newDDSketchWithValues(t, tc.values...)

				actual, err := s.Quantile(tc.q)
				require.NoError(t, err)
				require.InDelta(t, tc.exact, actual, tc.toleratedDelta)
			})
		}
	})

	t.Run("empty sketch", func(t *testing.T) {
		s := NewDDSketch()

		actual, err := s.Quantile(0.5)
		require.NoError(t, err)
		require.True(t, math.IsNaN(actual))
	})

	t.Run("single value", func(t *testing.T) {
		s := newDDSketchWithValues(t, 5)

		for _, q := range []float64{0, 0.3, 0.5, 1} {
			actual, err := s.Quantile(q)
			require.NoError(t, err)
			require.InEpsilon(t, 5.0, actual, 0.02)
		}
	})

	t.Run("0th and 100th quantile", func(t *testing.T) {
		s := newDDSketchWithValues(t, 2, 4, 10, 20)

		minQuantile, err := s.Quantile(0)
		require.NoError(t, err)
		require.InEpsilon(t, 2.0, minQuantile, 0.02)

		maxQuantile, err := s.Quantile(1)
		require.NoError(t, err)
		require.InEpsilon(t, 20.0, maxQuantile, 0.02)
	})

	t.Run("quantile outside [0, 1]", func(t *testing.T) {
		s := newDDSketchWithValues(t, 1, 2, 3)

		below, err := s.Quantile(-0.1)
		require.NoError(t, err)
		require.Equal(t, math.Inf(-1), below)

		above, err := s.Quantile(1.1)
		require.NoError(t, err)
		require.Equal(t, math.Inf(1), above)
	})
}

// newDDSketchWithValues returns a DDSketchQuantile with each of values added to it.
func newDDSketchWithValues(t *testing.T, values ...float64) *DDSketchQuantile {
	t.Helper()

	s := NewDDSketch()
	for _, v := range values {
		require.NoError(t, s.Add(v))
	}
	return s
}
