package querier

import (
	"container/heap"
	"fmt"
	"io"

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

func (h iteratorHeap) Len() int { return len(h) }
func (h iteratorHeap) Less(i, j int) bool {
	return h[i].Entry().Timestamp.Before(h[j].Entry().Timestamp)
}
func (h iteratorHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

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

// heapIterator iterates over a heap of iterators.
type heapIterator struct {
	iterators iteratorHeap
	curr      EntryIterator
	errs      []error
}

func NewHeapIterator(is []EntryIterator) EntryIterator {
	result := &heapIterator{
		iterators: make(iteratorHeap, 0, len(is)),
	}

	// pre-next each iterator, drop empty.
	for _, i := range is {
		if i.Next() {
			result.iterators = append(result.iterators, i)
		} else {
			result.recordError(i)
			i.Close()
		}
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
			heap.Push(&i.iterators, i.curr)
		} else {
			i.recordError(i.curr)
			i.curr.Close()
		}
	}

	if len(i.iterators) == 0 {
		return false
	}

	i.curr = heap.Pop(&i.iterators).(EntryIterator)
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
	for _, i := range i.iterators {
		if err := i.Close(); err != nil {
			return err
		}
	}
	return nil
}

func NewQueryResponseIterator(resp *logproto.QueryResponse) EntryIterator {
	is := make([]EntryIterator, 0, len(resp.Streams))
	for i := range resp.Streams {
		is = append(is, newStreamIterator(resp.Streams[i]))
	}
	return NewHeapIterator(is)
}

type queryClientIterator struct {
	client logproto.Querier_QueryClient
	err    error
	curr   EntryIterator
}

func newQueryClientIterator(client logproto.Querier_QueryClient) EntryIterator {
	return &queryClientIterator{
		client: client,
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

		i.curr = NewQueryResponseIterator(batch)
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
