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

	t.Run("empty pool", func(t *testing.T) {
		pool := New("test", []Bucket{}, nil)
		_, err := pool.Get(256)
		require.Error(t, err)
	})

	t.Run("requested size too big", func(t *testing.T) {
		pool := New("test", []Bucket{
			{Size: 1, Capacity: 128},
		}, nil)
		_, err := pool.Get(256)
		require.Error(t, err)
	})

	t.Run("requested size within bucket", func(t *testing.T) {
		pool := New("test", []Bucket{
			{Size: 1, Capacity: 128},
			{Size: 1, Capacity: 256},
			{Size: 1, Capacity: 512},
		}, nil)
		res, err := pool.Get(200)
		require.NoError(t, err)
		require.Equal(t, 200, len(res))
		require.Equal(t, 256, cap(res))

		res, err = pool.Get(300)
		require.NoError(t, err)
		require.Equal(t, 300, len(res))
		require.Equal(t, 512, cap(res))
	})

	t.Run("pool blocks when no buffer is available", func(t *testing.T) {
		pool := New("test", []Bucket{
			{Size: 1, Capacity: 64},
		}, nil)
		buf1, _ := pool.Get(32)
		require.Equal(t, 32, len(buf1))

		delay := 20 * time.Millisecond
		start := time.Now()

		go func(p *MemPool) {
			time.Sleep(delay)
			p.Put(make([]byte, 16))
		}(pool)

		_, err := pool.Get(16)
		duration := time.Since(start)

		require.NoError(t, err)
		require.Greater(t, duration, delay)
	})

	t.Run("test ring buffer returns same backing array", func(t *testing.T) {
		pool := New("test", []Bucket{
			{Size: 2, Capacity: 128},
		}, nil)
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
		numWorkers := 256

		pool := New("test", []Bucket{
			{Size: numWorkers, Capacity: 2 << 10},
			{Size: numWorkers, Capacity: 4 << 10},
			{Size: numWorkers, Capacity: 8 << 10},
			{Size: numWorkers, Capacity: 16 << 10},
			{Size: numWorkers, Capacity: 32 << 10},
		}, nil)

		var wg sync.WaitGroup
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

func BenchmarkSlab(b *testing.B) {
	for _, sz := range []int{
		1 << 10,   // 1KB
		1 << 20,   // 1MB
		128 << 20, // 128MB
	} {
		b.Run(flagext.ByteSize(uint64(sz)).String(), func(b *testing.B) {
			slab := newSlab(sz, 1, newMetrics(nil, "test"))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				b, err := slab.get(sz)
				if err != nil {
					panic(err)
				}
				slab.put(b)
			}
		})
	}
}
