package mempool

import (
	"math/rand"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestMemPool(t *testing.T) {

	t.Run("empty pool", func(t *testing.T) {
		pool := New([]Bucket{})
		require.Panics(t, func() {
			pool.Get(128)
		})
	})

	t.Run("requested size too big", func(t *testing.T) {
		pool := New([]Bucket{
			{Size: 1, Capacity: 128},
		})
		require.Panics(t, func() {
			pool.Get(256)
		})
	})

	t.Run("requested size within bucket", func(t *testing.T) {
		pool := New([]Bucket{
			{Size: 1, Capacity: 128},
			{Size: 1, Capacity: 256},
			{Size: 1, Capacity: 512},
		})
		res, err := pool.Get(200)
		require.NoError(t, err)
		require.Equal(t, 200, len(res))
		require.Equal(t, 256, cap(res))

		res, err = pool.Get(300)
		require.NoError(t, err)
		require.Equal(t, 300, len(res))
		require.Equal(t, 512, cap(res))
	})

	t.Run("buffer is cleared when returned", func(t *testing.T) {
		pool := New([]Bucket{
			{Size: 1, Capacity: 64},
		})
		res, err := pool.Get(8)
		require.NoError(t, err)
		require.Equal(t, 8, len(res))
		source := []byte{0, 1, 2, 3, 4, 5, 6, 7}
		copy(res, source)

		pool.Put(res)

		res, err = pool.Get(8)
		require.NoError(t, err)
		require.Equal(t, 8, len(res))
		require.Equal(t, make([]byte, 8), res)
	})

	t.Run("pool blocks when no buffer is available", func(t *testing.T) {
		pool := New([]Bucket{
			{Size: 1, Capacity: 64},
		})
		buf1, _ := pool.Get(32)
		require.Equal(t, 32, len(buf1))

		delay := 100 * time.Millisecond
		start := time.Now()
		go func(p Pool) {
			time.Sleep(delay)
			p.Put(buf1)
		}(pool)

		buf2, _ := pool.Get(16)
		dur := time.Since(start)
		require.GreaterOrEqual(t, dur, delay)
		require.Equal(t, 16, len(buf2))
	})

	t.Run("test ring buffer returns same backing array", func(t *testing.T) {
		pool := New([]Bucket{
			{Size: 2, Capacity: 128},
		})
		res1, _ := pool.Get(32)
		ptr1 := unsafe.Pointer(unsafe.SliceData(res1))

		res2, _ := pool.Get(64)
		ptr2 := unsafe.Pointer(unsafe.SliceData(res2))

		pool.Put(res2)
		pool.Put(res1)

		res3, _ := pool.Get(48)
		ptr3 := unsafe.Pointer(unsafe.SliceData(res3))

		res4, _ := pool.Get(96)
		ptr4 := unsafe.Pointer(unsafe.SliceData(res4))

		require.Equal(t, ptr1, ptr4)
		require.Equal(t, ptr2, ptr3)
	})

	t.Run("concurrent access", func(t *testing.T) {
		pool := New([]Bucket{
			{Size: 32, Capacity: 2 << 10},
			{Size: 16, Capacity: 4 << 10},
			{Size: 8, Capacity: 8 << 10},
			{Size: 4, Capacity: 16 << 10},
			{Size: 2, Capacity: 32 << 10},
		})

		var wg sync.WaitGroup
		numWorkers := 256
		n := 10

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < n; i++ {
					s := 2 << rand.Intn(5)
					buf1, err1 := pool.Get(s)
					buf2, err2 := pool.Get(s)
					if err2 == nil {
						pool.Put(buf2)
					}
					time.Sleep(time.Millisecond * time.Duration(rand.Intn(10)))
					if err1 == nil {
						pool.Put(buf1)
					}
				}
			}()
		}

		wg.Wait()
		t.Log("finished")
	})
}
