package querier

import (
	"container/heap"
	"fmt"
	"io"
	"regexp"

	"github.com/grafana/logish/pkg/logproto"
)

// EntryIterator iterates over entries in time-order.
type EntryIterator interface {
	Next() bool
	Entry() logproto.Entry
	Labels() string
	Error() error
	Close() error
}

// streamIterator iterates over entries in a stream.
type streamIterator struct {
	i       int
	entries []logproto.Entry
	labels  string
}

func newStreamIterator(stream *logproto.Stream) EntryIterator {
	return &streamIterator{
		i:       -1,
		entries: stream.Entries,
		labels:  stream.Labels,
	}
}

func (i *streamIterator) Next() bool {
	i.i++
	return i.i < len(i.entries)
}

func (i *streamIterator) Error() error {
	return nil
}

func (i *streamIterator) Labels() string {
	return i.labels
}

func (i *streamIterator) Entry() logproto.Entry {
	return i.entries[i.i]
}

func (i *streamIterator) Close() error {
	return nil
}

type iteratorHeap []EntryIterator

func (h iteratorHeap) Len() int            { return len(h) }
func (h iteratorHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h iteratorHeap) Peek() EntryIterator { return h[0] }
func (h *iteratorHeap) Push(x interface{}) {
	*h = append(*h, x.(EntryIterator))
}

func (h *iteratorHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type iteratorMinHeap struct {
	iteratorHeap
}

func (h iteratorMinHeap) Less(i, j int) bool {
	return h.iteratorHeap[i].Entry().Timestamp.Before(h.iteratorHeap[j].Entry().Timestamp)
}

type iteratorMaxHeap struct {
	iteratorHeap
}

func (h iteratorMaxHeap) Less(i, j int) bool {
	return h.iteratorHeap[i].Entry().Timestamp.After(h.iteratorHeap[j].Entry().Timestamp)
}

// heapIterator iterates over a heap of iterators.
type heapIterator struct {
	heap interface {
		heap.Interface
		Peek() EntryIterator
	}
	curr EntryIterator
	errs []error
}

func NewHeapIterator(is []EntryIterator, direction logproto.Direction) EntryIterator {
	result := &heapIterator{}
	iterators := make(iteratorHeap, 0, len(is))
	// pre-next each iterator, drop empty.
	for _, i := range is {
		if i.Next() {
			iterators = append(iterators, i)
		} else {
			result.recordError(i)
			i.Close()
		}
	}

	switch direction {
	case logproto.BACKWARD:
		result.heap = &iteratorMaxHeap{iterators}
	case logproto.FORWARD:
		result.heap = &iteratorMinHeap{iterators}
	default:
		panic("bad direction")
	}
	return result
}

func (i *heapIterator) recordError(ei EntryIterator) {
	err := ei.Error()
	if err != nil {
		i.errs = append(i.errs, err)
	}
}

func (i *heapIterator) Next() bool {
	if i.curr != nil {
		if i.curr.Next() {
			heap.Push(i.heap, i.curr)
		} else {
			i.recordError(i.curr)
			i.curr.Close()
		}
	}

	if i.heap.Len() == 0 {
		return false
	}

	i.curr = heap.Pop(i.heap).(EntryIterator)

	// keep popping entries off if they match, to dedupe
	for i.heap.Len() > 0 {
		curr := i.curr.Entry()
		next := i.heap.Peek().Entry()
		if !curr.Equal(next) {
			break
		}
	}

	return true
}

func (i *heapIterator) Entry() logproto.Entry {
	return i.curr.Entry()
}

func (i *heapIterator) Labels() string {
	return i.curr.Labels()
}

func (i *heapIterator) Error() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return fmt.Errorf("Multiple errors: %+v", i.errs)
	}
}

func (i *heapIterator) Close() error {
	for i.heap.Len() > 0 {
		if err := i.heap.Pop().(EntryIterator).Close(); err != nil {
			return err
		}
	}
	return nil
}

func NewQueryResponseIterator(resp *logproto.QueryResponse, direction logproto.Direction) EntryIterator {
	is := make([]EntryIterator, 0, len(resp.Streams))
	for i := range resp.Streams {
		is = append(is, newStreamIterator(resp.Streams[i]))
	}
	return NewHeapIterator(is, direction)
}

type queryClientIterator struct {
	client    logproto.Querier_QueryClient
	direction logproto.Direction
	err       error
	curr      EntryIterator
}

func newQueryClientIterator(client logproto.Querier_QueryClient, direction logproto.Direction) EntryIterator {
	return &queryClientIterator{
		client:    client,
		direction: direction,
	}
}

func (i *queryClientIterator) Next() bool {
	for i.curr == nil || !i.curr.Next() {
		batch, err := i.client.Recv()
		if err == io.EOF {
			return false
		} else if err != nil {
			i.err = err
			return false
		}

		i.curr = NewQueryResponseIterator(batch, i.direction)
	}

	return true
}

func (i *queryClientIterator) Entry() logproto.Entry {
	return i.curr.Entry()
}

func (i *queryClientIterator) Labels() string {
	return i.curr.Labels()
}

func (i *queryClientIterator) Error() error {
	return i.err
}

func (i *queryClientIterator) Close() error {
	return i.client.CloseSend()
}

type regexpFilter struct {
	re *regexp.Regexp
	EntryIterator
}

func NewRegexpFilter(r string, i EntryIterator) (EntryIterator, error) {
	re, err := regexp.Compile(r)
	if err != nil {
		return nil, err
	}
	return &regexpFilter{
		re:            re,
		EntryIterator: i,
	}, nil
}

func (i *regexpFilter) Next() bool {
	for i.EntryIterator.Next() {
		if i.re.MatchString(i.Entry().Line) {
			return true
		}
	}
	return false
}
