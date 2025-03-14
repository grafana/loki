package sketch

import (
	"container/heap"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeap(t *testing.T) {
	h := MinHeap{}

	heap.Init(&h)

	heap.Push(&h, &node{event: "1", count: 70.0})
	assert.Equal(t, 70.0, h.Peek().(*node).count, "expected: %f and got %f", 70.0, h.Peek().(*node).count)

	heap.Push(&h, &node{event: "2", count: 20.0})
	assert.Equal(t, 20.0, h.Peek().(*node).count, "expected: %f and got %f", 20, h.Peek().(*node).count)

	heap.Push(&h, &node{event: "3", count: 50})
	assert.Equal(t, 20.0, h.Peek().(*node).count, "expected: %f and got %f", 20.0, h.Peek().(*node).count)

	heap.Push(&h, &node{event: "4", count: 60.0})
	assert.Equal(t, 20.0, h.Peek().(*node).count, "expected: %f and got %f", 20.0, h.Peek().(*node).count)

	heap.Push(&h, &node{event: "5", count: 10.0})
	assert.Equal(t, 10.0, h.Peek().(*node).count, "expected: %f and got %f", 10.0, h.Peek().(*node).count)

	assert.Equal(t, heap.Pop(&h).(*node).count, 10.0)
	assert.Equal(t, h.Peek().(*node).count, 20.0)
}
