package iter

import (
	"github.com/grafana/loki/v3/pkg/logproto"
)

var Empty Iterator = &emptyIterator{}

// TODO(chaudum): inline v2.Iteratpr[logproto.PatternSample]
type Iterator interface {
	Next() bool

	Pattern() string
	At() logproto.PatternSample

	Error() error
	Close() error
}

func NewSlice(pattern string, s []logproto.PatternSample) Iterator {
	// TODO(chaudum): replace with v2.NewSliceIter()
	return &sliceIterator{
		values:  s,
		pattern: pattern,
		i:       -1,
	}
}

type sliceIterator struct {
	i       int
	pattern string
	values  []logproto.PatternSample
}

func (s *sliceIterator) Next() bool {
	s.i++
	return s.i < len(s.values)
}

func (s *sliceIterator) Pattern() string {
	return s.pattern
}

func (s *sliceIterator) At() logproto.PatternSample {
	return s.values[s.i]
}

func (s *sliceIterator) Error() error {
	return nil
}

func (s *sliceIterator) Close() error {
	return nil
}

type emptyIterator struct {
	pattern string
}

func (e *emptyIterator) Next() bool {
	return false
}

func (e *emptyIterator) Pattern() string {
	return e.pattern
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
}

// NewNonOverlappingIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingIterator(pattern string, iterators []Iterator) Iterator {
	return &nonOverlappingIterator{
		iterators: iterators,
		pattern:   pattern,
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
