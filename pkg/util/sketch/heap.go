package sketch

import (
	"container/heap"
)

type node struct {
	event string
	count uint32
	// used for the container heap Fix function
	index uint16
}

type MinHeap []*node

func (h MinHeap) Len() int {
	return len(h)
}

// less is only used in the underlying pop implementation
func (h MinHeap) Less(i, j int) bool {
	return h[i].count < h[j].count
}
func (h MinHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = uint16(i)
	h[j].index = uint16(j)
}

func (h *MinHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*node)
	item.index = uint16(n)
	*h = append(*h, item)
}

func (h *MinHeap) Pop() interface{} {
	if len(*h) == 0 {
		return nil
	}
	top := (*h)[0]
	heapSize := len(*h)

	if heapSize > 1 {
		(*h)[0] = (*h)[heapSize-1]
	}
	(*h) = (*h)[:len(*h)-1]
	heap.Fix(h, 0)
	return top
}

func (h *MinHeap) Peek() interface{} {
	return (*h)[0]
}

// update modifies the count and value of an Item in the queue.
func (h *MinHeap) update(event string, count uint32) {
	updateNode := -1
	for i, k := range *h {
		if k.event == event {
			k.count = count
			updateNode = i
			break
		}
	}
	heap.Fix(h, updateNode)
}

func (h *MinHeap) Find(e string) (int, bool) {
	for i := 0; i < len(*h); i++ {
		if (*h)[i].event == e {
			return i, true
		}
	}
	return 0, false
}
