package iter

import (
	"container/heap"
	"context"
	"io"
	"sync"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/util"
)

// ExemplarIterator iterates over exemplars in time-order.
type ExemplarIterator interface {
	Iterator
	// This way we can skip when ranging over exemplars.
	Exemplar() logproto.Exemplar
}

// PeekingExemplarIterator is a exemplar iterator that can peek exemplar without moving the current exemplar.
type PeekingExemplarIterator interface {
	ExemplarIterator
	Peek() (string, logproto.Exemplar, bool)
}

type peekingExemplarIterator struct {
	iter ExemplarIterator

	cache *exemplarWithLabels
	next  *exemplarWithLabels
}

type exemplarWithLabels struct {
	logproto.Exemplar
	labels     string
	streamHash uint64
}

func NewPeekingExemplarIterator(iter ExemplarIterator) PeekingExemplarIterator {
	// initialize the next entry so we can peek right from the start.
	var cache *exemplarWithLabels
	next := &exemplarWithLabels{}
	if iter.Next() {
		cache = &exemplarWithLabels{
			Exemplar:   iter.Exemplar(),
			labels:     iter.Labels(),
			streamHash: iter.StreamHash(),
		}
		next.Exemplar = cache.Exemplar
		next.labels = cache.labels
	}
	return &peekingExemplarIterator{
		iter:  iter,
		cache: cache,
		next:  next,
	}
}

func (it *peekingExemplarIterator) Close() error {
	return it.iter.Close()
}

func (it *peekingExemplarIterator) Labels() string {
	if it.next != nil {
		return it.next.labels
	}
	return ""
}

func (it *peekingExemplarIterator) StreamHash() uint64 {
	if it.next != nil {
		return it.next.streamHash
	}
	return 0
}

func (it *peekingExemplarIterator) Next() bool {
	if it.cache != nil {
		it.next.Exemplar = it.cache.Exemplar
		it.next.labels = it.cache.labels
		it.next.streamHash = it.cache.streamHash
		it.cacheNext()
		return true
	}
	return false
}

// cacheNext caches the next element if it exists.
func (it *peekingExemplarIterator) cacheNext() {
	if it.iter.Next() {
		it.cache.Exemplar = it.iter.Exemplar()
		it.cache.labels = it.iter.Labels()
		it.cache.streamHash = it.iter.StreamHash()
		return
	}
	// nothing left removes the cached entry
	it.cache = nil
}

func (it *peekingExemplarIterator) Exemplar() logproto.Exemplar {
	if it.next != nil {
		return it.next.Exemplar
	}
	return logproto.Exemplar{}
}

func (it *peekingExemplarIterator) Peek() (string, logproto.Exemplar, bool) {
	if it.cache != nil {
		return it.cache.labels, it.cache.Exemplar, true
	}
	return "", logproto.Exemplar{}, false
}

func (it *peekingExemplarIterator) Error() error {
	return it.iter.Error()
}

type exemplarIteratorHeap struct {
	its []ExemplarIterator
}

func (h exemplarIteratorHeap) Len() int               { return len(h.its) }
func (h exemplarIteratorHeap) Swap(i, j int)          { h.its[i], h.its[j] = h.its[j], h.its[i] }
func (h exemplarIteratorHeap) Peek() ExemplarIterator { return h.its[0] }
func (h *exemplarIteratorHeap) Push(x interface{}) {
	h.its = append(h.its, x.(ExemplarIterator))
}

func (h *exemplarIteratorHeap) Pop() interface{} {
	n := len(h.its)
	x := h.its[n-1]
	h.its = h.its[0 : n-1]
	return x
}

func (h exemplarIteratorHeap) Less(i, j int) bool {
	s1, s2 := h.its[i].Exemplar(), h.its[j].Exemplar()
	if s1.TimestampMs == s2.TimestampMs {
		if h.its[i].StreamHash() == 0 {
			return h.its[i].Labels() < h.its[j].Labels()
		}
		return h.its[i].StreamHash() < h.its[j].StreamHash()
	}
	return s1.TimestampMs < s2.TimestampMs
}

// mergeExemplarIterator iterates over a heap of iterators by merging exemplars.
type mergeExemplarIterator struct {
	heap       *exemplarIteratorHeap
	is         []ExemplarIterator
	prefetched bool
	stats      *stats.Context
	// pushBuffer contains the list of iterators that needs to be pushed to the heap
	// This is to avoid allocations.
	pushBuffer []ExemplarIterator

	// buffer of entries to be returned by Next()
	// We buffer entries with the same timestamp to correctly dedupe them.
	buffer []exemplarWithLabels
	curr   exemplarWithLabels
	errs   []error
}

// NewMergeExemplarIterator returns a new iterator which uses a heap to merge together exemplars for multiple iterators and deduplicate if any.
// The iterator only order and merge entries across given `is` iterators, it does not merge entries within individual iterator.
// This means using this iterator with a single iterator will result in the same result as the input iterator.
// If you don't need to deduplicate exemplar, use `NewSortExemplarIterator` instead.
func NewMergeExemplarIterator(ctx context.Context, is []ExemplarIterator) ExemplarIterator {
	h := exemplarIteratorHeap{
		its: make([]ExemplarIterator, 0, len(is)),
	}
	return &mergeExemplarIterator{
		stats:      stats.FromContext(ctx),
		is:         is,
		heap:       &h,
		buffer:     make([]exemplarWithLabels, 0, len(is)),
		pushBuffer: make([]ExemplarIterator, 0, len(is)),
	}
}

// prefetch iterates over all inner iterators to merge together, calls Next() on
// each of them to prefetch the first entry and pushes of them - who are not
// empty - to the heap
func (i *mergeExemplarIterator) prefetch() {
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
func (i *mergeExemplarIterator) requeue(ei ExemplarIterator, advanced bool) {
	if advanced || ei.Next() {
		heap.Push(i.heap, ei)
		return
	}

	if err := ei.Error(); err != nil {
		i.errs = append(i.errs, err)
	}
	util.LogError("closing iterator", ei.Close)
}

func (i *mergeExemplarIterator) Next() bool {
	i.prefetch()

	if len(i.buffer) != 0 {
		i.nextFromBuffer()
		return true
	}

	if i.heap.Len() == 0 {
		return false
	}

	// shortcut for the last iterator.
	if i.heap.Len() == 1 {
		i.curr.Exemplar = i.heap.Peek().Exemplar()
		i.curr.labels = i.heap.Peek().Labels()
		i.curr.streamHash = i.heap.Peek().StreamHash()
		if !i.heap.Peek().Next() {
			i.heap.Pop()
		}
		return true
	}

	// We support multiple entries with the same timestamp, and we want to
	// preserve their original order. We look at all the top entries in the
	// heap with the same timestamp, and pop the ones whose common value
	// occurs most often.
Outer:
	for i.heap.Len() > 0 {
		next := i.heap.Peek()
		exemplar := next.Exemplar()
		if len(i.buffer) > 0 && (i.buffer[0].streamHash != next.StreamHash() || i.buffer[0].TimestampMs != exemplar.TimestampMs) {
			break
		}
		heap.Pop(i.heap)
		previous := i.buffer
		var dupe bool
		for _, t := range previous {
			if t.Exemplar.Hash == exemplar.Hash {
				i.stats.AddDuplicates(1)
				dupe = true
				break
			}
		}
		if !dupe {
			i.buffer = append(i.buffer, exemplarWithLabels{
				Exemplar:   exemplar,
				labels:     next.Labels(),
				streamHash: next.StreamHash(),
			})
		}
	inner:
		for {
			if !next.Next() {
				continue Outer
			}
			exemplar := next.Exemplar()
			if next.StreamHash() != i.buffer[0].streamHash ||
				exemplar.TimestampMs != i.buffer[0].TimestampMs {
				break
			}
			for _, t := range previous {
				if t.Hash == exemplar.Hash {
					i.stats.AddDuplicates(1)
					continue inner
				}
			}
			i.buffer = append(i.buffer, exemplarWithLabels{
				Exemplar:   exemplar,
				labels:     next.Labels(),
				streamHash: next.StreamHash(),
			})
		}
		i.pushBuffer = append(i.pushBuffer, next)
	}

	for _, ei := range i.pushBuffer {
		heap.Push(i.heap, ei)
	}
	i.pushBuffer = i.pushBuffer[:0]

	i.nextFromBuffer()

	return true
}

func (i *mergeExemplarIterator) nextFromBuffer() {
	i.curr.Exemplar = i.buffer[0].Exemplar
	i.curr.labels = i.buffer[0].labels
	i.curr.streamHash = i.buffer[0].streamHash
	if len(i.buffer) == 1 {
		i.buffer = i.buffer[:0]
		return
	}
	i.buffer = i.buffer[1:]
}

func (i *mergeExemplarIterator) Exemplar() logproto.Exemplar {
	return i.curr.Exemplar
}

func (i *mergeExemplarIterator) Labels() string {
	return i.curr.labels
}

func (i *mergeExemplarIterator) StreamHash() uint64 {
	return i.curr.streamHash
}

func (i *mergeExemplarIterator) Error() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return util.MultiError(i.errs)
	}
}

func (i *mergeExemplarIterator) Close() error {
	for i.heap.Len() > 0 {
		if err := i.heap.Pop().(ExemplarIterator).Close(); err != nil {
			return err
		}
	}
	i.buffer = nil
	return nil
}

// sortExemplarIterator iterates over a heap of iterators by sorting exemplars.
type sortExemplarIterator struct {
	heap       *exemplarIteratorHeap
	is         []ExemplarIterator
	prefetched bool

	curr exemplarWithLabels
	errs []error
}

// NewSortExemplarIterator returns a new ExemplarIterator that sorts exemplars by ascending timestamp the input iterators.
// The iterator only order exemplar across given `is` iterators, it does not sort exemplars within individual iterator.
// This means using this iterator with a single iterator will result in the same result as the input iterator.
// When timestamp is equal, the iterator sorts exemplars by their label alphabetically.
func NewSortExemplarIterator(is []ExemplarIterator) ExemplarIterator {
	if len(is) == 0 {
		return NoopIterator
	}
	if len(is) == 1 {
		return is[0]
	}
	h := exemplarIteratorHeap{
		its: make([]ExemplarIterator, 0, len(is)),
	}
	return &sortExemplarIterator{
		is:   is,
		heap: &h,
	}
}

// init initialize the underlaying heap
func (i *sortExemplarIterator) init() {
	if i.prefetched {
		return
	}

	i.prefetched = true
	for _, it := range i.is {
		if it.Next() {
			i.heap.Push(it)
			continue
		}

		if err := it.Error(); err != nil {
			i.errs = append(i.errs, err)
		}
		util.LogError("closing iterator", it.Close)
	}
	heap.Init(i.heap)

	// We can now clear the list of input iterators to merge, given they have all
	// been processed and the non empty ones have been pushed to the heap
	i.is = nil
}

func (i *sortExemplarIterator) Next() bool {
	i.init()

	if i.heap.Len() == 0 {
		return false
	}

	next := i.heap.Peek()
	i.curr.Exemplar = next.Exemplar()
	i.curr.labels = next.Labels()
	i.curr.streamHash = next.StreamHash()
	// if the top iterator is empty, we remove it.
	if !next.Next() {
		heap.Pop(i.heap)
		if err := next.Error(); err != nil {
			i.errs = append(i.errs, err)
		}
		util.LogError("closing iterator", next.Close)
		return true
	}
	if i.heap.Len() > 1 {
		heap.Fix(i.heap, 0)
	}
	return true
}

func (i *sortExemplarIterator) Exemplar() logproto.Exemplar {
	return i.curr.Exemplar
}

func (i *sortExemplarIterator) Labels() string {
	return i.curr.labels
}

func (i *sortExemplarIterator) StreamHash() uint64 {
	return i.curr.streamHash
}

func (i *sortExemplarIterator) Error() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return util.MultiError(i.errs)
	}
}

func (i *sortExemplarIterator) Close() error {
	for i.heap.Len() > 0 {
		if err := i.heap.Pop().(ExemplarIterator).Close(); err != nil {
			return err
		}
	}
	return nil
}

type exemplarQueryClientIterator struct {
	client QueryExemplarClient
	err    error
	curr   ExemplarIterator
}

// QueryExemplarClient is GRPC stream client with only method used by the ExemplarQueryClientIterator
type QueryExemplarClient interface {
	Recv() (*logproto.SampleQueryResponse, error)
	Context() context.Context
	CloseSend() error
}

// NewExemplarQueryClientIterator returns an iterator over a QueryClient.
func NewExemplarQueryClientIterator(client QuerySampleClient) ExemplarIterator {
	return &exemplarQueryClientIterator{
		client: client,
	}
}

func (i *exemplarQueryClientIterator) Next() bool {
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
		i.curr = NewExemplarQueryResponseIterator(batch)
	}
	return true
}

func (i *exemplarQueryClientIterator) Exemplar() logproto.Exemplar {
	return i.curr.Exemplar()
}

func (i *exemplarQueryClientIterator) Labels() string {
	return i.curr.Labels()
}

func (i *exemplarQueryClientIterator) StreamHash() uint64 {
	return i.curr.StreamHash()
}

func (i *exemplarQueryClientIterator) Error() error {
	return i.err
}

func (i *exemplarQueryClientIterator) Close() error {
	return i.client.CloseSend()
}

// NewExemplarQueryResponseIterator returns an iterator over a ExemplarIterator.
func NewExemplarQueryResponseIterator(resp *logproto.SampleQueryResponse) ExemplarIterator {
	return NewMultiExemplarSeriesIterator(resp.Series)
}

type seriesExemplarIterator struct {
	i      int
	series logproto.Series
}

type withCloseExemplarIterator struct {
	closeOnce sync.Once
	closeFn   func() error
	errs      []error
	ExemplarIterator
}

func (w *withCloseExemplarIterator) Close() error {
	w.closeOnce.Do(func() {
		if err := w.ExemplarIterator.Close(); err != nil {
			w.errs = append(w.errs, err)
		}
		if err := w.closeFn(); err != nil {
			w.errs = append(w.errs, err)
		}
	})
	if len(w.errs) == 0 {
		return nil
	}
	return util.MultiError(w.errs)
}

func ExemplarIteratorWithClose(it ExemplarIterator, closeFn func() error) ExemplarIterator {
	return &withCloseExemplarIterator{
		closeOnce:        sync.Once{},
		closeFn:          closeFn,
		ExemplarIterator: it,
	}
}

// NewMultiSeriesIterator returns an iterator over multiple logproto.Series
func NewMultiExemplarSeriesIterator(series []logproto.Series) ExemplarIterator {
	is := make([]ExemplarIterator, 0, len(series))
	for i := range series {
		is = append(is, NewSeriesExemplarIterator(series[i]))
	}
	return NewSortExemplarIterator(is)
}

// NewSeriesExemplarIterator iterates over exemplar in a series.
func NewSeriesExemplarIterator(series logproto.Series) ExemplarIterator {
	return &seriesExemplarIterator{
		i:      -1,
		series: series,
	}
}

func (i *seriesExemplarIterator) Next() bool {
	i.i++
	return i.i < len(i.series.Samples)
}

func (i *seriesExemplarIterator) Error() error {
	return nil
}

func (i *seriesExemplarIterator) Labels() string {
	return i.series.Labels
}

func (i *seriesExemplarIterator) StreamHash() uint64 {
	return i.series.StreamHash
}

func (i *seriesExemplarIterator) Exemplar() logproto.Exemplar {
	return i.series.Exemplars[i.i]
}

func (i *seriesExemplarIterator) Close() error {
	return nil
}

type nonOverlappingExemplarIterator struct {
	i         int
	iterators []ExemplarIterator
	curr      ExemplarIterator
}

// NewNonOverlappingExemplarIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingExemplarIterator(iterators []ExemplarIterator) ExemplarIterator {
	return &nonOverlappingExemplarIterator{
		iterators: iterators,
	}
}

func (i *nonOverlappingExemplarIterator) Next() bool {
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

func (i *nonOverlappingExemplarIterator) Exemplar() logproto.Exemplar {
	return i.curr.Exemplar()
}

func (i *nonOverlappingExemplarIterator) Labels() string {
	if i.curr == nil {
		return ""
	}
	return i.curr.Labels()
}

func (i *nonOverlappingExemplarIterator) StreamHash() uint64 {
	if i.curr == nil {
		return 0
	}
	return i.curr.StreamHash()
}

func (i *nonOverlappingExemplarIterator) Error() error {
	if i.curr == nil {
		return nil
	}
	return i.curr.Error()
}

func (i *nonOverlappingExemplarIterator) Close() error {
	if i.curr != nil {
		i.curr.Close()
	}
	for _, iter := range i.iterators {
		iter.Close()
	}
	i.iterators = nil
	return nil
}

type timeRangedExemplarIterator struct {
	ExemplarIterator
	mint, maxt int64
}

// NewTimeRangedExemplarIterator returns an iterator which filters entries by time range.
func NewTimeRangedExemplarIterator(it ExemplarIterator, mint, maxt int64) ExemplarIterator {
	return &timeRangedExemplarIterator{
		ExemplarIterator: it,
		mint:             mint,
		maxt:             maxt,
	}
}

func (i *timeRangedExemplarIterator) Next() bool {
	ok := i.ExemplarIterator.Next()
	if !ok {
		i.ExemplarIterator.Close()
		return ok
	}
	ts := i.ExemplarIterator.Exemplar().TimestampMs
	for ok && i.mint > ts {
		ok = i.ExemplarIterator.Next()
		if !ok {
			continue
		}
		ts = i.ExemplarIterator.Exemplar().TimestampMs
	}
	if ok {
		if ts == i.mint { // The mint is inclusive
			return true
		}
		if i.maxt < ts || i.maxt == ts { // The maxt is exclusive.
			ok = false
		}
	}
	if !ok {
		i.ExemplarIterator.Close()
	}
	return ok
}

// ReadBatch reads a set of entries off an iterator.
func ReadExemplarBatch(i ExemplarIterator, size uint32) (*logproto.SampleQueryResponse, uint32, error) {
	var (
		series      = map[uint64]map[string]*logproto.Series{}
		respSize    uint32
		seriesCount int
	)
	for ; respSize < size && i.Next(); respSize++ {
		labels, hash, exemplar := i.Labels(), i.StreamHash(), i.Exemplar()
		streams, ok := series[hash]
		if !ok {
			streams = map[string]*logproto.Series{}
			series[hash] = streams
		}
		s, ok := streams[labels]
		if !ok {
			seriesCount++
			s = &logproto.Series{
				Labels:     labels,
				StreamHash: hash,
			}
			streams[labels] = s
		}
		s.Exemplars = append(s.Exemplars, exemplar)
	}

	result := logproto.SampleQueryResponse{
		Series: make([]logproto.Series, 0, seriesCount),
	}
	for _, streams := range series {
		for _, s := range streams {
			result.Series = append(result.Series, *s)
		}
	}
	return &result, respSize, i.Error()
}
