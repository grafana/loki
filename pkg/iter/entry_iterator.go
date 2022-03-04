package iter

import (
	"container/heap"
	"context"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/util"
)

// EntryIterator iterates over entries in time-order.
type EntryIterator interface {
	Iterator
	Entry() logproto.Entry
}

// streamIterator iterates over entries in a stream.
type streamIterator struct {
	i      int
	stream logproto.Stream
}

// NewStreamIterator iterates over entries in a stream.
func NewStreamIterator(stream logproto.Stream) EntryIterator {
	return &streamIterator{
		i:      -1,
		stream: stream,
	}
}

func (i *streamIterator) Next() bool {
	i.i++
	return i.i < len(i.stream.Entries)
}

func (i *streamIterator) Error() error {
	return nil
}

func (i *streamIterator) Labels() string {
	return i.stream.Labels
}

func (i *streamIterator) StreamHash() uint64 { return i.stream.Hash }

func (i *streamIterator) Entry() logproto.Entry {
	return i.stream.Entries[i.i]
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

type iteratorSortHeap struct {
	iteratorHeap
	byAscendingTime bool
}

func (h iteratorSortHeap) Less(i, j int) bool {
	t1, t2 := h.iteratorHeap[i].Entry().Timestamp.UnixNano(), h.iteratorHeap[j].Entry().Timestamp.UnixNano()
	if t1 == t2 {
		return h.iteratorHeap[i].StreamHash() < h.iteratorHeap[j].StreamHash()
	}
	if h.byAscendingTime {
		return t1 < t2
	}
	return t1 > t2
}

// HeapIterator iterates over a heap of iterators with ability to push new iterators and get some properties like time of entry at peek and len
// Not safe for concurrent use
type HeapIterator interface {
	EntryIterator
	Peek() time.Time
	Len() int
	Push(EntryIterator)
}

// mergeEntryIterator iterates over a heap of iterators and merge duplicate entries.
type mergeEntryIterator struct {
	heap interface {
		heap.Interface
		Peek() EntryIterator
	}
	is         []EntryIterator
	prefetched bool
	stats      *stats.Context

	tuples    []tuple
	currEntry entryWithLabels
	errs      []error
}

// NewMergeEntryIterator returns a new iterator which uses a heap to merge together entries for multiple iterators and deduplicate entries if any.
// The iterator only order and merge entries across given `is` iterators, it does not merge entries within individual iterator.
// This means using this iterator with a single iterator will result in the same result as the input iterator.
// If you don't need to deduplicate entries, use `NewSortEntryIterator` instead.
func NewMergeEntryIterator(ctx context.Context, is []EntryIterator, direction logproto.Direction) HeapIterator {
	result := &mergeEntryIterator{is: is, stats: stats.FromContext(ctx)}
	switch direction {
	case logproto.BACKWARD:
		result.heap = &iteratorSortHeap{iteratorHeap: make([]EntryIterator, 0, len(is)), byAscendingTime: false}
	case logproto.FORWARD:
		result.heap = &iteratorSortHeap{iteratorHeap: make([]EntryIterator, 0, len(is)), byAscendingTime: true}
	default:
		panic("bad direction")
	}

	result.tuples = make([]tuple, 0, len(is))
	return result
}

// prefetch iterates over all inner iterators to merge together, calls Next() on
// each of them to prefetch the first entry and pushes of them - who are not
// empty - to the heap
func (i *mergeEntryIterator) prefetch() {
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
func (i *mergeEntryIterator) requeue(ei EntryIterator, advanced bool) {
	if advanced || ei.Next() {
		heap.Push(i.heap, ei)
		return
	}

	if err := ei.Error(); err != nil {
		i.errs = append(i.errs, err)
	}
	util.LogError("closing iterator", ei.Close)
}

func (i *mergeEntryIterator) Push(ei EntryIterator) {
	i.requeue(ei, false)
}

type tuple struct {
	logproto.Entry
	EntryIterator
}

func (i *mergeEntryIterator) Next() bool {
	i.prefetch()

	if i.heap.Len() == 0 {
		return false
	}

	// shortcut for the last iterator.
	if i.heap.Len() == 1 {
		i.currEntry.entry = i.heap.Peek().Entry()
		i.currEntry.labels = i.heap.Peek().Labels()
		i.currEntry.streamHash = i.heap.Peek().StreamHash()

		if !i.heap.Peek().Next() {
			i.heap.Pop()
		}
		return true
	}

	// We support multiple entries with the same timestamp, and we want to
	// preserve their original order. We look at all the top entries in the
	// heap with the same timestamp, and pop the ones whose common value
	// occurs most often.
	for i.heap.Len() > 0 {
		next := i.heap.Peek()
		entry := next.Entry()
		if len(i.tuples) > 0 && (i.tuples[0].StreamHash() != next.StreamHash() || !i.tuples[0].Timestamp.Equal(entry.Timestamp)) {
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
		i.currEntry.entry = i.tuples[0].Entry
		i.currEntry.labels = i.tuples[0].Labels()
		i.currEntry.streamHash = i.tuples[0].StreamHash()
		i.requeue(i.tuples[0].EntryIterator, false)
		i.tuples = i.tuples[:0]
		return true
	}

	// Find in tuples which entry occurs most often which, due to quorum based
	// replication, is guaranteed to be the correct next entry.
	t := i.tuples[0]
	i.currEntry.entry = t.Entry
	i.currEntry.labels = t.Labels()
	i.currEntry.streamHash = i.tuples[0].StreamHash()

	// Requeue the iterators, advancing them if they were consumed.
	for j := range i.tuples {
		if i.tuples[j].Line != i.currEntry.entry.Line {
			i.requeue(i.tuples[j].EntryIterator, true)
			continue
		}
		// we count as duplicates only if the tuple is not the one (t) used to fill the current entry
		if i.tuples[j] != t {
			i.stats.AddDuplicates(1)
		}
		i.requeue(i.tuples[j].EntryIterator, false)
	}
	i.tuples = i.tuples[:0]
	return true
}

func (i *mergeEntryIterator) Entry() logproto.Entry {
	return i.currEntry.entry
}

func (i *mergeEntryIterator) Labels() string {
	return i.currEntry.labels
}

func (i *mergeEntryIterator) StreamHash() uint64 { return i.currEntry.streamHash }

func (i *mergeEntryIterator) Error() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return util.MultiError(i.errs)
	}
}

func (i *mergeEntryIterator) Close() error {
	for i.heap.Len() > 0 {
		if err := i.heap.Pop().(EntryIterator).Close(); err != nil {
			return err
		}
	}
	i.tuples = nil
	return nil
}

func (i *mergeEntryIterator) Peek() time.Time {
	i.prefetch()

	return i.heap.Peek().Entry().Timestamp
}

// Len returns the number of inner iterators on the heap, still having entries
func (i *mergeEntryIterator) Len() int {
	i.prefetch()

	return i.heap.Len()
}

type entrySortIterator struct {
	is              []EntryIterator
	prefetched      bool
	byAscendingTime bool
	currEntry       entryWithLabels
	errs            []error
}

// NewSortEntryIterator returns a new EntryIterator that sorts entries by timestamp (depending on the direction) the input iterators.
// The iterator only order entries across given `is` iterators, it does not sort entries within individual iterator.
// This means using this iterator with a single iterator will result in the same result as the input iterator.
// When timestamp is equal, the iterator sorts samples by their label alphabetically.
func NewSortEntryIterator(is []EntryIterator, direction logproto.Direction) EntryIterator {
	if len(is) == 0 {
		return NoopIterator
	}
	if len(is) == 1 {
		return is[0]
	}
	result := &entrySortIterator{is: is}
	switch direction {
	case logproto.BACKWARD:
		result.byAscendingTime = false
	case logproto.FORWARD:
		result.byAscendingTime = true
	default:
		panic("bad direction")
	}
	return result
}

func (i *entrySortIterator) lessByIndex(k, j int) bool {
	t1, t2 := i.is[k].Entry().Timestamp.UnixNano(), i.is[j].Entry().Timestamp.UnixNano()
	if t1 == t2 {
		// The underlying stream hash may not be available, such as when merging LokiResponses in the
		// frontend which were sharded. Prefer to use the underlying stream hash when available,
		// which is needed in deduping code, but defer to label sorting when it's not present.
		if i.is[k].StreamHash() == 0 {
			return i.is[k].Labels() < i.is[j].Labels()
		}
		return i.is[k].StreamHash() < i.is[j].StreamHash()
	}
	if i.byAscendingTime {
		return t1 < t2
	}
	return t1 > t2
}

func (i *entrySortIterator) lessByValue(t1 int64, l1 uint64, lb string, index int) bool {
	t2 := i.is[index].Entry().Timestamp.UnixNano()
	if t1 == t2 {
		if l1 == 0 {
			return lb < i.is[index].Labels()
		}
		return l1 < i.is[index].StreamHash()
	}
	if i.byAscendingTime {
		return t1 < t2
	}
	return t1 > t2
}

// init throws out empty iterators and sorts them.
func (i *entrySortIterator) init() {
	if i.prefetched {
		return
	}

	i.prefetched = true
	tmp := make([]EntryIterator, 0, len(i.is))
	for _, it := range i.is {
		if it.Next() {
			tmp = append(tmp, it)
			continue
		}

		if err := it.Error(); err != nil {
			i.errs = append(i.errs, err)
		}
		util.LogError("closing iterator", it.Close)
	}
	i.is = tmp
	sort.Slice(i.is, i.lessByIndex)
}

func (i *entrySortIterator) fix() {
	head := i.is[0]
	t1 := head.Entry().Timestamp.UnixNano()
	l1 := head.StreamHash()
	lb := head.Labels()

	// shortcut
	if len(i.is) <= 1 || i.lessByValue(t1, l1, lb, 1) {
		return
	}

	// First element is out of place. So we reposition it.
	i.is = i.is[1:] // drop head
	index := sort.Search(len(i.is), func(in int) bool { return i.lessByValue(t1, l1, lb, in) })

	if index == len(i.is) {
		i.is = append(i.is, head)
	} else {
		i.is = append(i.is[:index+1], i.is[index:]...)
		i.is[index] = head
	}
}

func (i *entrySortIterator) Next() bool {
	i.init()

	if len(i.is) == 0 {
		return false
	}

	next := i.is[0]
	i.currEntry.entry = next.Entry()
	i.currEntry.labels = next.Labels()
	i.currEntry.streamHash = next.StreamHash()
	// if the top iterator is empty, we remove it.
	if !next.Next() {
		i.is = i.is[1:]
		if err := next.Error(); err != nil {
			i.errs = append(i.errs, err)
		}
		util.LogError("closing iterator", next.Close)
		return true
	}

	if len(i.is) > 1 {
		i.fix()
	}

	return true
}

func (i *entrySortIterator) Entry() logproto.Entry {
	return i.currEntry.entry
}

func (i *entrySortIterator) Labels() string {
	return i.currEntry.labels
}

func (i *entrySortIterator) StreamHash() uint64 {
	return i.currEntry.streamHash
}

func (i *entrySortIterator) Error() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return util.MultiError(i.errs)
	}
}

func (i *entrySortIterator) Close() error {
	for _, entryIterator := range i.is {
		if err := entryIterator.Close(); err != nil {
			return err
		}
	}
	return nil
}

// NewStreamsIterator returns an iterator over logproto.Stream
func NewStreamsIterator(streams []logproto.Stream, direction logproto.Direction) EntryIterator {
	is := make([]EntryIterator, 0, len(streams))
	for i := range streams {
		is = append(is, NewStreamIterator(streams[i]))
	}
	return NewSortEntryIterator(is, direction)
}

// NewQueryResponseIterator returns an iterator over a QueryResponse.
func NewQueryResponseIterator(resp *logproto.QueryResponse, direction logproto.Direction) EntryIterator {
	return NewStreamsIterator(resp.Streams, direction)
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
	ctx := i.client.Context()
	for i.curr == nil || !i.curr.Next() {
		batch, err := i.client.Recv()
		if err == io.EOF {
			return false
		} else if err != nil {
			i.err = err
			return false
		}
		stats.JoinIngesters(ctx, batch.Stats)
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

func (i *queryClientIterator) StreamHash() uint64 { return i.curr.StreamHash() }

func (i *queryClientIterator) Error() error {
	return i.err
}

func (i *queryClientIterator) Close() error {
	return i.client.CloseSend()
}

type nonOverlappingIterator struct {
	iterators []EntryIterator
	curr      EntryIterator
}

// NewNonOverlappingIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingIterator(iterators []EntryIterator) EntryIterator {
	return &nonOverlappingIterator{
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
		i.curr, i.iterators = i.iterators[0], i.iterators[1:]
	}

	return true
}

func (i *nonOverlappingIterator) Entry() logproto.Entry {
	return i.curr.Entry()
}

func (i *nonOverlappingIterator) Labels() string {
	if i.curr == nil {
		return ""
	}
	return i.curr.Labels()
}

func (i *nonOverlappingIterator) StreamHash() uint64 {
	if i.curr == nil {
		return 0
	}
	return i.curr.StreamHash()
}

func (i *nonOverlappingIterator) Error() error {
	if i.curr == nil {
		return nil
	}
	return i.curr.Error()
}

func (i *nonOverlappingIterator) Close() error {
	if i.curr != nil {
		i.curr.Close()
	}
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
// Note: Only works with iterators that go forwards.
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
	entry      logproto.Entry
	labels     string
	streamHash uint64
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
			i.entriesWithLabels = append(i.entriesWithLabels, entryWithLabels{i.iter.Entry(), i.iter.Labels(), i.iter.StreamHash()})
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

func (i *reverseIterator) StreamHash() uint64 {
	return i.cur.streamHash
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
			i.buf.entries = append(i.buf.entries, entryWithLabels{i.iter.Entry(), i.iter.Labels(), i.iter.StreamHash()})
		}
		i.iter.Close()
	}
}

func (i *reverseEntryIterator) Next() bool {
	i.load()
	if i.buf == nil || len(i.buf.entries) == 0 {
		i.release()
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

func (i *reverseEntryIterator) StreamHash() uint64 {
	return i.cur.streamHash
}

func (i *reverseEntryIterator) Error() error { return nil }

func (i *reverseEntryIterator) release() {
	if i.buf == nil {
		return
	}

	if i.buf.entries != nil {
		// preserve the underlying slice before releasing to pool
		i.buf.entries = i.buf.entries[:0]
	}
	entryBufferPool.Put(i.buf)
	i.buf = nil
}

func (i *reverseEntryIterator) Close() error {
	i.release()
	if !i.loaded {
		return i.iter.Close()
	}
	return nil
}

// ReadBatch reads a set of entries off an iterator.
func ReadBatch(i EntryIterator, size uint32) (*logproto.QueryResponse, uint32, error) {
	var (
		streams      = map[uint64]map[string]*logproto.Stream{}
		respSize     uint32
		streamsCount int
	)
	for ; respSize < size && i.Next(); respSize++ {
		labels, hash, entry := i.Labels(), i.StreamHash(), i.Entry()
		mutatedStreams, ok := streams[hash]
		if !ok {
			mutatedStreams = map[string]*logproto.Stream{}
			streams[hash] = mutatedStreams
		}
		mutatedStream, ok := mutatedStreams[labels]
		if !ok {
			streamsCount++
			mutatedStream = &logproto.Stream{
				Labels: labels,
				Hash:   hash,
			}
			mutatedStreams[labels] = mutatedStream
		}
		mutatedStream.Entries = append(mutatedStream.Entries, entry)
	}

	result := logproto.QueryResponse{
		Streams: make([]logproto.Stream, 0, streamsCount),
	}
	for _, mutatedStreams := range streams {
		for _, s := range mutatedStreams {
			result.Streams = append(result.Streams, *s)
		}
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
			entry:      iter.Entry(),
			labels:     iter.Labels(),
			streamHash: iter.StreamHash(),
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
		it.next.streamHash = it.cache.streamHash
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
		it.cache.streamHash = it.iter.StreamHash()
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

func (it *peekingEntryIterator) StreamHash() uint64 {
	if it.next != nil {
		return it.next.streamHash
	}
	return 0
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
