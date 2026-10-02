package logql

import (
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
