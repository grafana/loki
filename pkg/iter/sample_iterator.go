package iter

import (
	"container/heap"
	"context"
	"io"
	"sync"

	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util"
)

// PeekingSampleIterator is a sample iterator that can peek sample without moving the current sample.
type PeekingSampleIterator interface {
	SampleIterator
	Peek() (string, logproto.Sample, bool)
}

type peekingSampleIterator struct {
	iter SampleIterator

	cache *sampleWithLabels
	next  *sampleWithLabels
}

type sampleWithLabels struct {
	logproto.Sample
	labels     string
	streamHash uint64
}

func NewPeekingSampleIterator(iter SampleIterator) PeekingSampleIterator {
	// initialize the next entry so we can peek right from the start.
	var cache *sampleWithLabels
	next := &sampleWithLabels{}
	if iter.Next() {
		cache = &sampleWithLabels{
			Sample:     iter.At(),
			labels:     iter.Labels(),
			streamHash: iter.StreamHash(),
		}
		next.Sample = cache.Sample
		next.labels = cache.labels
	}
	return &peekingSampleIterator{
		iter:  iter,
		cache: cache,
		next:  next,
	}
}

func (it *peekingSampleIterator) Close() error {
	return it.iter.Close()
}

func (it *peekingSampleIterator) Labels() string {
	if it.next != nil {
		return it.next.labels
	}
	return ""
}

func (it *peekingSampleIterator) StreamHash() uint64 {
	if it.next != nil {
		return it.next.streamHash
	}
	return 0
}

func (it *peekingSampleIterator) Next() bool {
	if it.cache != nil {
		it.next.Sample = it.cache.Sample
		it.next.labels = it.cache.labels
		it.next.streamHash = it.cache.streamHash
		it.cacheNext()
		return true
	}
	return false
}

// cacheNext caches the next element if it exists.
func (it *peekingSampleIterator) cacheNext() {
	if it.iter.Next() {
		it.cache.Sample = it.iter.At()
		it.cache.labels = it.iter.Labels()
		it.cache.streamHash = it.iter.StreamHash()
		return
	}
	// nothing left removes the cached entry
	it.cache = nil
}

func (it *peekingSampleIterator) At() logproto.Sample {
	if it.next != nil {
		return it.next.Sample
	}
	return logproto.Sample{}
}

func (it *peekingSampleIterator) Peek() (string, logproto.Sample, bool) {
	if it.cache != nil {
		return it.cache.labels, it.cache.Sample, true
	}
	return "", logproto.Sample{}, false
}

func (it *peekingSampleIterator) Err() error {
	return it.iter.Err()
}

type SampleIteratorHeap struct {
	its []SampleIterator
}

func NewSampleIteratorHeap(its []SampleIterator) SampleIteratorHeap {
	return SampleIteratorHeap{
		its: its,
	}
}

func (h SampleIteratorHeap) Len() int             { return len(h.its) }
func (h SampleIteratorHeap) Swap(i, j int)        { h.its[i], h.its[j] = h.its[j], h.its[i] }
func (h SampleIteratorHeap) Peek() SampleIterator { return h.its[0] }
func (h *SampleIteratorHeap) Push(x interface{}) {
	h.its = append(h.its, x.(SampleIterator))
}

func (h *SampleIteratorHeap) Pop() interface{} {
	n := len(h.its)
	x := h.its[n-1]
	h.its = h.its[0 : n-1]
	return x
}

func (h SampleIteratorHeap) Less(i, j int) bool {
	s1, s2 := h.its[i].At(), h.its[j].At()
	if s1.Timestamp == s2.Timestamp {
		if h.its[i].StreamHash() == 0 {
			return h.its[i].Labels() < h.its[j].Labels()
		}
		return h.its[i].StreamHash() < h.its[j].StreamHash()
	}
	return s1.Timestamp < s2.Timestamp
}

// mergeSampleIterator iterates over a heap of iterators by merging samples.
type mergeSampleIterator struct {
	heap       *SampleIteratorHeap
	is         []SampleIterator
	prefetched bool
	stats      *stats.Context
	// pushBuffer contains the list of iterators that needs to be pushed to the heap
	// This is to avoid allocations.
	pushBuffer []SampleIterator

	// buffer of entries to be returned by Next()
	// We buffer entries with the same timestamp to correctly dedupe them.
	buffer []sampleWithLabels
	curr   sampleWithLabels
	errs   []error
}

// NewMergeSampleIterator returns a new iterator which uses a heap to merge together samples for multiple iterators and deduplicate if any.
// The iterator only order and merge entries across given `is` iterators, it does not merge entries within individual iterator.
// This means using this iterator with a single iterator will result in the same result as the input iterator.
// If you don't need to deduplicate sample, use `NewSortSampleIterator` instead.
func NewMergeSampleIterator(ctx context.Context, is []SampleIterator) SampleIterator {
	h := SampleIteratorHeap{
		its: make([]SampleIterator, 0, len(is)),
	}
	return &mergeSampleIterator{
		stats:      stats.FromContext(ctx),
		is:         is,
		heap:       &h,
		buffer:     make([]sampleWithLabels, 0, len(is)),
		pushBuffer: make([]SampleIterator, 0, len(is)),
	}
}

// prefetch iterates over all inner iterators to merge together, calls Next() on
// each of them to prefetch the first entry and pushes of them - who are not
// empty - to the heap
func (i *mergeSampleIterator) prefetch() {
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
func (i *mergeSampleIterator) requeue(ei SampleIterator, advanced bool) {
	if advanced || ei.Next() {
		heap.Push(i.heap, ei)
		return
	}

	if err := ei.Err(); err != nil {
		i.errs = append(i.errs, err)
	}
	util.LogError("closing iterator", ei.Close)
}

func (i *mergeSampleIterator) Next() bool {
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
		i.curr.Sample = i.heap.Peek().At()
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
		sample := next.At()
		if len(i.buffer) > 0 && (i.buffer[0].streamHash != next.StreamHash() || i.buffer[0].Timestamp != sample.Timestamp) {
			break
		}
		heap.Pop(i.heap)
		previous := i.buffer
		var dupe bool
		for _, t := range previous {
			if t.Sample.Hash == sample.Hash {
				i.stats.AddDuplicates(1)
				dupe = true
				break
			}
		}
		if !dupe {
			i.buffer = append(i.buffer, sampleWithLabels{
				Sample:     sample,
				labels:     next.Labels(),
				streamHash: next.StreamHash(),
			})
		}
	inner:
		for {
			if !next.Next() {
				continue Outer
			}
			sample := next.At()
			if next.StreamHash() != i.buffer[0].streamHash ||
				sample.Timestamp != i.buffer[0].Timestamp {
				break
			}
			for _, t := range previous {
				if t.Hash == sample.Hash {
					i.stats.AddDuplicates(1)
					continue inner
				}
			}
			i.buffer = append(i.buffer, sampleWithLabels{
				Sample:     sample,
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

func (i *mergeSampleIterator) nextFromBuffer() {
	i.curr.Sample = i.buffer[0].Sample
	i.curr.labels = i.buffer[0].labels
	i.curr.streamHash = i.buffer[0].streamHash
	if len(i.buffer) == 1 {
		i.buffer = i.buffer[:0]
		return
	}
	i.buffer = i.buffer[1:]
}

func (i *mergeSampleIterator) At() logproto.Sample {
	return i.curr.Sample
}

func (i *mergeSampleIterator) Labels() string {
	return i.curr.labels
}

func (i *mergeSampleIterator) StreamHash() uint64 {
	return i.curr.streamHash
}

func (i *mergeSampleIterator) Err() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return util.MultiError(i.errs)
	}
}

func (i *mergeSampleIterator) Close() error {
	for i.heap.Len() > 0 {
		if err := i.heap.Pop().(SampleIterator).Close(); err != nil {
			return err
		}
	}
	i.buffer = nil
	return nil
}

// sortSampleIterator iterates over a heap of iterators by sorting samples.
type sortSampleIterator struct {
	heap       *SampleIteratorHeap
	is         []SampleIterator
	prefetched bool

	curr sampleWithLabels
	errs []error
}

// NewSortSampleIterator returns a new SampleIterator that sorts samples by ascending timestamp the input iterators.
// The iterator only order sample across given `is` iterators, it does not sort samples within individual iterator.
// This means using this iterator with a single iterator will result in the same result as the input iterator.
// When timestamp is equal, the iterator sorts samples by their label alphabetically.
func NewSortSampleIterator(is []SampleIterator) SampleIterator {
	if len(is) == 0 {
		return NoopSampleIterator
	}
	if len(is) == 1 {
		return is[0]
	}
	h := SampleIteratorHeap{
		its: make([]SampleIterator, 0, len(is)),
	}
	return &sortSampleIterator{
		is:   is,
		heap: &h,
	}
}

// init initialize the underlying heap
func (i *sortSampleIterator) init() {
	if i.prefetched {
		return
	}

	i.prefetched = true
	for _, it := range i.is {
		if it.Next() {
			i.heap.Push(it)
			continue
		}

		if err := it.Err(); err != nil {
			i.errs = append(i.errs, err)
		}
		util.LogError("closing iterator", it.Close)
	}
	heap.Init(i.heap)

	// We can now clear the list of input iterators to merge, given they have all
	// been processed and the non empty ones have been pushed to the heap
	i.is = nil
}

func (i *sortSampleIterator) Next() bool {
	i.init()

	if i.heap.Len() == 0 {
		return false
	}

	next := i.heap.Peek()
	i.curr.Sample = next.At()
	i.curr.labels = next.Labels()
	i.curr.streamHash = next.StreamHash()
	// if the top iterator is empty, we remove it.
	if !next.Next() {
		heap.Pop(i.heap)
		if err := next.Err(); err != nil {
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

func (i *sortSampleIterator) At() logproto.Sample {
	return i.curr.Sample
}

func (i *sortSampleIterator) Labels() string {
	return i.curr.labels
}

func (i *sortSampleIterator) StreamHash() uint64 {
	return i.curr.streamHash
}

func (i *sortSampleIterator) Err() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return util.MultiError(i.errs)
	}
}

func (i *sortSampleIterator) Close() error {
	for i.heap.Len() > 0 {
		if err := i.heap.Pop().(SampleIterator).Close(); err != nil {
			return err
		}
	}
	return nil
}

type sampleQueryClientIterator struct {
	client QuerySampleClient
	err    error
	curr   SampleIterator
}

// QuerySampleClient is GRPC stream client with only method used by the SampleQueryClientIterator
type QuerySampleClient interface {
	Recv() (*logproto.SampleQueryResponse, error)
	Context() context.Context
	CloseSend() error
}

// NewSampleQueryClientIterator returns an iterator over a QueryClient.
func NewSampleQueryClientIterator(client QuerySampleClient) SampleIterator {
	return &sampleQueryClientIterator{
		client: client,
	}
}

func (i *sampleQueryClientIterator) Next() bool {
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
		_ = metadata.AddWarnings(ctx, batch.Warnings...)

		i.curr = NewSampleQueryResponseIterator(batch)
	}
	return true
}

func (i *sampleQueryClientIterator) At() logproto.Sample {
	return i.curr.At()
}

func (i *sampleQueryClientIterator) Labels() string {
	return i.curr.Labels()
}

func (i *sampleQueryClientIterator) StreamHash() uint64 {
	return i.curr.StreamHash()
}

func (i *sampleQueryClientIterator) Err() error {
	return i.err
}

func (i *sampleQueryClientIterator) Close() error {
	return i.client.CloseSend()
}

// NewSampleQueryResponseIterator returns an iterator over a SampleQueryResponse.
func NewSampleQueryResponseIterator(resp *logproto.SampleQueryResponse) SampleIterator {
	return NewMultiSeriesIterator(resp.Series)
}

type seriesIterator struct {
	i      int
	series logproto.Series
}

type withCloseSampleIterator struct {
	closeOnce sync.Once
	closeFn   func() error
	errs      []error
	SampleIterator
}

func (w *withCloseSampleIterator) Close() error {
	w.closeOnce.Do(func() {
		if err := w.SampleIterator.Close(); err != nil {
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

func SampleIteratorWithClose(it SampleIterator, closeFn func() error) SampleIterator {
	return &withCloseSampleIterator{
		closeOnce:      sync.Once{},
		closeFn:        closeFn,
		SampleIterator: it,
	}
}

// NewMultiSeriesIterator returns an iterator over multiple logproto.Series
func NewMultiSeriesIterator(series []logproto.Series) SampleIterator {
	is := make([]SampleIterator, 0, len(series))
	for i := range series {
		is = append(is, NewSeriesIterator(series[i]))
	}
	return NewSortSampleIterator(is)
}

// NewSeriesIterator iterates over sample in a series.
func NewSeriesIterator(series logproto.Series) SampleIterator {
	return &seriesIterator{
		i:      -1,
		series: series,
	}
}

func (i *seriesIterator) Next() bool {
	i.i++
	return i.i < len(i.series.Samples)
}

func (i *seriesIterator) Err() error {
	return nil
}

func (i *seriesIterator) Labels() string {
	return i.series.Labels
}

func (i *seriesIterator) StreamHash() uint64 {
	return i.series.StreamHash
}

func (i *seriesIterator) At() logproto.Sample {
	return i.series.Samples[i.i]
}

func (i *seriesIterator) Close() error {
	return nil
}

type nonOverlappingSampleIterator struct {
	i         int
	iterators []SampleIterator
	curr      SampleIterator
}

// NewNonOverlappingSampleIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingSampleIterator(iterators []SampleIterator) SampleIterator {
	return &nonOverlappingSampleIterator{
		iterators: iterators,
	}
}

func (i *nonOverlappingSampleIterator) Next() bool {
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

func (i *nonOverlappingSampleIterator) At() logproto.Sample {
	return i.curr.At()
}

func (i *nonOverlappingSampleIterator) Labels() string {
	if i.curr == nil {
		return ""
	}
	return i.curr.Labels()
}

func (i *nonOverlappingSampleIterator) StreamHash() uint64 {
	if i.curr == nil {
		return 0
	}
	return i.curr.StreamHash()
}

func (i *nonOverlappingSampleIterator) Err() error {
	if i.curr == nil {
		return nil
	}
	return i.curr.Err()
}

func (i *nonOverlappingSampleIterator) Close() error {
	if i.curr != nil {
		i.curr.Close()
	}
	for _, iter := range i.iterators {
		iter.Close()
	}
	i.iterators = nil
	return nil
}

type timeRangedSampleIterator struct {
	SampleIterator
	mint, maxt int64
}

// NewTimeRangedSampleIterator returns an iterator which filters entries by time range.
func NewTimeRangedSampleIterator(it SampleIterator, mint, maxt int64) SampleIterator {
	return &timeRangedSampleIterator{
		SampleIterator: it,
		mint:           mint,
		maxt:           maxt,
	}
}

func (i *timeRangedSampleIterator) Next() bool {
	ok := i.SampleIterator.Next()
	if !ok {
		i.SampleIterator.Close()
		return ok
	}
	ts := i.SampleIterator.At().Timestamp
	for ok && i.mint > ts {
		ok = i.SampleIterator.Next()
		if !ok {
			continue
		}
		ts = i.SampleIterator.At().Timestamp
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
		i.SampleIterator.Close()
	}
	return ok
}

// ReadSampleBatch reads a set of entries off an iterator.
func ReadSampleBatch(i SampleIterator, size uint32) (*logproto.SampleQueryResponse, uint32, error) {
	var (
		series      = map[uint64]map[string]*logproto.Series{}
		respSize    uint32
		seriesCount int
	)
	for ; respSize < size && i.Next(); respSize++ {
		labels, hash, sample := i.Labels(), i.StreamHash(), i.At()
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
		s.Samples = append(s.Samples, sample)
	}

	result := logproto.SampleQueryResponse{
		Series: make([]logproto.Series, 0, seriesCount),
	}
	for _, streams := range series {
		for _, s := range streams {
			result.Series = append(result.Series, *s)
		}
	}
	return &result, respSize, i.Err()
}
