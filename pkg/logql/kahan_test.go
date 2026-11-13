package logql

import (
	"fmt"
	"math"
	"math/big"
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

func TestKahanVariance(t *testing.T) {
	// sequences holds random sequences with a wide range of magnitudes, and sequences with
	// infinities and NaN.
	sequences := map[string][]float64{
		"infinity after finite values":    {1, 2, math.Inf(1)},
		"infinity before finite values":   {math.Inf(-1), 1, 2},
		"opposite infinities":             {math.Inf(1), math.Inf(-1)},
		"NaN between finite values":       {1, math.NaN(), 2},
		"values that overflow the deltas": {math.MaxFloat64, -math.MaxFloat64, 1},
	}
	rng := rand.New(rand.NewPCG(7, 8))
	for i := range 300 {
		values := make([]float64, 1+rng.IntN(30))
		for j := range values {
			values[j] = rng.NormFloat64() * math.Pow(10, float64(rng.IntN(30)-10))
		}
		sequences[fmt.Sprintf("random values %d", i)] = values
	}

	t.Run("returns NaN when nothing was added", func(t *testing.T) {
		var v kahanVariance

		require.True(t, math.IsNaN(v.result()))
	})

	t.Run("returns zero for a single value", func(t *testing.T) {
		var v kahanVariance
		v.addValue(math.MaxFloat64)

		require.Equal(t, 0.0, v.result())
	})

	t.Run("returns the population variance of the added values", func(t *testing.T) {
		var v kahanVariance
		for _, x := range []float64{0, 8, 8, 2, 3} {
			v.addValue(x)
		}

		require.InEpsilon(t, 10.56, v.result(), 1e-12)
	})

	t.Run("returns zero for repeated values that a float cannot represent exactly", func(t *testing.T) {
		var v kahanVariance
		for range 3 {
			v.addValue(1.5990505637277868)
		}

		require.Equal(t, 0.0, v.result())
	})

	t.Run("returns NaN when a value is NaN", func(t *testing.T) {
		var v kahanVariance
		v.addValue(1)
		v.addValue(math.NaN())

		require.True(t, math.IsNaN(v.result()))
	})

	t.Run("returns NaN when a value is infinite", func(t *testing.T) {
		for _, inf := range []float64{math.Inf(1), math.Inf(-1)} {
			var v kahanVariance
			v.addValue(1)
			v.addValue(inf)

			require.True(t, math.IsNaN(v.result()), "infinity %v", inf)
		}
	})

	t.Run("keeps the precision of the variance of many values around a large mean", func(t *testing.T) {
		const mean = 1e10
		values := make([]float64, 100_000)
		var sum, sumOfSquares, count big.Int
		count.SetInt64(int64(len(values)))
		for i := range values {
			offset := int64(i % 7)
			values[i] = mean + float64(offset)
			x := big.NewInt(mean + offset)
			sum.Add(&sum, x)
			sumOfSquares.Add(&sumOfSquares, new(big.Int).Mul(x, x))
		}
		// The exact variance is (count*sumOfSquares - sum*sum) / count^2.
		numerator := new(big.Int).Sub(new(big.Int).Mul(&count, &sumOfSquares), new(big.Int).Mul(&sum, &sum))
		exact, _ := new(big.Rat).SetFrac(numerator, new(big.Int).Mul(&count, &count)).Float64()

		var plain, plainCount, plainMean float64
		for _, x := range values {
			plainCount++
			delta := x - plainMean
			plainMean += delta / plainCount
			plain += delta * (x - plainMean)
		}
		require.Greater(t, math.Abs(plain/plainCount-exact)/exact, 1e-9, "plain Welford should not reach this precision")

		var v kahanVariance
		for _, x := range values {
			v.addValue(x)
		}
		require.InEpsilon(t, exact, v.result(), 1e-9)

		var points kahanVariance
		points.addPoints(fpoints(values...))
		require.Equal(t, v.result(), points.result())
	})

	t.Run("keeps the small deviations that follow a large deviation", func(t *testing.T) {
		// The mean is 1e8. The squared deviations add up to 2*1e16 + 10000, and the variance is
		// that sum divided by the number of values.
		const small = 10_000
		values := []float64{0, 2e8}
		for i := range small {
			values = append(values, 1e8+float64(1-2*(i%2)))
		}
		want := (2e16 + small) / float64(len(values))

		var plain, plainCount, plainMean float64
		for _, x := range values {
			plainCount++
			delta := x - plainMean
			plainMean += delta / plainCount
			plain += delta * (x - plainMean)
		}
		require.Greater(t, math.Abs(plain/plainCount-want)/want, 1e-14, "plain Welford should not reach this precision")

		var v kahanVariance
		for _, x := range values {
			v.addValue(x)
		}
		require.InEpsilon(t, want, v.result(), 1e-14)

		var points kahanVariance
		points.addPoints(fpoints(values...))
		require.Equal(t, v.result(), points.result())
	})

	t.Run("returns the same value as stdvarOverTime and StdvarOverTime for the same values", func(t *testing.T) {
		for name, values := range sequences {
			t.Run(name, func(t *testing.T) {
				var oneByOne kahanVariance
				var streaming StdvarOverTime
				for _, v := range values {
					oneByOne.addValue(v)
					streaming.agg(promql.FPoint{F: v})
				}

				want := oneByOne.result()
				requireSameFloat(t, want, stdvarOverTime(fpoints(values...), 0))
				requireSameFloat(t, want, streaming.at())
			})
		}
	})

	t.Run("returns the square root of the variance for stddevOverTime and StddevOverTime", func(t *testing.T) {
		for name, values := range sequences {
			t.Run(name, func(t *testing.T) {
				var oneByOne kahanVariance
				var streaming StddevOverTime
				for _, v := range values {
					oneByOne.addValue(v)
					streaming.agg(promql.FPoint{F: v})
				}

				want := math.Sqrt(oneByOne.result())
				requireSameFloat(t, want, stddevOverTime(fpoints(values...), 0))
				requireSameFloat(t, want, streaming.at())
			})
		}
	})

	t.Run("addPoints returns NaN when no point is given", func(t *testing.T) {
		var v kahanVariance
		v.addPoints(nil)

		require.True(t, math.IsNaN(v.result()))
	})

	t.Run("addPoints returns the same value as addValue when the points arrive in two calls", func(t *testing.T) {
		for name, values := range sequences {
			t.Run(name, func(t *testing.T) {
				var oneByOne kahanVariance
				for _, v := range values {
					oneByOne.addValue(v)
				}

				for k := 0; k <= len(values); k++ {
					var v kahanVariance
					v.addPoints(fpoints(values[:k]...))
					v.addPoints(fpoints(values[k:]...))

					requireSameFloat(t, oneByOne.result(), v.result(), "split at %d", k)
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
