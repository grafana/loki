package sketch

import (
	"testing"
)

func TestHeap(t *testing.T) {
	h := MinHeap{10, make([]*node, 0)}
	h.Push("1", 70)
	h.Push("2", 30)
	h.Push("3", 20)
	h.Push("4", 60)
	h.Push("5", 80)
	h.Push("6", 50)
	h.Push("7", 10)
	h.Push("8", 90)
	// todo: finish this test
}
