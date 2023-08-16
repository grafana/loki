package sketch

import (
	"container/heap"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeap(t *testing.T) {
	h := MinHeap{}

	heap.Init(&h)

	heap.Push(&h, &Node{Event: "1", count: 70})
	assert.Equal(t, uint32(70), h.Peek().(*Node).count, "expected: %d and got %d", uint32(70), h.Peek().(*Node).count)

	heap.Push(&h, &Node{Event: "2", count: 20})
	assert.Equal(t, uint32(20), h.Peek().(*Node).count, "expected: %d and got %d", uint32(20), h.Peek().(*Node).count)

	heap.Push(&h, &Node{Event: "3", count: 50})
	assert.Equal(t, uint32(20), h.Peek().(*Node).count, "expected: %d and got %d", uint32(20), h.Peek().(*Node).count)

	heap.Push(&h, &Node{Event: "4", count: 60})
	assert.Equal(t, uint32(20), h.Peek().(*Node).count, "expected: %d and got %d", uint32(20), h.Peek().(*Node).count)

	heap.Push(&h, &Node{Event: "5", count: 10})
	assert.Equal(t, uint32(10), h.Peek().(*Node).count, "expected: %d and got %d", uint32(10), h.Peek().(*Node).count)

	assert.Equal(t, heap.Pop(&h).(*Node).count, uint32(10))
	assert.Equal(t, h.Peek().(*Node).count, uint32(20))
}
