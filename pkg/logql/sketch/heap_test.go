package sketch

import (
	"container/heap"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeap(t *testing.T) {
	h := MinHeap{}

	heap.Init(&h)

	heap.Push(&h, &node{event: "1", count: 70})
	assert.Equal(t, uint32(70), h.Peek().(*node).count, "expected: %d and got %d", uint32(70), h.Peek().(*node).count)

	heap.Push(&h, &node{event: "2", count: 20})
	assert.Equal(t, uint32(20), h.Peek().(*node).count, "expected: %d and got %d", uint32(20), h.Peek().(*node).count)

	heap.Push(&h, &node{event: "3", count: 50})
	assert.Equal(t, uint32(20), h.Peek().(*node).count, "expected: %d and got %d", uint32(20), h.Peek().(*node).count)

	heap.Push(&h, &node{event: "4", count: 60})
	assert.Equal(t, uint32(20), h.Peek().(*node).count, "expected: %d and got %d", uint32(20), h.Peek().(*node).count)

	heap.Push(&h, &node{event: "5", count: 10})
	assert.Equal(t, uint32(10), h.Peek().(*node).count, "expected: %d and got %d", uint32(10), h.Peek().(*node).count)

	assert.Equal(t, heap.Pop(&h).(*node).count, uint32(10))
	assert.Equal(t, h.Peek().(*node).count, uint32(20))
}
