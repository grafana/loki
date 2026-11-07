package pool

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSlabPool_Get(t *testing.T) {
	t.Run("returns slices that do not overlap", func(t *testing.T) {
		p := NewSlabPool(&sync.Pool{}, 100)

		a, b := p.Get(60), p.Get(60)
		for i := range a {
			a[i] = 1
		}
		for i := range b {
			b[i] = 2
		}

		require.Equal(t, bytesOf(60, 1), a)
		require.Equal(t, bytesOf(60, 2), b)
		require.Equal(t, 60, cap(a), "a slice cannot grow into its neighbour")
	})

	t.Run("returns a slice of the right length from a released slab", func(t *testing.T) {
		shared := &sync.Pool{}

		for range 50 {
			p := NewSlabPool(shared, 100)
			require.Len(t, p.Get(60), 60)
			require.Len(t, p.Get(30), 30)
			p.Release()
		}
	})

	t.Run("returns a slice larger than the slab", func(t *testing.T) {
		p := NewSlabPool(&sync.Pool{}, 100)

		require.Len(t, p.Get(101), 101)
	})

	t.Run("is safe for concurrent use", func(_ *testing.T) {
		p := NewSlabPool(&sync.Pool{}, 1000)

		var wg sync.WaitGroup
		for i := range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 100 {
					b := p.Get(30)
					for j := range b {
						b[j] = byte(i)
					}
				}
			}()
		}
		wg.Wait()
		p.Release()
	})
}

func bytesOf(n int, v byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = v
	}
	return b
}
