package iter

import (
	"container/heap"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/logproto"
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

// NewHeapIterator returns a new iterator which uses a heap to merge together
// entries for multiple interators.
func NewHeapIterator(is []EntryIterator, direction logproto.Direction) EntryIterator {
	result := &heapIterator{}
	switch direction {
	case logproto.BACKWARD:
		result.heap = &iteratorMaxHeap{}
	case logproto.FORWARD:
		result.heap = &iteratorMinHeap{}
	default:
		panic("bad direction")
	}

	// pre-next each iterator, drop empty.
	for _, i := range is {
		result.requeue(i)
	}

	return result
}

func (i *heapIterator) requeue(ei EntryIterator) {
	if ei.Next() {
		heap.Push(i.heap, ei)
		return
	}

	if err := ei.Error(); err != nil {
		i.errs = append(i.errs, err)
	}
	helpers.LogError("closing iterator", ei.Close)
}

func (i *heapIterator) Next() bool {
	if i.curr != nil {
		i.requeue(i.curr)
	}

	if i.heap.Len() == 0 {
		return false
	}

	i.curr = heap.Pop(i.heap).(EntryIterator)
	currEntry := i.curr.Entry()

	// keep popping entries off if they match, to dedupe
	for i.heap.Len() > 0 {
		next := i.heap.Peek()
		nextEntry := next.Entry()
		if !currEntry.Equal(nextEntry) {
			break
		}

		next = heap.Pop(i.heap).(EntryIterator)
		i.requeue(next)
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

// NewQueryResponseIterator returns an iterator over a QueryResponse.
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

// NewQueryClientIterator returns an iterator over a QueryClient.
func NewQueryClientIterator(client logproto.Querier_QueryClient, direction logproto.Direction) EntryIterator {
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

// NewRegexpFilter returns an iterator that filters entries by regexp.
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

type nonOverlappingIterator struct {
	labels    string
	i         int
	iterators []EntryIterator
	curr      EntryIterator
}

// NewNonOverlappingIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingIterator(iterators []EntryIterator, labels string) EntryIterator {
	return &nonOverlappingIterator{
		labels:    labels,
		iterators: iterators,
	}
}

func (i *nonOverlappingIterator) Next() bool {
	for i.curr == nil || !i.curr.Next() {
		if i.i >= len(i.iterators) {
			return false
		}

		i.curr = i.iterators[i.i]
		i.i++
	}

	return true
}

func (i *nonOverlappingIterator) Entry() logproto.Entry {
	return i.curr.Entry()
}

func (i *nonOverlappingIterator) Labels() string {
	return i.labels
}

func (i *nonOverlappingIterator) Error() error {
	return nil
}

func (i *nonOverlappingIterator) Close() error {
	return nil
}

type timeRangedIterator struct {
	EntryIterator
	mint, maxt time.Time
}

// NewTimeRangedIterator returns an iterator which filters entries by time range.
func NewTimeRangedIterator(it EntryIterator, mint, maxt time.Time) EntryIterator {
	return &timeRangedIterator{
		EntryIterator: it,
		mint:          mint,
		maxt:          maxt,
	}
}

func (i *timeRangedIterator) Next() bool {
	ok := i.EntryIterator.Next()

	ts := i.EntryIterator.Entry().Timestamp
	for ok && i.mint.After(ts) {
		ok = i.EntryIterator.Next()
		ts = i.EntryIterator.Entry().Timestamp
	}

	if ok && (i.maxt.Before(ts) || i.maxt.Equal(ts)) { // The maxt is exclusive.
		ok = false
	}

	return ok
}

type entryIteratorBackward struct {
	cur     logproto.Entry
	entries []logproto.Entry
}

// NewEntryIteratorBackward returns an iterator which loads all the entries
// of an existing iterator, and then iterates over them backward.
func NewEntryIteratorBackward(it EntryIterator) (EntryIterator, error) {
	entries := make([]logproto.Entry, 0, 128)
	for it.Next() {
		entries = append(entries, it.Entry())
	}

	return &entryIteratorBackward{entries: entries}, it.Error()
}

func (i *entryIteratorBackward) Next() bool {
	if len(i.entries) == 0 {
		return false
	}

	i.cur = i.entries[len(i.entries)-1]
	i.entries = i.entries[:len(i.entries)-1]

	return true
}

func (i *entryIteratorBackward) Entry() logproto.Entry {
	return i.cur
}

func (i *entryIteratorBackward) Close() error { return nil }

func (i *entryIteratorBackward) Error() error { return nil }

func (i *entryIteratorBackward) Labels() string {
	return ""
}
