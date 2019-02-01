package util

import (
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type simpleItem int64

func (i simpleItem) Priority() int64 {
	return int64(i)
}

func (i simpleItem) Key() string {
	return strconv.FormatInt(int64(i), 10)
}

type richItem struct {
	priority int64
	key      string
	value    string
}

func (r richItem) Priority() int64 {
	return r.priority
}

func (r richItem) Key() string {
	return r.key
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
