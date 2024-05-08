package iter

import (
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/prometheus/model/labels"
)

var Empty Iterator = &emptyIterator{}

type Iterator interface {
	Next() bool

	Pattern() string
	Labels() labels.Labels
	At() logproto.PatternSample

	Error() error
	Close() error
}

type SampleIterator interface {
	Iterator
	Sample() logproto.PatternSample
}

type PeekingIterator interface {
	SampleIterator
	Peek() (string, logproto.PatternSample, bool)
}

func NewPatternSlice(pattern string, s []logproto.PatternSample) Iterator {
	return &patternSliceIterator{
		values:  s,
		pattern: pattern,
		labels:  labels.EmptyLabels(),
		i:       -1,
	}
}

func NewLabelsSlice(lbls labels.Labels, s []logproto.PatternSample) Iterator {
	return &patternSliceIterator{
		values: s,
		labels: lbls,
		i:      -1,
	}
}

type patternSliceIterator struct {
	i       int
	pattern string
	labels  labels.Labels
	values  []logproto.PatternSample
}

func (s *patternSliceIterator) Next() bool {
	s.i++
	return s.i < len(s.values)
}

func (s *patternSliceIterator) Pattern() string {
	return s.pattern
}

func (s *patternSliceIterator) Labels() labels.Labels {
	return s.labels
}

func (s *patternSliceIterator) At() logproto.PatternSample {
	return s.values[s.i]
}

func (s *patternSliceIterator) Error() error {
	return nil
}

func (s *patternSliceIterator) Close() error {
	return nil
}

type emptyIterator struct {
	pattern string
	labels  labels.Labels
}

func (e *emptyIterator) Next() bool {
	return false
}

func (e *emptyIterator) Pattern() string {
	return e.pattern
}

func (e *emptyIterator) Labels() labels.Labels {
	return e.labels
}

func (e *emptyIterator) At() logproto.PatternSample {
	return logproto.PatternSample{}
}

func (e *emptyIterator) Error() error {
	return nil
}

func (e *emptyIterator) Close() error {
	return nil
}

type nonOverlappingIterator struct {
	iterators []Iterator
	curr      Iterator
	pattern   string
	labels    labels.Labels
}

// NewNonOverlappingPatternIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingPatternIterator(pattern string, iterators []Iterator) Iterator {
	return &nonOverlappingIterator{
		iterators: iterators,
		pattern:   pattern,
	}
}

// NewNonOverlappingLabelsIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingLabelsIterator(labels labels.Labels, iterators []Iterator) Iterator {
	return &nonOverlappingIterator{
		iterators: iterators,
		labels:    labels,
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

func (i *nonOverlappingIterator) At() logproto.PatternSample {
	return i.curr.At()
}

func (i *nonOverlappingIterator) Pattern() string {
	return i.pattern
}

func (i *nonOverlappingIterator) Labels() labels.Labels {
	return i.labels
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

type peekingIterator struct {
	iter Iterator

	cache  *sampleWithLabels
	next   *sampleWithLabels
	labels labels.Labels
}

type sampleWithLabels struct {
	logproto.PatternSample
	labels labels.Labels
}

func (s *sampleWithLabels) Sample() logproto.Sample {
	return logproto.Sample{
		Timestamp: s.PatternSample.Timestamp.UnixNano(), // logproto.Sample expects nano seconds
		Value:     float64(s.PatternSample.Value),
		Hash:      0,
	}
}

func NewPeekingSampleIterator(iter Iterator) iter.PeekingSampleIterator {
	// initialize the next entry so we can peek right from the start.
	var cache *sampleWithLabels
	next := &sampleWithLabels{}
	if iter.Next() {
		cache = &sampleWithLabels{
			PatternSample: iter.At(),
			labels:        iter.Labels(),
		}
		next.PatternSample = cache.PatternSample
		next.labels = cache.labels
	}

	return &peekingIterator{
		iter:   iter,
		cache:  cache,
		next:   next,
		labels: iter.Labels(),
	}
}

func (it *peekingIterator) Close() error {
	return it.iter.Close()
}

func (it *peekingIterator) Labels() string {
	return it.labels.String()
}

func (it *peekingIterator) Next() bool {
	if it.cache != nil {
		it.next.PatternSample = it.cache.PatternSample
		it.next.labels = it.cache.labels
		it.cacheNext()
		return true
	}
	return false
}

func (it *peekingIterator) Sample() logproto.Sample {
	if it.next != nil {
		return logproto.Sample{
			Timestamp: it.next.PatternSample.Timestamp.UnixNano(), // expecting nano seconds
			Value:     float64(it.next.PatternSample.Value),
			Hash:      0,
		}
	}
	return logproto.Sample{}
}

func (it *peekingIterator) At() logproto.PatternSample {
	if it.next != nil {
		return it.next.PatternSample
	}
	return logproto.PatternSample{}
}

// cacheNext caches the next element if it exists.
func (it *peekingIterator) cacheNext() {
	if it.iter.Next() {
		it.cache.PatternSample = it.iter.At()
		it.cache.labels = it.iter.Labels()
		return
	}
	// nothing left, remove the cached entry
	it.cache = nil
}

func (it *peekingIterator) Pattern() logproto.PatternSample {
	if it.next != nil {
		return it.next.PatternSample
	}
	return logproto.PatternSample{}
}

func (it *peekingIterator) Peek() (string, logproto.Sample, bool) {
	if it.cache != nil {
		return it.cache.labels.String(), it.cache.Sample(), true
	}
	return "", logproto.Sample{}, false
}

func (it *peekingIterator) Error() error {
	return it.iter.Error()
}

func (it *peekingIterator) StreamHash() uint64 {
	return 0
}
