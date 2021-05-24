package util

import (
	"math/rand"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func noopOnEvict() {}

func TestQueueAppend(t *testing.T) {
	q := NewEvictingQueue(10, noopOnEvict)

	q.Append(1)
	q.Append(2)
	q.Append(3)
	q.Append(4)
	q.Append(5)

	require.Equal(t, 5, q.Length())
}

func TestQueueEvict(t *testing.T) {
	q := NewEvictingQueue(3, noopOnEvict)

	// appending 5 entries will cause the first (oldest) 2 entries to be evicted
	entries := []interface{}{1, 2, 3, 4, 5}
	for _, entry := range entries {
		q.Append(entry)
	}

	require.Equal(t, 3, q.Length())
	require.Equal(t, entries[2:], q.Entries())
}

func TestQueueClear(t *testing.T) {
	q := NewEvictingQueue(3, noopOnEvict)

	q.Append(1)
	q.Clear()

	require.Equal(t, 0, q.Length())
}

func TestQueueEvictionCallback(t *testing.T) {
	var evictionCallbackCalls int

	q := NewEvictingQueue(3, func() {
		evictionCallbackCalls++
	})

	for i := 0; i < 5; i++ {
		q.Append(i)
	}

	require.Equal(t, 2, evictionCallbackCalls)
}

func TestSafeConcurrentAccess(t *testing.T) {
	q := NewEvictingQueue(3, noopOnEvict)

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
