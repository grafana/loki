package logql

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestCountAccumulator(t *testing.T) {
	// stepValues returns the value of every step, with a negative value for a step that has none.
	stepValues := func(t *testing.T, a stepAccumulator, steps int) []float64 {
		t.Helper()
		out := make([]float64, steps)
		for k := range steps {
			v, ok := a.value(k)
			if !ok {
				require.Zero(t, v)
				v = -1
			}
			out[k] = v
		}
		return out
	}

	t.Run("counts a sample in every step from lo to hi", func(t *testing.T) {
		a := newCountAccumulator(5)
		a.add(1, 3, 1)
		a.finish()

		require.Equal(t, []float64{-1, 1, 1, 1, -1}, stepValues(t, a, 5))
	})

	t.Run("adds up the samples that share a step", func(t *testing.T) {
		a := newCountAccumulator(4)
		a.add(0, 2, 1)
		a.add(1, 3, 1)
		a.add(1, 1, 1)
		a.finish()

		require.Equal(t, []float64{1, 3, 2, 1}, stepValues(t, a, 4))
	})

	t.Run("counts a sample in the last step", func(t *testing.T) {
		a := newCountAccumulator(3)
		a.add(2, 2, 1)
		a.finish()

		require.Equal(t, []float64{-1, -1, 1}, stepValues(t, a, 3))
	})

	t.Run("counts a sample in the only step of an instant query", func(t *testing.T) {
		a := newCountAccumulator(1)
		a.add(0, 0, 1)
		a.add(0, 0, 1)
		a.finish()

		require.Equal(t, []float64{2}, stepValues(t, a, 1))
	})

	t.Run("counts samples whatever their value", func(t *testing.T) {
		a := newCountAccumulator(2)
		a.add(0, 1, 0)
		a.add(0, 1, -7.5)
		a.finish()

		require.Equal(t, []float64{2, 2}, stepValues(t, a, 2))
	})

	t.Run("allocates once when created and never while adding or reading samples", func(t *testing.T) {
		require.Equal(t, 1.0, testing.AllocsPerRun(100, func() { _ = newCountAccumulator(10) }))

		a := newCountAccumulator(10)
		require.Zero(t, testing.AllocsPerRun(100, func() {
			a.add(1, 3, 1)
			a.finish()
			_, _ = a.value(2)
		}))
	})

	t.Run("panics when value is called before finish", func(t *testing.T) {
		a := newCountAccumulator(2)
		a.add(0, 1, 1)

		require.Panics(t, func() { _, _ = a.value(0) })
	})

	t.Run("reports no value for any step without samples", func(t *testing.T) {
		a := newCountAccumulator(3)
		a.finish()

		require.Equal(t, []float64{-1, -1, -1}, stepValues(t, a, 3))
	})
}

func TestSumAccumulator(t *testing.T) {
	type stepValue struct {
		value   float64
		present bool
	}

	stepValues := func(t *testing.T, a stepAccumulator, steps int) []stepValue {
		t.Helper()
		out := make([]stepValue, steps)
		for k := range steps {
			v, ok := a.value(k)
			if !ok {
				require.Zero(t, v)
			}
			out[k] = stepValue{value: v, present: ok}
		}
		return out
	}

	t.Run("adds the value of a sample to every step from lo to hi", func(t *testing.T) {
		a := newSumAccumulator(5)
		a.add(1, 3, 2.5)
		a.finish()

		require.Equal(t, []stepValue{{0, false}, {2.5, true}, {2.5, true}, {2.5, true}, {0, false}}, stepValues(t, a, 5))
	})

	t.Run("adds up the samples that share a step", func(t *testing.T) {
		a := newSumAccumulator(4)
		a.add(0, 2, 1)
		a.add(1, 3, 10)
		a.add(1, 1, 100)
		a.finish()

		require.Equal(t, []stepValue{{1, true}, {111, true}, {11, true}, {10, true}}, stepValues(t, a, 4))
	})

	t.Run("adds a sample in the last step", func(t *testing.T) {
		a := newSumAccumulator(3)
		a.add(2, 2, 4)
		a.finish()

		require.Equal(t, []stepValue{{0, false}, {0, false}, {4, true}}, stepValues(t, a, 3))
	})

	t.Run("reports no value for a step between two steps that hold samples", func(t *testing.T) {
		a := newSumAccumulator(3)
		a.add(0, 0, 2)
		a.add(2, 2, 3)
		a.finish()

		require.Equal(t, []stepValue{{2, true}, {0, false}, {3, true}}, stepValues(t, a, 3))
	})

	t.Run("adds a sample in the only step of an instant query", func(t *testing.T) {
		a := newSumAccumulator(1)
		a.add(0, 0, 3)
		a.add(0, 0, 4)
		a.finish()

		require.Equal(t, []stepValue{{7, true}}, stepValues(t, a, 1))
	})

	t.Run("reports a value for a step whose samples sum to zero", func(t *testing.T) {
		a := newSumAccumulator(3)
		a.add(0, 1, 1.5)
		a.add(0, 0, -1.5)
		a.add(2, 2, 0)
		a.finish()

		require.Equal(t, []stepValue{{0, true}, {1.5, true}, {0, true}}, stepValues(t, a, 3))
	})

	t.Run("reports no value for any step without samples", func(t *testing.T) {
		a := newSumAccumulator(3)
		a.finish()

		require.Equal(t, []stepValue{{0, false}, {0, false}, {0, false}}, stepValues(t, a, 3))
	})

	t.Run("keeps the sum of later steps exact after a large value in an earlier step", func(t *testing.T) {
		a := newSumAccumulator(3)
		a.add(0, 0, 1e16)
		a.add(1, 1, 1.5)
		a.add(2, 2, 0.1)
		a.finish()

		require.Equal(t, []stepValue{{1e16, true}, {1.5, true}, {0.1, true}}, stepValues(t, a, 3))
	})

	t.Run("keeps the sum of later steps exact after a large value that spans several steps", func(t *testing.T) {
		a := newSumAccumulator(4)
		a.add(0, 1, 123456789.123)
		a.add(2, 3, 0.1)
		a.finish()

		require.Equal(t, []stepValue{{123456789.123, true}, {123456789.123, true}, {0.1, true}, {0.1, true}}, stepValues(t, a, 4))
	})

	t.Run("does not spread NaN to the steps after it", func(t *testing.T) {
		a := newSumAccumulator(3)
		a.add(0, 0, math.NaN())
		a.add(1, 2, 2)
		a.finish()

		got := stepValues(t, a, 3)
		require.True(t, math.IsNaN(got[0].value))
		require.True(t, got[0].present)
		require.Equal(t, []stepValue{{2, true}, {2, true}}, got[1:])
	})

	t.Run("does not spread infinity to the steps after it", func(t *testing.T) {
		a := newSumAccumulator(4)
		a.add(0, 0, math.Inf(1))
		a.add(1, 1, 3)
		a.add(2, 2, math.Inf(-1))
		a.add(3, 3, 5)
		a.finish()

		require.Equal(t, []stepValue{{math.Inf(1), true}, {3, true}, {math.Inf(-1), true}, {5, true}}, stepValues(t, a, 4))
	})

	t.Run("sums integer values exactly whatever the order of the samples", func(t *testing.T) {
		const steps = 20

		type sample struct {
			lo, hi int
			value  int64
		}
		rng := rand.New(rand.NewPCG(1, 2))
		samples := make([]sample, 500)
		want := make([]int64, steps)
		for i := range samples {
			lo := rng.IntN(steps)
			hi := lo + rng.IntN(steps-lo)
			samples[i] = sample{lo: lo, hi: hi, value: rng.Int64N(2_000_000_000_000) - 1_000_000_000_000}
			for k := lo; k <= hi; k++ {
				want[k] += samples[i].value
			}
		}

		sumInOrder := func(samples []sample) []stepValue {
			a := newSumAccumulator(steps)
			for _, s := range samples {
				a.add(s.lo, s.hi, float64(s.value))
			}
			a.finish()
			return stepValues(t, a, steps)
		}

		forward := sumInOrder(samples)
		slices.Reverse(samples)
		reversed := sumInOrder(samples)
		rng.Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })
		shuffled := sumInOrder(samples)

		require.Equal(t, forward, reversed)
		require.Equal(t, forward, shuffled)
		for k := range steps {
			require.Equal(t, float64(want[k]), forward[k].value, "step %d", k)
		}
	})

	t.Run("keeps the small samples that follow a large one in the same step", func(t *testing.T) {
		a := newSumAccumulator(1)
		a.add(0, 0, 1e16)
		for range 10 {
			a.add(0, 0, 1)
		}
		a.finish()

		require.Equal(t, []stepValue{{1e16 + 10, true}}, stepValues(t, a, 1))
	})

	t.Run("keeps the small samples between a large value and the same value negated", func(t *testing.T) {
		a := newSumAccumulator(1)
		a.add(0, 0, 1e16)
		for range 10 {
			a.add(0, 0, 1)
		}
		a.add(0, 0, -1e16)
		a.finish()

		require.Equal(t, []stepValue{{10, true}}, stepValues(t, a, 1))
	})

	t.Run("keeps the small samples between a large value and the same value negated in every step of a range", func(t *testing.T) {
		a := newSumAccumulator(4)
		a.add(0, 3, 1e16)
		for range 10 {
			a.add(0, 3, 1)
		}
		a.add(0, 3, -1e16)
		a.add(2, 3, 0.5)
		a.finish()

		require.Equal(t, []stepValue{{10, true}, {10, true}, {10.5, true}, {10.5, true}}, stepValues(t, a, 4))
	})

	t.Run("keeps a negative compensation", func(t *testing.T) {
		a := newSumAccumulator(1)
		a.add(0, 0, 1e16)
		a.add(0, 0, -1)
		a.add(0, 0, -1)
		a.finish()

		require.Equal(t, []stepValue{{1e16 - 2, true}}, stepValues(t, a, 1))
	})

	t.Run("keeps the compensation of steps after the first step of the range that first needs one", func(t *testing.T) {
		a := newSumAccumulator(3)
		a.add(1, 2, 1e16)
		a.add(0, 2, 1)
		a.add(0, 2, 1)
		a.finish()

		require.Equal(t, []stepValue{{2, true}, {1e16 + 2, true}, {1e16 + 2, true}}, stepValues(t, a, 3))
	})

	t.Run("returns NaN for a positive and a negative infinity in one step", func(t *testing.T) {
		a := newSumAccumulator(1)
		a.add(0, 0, math.Inf(1))
		a.add(0, 0, math.Inf(-1))
		a.finish()

		got := stepValues(t, a, 1)
		require.True(t, math.IsNaN(got[0].value))
		require.True(t, got[0].present)
	})

	t.Run("returns infinity for an infinity and finite samples in one step", func(t *testing.T) {
		a := newSumAccumulator(1)
		a.add(0, 0, 1)
		a.add(0, 0, math.Inf(1))
		a.add(0, 0, 2.5)
		a.finish()

		require.Equal(t, []stepValue{{math.Inf(1), true}}, stepValues(t, a, 1))
	})

	t.Run("returns the same value as SumOverTime for the same samples in the same order", func(t *testing.T) {
		const steps = 20

		rng := rand.New(rand.NewPCG(3, 4))
		acc := newSumAccumulator(steps)
		want := make([]SumOverTime, steps)
		for range 2000 {
			lo := rng.IntN(steps)
			hi := lo + rng.IntN(steps-lo)
			v := rng.Float64() * math.Pow(10, float64(rng.IntN(17)))
			if rng.IntN(2) == 0 {
				v = -v
			}
			acc.add(lo, hi, v)
			for k := lo; k <= hi; k++ {
				want[k].agg(promql.FPoint{F: v})
			}
		}
		acc.finish()

		got := stepValues(t, acc, steps)
		for k := range steps {
			require.Equal(t, want[k].at(), got[k].value, "step %d", k)
		}
	})

	t.Run("does not allocate the compensation while the sums are exact", func(t *testing.T) {
		a := newSumAccumulator(10).(*sumAccumulator)
		require.Zero(t, testing.AllocsPerRun(100, func() {
			a.add(0, 9, 1234567)
			a.add(3, 5, -89)
		}))
		require.Nil(t, a.compensations)
	})

	t.Run("does not allocate the compensation for fractional values that fall in separate steps", func(t *testing.T) {
		a := newSumAccumulator(3).(*sumAccumulator)
		a.add(0, 0, 0.1)
		a.add(1, 1, 1e16)
		a.add(2, 2, 123456789.123)

		require.Nil(t, a.compensations)
	})

	t.Run("allocates the compensation once at the first inexact sum and never again", func(t *testing.T) {
		var created stepAccumulator
		withoutCompensation := testing.AllocsPerRun(100, func() {
			created = newSumAccumulator(10)
			created.add(0, 9, 1e16)
		})
		withCompensation := testing.AllocsPerRun(100, func() {
			created = newSumAccumulator(10)
			created.add(0, 9, 1e16)
			created.add(0, 9, 1)
			created.add(0, 9, 0.1)
		})
		require.Equal(t, withoutCompensation+1, withCompensation)

		a := newSumAccumulator(10).(*sumAccumulator)
		a.add(0, 9, 1e16)
		a.add(0, 9, 1)
		require.NotNil(t, a.compensations)
		require.Zero(t, testing.AllocsPerRun(100, func() { a.add(0, 9, 0.1) }))
	})

	t.Run("allocates the struct and two slices when created and nothing while adding exact values or reading samples", func(t *testing.T) {
		var created stepAccumulator
		require.Equal(t, 3.0, testing.AllocsPerRun(100, func() { created = newSumAccumulator(10) }))
		require.NotNil(t, created)

		a := newSumAccumulator(10)
		require.Zero(t, testing.AllocsPerRun(100, func() {
			a.add(1, 3, 1)
			a.finish()
			_, _ = a.value(2)
		}))
	})

	t.Run("panics when value is called before finish", func(t *testing.T) {
		a := newSumAccumulator(2)
		a.add(0, 1, 1)

		require.Panics(t, func() { _, _ = a.value(0) })
	})
}

func TestStreamFirstRangeAggregationExpr(t *testing.T) {
	supported := []string{
		`sum(count_over_time({app="foo"}[5m]))`,
		`sum by (app) (count_over_time({app="foo"}[5m]))`,
		`sum(sum_over_time({app="foo"} | logfmt | unwrap n [5m]))`,
		`sum by (app) (sum_over_time({app="foo"} | logfmt | unwrap n [5m]))`,
		`sum without (pod) (sum_over_time({app="foo"} | logfmt | unwrap n [5m]))`,
		`sum(sum_over_time({app="foo"} | logfmt | unwrap bytes(size) | __error__="" [5m] offset 1m))`,
	}
	unsupported := []string{
		`count_over_time({app="foo"}[5m])`,
		`sum_over_time({app="foo"} | logfmt | unwrap n [5m])`,
		`sum(rate({app="foo"}[5m]))`,
		`sum(bytes_over_time({app="foo"}[5m]))`,
		`sum(avg_over_time({app="foo"} | logfmt | unwrap n [5m]))`,
		`sum(max_over_time({app="foo"} | logfmt | unwrap n [5m]))`,
		`max(count_over_time({app="foo"}[5m]))`,
		`max(sum_over_time({app="foo"} | logfmt | unwrap n [5m]))`,
		`{app="foo"}`,
	}

	t.Run("accepts every supported query", func(t *testing.T) {
		for _, query := range supported {
			t.Run(query, func(t *testing.T) {
				rng, ok := StreamFirstRangeAggregationExpr(syntax.MustParseExpr(query))
				require.True(t, ok)
				require.NotNil(t, rng)
			})
		}
	})

	t.Run("rejects every unsupported query", func(t *testing.T) {
		for _, query := range unsupported {
			t.Run(query, func(t *testing.T) {
				_, ok := StreamFirstRangeAggregationExpr(syntax.MustParseExpr(query))
				require.False(t, ok)
			})
		}
	})

	t.Run("rejects an expression that only holds a supported query", func(t *testing.T) {
		for _, query := range []string{
			`topk(1, sum by (app) (count_over_time({app="foo"}[5m])))`,
			`label_replace(sum(count_over_time({app="foo"}[5m])), "a", "b", "c", "d")`,
			`sum(count_over_time({app="foo"}[5m])) > 1`,
		} {
			t.Run(query, func(t *testing.T) {
				_, ok := StreamFirstRangeAggregationExpr(syntax.MustParseExpr(query))
				require.False(t, ok)
			})
		}
	})
}

func TestStreamFirstRangeAggregations(t *testing.T) {
	intervals := func(query string) []time.Duration {
		var out []time.Duration
		for _, rng := range StreamFirstRangeAggregations(syntax.MustParseExpr(query)) {
			out = append(out, rng.Left.Interval)
		}
		return out
	}

	t.Run("returns the range aggregation of a supported query", func(t *testing.T) {
		require.Equal(t, []time.Duration{5 * time.Minute}, intervals(`sum by (app) (count_over_time({app="foo"}[5m]))`))
	})

	t.Run("returns a supported query nested in another expression", func(t *testing.T) {
		for _, query := range []string{
			`label_replace(sum by (app) (sum_over_time({app="foo"} | logfmt | unwrap n [5m])), "a", "b", "app", ".+")`,
			`max by (node) (sum by (node, pod) (count_over_time({app="foo"}[5m])))`,
			`topk(1, sum by (app) (count_over_time({app="foo"}[5m])))`,
			`(sum(count_over_time({app="foo"}[5m])) or vector(0)) > bool 0`,
			`sum(sum by (app) (count_over_time({app="foo"}[5m])))`,
		} {
			t.Run(query, func(t *testing.T) {
				require.Equal(t, []time.Duration{5 * time.Minute}, intervals(query))
			})
		}
	})

	t.Run("returns every supported query of a binary operator in walk order", func(t *testing.T) {
		require.Equal(t,
			[]time.Duration{time.Minute, 5 * time.Minute},
			intervals(`sum(count_over_time({app="foo"}[1m])) / sum(sum_over_time({app="bar"} | logfmt | unwrap n [5m]))`),
		)
	})

	t.Run("returns only the supported query of a binary operator with an unsupported one", func(t *testing.T) {
		require.Equal(t,
			[]time.Duration{time.Minute},
			intervals(`sum(count_over_time({app="foo"}[1m])) + sum(rate({app="bar"}[5m]))`),
		)
	})

	t.Run("returns nothing when no part of the query is supported", func(t *testing.T) {
		for _, query := range []string{
			`sum(rate({app="foo"}[5m]))`,
			`count_over_time({app="foo"}[5m])`,
			`max(count_over_time({app="foo"}[5m]))`,
			`label_replace(count_over_time({app="foo"}[5m]), "a", "b", "c", "d")`,
			`{app="foo"}`,
			`vector(1)`,
		} {
			t.Run(query, func(t *testing.T) {
				require.Empty(t, StreamFirstRangeAggregations(syntax.MustParseExpr(query)))
			})
		}
	})
}

// BenchmarkSumAccumulator measures the cost of adding one sample to the steps of its window. The
// number of steps per sample is about selRange/step.
func BenchmarkSumAccumulator(b *testing.B) {
	const steps = 1440

	for _, kind := range []string{"integer", "fractional"} {
		rng := rand.New(rand.NewPCG(7, 8))
		values := make([]float64, steps)
		for i := range values {
			values[i] = float64(rng.IntN(1000))
			if kind == "fractional" {
				values[i] += rng.Float64()
			}
		}

		for _, stepsPerSample := range []int{1, 5, 60, 288} {
			b.Run(fmt.Sprintf("values=%s/steps-per-sample=%d", kind, stepsPerSample), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					a := newSumAccumulator(steps)
					for i, v := range values {
						a.add(i, min(i+stepsPerSample-1, steps-1), v)
					}
					a.finish()
				}
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/steps, "ns/sample")
			})
		}
	}
}
