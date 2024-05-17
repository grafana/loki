package mempool

import (
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
		res := pool.Get(200)
		require.Equal(t, 200, len(res))
		require.Equal(t, 256, cap(res))

		res = pool.Get(300)
		require.Equal(t, 300, len(res))
		require.Equal(t, 512, cap(res))
	})

	t.Run("buffer is cleared when returned", func(t *testing.T) {
		pool := New([]Bucket{
			{Size: 1, Capacity: 64},
		})
		res := pool.Get(8)
		require.Equal(t, 8, len(res))
		source := []byte{0, 1, 2, 3, 4, 5, 6, 7}
		copy(res, source)

		pool.Put(res)

		res = pool.Get(8)
		require.Equal(t, 8, len(res))
		require.Equal(t, make([]byte, 8), res)
	})

	t.Run("pool blocks when no buffer is available", func(t *testing.T) {
		pool := New([]Bucket{
			{Size: 1, Capacity: 64},
		})
		buf1 := pool.Get(32)
		require.Equal(t, 32, len(buf1))

		delay := 100 * time.Millisecond
		start := time.Now()
		go func(p Pool) {
			time.Sleep(delay)
			p.Put(buf1)
		}(pool)

		buf2 := pool.Get(16)
		dur := time.Since(start)
		require.GreaterOrEqual(t, dur, delay)
		require.Equal(t, 16, len(buf2))
	})

	t.Run("test ring buffer returns same backing array", func(t *testing.T) {
		pool := New([]Bucket{
			{Size: 2, Capacity: 128},
		})
		res1 := pool.Get(32)
		ptr1 := unsafe.Pointer(unsafe.SliceData(res1))

		res2 := pool.Get(64)
		ptr2 := unsafe.Pointer(unsafe.SliceData(res2))

		pool.Put(res2)
		pool.Put(res1)

		res3 := pool.Get(48)
		ptr3 := unsafe.Pointer(unsafe.SliceData(res3))

		res4 := pool.Get(96)
		ptr4 := unsafe.Pointer(unsafe.SliceData(res4))

		require.Equal(t, ptr1, ptr4)
		require.Equal(t, ptr2, ptr3)
	})
}
