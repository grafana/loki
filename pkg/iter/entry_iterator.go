package iter

import (
	"container/heap"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/stats"
)

// EntryIterator iterates over entries in time-order.
type EntryIterator interface {
	Next() bool
	Entry() logproto.Entry
	Labels() string
	Error() error
	Close() error
}

type noOpIterator struct{}

var NoopIterator = noOpIterator{}

func (noOpIterator) Next() bool              { return false }
func (noOpIterator) Error() error            { return nil }
func (noOpIterator) Labels() string          { return "" }
func (noOpIterator) Entry() logproto.Entry   { return logproto.Entry{} }
func (noOpIterator) Sample() logproto.Sample { return logproto.Sample{} }
func (noOpIterator) Close() error            { return nil }

// streamIterator iterates over entries in a stream.
type streamIterator struct {
	i       int
	entries []logproto.Entry
	labels  string
}

// NewStreamIterator iterates over entries in a stream.
func NewStreamIterator(stream logproto.Stream) EntryIterator {
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
	t1, t2 := h.iteratorHeap[i].Entry().Timestamp, h.iteratorHeap[j].Entry().Timestamp

	un1 := t1.UnixNano()
	un2 := t2.UnixNano()

	switch {
	case un1 < un2:
		return true
	case un1 > un2:
		return false
	default: // un1 == un2:
		return h.iteratorHeap[i].Labels() < h.iteratorHeap[j].Labels()
	}
}

type iteratorMaxHeap struct {
	iteratorHeap
}

func (h iteratorMaxHeap) Less(i, j int) bool {
	t1, t2 := h.iteratorHeap[i].Entry().Timestamp, h.iteratorHeap[j].Entry().Timestamp

	un1 := t1.UnixNano()
	un2 := t2.UnixNano()

	switch {
	case un1 < un2:
		return false
	case un1 > un2:
		return true
	default: // un1 == un2
		return h.iteratorHeap[i].Labels() < h.iteratorHeap[j].Labels()
	}
}

// HeapIterator iterates over a heap of iterators with ability to push new iterators and get some properties like time of entry at peek and len
// Not safe for concurrent use
type HeapIterator interface {
	EntryIterator
	Peek() time.Time
	Len() int
	Push(EntryIterator)
}

// heapIterator iterates over a heap of iterators.
type heapIterator struct {
	heap interface {
		heap.Interface
		Peek() EntryIterator
	}
	is         []EntryIterator
	prefetched bool
	stats      *stats.ChunkData

	tuples     []tuple
	currEntry  logproto.Entry
	currLabels string
	errs       []error
}

// NewHeapIterator returns a new iterator which uses a heap to merge together
// entries for multiple interators.
func NewHeapIterator(ctx context.Context, is []EntryIterator, direction logproto.Direction) HeapIterator {
	result := &heapIterator{is: is, stats: stats.GetChunkData(ctx)}
	switch direction {
	case logproto.BACKWARD:
		result.heap = &iteratorMaxHeap{}
	case logproto.FORWARD:
		result.heap = &iteratorMinHeap{}
	default:
		panic("bad direction")
	}

	result.tuples = make([]tuple, 0, len(is))
	return result
}

// prefetch iterates over all inner iterators to merge together, calls Next() on
// each of them to prefetch the first entry and pushes of them - who are not
// empty - to the heap
func (i *heapIterator) prefetch() {
	if i.prefetched {
		return
	}

	i.prefetched = true
	for _, it := range i.is {
		i.requeue(it, false)
	}

	// We can now clear the list of input iterators to merge, given they have all
	// been processed and the non empty ones have been pushed to the heap
	i.is = nil
}

// requeue pushes the input ei EntryIterator to the heap, advancing it via an ei.Next()
// call unless the advanced input parameter is true. In this latter case it expects that
// the iterator has already been advanced before calling requeue().
//
// If the iterator has no more entries or an error occur while advancing it, the iterator
// is not pushed to the heap and any possible error captured, so that can be get via Error().
func (i *heapIterator) requeue(ei EntryIterator, advanced bool) {
	if advanced || ei.Next() {
		heap.Push(i.heap, ei)
		return
	}

	if err := ei.Error(); err != nil {
		i.errs = append(i.errs, err)
	}
	helpers.LogError("closing iterator", ei.Close)
}

func (i *heapIterator) Push(ei EntryIterator) {
	i.requeue(ei, false)
}

type tuple struct {
	logproto.Entry
	EntryIterator
}

func (i *heapIterator) Next() bool {
	i.prefetch()

	if i.heap.Len() == 0 {
		return false
	}

	// We support multiple entries with the same timestamp, and we want to
	// preserve their original order. We look at all the top entries in the
	// heap with the same timestamp, and pop the ones whose common value
	// occurs most often.
	for i.heap.Len() > 0 {
		next := i.heap.Peek()
		entry := next.Entry()
		if len(i.tuples) > 0 && (i.tuples[0].Labels() != next.Labels() || !i.tuples[0].Timestamp.Equal(entry.Timestamp)) {
			break
		}

		heap.Pop(i.heap)
		i.tuples = append(i.tuples, tuple{
			Entry:         entry,
			EntryIterator: next,
		})
	}

	// shortcut if we have a single tuple.
	if len(i.tuples) == 1 {
		i.currEntry = i.tuples[0].Entry
		i.currLabels = i.tuples[0].Labels()
		i.requeue(i.tuples[0].EntryIterator, false)
		i.tuples = i.tuples[:0]
		return true
	}

	// Find in tuples which entry occurs most often which, due to quorum based
	// replication, is guaranteed to be the correct next entry.
	t := i.tuples[0]
	i.currEntry = t.Entry
	i.currLabels = t.Labels()

	// Requeue the iterators, advancing them if they were consumed.
	for j := range i.tuples {
		if i.tuples[j].Line != i.currEntry.Line {
			i.requeue(i.tuples[j].EntryIterator, true)
			continue
		}
		// we count as duplicates only if the tuple is not the one (t) used to fill the current entry
		if i.tuples[j] != t {
			i.stats.TotalDuplicates++
		}
		i.requeue(i.tuples[j].EntryIterator, false)
	}
	i.tuples = i.tuples[:0]
	return true
}

func (i *heapIterator) Entry() logproto.Entry {
	return i.currEntry
}

func (i *heapIterator) Labels() string {
	return i.currLabels
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
	i.tuples = nil
	return nil
}

func (i *heapIterator) Peek() time.Time {
	i.prefetch()

	return i.heap.Peek().Entry().Timestamp
}

// Len returns the number of inner iterators on the heap, still having entries
func (i *heapIterator) Len() int {
	i.prefetch()

	return i.heap.Len()
}

// NewStreamsIterator returns an iterator over logproto.Stream
func NewStreamsIterator(ctx context.Context, streams []logproto.Stream, direction logproto.Direction) EntryIterator {
	is := make([]EntryIterator, 0, len(streams))
	for i := range streams {
		is = append(is, NewStreamIterator(streams[i]))
	}
	return NewHeapIterator(ctx, is, direction)
}

// NewQueryResponseIterator returns an iterator over a QueryResponse.
func NewQueryResponseIterator(ctx context.Context, resp *logproto.QueryResponse, direction logproto.Direction) EntryIterator {
	is := make([]EntryIterator, 0, len(resp.Streams))
	for i := range resp.Streams {
		is = append(is, NewStreamIterator(resp.Streams[i]))
	}
	return NewHeapIterator(ctx, is, direction)
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

		i.curr = NewQueryResponseIterator(i.client.Context(), batch, i.direction)
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
		if len(i.iterators) == 0 {
			if i.curr != nil {
				i.curr.Close()
			}
			return false
		}
		if i.curr != nil {
			i.curr.Close()
		}
		i.i++
		i.curr, i.iterators = i.iterators[0], i.iterators[1:]
	}

	return true
}

func (i *nonOverlappingIterator) Entry() logproto.Entry {
	return i.curr.Entry()
}

func (i *nonOverlappingIterator) Labels() string {
	if i.labels != "" {
		return i.labels
	}

	return i.curr.Labels()
}

func (i *nonOverlappingIterator) Error() error {
	if i.curr != nil {
		return i.curr.Error()
	}
	return nil
}

func (i *nonOverlappingIterator) Close() error {
	for _, iter := range i.iterators {
		iter.Close()
	}
	i.iterators = nil
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
	if !ok {
		i.EntryIterator.Close()
		return ok
	}
	ts := i.EntryIterator.Entry().Timestamp
	for ok && i.mint.After(ts) {
		ok = i.EntryIterator.Next()
		if !ok {
			continue
		}
		ts = i.EntryIterator.Entry().Timestamp
	}
	if ok {
		if ts.Equal(i.mint) { // The mint is inclusive
			return true
		}
		if i.maxt.Before(ts) || i.maxt.Equal(ts) { // The maxt is exclusive.
			ok = false
		}
	}
	if !ok {
		i.EntryIterator.Close()
	}
	return ok
}

type entryWithLabels struct {
	entry  logproto.Entry
	labels string
}

type reverseIterator struct {
	iter              EntryIterator
	cur               entryWithLabels
	entriesWithLabels []entryWithLabels

	loaded bool
	limit  uint32
}

// NewReversedIter returns an iterator which loads all or up to N entries
// of an existing iterator, and then iterates over them backward.
// Preload entries when they are being queried with a timeout.
func NewReversedIter(it EntryIterator, limit uint32, preload bool) (EntryIterator, error) {
	iter, err := &reverseIterator{
		iter:              it,
		entriesWithLabels: make([]entryWithLabels, 0, 1024),
		limit:             limit,
	}, it.Error()

	if err != nil {
		return nil, err
	}

	if preload {
		iter.load()
	}

	return iter, nil
}

func (i *reverseIterator) load() {
	if !i.loaded {
		i.loaded = true
		for count := uint32(0); (i.limit == 0 || count < i.limit) && i.iter.Next(); count++ {
			i.entriesWithLabels = append(i.entriesWithLabels, entryWithLabels{i.iter.Entry(), i.iter.Labels()})
		}
		i.iter.Close()
	}
}

func (i *reverseIterator) Next() bool {
	i.load()
	if len(i.entriesWithLabels) == 0 {
		i.entriesWithLabels = nil
		return false
	}
	i.cur, i.entriesWithLabels = i.entriesWithLabels[len(i.entriesWithLabels)-1], i.entriesWithLabels[:len(i.entriesWithLabels)-1]
	return true
}

func (i *reverseIterator) Entry() logproto.Entry {
	return i.cur.entry
}

func (i *reverseIterator) Labels() string {
	return i.cur.labels
}

func (i *reverseIterator) Error() error { return nil }

func (i *reverseIterator) Close() error {
	if !i.loaded {
		return i.iter.Close()
	}
	return nil
}

var entryBufferPool = sync.Pool{
	New: func() interface{} {
		return &entryBuffer{
			entries: make([]entryWithLabels, 0, 1024),
		}
	},
}

type entryBuffer struct {
	entries []entryWithLabels
}

type reverseEntryIterator struct {
	iter EntryIterator
	cur  entryWithLabels
	buf  *entryBuffer

	loaded bool
}

// NewEntryReversedIter returns an iterator which loads all entries and iterates backward.
// The labels of entries is always empty.
func NewEntryReversedIter(it EntryIterator) (EntryIterator, error) {
	iter, err := &reverseEntryIterator{
		iter: it,
		buf:  entryBufferPool.Get().(*entryBuffer),
	}, it.Error()

	if err != nil {
		return nil, err
	}

	return iter, nil
}

func (i *reverseEntryIterator) load() {
	if !i.loaded {
		i.loaded = true
		for i.iter.Next() {
			i.buf.entries = append(i.buf.entries, entryWithLabels{i.iter.Entry(), i.iter.Labels()})
		}
		i.iter.Close()
	}
}

func (i *reverseEntryIterator) Next() bool {
	i.load()
	if len(i.buf.entries) == 0 {
		entryBufferPool.Put(i.buf)
		i.buf.entries = nil
		return false
	}
	i.cur, i.buf.entries = i.buf.entries[len(i.buf.entries)-1], i.buf.entries[:len(i.buf.entries)-1]
	return true
}

func (i *reverseEntryIterator) Entry() logproto.Entry {
	return i.cur.entry
}

func (i *reverseEntryIterator) Labels() string {
	return i.cur.labels
}

func (i *reverseEntryIterator) Error() error { return nil }

func (i *reverseEntryIterator) Close() error {
	if i.buf.entries != nil {
		i.buf.entries = i.buf.entries[:0]
		entryBufferPool.Put(i.buf)
		i.buf.entries = nil
	}
	if !i.loaded {
		return i.iter.Close()
	}
	return nil
}

// ReadBatch reads a set of entries off an iterator.
func ReadBatch(i EntryIterator, size uint32) (*logproto.QueryResponse, uint32, error) {
	streams := map[string]*logproto.Stream{}
	respSize := uint32(0)
	for ; respSize < size && i.Next(); respSize++ {
		labels, entry := i.Labels(), i.Entry()
		stream, ok := streams[labels]
		if !ok {
			stream = &logproto.Stream{
				Labels: labels,
			}
			streams[labels] = stream
		}
		stream.Entries = append(stream.Entries, entry)
	}

	result := logproto.QueryResponse{
		Streams: make([]logproto.Stream, 0, len(streams)),
	}
	for _, stream := range streams {
		result.Streams = append(result.Streams, *stream)
	}
	return &result, respSize, i.Error()
}

type peekingEntryIterator struct {
	iter EntryIterator

	cache *entryWithLabels
	next  *entryWithLabels
}

// PeekingEntryIterator is an entry iterator that can look ahead an entry
// using `Peek` without advancing its cursor.
type PeekingEntryIterator interface {
	EntryIterator
	Peek() (string, logproto.Entry, bool)
}

// NewPeekingIterator creates a new peeking iterator.
func NewPeekingIterator(iter EntryIterator) PeekingEntryIterator {
	// initialize the next entry so we can peek right from the start.
	var cache *entryWithLabels
	next := &entryWithLabels{}
	if iter.Next() {
		cache = &entryWithLabels{
			entry:  iter.Entry(),
			labels: iter.Labels(),
		}
		next.entry = cache.entry
		next.labels = cache.labels
	}
	return &peekingEntryIterator{
		iter:  iter,
		cache: cache,
		next:  next,
	}
}

// Next implements `EntryIterator`
func (it *peekingEntryIterator) Next() bool {
	if it.cache != nil {
		it.next.entry = it.cache.entry
		it.next.labels = it.cache.labels
		it.cacheNext()
		return true
	}
	return false
}

// cacheNext caches the next element if it exists.
func (it *peekingEntryIterator) cacheNext() {
	if it.iter.Next() {
		it.cache.entry = it.iter.Entry()
		it.cache.labels = it.iter.Labels()
		return
	}
	// nothing left removes the cached entry
	it.cache = nil
}

// Peek implements `PeekingEntryIterator`
func (it *peekingEntryIterator) Peek() (string, logproto.Entry, bool) {
	if it.cache != nil {
		return it.cache.labels, it.cache.entry, true
	}
	return "", logproto.Entry{}, false
}

// Labels implements `EntryIterator`
func (it *peekingEntryIterator) Labels() string {
	if it.next != nil {
		return it.next.labels
	}
	return ""
}

// Entry implements `EntryIterator`
func (it *peekingEntryIterator) Entry() logproto.Entry {
	if it.next != nil {
		return it.next.entry
	}
	return logproto.Entry{}
}

// Error implements `EntryIterator`
func (it *peekingEntryIterator) Error() error {
	return it.iter.Error()
}

// Close implements `EntryIterator`
func (it *peekingEntryIterator) Close() error {
	return it.iter.Close()
}
