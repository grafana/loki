package sketch

type node struct {
	event string
	count int64
}

// MinHeap is a binary heap implementation, we could switch to something with better than O(n) merging if we need to
type MinHeap struct {
	max  int
	heap []*node
}

func NewMinHeap(max int) *MinHeap {
	return &MinHeap{
		max:  max,
		heap: make([]*node, 0, max),
	}
}

func (mh *MinHeap) Push(event string, count int64) {
	mh.heap = append(mh.heap, &node{event: event, count: count})
	mh.siftUp(len(mh.heap) - 1)
}

func (mh *MinHeap) Pop() *node {
	top := mh.min()
	heapSize := len(mh.heap)

	if heapSize > 1 {
		mh.heap[0] = mh.heap[len(mh.heap)-1]
	}

	mh.heap = mh.heap[:len(mh.heap)-1]
	mh.siftDown(0)
	return top
}

func (mh *MinHeap) UpdateValue(event string) {
	for _, k := range mh.heap {
		if k.event == event {
			k.count += 1
		}
	}
	// When we added something to the heap it might have become the
	// new minimum element, but updating it's value could have changed that
	// so we should re-heapify.
	mh.siftDown(0)
}

func (mh *MinHeap) Peek() *node {
	return mh.min()
}

func (mh *MinHeap) parent(i int) (*node, int) {
	if i == 0 {
		return mh.heap[0], 0
	}
	return mh.heap[(i-1)/2], (i - 1) / 2
}

func (mh *MinHeap) leftChild(i int) (*node, int) {
	// todo nicer interface for nil node
	if len(mh.heap) <= (2*i + 1) {
		return &node{}, -1
	}
	return mh.heap[2*i+1], 2*i + 1
}

func (mh *MinHeap) rightChild(i int) (*node, int) {
	// todo nicer interface for nil node
	if len(mh.heap) <= (2*i + 2) {
		return &node{}, -1
	}
	return mh.heap[2*i+2], 2*i + 2
}

func (mh *MinHeap) minChildIndex(index int) int {
	leftChild, leftChildIndex := mh.leftChild(index)
	if leftChildIndex == -1 {
		// no right child yet
		return index
	}
	if leftChildIndex >= len(mh.heap) {
		return leftChildIndex
	}

	rightChild, rightChildIndex := mh.rightChild(index)
	if rightChildIndex == -1 {
		// no right child yet
		return leftChildIndex
	}
	if rightChild.count < leftChild.count {
		return rightChildIndex
	}

	return leftChildIndex
}

func (mh *MinHeap) min() *node {
	if len(mh.heap) == 0 {
		return &node{
			count: -1,
		}
	}
	return mh.heap[0]
}

// move a node up the tree
func (mh *MinHeap) siftUp(index int) {
	for index > 0 {
		parentIndex := (index - 1) / 2

		if mh.heap[parentIndex].count < mh.heap[index].count {
			return
		}

		mh.heap[parentIndex], mh.heap[index] = mh.heap[index], mh.heap[parentIndex]
		index = parentIndex
	}
}

// move a node down the tree
func (mh *MinHeap) siftDown(index int) {
	_, i := mh.leftChild(index)
	for i < len(mh.heap) {
		_, i = mh.leftChild(index)
		if i == -1 {
			return
		}
		minChildIndex := mh.minChildIndex(index)
		if minChildIndex == -1 {
			return
		}

		if mh.heap[minChildIndex].count >= mh.heap[index].count {
			return
		}

		mh.heap[minChildIndex], mh.heap[index] = mh.heap[index], mh.heap[minChildIndex]
		index = minChildIndex
	}
}
