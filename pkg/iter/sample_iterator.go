package iter

import (
	"container/heap"
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/stats"
)

// SampleIterator iterates over samples in time-order.
type SampleIterator interface {
	Next() bool
	// todo(ctovena) we should add `Seek(t int64) bool`
	// This way we can skip when ranging over samples.
	Sample() logproto.Sample
	Labels() string
	Error() error
	Close() error
}

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
	labels string
}

func NewPeekingSampleIterator(iter SampleIterator) PeekingSampleIterator {
	// initialize the next entry so we can peek right from the start.
	var cache *sampleWithLabels
	next := &sampleWithLabels{}
	if iter.Next() {
		cache = &sampleWithLabels{
			Sample: iter.Sample(),
			labels: iter.Labels(),
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

func (it *peekingSampleIterator) Next() bool {
	if it.cache != nil {
		it.next.Sample = it.cache.Sample
		it.next.labels = it.cache.labels
		it.cacheNext()
		return true
	}
	return false
}

// cacheNext caches the next element if it exists.
func (it *peekingSampleIterator) cacheNext() {
	if it.iter.Next() {
		it.cache.Sample = it.iter.Sample()
		it.cache.labels = it.iter.Labels()
		return
	}
	// nothing left removes the cached entry
	it.cache = nil
}

func (it *peekingSampleIterator) Sample() logproto.Sample {
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

func (it *peekingSampleIterator) Error() error {
	return it.iter.Error()
}

type sampleIteratorHeap []SampleIterator

func (h sampleIteratorHeap) Len() int             { return len(h) }
func (h sampleIteratorHeap) Swap(i, j int)        { h[i], h[j] = h[j], h[i] }
func (h sampleIteratorHeap) Peek() SampleIterator { return h[0] }
func (h *sampleIteratorHeap) Push(x interface{}) {
	*h = append(*h, x.(SampleIterator))
}

func (h *sampleIteratorHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

func (h sampleIteratorHeap) Less(i, j int) bool {
	s1, s2 := h[i].Sample(), h[j].Sample()
	switch {
	case s1.Timestamp < s2.Timestamp:
		return true
	case s1.Timestamp > s2.Timestamp:
		return false
	default:
		return h[i].Labels() < h[j].Labels()
	}
}

// heapSampleIterator iterates over a heap of iterators.
type heapSampleIterator struct {
	heap       *sampleIteratorHeap
	is         []SampleIterator
	prefetched bool
	stats      *stats.ChunkData

	tuples     []sampletuple
	curr       logproto.Sample
	currLabels string
	errs       []error
}

// NewHeapSampleIterator returns a new iterator which uses a heap to merge together
// entries for multiple iterators.
func NewHeapSampleIterator(ctx context.Context, is []SampleIterator) SampleIterator {

	return &heapSampleIterator{
		stats:  stats.GetChunkData(ctx),
		is:     is,
		heap:   &sampleIteratorHeap{},
		tuples: make([]sampletuple, 0, len(is)),
	}
}

// prefetch iterates over all inner iterators to merge together, calls Next() on
// each of them to prefetch the first entry and pushes of them - who are not
// empty - to the heap
func (i *heapSampleIterator) prefetch() {
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
func (i *heapSampleIterator) requeue(ei SampleIterator, advanced bool) {
	if advanced || ei.Next() {
		heap.Push(i.heap, ei)
		return
	}

	if err := ei.Error(); err != nil {
		i.errs = append(i.errs, err)
	}
	helpers.LogError("closing iterator", ei.Close)
}

type sampletuple struct {
	logproto.Sample
	SampleIterator
}

func (i *heapSampleIterator) Next() bool {
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
		sample := next.Sample()
		if len(i.tuples) > 0 && (i.tuples[0].Labels() != next.Labels() || i.tuples[0].Timestamp != sample.Timestamp) {
			break
		}

		heap.Pop(i.heap)
		i.tuples = append(i.tuples, sampletuple{
			Sample:         sample,
			SampleIterator: next,
		})
	}

	i.curr = i.tuples[0].Sample
	i.currLabels = i.tuples[0].Labels()
	t := i.tuples[0]
	if len(i.tuples) == 1 {
		i.requeue(i.tuples[0].SampleIterator, false)
		i.tuples = i.tuples[:0]
		return true
	}
	// Requeue the iterators, advancing them if they were consumed.
	for j := range i.tuples {
		if i.tuples[j].Hash != i.curr.Hash {
			i.requeue(i.tuples[j].SampleIterator, true)
			continue
		}
		// we count as duplicates only if the tuple is not the one (t) used to fill the current entry
		if i.tuples[j] != t {
			i.stats.TotalDuplicates++
		}
		i.requeue(i.tuples[j].SampleIterator, false)
	}
	i.tuples = i.tuples[:0]
	return true
}

func (i *heapSampleIterator) Sample() logproto.Sample {
	return i.curr
}

func (i *heapSampleIterator) Labels() string {
	return i.currLabels
}

func (i *heapSampleIterator) Error() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return fmt.Errorf("Multiple errors: %+v", i.errs)
	}
}

func (i *heapSampleIterator) Close() error {
	for i.heap.Len() > 0 {
		if err := i.heap.Pop().(SampleIterator).Close(); err != nil {
			return err
		}
	}
	i.tuples = nil
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

// NewQueryClientIterator returns an iterator over a QueryClient.
func NewSampleQueryClientIterator(client QuerySampleClient) SampleIterator {
	return &sampleQueryClientIterator{
		client: client,
	}
}

func (i *sampleQueryClientIterator) Next() bool {
	for i.curr == nil || !i.curr.Next() {
		batch, err := i.client.Recv()
		if err == io.EOF {
			return false
		} else if err != nil {
			i.err = err
			return false
		}

		i.curr = NewSampleQueryResponseIterator(i.client.Context(), batch)
	}

	return true
}

func (i *sampleQueryClientIterator) Sample() logproto.Sample {
	return i.curr.Sample()
}

func (i *sampleQueryClientIterator) Labels() string {
	return i.curr.Labels()
}

func (i *sampleQueryClientIterator) Error() error {
	return i.err
}

func (i *sampleQueryClientIterator) Close() error {
	return i.client.CloseSend()
}

// NewSampleQueryResponseIterator returns an iterator over a SampleQueryResponse.
func NewSampleQueryResponseIterator(ctx context.Context, resp *logproto.SampleQueryResponse) SampleIterator {
	return NewMultiSeriesIterator(ctx, resp.Series)
}

type seriesIterator struct {
	i       int
	samples []logproto.Sample
	labels  string
}

// NewMultiSeriesIterator returns an iterator over multiple logproto.Series
func NewMultiSeriesIterator(ctx context.Context, series []logproto.Series) SampleIterator {
	is := make([]SampleIterator, 0, len(series))
	for i := range series {
		is = append(is, NewSeriesIterator(series[i]))
	}
	return NewHeapSampleIterator(ctx, is)
}

// NewSeriesIterator iterates over sample in a series.
func NewSeriesIterator(series logproto.Series) SampleIterator {
	return &seriesIterator{
		i:       -1,
		samples: series.Samples,
		labels:  series.Labels,
	}
}

func (i *seriesIterator) Next() bool {
	i.i++
	return i.i < len(i.samples)
}

func (i *seriesIterator) Error() error {
	return nil
}

func (i *seriesIterator) Labels() string {
	return i.labels
}

func (i *seriesIterator) Sample() logproto.Sample {
	return i.samples[i.i]
}

func (i *seriesIterator) Close() error {
	return nil
}

type nonOverlappingSampleIterator struct {
	labels    string
	i         int
	iterators []SampleIterator
	curr      SampleIterator
}

// NewNonOverlappingSampleIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingSampleIterator(iterators []SampleIterator, labels string) SampleIterator {
	return &nonOverlappingSampleIterator{
		labels:    labels,
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

func (i *nonOverlappingSampleIterator) Sample() logproto.Sample {
	return i.curr.Sample()
}

func (i *nonOverlappingSampleIterator) Labels() string {
	if i.labels != "" {
		return i.labels
	}

	return i.curr.Labels()
}

func (i *nonOverlappingSampleIterator) Error() error {
	if i.curr != nil {
		return i.curr.Error()
	}
	return nil
}

func (i *nonOverlappingSampleIterator) Close() error {
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
	ts := i.SampleIterator.Sample().Timestamp
	for ok && i.mint > ts {
		ok = i.SampleIterator.Next()
		if !ok {
			continue
		}
		ts = i.SampleIterator.Sample().Timestamp
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

// ReadBatch reads a set of entries off an iterator.
func ReadSampleBatch(i SampleIterator, size uint32) (*logproto.SampleQueryResponse, uint32, error) {
	series := map[string]*logproto.Series{}
	respSize := uint32(0)
	for ; respSize < size && i.Next(); respSize++ {
		labels, sample := i.Labels(), i.Sample()
		s, ok := series[labels]
		if !ok {
			s = &logproto.Series{
				Labels: labels,
			}
			series[labels] = s
		}
		s.Samples = append(s.Samples, sample)
	}

	result := logproto.SampleQueryResponse{
		Series: make([]logproto.Series, 0, len(series)),
	}
	for _, s := range series {
		result.Series = append(result.Series, *s)
	}
	return &result, respSize, i.Error()
}
