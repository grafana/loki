package mempool

import (
	"math/rand"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/flagext"
)

func TestMemPool(t *testing.T) {

	t.Run("requested size within bucket", func(t *testing.T) {
		pool := New("test", []Bucket{
			{Size: 1, Capacity: 128},
			{Size: 1, Capacity: 256},
			{Size: 1, Capacity: 512},
		}, nil)
		res := pool.Get(200)
		require.Equal(t, 200, len(res))
		require.Equal(t, 256, cap(res))

		res = pool.Get(300)
		require.Equal(t, 300, len(res))
		require.Equal(t, 512, cap(res))
	})

	t.Run("test ring buffer returns same backing array", func(t *testing.T) {
		pool := New("test", []Bucket{
			{Size: 2, Capacity: 128},
		}, nil)
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

	t.Run("concurrent access", func(t *testing.T) {
		pool := New("test", []Bucket{
			{Size: 32, Capacity: 2 << 10},
			{Size: 16, Capacity: 4 << 10},
			{Size: 8, Capacity: 8 << 10},
			{Size: 4, Capacity: 16 << 10},
			{Size: 2, Capacity: 32 << 10},
		}, nil)

		var wg sync.WaitGroup
		numWorkers := 256
		n := 10

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < n; i++ {
					s := 2 << rand.Intn(5)
					buf1 := pool.Get(s)
					buf2 := pool.Get(s)
					pool.Put(buf2)
					time.Sleep(time.Millisecond * time.Duration(rand.Intn(10)))
					pool.Put(buf1)
				}
			}()
		}

		wg.Wait()
		t.Log("finished")
	})
}

func TestMemPoolPutRequiresCorrectSizing(t *testing.T) {
	pool := New("test", []Bucket{
		{Size: 1, Capacity: 10},
		{Size: 1, Capacity: 20},
		{Size: 1, Capacity: 30},
	}, nil)

	// exhaust the slabs so we'll have room to return
	_ = pool.Get(10)
	_ = pool.Get(20)
	_ = pool.Get(30)

	// put a buffer that is too small
	require.False(t, pool.Put(make([]byte, 9)))

	// put a buffer that is too large
	require.False(t, pool.Put(make([]byte, 31)))

	// put a buffer that is the correct size for each slab
	require.True(t, pool.Put(make([]byte, 10)))
	require.True(t, pool.Put(make([]byte, 20)))
	require.True(t, pool.Put(make([]byte, 30)))
}

func BenchmarkSlab(b *testing.B) {
	for _, sz := range []int{
		1 << 10,   // 1KB
		1 << 20,   // 1MB
		128 << 20, // 128MB
	} {
		b.Run(flagext.ByteSize(uint64(sz)).String(), func(b *testing.B) {
			slab := newSlab(sz, 1, newMetrics(nil, "test"))
			slab.init()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				b := slab.get(sz)
				slab.put(b)
			}
		})
	}
}
