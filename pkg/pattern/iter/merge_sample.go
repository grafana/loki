package iter

import (
	"container/heap"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

type sampleWithLabels struct {
	logproto.Sample
	labels     string
	streamHash uint64
}

// sumMergeSampleIterator iterates over a heap of iterators by merging samples.
type sumMergeSampleIterator struct {
	heap       *iter.SampleIteratorHeap
	is         []iter.SampleIterator
	prefetched bool
	// pushBuffer contains the list of iterators that needs to be pushed to the heap
	// This is to avoid allocations.
	pushBuffer []iter.SampleIterator

	// buffer of entries to be returned by Next()
	// We buffer entries with the same timestamp to correctly dedupe them.
	buffer []sampleWithLabels
	curr   sampleWithLabels
	errs   []error
}

// NewSumMergeSampleIterator is a lot like iter.NewMergeSampleIterator, with 2 notable
// difference. For one, it does not care about the hash value of a Sample as it is
// assuming these samples are not coming from log lines, nor are in need of de-duplication.
// Second, when there are two identical samples from the same series and time, it sums the
// values.
// NewSumMergeSampleIterator returns an iterator which uses a heap to merge together samples for multiple iterators.
// The iterator only order and merge entries across given `is` iterators, it does not merge entries within individual iterator.
// This means using this iterator with a single iterator will result in the same result as the input iterator.
func NewSumMergeSampleIterator(is []iter.SampleIterator) iter.SampleIterator {
	h := iter.NewSampleIteratorHeap(make([]iter.SampleIterator, 0, len(is)))
	return &sumMergeSampleIterator{
		is:         is,
		heap:       &h,
		buffer:     make([]sampleWithLabels, 0, len(is)),
		pushBuffer: make([]iter.SampleIterator, 0, len(is)),
	}
}

// prefetch iterates over all inner iterators to merge together, calls Next() on
// each of them to prefetch the first entry and pushes of them - who are not
// empty - to the heap
func (i *sumMergeSampleIterator) prefetch() {
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
func (i *sumMergeSampleIterator) requeue(ei iter.SampleIterator, advanced bool) {
	if advanced || ei.Next() {
		heap.Push(i.heap, ei)
		return
	}

	if err := ei.Err(); err != nil {
		i.errs = append(i.errs, err)
	}
	util.LogError("closing iterator", ei.Close)
}

func (i *sumMergeSampleIterator) Next() bool {
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
	// heap with the same timestamp, and add them to the buffer to sum their values.
	for i.heap.Len() > 0 {
		next := i.heap.Peek()
		sample := next.At()

		if len(i.buffer) > 0 && (i.buffer[0].streamHash != next.StreamHash() ||
			i.buffer[0].Timestamp != sample.Timestamp) {
			break
		}
		heap.Pop(i.heap)
		i.buffer = append(i.buffer, sampleWithLabels{
			Sample:     sample,
			labels:     next.Labels(),
			streamHash: next.StreamHash(),
		})

		if next.Next() {
			i.pushBuffer = append(i.pushBuffer, next)
		}
	}

	for _, ei := range i.pushBuffer {
		heap.Push(i.heap, ei)
	}
	i.pushBuffer = i.pushBuffer[:0]

	i.nextFromBuffer()

	return true
}

func (i *sumMergeSampleIterator) nextFromBuffer() {
	if len(i.buffer) == 1 {
		i.curr.Sample = i.buffer[0].Sample
		i.curr.labels = i.buffer[0].labels
		i.curr.streamHash = i.buffer[0].streamHash
		i.buffer = i.buffer[:0]
		return
	}

	mergedSample := i.buffer[0]

	numSamples := 1
	for _, sample := range i.buffer[1:] {
		if mergedSample.labels != sample.labels ||
			mergedSample.streamHash != sample.streamHash ||
			mergedSample.Timestamp != sample.Timestamp {
			i.curr = mergedSample
			i.buffer = i.buffer[numSamples:]
			return
		}

		mergedSample.Sample.Value += sample.Value
		numSamples++
	}

	i.curr = mergedSample
	i.buffer = i.buffer[numSamples:]
}

func (i *sumMergeSampleIterator) At() logproto.Sample {
	return i.curr.Sample
}

func (i *sumMergeSampleIterator) Labels() string {
	return i.curr.labels
}

func (i *sumMergeSampleIterator) StreamHash() uint64 {
	return i.curr.streamHash
}

func (i *sumMergeSampleIterator) Err() error {
	switch len(i.errs) {
	case 0:
		return nil
	case 1:
		return i.errs[0]
	default:
		return util.MultiError(i.errs)
	}
}

func (i *sumMergeSampleIterator) Close() error {
	for i.heap.Len() > 0 {
		if err := i.heap.Pop().(iter.SampleIterator).Close(); err != nil {
			return err
		}
	}
	i.buffer = nil
	return nil
}
