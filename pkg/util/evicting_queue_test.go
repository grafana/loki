package util

import (
	"math"
	"math/rand"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func noopOnEvict() {}

func TestQueueAppend(t *testing.T) {
	q, err := NewEvictingQueue(10, noopOnEvict)
	require.Nil(t, err)

	q.Append(1)
	q.Append(2)
	q.Append(3)
	q.Append(4)
	q.Append(5)

	require.Equal(t, 5, q.Length())
}

func TestQueueCapacity(t *testing.T) {
	q, err := NewEvictingQueue(9, noopOnEvict)
	require.Nil(t, err)
	require.Equal(t, 9, q.Capacity())

	q.capacity = 11
	require.Equal(t, 11, q.Capacity())
}

func TestZeroCapacityQueue(t *testing.T) {
	q, err := NewEvictingQueue(0, noopOnEvict)
	require.Error(t, err)
	require.Nil(t, q)
}

func TestNegativeCapacityQueue(t *testing.T) {
	q, err := NewEvictingQueue(-1, noopOnEvict)
	require.Error(t, err)
	require.Nil(t, q)
}

func TestQueueEvict(t *testing.T) {
	q, err := NewEvictingQueue(3, noopOnEvict)
	require.Nil(t, err)

	// appending 5 entries will cause the first (oldest) 2 entries to be evicted
	entries := []interface{}{1, 2, 3, 4, 5}
	for _, entry := range entries {
		q.Append(entry)
	}

	require.Equal(t, 3, q.Length())
	require.Equal(t, entries[2:], q.Entries())
}

func TestQueueClear(t *testing.T) {
	q, err := NewEvictingQueue(3, noopOnEvict)
	require.Nil(t, err)

	q.Append(1)
	q.Clear()

	require.Equal(t, 0, q.Length())
}

func TestQueueEvictionCallback(t *testing.T) {
	var evictionCallbackCalls int

	q, err := NewEvictingQueue(3, func() {
		evictionCallbackCalls++
	})
	require.Nil(t, err)

	for i := 0; i < 5; i++ {
		q.Append(i)
	}

	require.Equal(t, 2, evictionCallbackCalls)
}

func TestSafeConcurrentAccess(t *testing.T) {
	q, err := NewEvictingQueue(3, noopOnEvict)
	require.Nil(t, err)

	var wg sync.WaitGroup

	for w := 0; w < 30; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for i := 0; i < 500; i++ {
				q.Append(rand.Int())
			}
		}()
	}

	wg.Wait()

	require.Equal(t, 3, q.Length())
}

type queueEntry struct {
	key   string
	value interface{}
}

func BenchmarkAppendAndEvict(b *testing.B) {
	capacity := 5000
	q, err := NewEvictingQueue(capacity, noopOnEvict)
	require.Nil(b, err)

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		q.Append(&queueEntry{
			key:   "hello",
			value: "world",
		})
	}

	require.EqualValues(b, math.Min(float64(b.N), float64(capacity)), q.Length())
}
