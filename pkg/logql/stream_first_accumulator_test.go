package logql

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
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

	t.Run("allocates the struct and two slices when created and nothing while adding or reading samples", func(t *testing.T) {
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

func TestStreamFirstRangeAggregation(t *testing.T) {
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
		`sum(sum_over_time({app="foo"}[5m]))`,
		`{app="foo"}`,
		`sum(`,
	}

	t.Run("accepts every supported query", func(t *testing.T) {
		for _, query := range supported {
			t.Run(query, func(t *testing.T) {
				rng, ok := StreamFirstRangeAggregation(query)
				require.True(t, ok)
				require.NotNil(t, rng)
			})
		}
	})

	t.Run("rejects every unsupported query", func(t *testing.T) {
		for _, query := range unsupported {
			t.Run(query, func(t *testing.T) {
				_, ok := StreamFirstRangeAggregation(query)
				require.False(t, ok)
			})
		}
	})
}
