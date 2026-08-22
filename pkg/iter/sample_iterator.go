package iter

import (
	"container/heap"
	"context"
	"io"
	"sync"
	"time"

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

	// orderByStream orders samples stream-first instead of the default global timestamp order.
	// The zero value keeps the timestamp ordering.
	orderByStream bool
}

func NewSampleIteratorHeap(its []SampleIterator, orderByStream bool) SampleIteratorHeap {
	return SampleIteratorHeap{
		its:           its,
		orderByStream: orderByStream,
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
	if h.orderByStream {
		// Stream-first: order by streamHash, then labels, then timestamp. Comparing labels before
		// the timestamp keeps each stream's samples contiguous even when two distinct streams
		// collide on the same streamHash; ordering by timestamp first would interleave them.
		h1, h2 := h.its[i].StreamHash(), h.its[j].StreamHash()
		if h1 != h2 {
			return h1 < h2
		}
		if l1, l2 := h.its[i].Labels(), h.its[j].Labels(); l1 != l2 {
			return l1 < l2
		}
		return s1.Timestamp < s2.Timestamp
	}
	// Timestamp-first (default): order by timestamp, then streamHash (or labels when no hash).
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

// NewTimestampFirstMergeSampleIterator returns a new iterator which uses a heap to merge together samples for multiple iterators and deduplicate if any.
// The iterator only order and merge entries across given `is` iterators, it does not merge entries within individual iterator.
// This means using this iterator with a single iterator will result in the same result as the input iterator.
// Samples are returned in global timestamp order. If you don't need to deduplicate sample, use `NewSortSampleIterator` instead.
func NewTimestampFirstMergeSampleIterator(ctx context.Context, is []SampleIterator) SampleIterator {
	return newMergeSampleIterator(ctx, is, false)
}

// newMergeSampleIterator is the shared merge+dedup core for the timestamp-first and
// stream-first sample iterators. The two differ only in the heap ordering (orderByStream);
// the buffering and per-(streamHash, timestamp) deduplication are identical.
func newMergeSampleIterator(ctx context.Context, is []SampleIterator, orderByStream bool) SampleIterator {
	h := NewSampleIteratorHeap(make([]SampleIterator, 0, len(is)), orderByStream)
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

	i.closeIterator(ei)
}

// closeIterator closes a drained input iterator and records its pending error.
//
// This function runs while Next drains an iterator. Close reaches only the iterators left
// on the heap, so without this a fully drained merge would leak every source.
func (i *mergeSampleIterator) closeIterator(ei SampleIterator) {
	if err := ei.Err(); err != nil {
		i.errs = append(i.errs, err)
	}
	util.LogError("closing iterator", ei.Close)
}

// sameGroup reports whether it's current sample belongs to the buffer's current dedup group.
// It must be called with a non-empty buffer.
func (i *mergeSampleIterator) sameGroup(it SampleIterator, ts int64) bool {
	if i.buffer[0].streamHash != it.StreamHash() || i.buffer[0].Timestamp != ts {
		return false
	}

	// In stream-first mode equal labels are also required so two distinct streams that collide on
	// the same streamHash stay separate.
	return !i.heap.orderByStream || i.buffer[0].labels == it.Labels()
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
			i.closeIterator(i.heap.Pop().(SampleIterator))
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
		if len(i.buffer) > 0 && !i.sameGroup(next, sample.Timestamp) {
			break
		}
		heap.Pop(i.heap)
		previous := i.buffer
		var dupe bool
		if sample.Hash != 0 {
			for _, t := range previous {
				if t.Hash == sample.Hash {
					i.stats.AddDuplicates(1)
					dupe = true
					break
				}
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
				i.closeIterator(next)
				continue Outer
			}
			sample := next.At()
			if !i.sameGroup(next, sample.Timestamp) {
				break
			}
			if sample.Hash != 0 {
				for _, t := range previous {
					if t.Hash == sample.Hash {
						i.stats.AddDuplicates(1)
						continue inner
					}
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

// Close closes every input iterator and returns any error the merge collected.
func (i *mergeSampleIterator) Close() error {
	// Closes the sources not yet moved onto the heap (Close before the first Next).
	for _, it := range i.is {
		i.closeIterator(it)
	}
	i.is = nil

	// Close the sources still on the heap, and closes all of them even when one fails,
	// so no source leaks.
	for i.heap.Len() > 0 {
		i.closeIterator(i.heap.Pop().(SampleIterator))
	}
	i.buffer = nil
	return i.Err()
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
	h := NewSampleIteratorHeap(make([]SampleIterator, 0, len(is)), false)
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

		i.closeIterator(it)
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
		i.closeIterator(next)
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

// closeIterator closes a drained input iterator and records its pending error.
func (i *sortSampleIterator) closeIterator(it SampleIterator) {
	if err := it.Err(); err != nil {
		i.errs = append(i.errs, err)
	}
	util.LogError("closing iterator", it.Close)
}

// Close closes every input iterator and returns any error the sort collected.
func (i *sortSampleIterator) Close() error {
	// Closes the sources not yet moved onto the heap.
	for _, it := range i.is {
		i.closeIterator(it)
	}
	i.is = nil

	// Close the sources still on the heap, and closes all of them even when one fails,
	// so no source leaks.
	for i.heap.Len() > 0 {
		i.closeIterator(i.heap.Pop().(SampleIterator))
	}
	return i.Err()
}

type sampleQueryClientIterator struct {
	client QuerySampleClient
	err    error
	curr   SampleIterator

	// orderedByStream assembles each received batch stream-first (preserving the Series order the
	// sender emitted) instead of re-sorting globally by timestamp.
	orderedByStream bool
}

// QuerySampleClient is GRPC stream client with only method used by the SampleQueryClientIterator
type QuerySampleClient interface {
	Recv() (*logproto.SampleQueryResponse, error)
	Context() context.Context
	CloseSend() error
}

// NewTimestampFirstSampleQueryClientIterator returns a timestamp-first iterator over a QueryClient.
func NewTimestampFirstSampleQueryClientIterator(client QuerySampleClient) SampleIterator {
	return &sampleQueryClientIterator{client: client}
}

// NewStreamFirstSampleQueryClientIterator returns a stream-first iterator over a QueryClient whose
// batches were encoded with ReadSampleBatchOrdered. It preserves the sender's ordering across batches
// so the result can feed the stream-first cross-source merge directly.
func NewStreamFirstSampleQueryClientIterator(client QuerySampleClient) SampleIterator {
	return &sampleQueryClientIterator{client: client, orderedByStream: true}
}

func (i *sampleQueryClientIterator) Next() bool {
	ctx := i.client.Context()
	for i.curr == nil || !i.curr.Next() {
		start := time.Now()
		batch, err := i.client.Recv()
		stats.FromContext(ctx).AddIngesterRecvWait(time.Since(start))
		if err == io.EOF {
			return false
		} else if err != nil {
			i.err = err
			return false
		}
		stats.JoinIngesters(ctx, batch.Stats)
		_ = metadata.AddWarnings(ctx, batch.Warnings...)

		if i.orderedByStream {
			i.curr = NewStreamFirstSampleQueryResponseIterator(batch)
		} else {
			i.curr = NewTimestampFirstSampleQueryResponseIterator(batch)
		}
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

// NewTimestampFirstSampleQueryResponseIterator returns an iterator over a SampleQueryResponse.
func NewTimestampFirstSampleQueryResponseIterator(resp *logproto.SampleQueryResponse) SampleIterator {
	return NewMultiSeriesIterator(resp.Series)
}

// NewStreamFirstSampleQueryResponseIterator returns a stream-first iterator over a
// SampleQueryResponse whose Series are already in streamHash ASC order.
// It concatenates the series without re-sorting, preserving that order.
func NewStreamFirstSampleQueryResponseIterator(resp *logproto.SampleQueryResponse) SampleIterator {
	return NewMultiSeriesIteratorOrdered(resp.Series)
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

// NewMultiSeriesIteratorOrdered returns an iterator over the given series, emitting the series —
// and the samples within each — in their exact input order, performing no sorting or re-ordering.
// Supplying them in the desired order is the caller's responsibility.
func NewMultiSeriesIteratorOrdered(series []logproto.Series) SampleIterator {
	is := make([]SampleIterator, 0, len(series))
	for i := range series {
		is = append(is, NewSeriesIterator(series[i]))
	}

	// A plain concatenation is correct even though different series' timestamp ranges may overlap:
	// NewNonOverlappingSampleIterator does not require disjoint timestamps, it just plays each series
	// to completion in turn.
	return NewNonOverlappingSampleIterator(is)
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
	err       error
}

// NewNonOverlappingSampleIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingSampleIterator(iterators []SampleIterator) SampleIterator {
	return &nonOverlappingSampleIterator{
		iterators: iterators,
	}
}

func (i *nonOverlappingSampleIterator) Next() bool {
	for i.curr == nil || !i.curr.Next() {
		if i.curr != nil {
			// The current iterator stopped. If it failed, surface the error and stop:
			// any error fails the query, so advancing would hide the failure as normal
			// exhaustion and read remaining streams whose data the query discards.
			if err := i.curr.Err(); err != nil {
				i.err = err
				return false
			}
			// A close error here is a cleanup failure, not a read failure. Reporting it
			// as a read failure would be inaccurate.
			i.curr.Close()
		}

		if len(i.iterators) == 0 {
			return false
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
	return i.err
}

func (i *nonOverlappingSampleIterator) Close() error {
	// Close every iterator and keep all errors: Add ignores nil, so a clean close
	// still returns nil.
	var errs util.MultiError
	if i.curr != nil {
		// If curr already failed, some implementations return that same read error
		// from Close too. It was already surfaced through Err, so closing here is
		// cleanup only: do not report it a second time as a close error.
		if err := i.curr.Close(); err != nil && i.err == nil {
			errs.Add(err)
		}
	}
	for _, iter := range i.iterators {
		errs.Add(iter.Close())
	}
	i.iterators = nil

	return errs.Err()
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
		i.Close()
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
		i.Close()
	}
	return ok
}

// ReadSampleBatchOrdered reads a set of samples off a stream-first iterator, preserving its
// ordering in the emitted Series.
//
// Unlike ReadSampleBatch, which groups samples into a map and emits Series in random order,
// this appends Series in the order streams first appear, so consecutive batches stay
// stream-ordered and a stream split across a batch boundary remains contiguous.
func ReadSampleBatchOrdered(i SampleIterator, size uint32) (*logproto.SampleQueryResponse, uint32, error) {
	var (
		series   []logproto.Series
		respSize uint32
		currIdx  = -1
	)

	for ; respSize < size && i.Next(); respSize++ {
		labels, hash, sample := i.Labels(), i.StreamHash(), i.At()
		if currIdx < 0 || series[currIdx].StreamHash != hash || series[currIdx].Labels != labels {
			series = append(series, logproto.Series{Labels: labels, StreamHash: hash})
			currIdx = len(series) - 1
		}
		series[currIdx].Samples = append(series[currIdx].Samples, sample)
	}

	return &logproto.SampleQueryResponse{Series: series}, respSize, i.Err()
}

// ReadSampleBatch reads up to size samples off an iterator, grouping them by stream into one
// Series each. The Series are emitted in map (random) order.
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
