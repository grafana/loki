// Package queue is a generic FIFO ring buffer adapted from
// github.com/eapache/queue. It is not safe for concurrent use.
package queue

// power of two so wrap-around can use bitwise AND
const minLen = 1 << 4

// Queue is a generic FIFO ring buffer. The zero value is an empty queue ready
// for use.
type Queue[T any] struct {
	buf               []T
	head, tail, count int
}

// Length returns the number of elements in the queue.
func (q *Queue[T]) Length() int {
	return q.count
}

// resize rebuilds buf with head back at index 0, sized to 2*count but never
// below minLen so the zero value of Queue grows on first Add.
func (q *Queue[T]) resize() {
	newBuf := make([]T, max(minLen, q.count<<1))
	if q.tail > q.head {
		copy(newBuf, q.buf[q.head:q.tail])
	} else {
		n := copy(newBuf, q.buf[q.head:])
		copy(newBuf[n:], q.buf[:q.tail])
	}
	q.head = 0
	q.tail = q.count
	q.buf = newBuf
}

// Add appends elem, growing the buffer if it is full.
func (q *Queue[T]) Add(elem T) {
	if q.count == len(q.buf) {
		q.resize()
	}
	q.buf[q.tail] = elem
	q.tail = (q.tail + 1) & (len(q.buf) - 1)
	q.count++
}

// Peek returns the head element. It panics if the queue is empty.
func (q *Queue[T]) Peek() T {
	if q.count <= 0 {
		panic("queue: Peek() called on empty queue")
	}
	return q.buf[q.head]
}

// Remove pops and returns the head element. It panics if the queue is empty.
func (q *Queue[T]) Remove() T {
	if q.count <= 0 {
		panic("queue: Remove() called on empty queue")
	}
	ret := q.buf[q.head]
	var zero T
	q.buf[q.head] = zero // clear slot so the popped element can be GC'd
	q.head = (q.head + 1) & (len(q.buf) - 1)
	q.count--
	// shrink once we're down to a quarter of the buffer
	if len(q.buf) > minLen && (q.count<<2) == len(q.buf) {
		q.resize()
	}
	return ret
}
