package bitmask_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bitmask"
)

func Fuzz(f *testing.F) {
	f.Add(int64(1234), 10)

	f.Fuzz(func(t *testing.T, seed int64, size int) {
		if size < 1 {
			t.Skip()
		}

		rnd := rand.New(rand.NewSource(seed))

		mask := bitmask.New(size)

		var expect []bool
		for range size {
			val := rnd.Intn(2) == 1
			if val {
				mask.Set(len(expect))
			}
			expect = append(expect, val)
		}

		var actual []bool
		for i := range mask.Len() {
			actual = append(actual, mask.Test(i))
		}
		require.Equal(t, expect, actual)
	})
}

func TestMask(t *testing.T) {
	m := bitmask.New(156) // Large enough for more than one byte

	for i := 0; i < m.Len(); i++ {
		require.False(t, m.Test(i), "All bits should be 0 initially")
		require.NotPanics(t, func() { m.Set(i) }, "All bits should be settable")
		require.True(t, m.Test(i), "All bits should be 1 after being set")
	}
}

func TestMask_Reset(t *testing.T) {
	m := bitmask.New(156)
	require.Equal(t, 156, m.Len(), "Length should be 156")

	m.Reset(5)
	require.Equal(t, 5, m.Len(), "Length should be 5 after reset")
}
