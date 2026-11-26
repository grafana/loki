package util //nolint:revive

import (
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type simpleItem int64

func (i simpleItem) Priority() int64 {
	return int64(i)
}

func (i simpleItem) Key() string {
	return strconv.FormatInt(int64(i), 10)
}

func TestPriorityQueueBasic(t *testing.T) {
	queue := NewPriorityQueue(nil)
	assert.Equal(t, 0, queue.Length(), "Expected length = 0")

	queue.Enqueue(simpleItem(1))
	assert.Equal(t, 1, queue.Length(), "Expected length = 1")

	i, ok := queue.Dequeue().(simpleItem)
	assert.True(t, ok, "Expected cast to succeed")
	assert.Equal(t, simpleItem(1), i, "Expected to dequeue simpleItem(1)")

	queue.Close()
	assert.Nil(t, queue.Dequeue(), "Expect nil dequeue")
}

func TestPriorityQueuePriorities(t *testing.T) {
	queue := NewPriorityQueue(nil)
	queue.Enqueue(simpleItem(1))
	queue.Enqueue(simpleItem(2))

	assert.Equal(t, simpleItem(2), queue.Dequeue().(simpleItem), "Expected to dequeue simpleItem(2)")
	assert.Equal(t, simpleItem(1), queue.Dequeue().(simpleItem), "Expected to dequeue simpleItem(1)")

	queue.Close()
	assert.Nil(t, queue.Dequeue(), "Expect nil dequeue")
}

func TestPriorityQueuePriorities2(t *testing.T) {
	queue := NewPriorityQueue(nil)
	queue.Enqueue(simpleItem(2))
	queue.Enqueue(simpleItem(1))

	assert.Equal(t, simpleItem(2), queue.Dequeue().(simpleItem), "Expected to dequeue simpleItem(2)")
	assert.Equal(t, simpleItem(1), queue.Dequeue().(simpleItem), "Expected to dequeue simpleItem(1)")

	queue.Close()
	assert.Nil(t, queue.Dequeue(), "Expect nil dequeue")
}

func TestPriorityQueueWait(t *testing.T) {
	queue := NewPriorityQueue(nil)

	done := make(chan struct{})
	go func() {
		assert.Nil(t, queue.Dequeue(), "Expect nil dequeue")
		close(done)
	}()

	queue.Close()
	runtime.Gosched()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Close didn't unblock Dequeue.")
	}
}

type mergeItem struct {
	key    string
	merged []int
}

func (m *mergeItem) Key() string     { return m.key }
func (m *mergeItem) Priority() int64 { return 0 }

func TestPriorityQueueEnqueueOrMerge(t *testing.T) {
	queue := NewPriorityQueue(nil)

	first := &mergeItem{key: "a"}
	require.True(t, queue.EnqueueOrMerge(first, func(Op) { t.Fatal("onDup must not be called for a new op") }))

	// A duplicate is dropped and handed the queued op to merge into.
	dup := &mergeItem{key: "a"}
	require.False(t, queue.EnqueueOrMerge(dup, func(existing Op) {
		existing.(*mergeItem).merged = append(existing.(*mergeItem).merged, 1)
	}))
	require.False(t, queue.EnqueueOrMerge(dup, func(existing Op) {
		existing.(*mergeItem).merged = append(existing.(*mergeItem).merged, 2)
	}))
	// A nil onDup behaves like Enqueue.
	require.False(t, queue.EnqueueOrMerge(dup, nil))
	require.False(t, queue.Enqueue(dup))
	require.Equal(t, 1, queue.Length())

	got := queue.Dequeue().(*mergeItem)
	require.Same(t, first, got)
	require.Equal(t, []int{1, 2}, got.merged)

	// Once dequeued the key is free again, so nothing can merge into a running op.
	require.True(t, queue.EnqueueOrMerge(&mergeItem{key: "a"}, func(Op) { t.Fatal("onDup must not be called after dequeue") }))
}
