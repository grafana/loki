package logql

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

func TestKahanSum(t *testing.T) {
	t.Run("returns zero when nothing was added", func(t *testing.T) {
		var s kahanSum

		require.Equal(t, 0.0, s.result())
	})

	t.Run("keeps the small values between a large value and the same value negated", func(t *testing.T) {
		var s kahanSum
		s.addValue(1e16)
		for range 10 {
			s.addValue(1)
		}
		s.addValue(-1e16)

		require.Equal(t, 10.0, s.result())
	})

	t.Run("returns an infinite sum as it is", func(t *testing.T) {
		var positive, negative kahanSum
		for _, v := range []float64{1, math.Inf(1), 0.1, 1e16} {
			positive.addValue(v)
		}
		for _, v := range []float64{1, math.Inf(-1), 0.1, 1e16} {
			negative.addValue(v)
		}

		require.Equal(t, math.Inf(1), positive.result())
		require.Equal(t, math.Inf(-1), negative.result())
	})

	t.Run("returns NaN for a positive and a negative infinity", func(t *testing.T) {
		var s kahanSum
		s.addValue(math.Inf(1))
		s.addValue(math.Inf(-1))

		require.True(t, math.IsNaN(s.result()))
	})

	t.Run("addPoints keeps the small values between a large value and the same value negated", func(t *testing.T) {
		var s kahanSum
		s.addPoints(fpoints(1e16, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, -1e16))

		require.Equal(t, 10.0, s.result())
	})

	t.Run("addPoints continues from the values added before", func(t *testing.T) {
		var s kahanSum
		s.addValue(1e16)
		s.addPoints(fpoints(1, 1, 1, 1, 1, 1, 1, 1, 1, 1))
		s.addValue(-1e16)

		require.Equal(t, 10.0, s.result())
	})

	t.Run("addPoints keeps the sum as it is when no point is given", func(t *testing.T) {
		var s kahanSum
		s.addValue(1.5)
		s.addPoints(nil)

		require.Equal(t, 1.5, s.result())
	})

	t.Run("addPoints returns the same sum as addValue when the points arrive in two calls", func(t *testing.T) {
		rng := rand.New(rand.NewPCG(1, 2))
		for i := range 200 {
			values := make([]float64, 1+rng.IntN(30))
			for j := range values {
				values[j] = rng.NormFloat64() * math.Pow(10, float64(rng.IntN(30)-10))
			}
			var oneByOne kahanSum
			for _, v := range values {
				oneByOne.addValue(v)
			}

			for k := 0; k <= len(values); k++ {
				var s kahanSum
				s.addPoints(fpoints(values[:k]...))
				s.addPoints(fpoints(values[k:]...))

				require.Equal(t, oneByOne.result(), s.result(), "sequence %d split at %d", i, k)
			}
		}
	})
}

func TestKahanAvg(t *testing.T) {
	// sequences holds the shared case tables and random sequences, some of which overflow the sum.
	sequences := map[string][]float64{}
	for _, tc := range avgPrecisionCases {
		sequences[tc.name] = tc.values
	}
	for _, tc := range avgInfHandlingCases {
		sequences[tc.name] = tc.values
	}
	rng := rand.New(rand.NewPCG(5, 6))
	for i := range 300 {
		values := make([]float64, 1+rng.IntN(30))
		for j := range values {
			switch i % 3 {
			case 0:
				values[j] = (rng.Float64() - 0.5) * math.MaxFloat64
			default:
				values[j] = rng.NormFloat64() * math.Pow(10, float64(rng.IntN(30)-10))
			}
		}
		sequences[fmt.Sprintf("random values %d", i)] = values
	}

	t.Run("returns NaN when nothing was added", func(t *testing.T) {
		var m kahanAvg

		require.True(t, math.IsNaN(m.result()))
	})

	t.Run("returns the only value that was added", func(t *testing.T) {
		var m kahanAvg
		m.addValue(math.MaxFloat64)

		require.Equal(t, math.MaxFloat64, m.result())
	})

	t.Run("divides the compensated sum while the sum does not overflow", func(t *testing.T) {
		var m kahanAvg
		for _, v := range []float64{1e10, 1e-6, 1e-6, 1e-6, 1e-6, 1e-6} {
			m.addValue(v)
		}

		require.False(t, m.incremental)
		require.Equal(t, 1.6666666666666675e+09, m.result())
	})

	t.Run("switches to the incremental mean when the sum overflows", func(t *testing.T) {
		big := 0.6 * math.MaxFloat64
		var m kahanAvg
		for _, v := range []float64{big, big, 3, 4} {
			m.addValue(v)
		}

		require.True(t, m.incremental)
		require.InEpsilon(t, 0.3*math.MaxFloat64, m.result(), 1e-9)
	})

	t.Run("switches to the incremental mean when a negative sum overflows", func(t *testing.T) {
		big := 0.6 * math.MaxFloat64
		var m kahanAvg
		for _, v := range []float64{-1, -2, -3, -big, -big, -3, -4} {
			m.addValue(v)
		}

		require.True(t, m.incremental)
		require.InEpsilon(t, -(2*(big/7) + 13.0/7), m.result(), 1e-9)
	})

	t.Run("returns the same value as avgOverTime and AvgOverTime for the same values", func(t *testing.T) {
		for name, values := range sequences {
			t.Run(name, func(t *testing.T) {
				var oneByOne kahanAvg
				var streaming AvgOverTime
				for _, v := range values {
					oneByOne.addValue(v)
					streaming.agg(promql.FPoint{F: v})
				}

				want := oneByOne.result()
				requireSameFloat(t, want, avgOverTime(fpoints(values...), 0))
				requireSameFloat(t, want, streaming.at())
			})
		}
	})

	t.Run("addPoints returns NaN when no point is given", func(t *testing.T) {
		var m kahanAvg
		m.addPoints(nil)

		require.True(t, math.IsNaN(m.result()))
	})

	t.Run("addPoints returns the only point that is given", func(t *testing.T) {
		var m kahanAvg
		m.addPoints(fpoints(math.MaxFloat64))

		require.Equal(t, math.MaxFloat64, m.result())
	})

	t.Run("addPoints switches to the incremental mean when the sum overflows", func(t *testing.T) {
		big := 0.6 * math.MaxFloat64
		var m kahanAvg
		m.addPoints(fpoints(big, big, 3, 4))

		require.True(t, m.incremental)
		require.InEpsilon(t, 0.3*math.MaxFloat64, m.result(), 1e-9)
	})

	t.Run("addPoints continues the incremental mean after the sum overflowed", func(t *testing.T) {
		big := 0.6 * math.MaxFloat64
		var m kahanAvg
		m.addValue(big)
		m.addValue(big)
		m.addPoints(fpoints(3, 4))

		require.True(t, m.incremental)
		require.InEpsilon(t, 0.3*math.MaxFloat64, m.result(), 1e-9)
	})

	t.Run("addPoints returns the same value as addValue when the points arrive in two calls", func(t *testing.T) {
		for name, values := range sequences {
			t.Run(name, func(t *testing.T) {
				var oneByOne kahanAvg
				for _, v := range values {
					oneByOne.addValue(v)
				}

				for k := 0; k <= len(values); k++ {
					var m kahanAvg
					m.addPoints(fpoints(values[:k]...))
					m.addPoints(fpoints(values[k:]...))

					requireSameFloat(t, oneByOne.result(), m.result(), "split at %d", k)
				}
			})
		}
	})
}

// requireSameFloat requires that got equals want, and that got is NaN when want is NaN.
func requireSameFloat(t *testing.T, want, got float64, msgAndArgs ...any) {
	t.Helper()
	if math.IsNaN(want) {
		require.True(t, math.IsNaN(got), msgAndArgs...)
		return
	}
	require.Equal(t, want, got, msgAndArgs...)
}
