package iter

import (
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type Iterator interface {
	iter.CloseIterator[logproto.PatternSample]

	Pattern() string
	Level() string
}

func NewSlice(pattern, lvl string, s []logproto.PatternSample) *PatternIter {
	return &PatternIter{
		CloseIterator: iter.WithClose(iter.NewSliceIter(s), nil),
		pattern:       pattern,
		level:         lvl,
	}
}

func NewEmpty(pattern string) *PatternIter {
	return &PatternIter{
		CloseIterator: iter.WithClose(iter.NewEmptyIter[logproto.PatternSample](), nil),
		pattern:       pattern,
	}
}

type PatternIter struct {
	iter.CloseIterator[logproto.PatternSample]
	pattern string
	level   string
}

func (s *PatternIter) Pattern() string {
	return s.pattern
}

func (s *PatternIter) Level() string {
	return s.level
}

type nonOverlappingIterator struct {
	iterators []Iterator
	curr      Iterator
	pattern   string
	level     string
}

// NewNonOverlappingIterator gives a chained iterator over a list of iterators.
func NewNonOverlappingIterator(pattern, lvl string, iterators []Iterator) Iterator {
	return &nonOverlappingIterator{
		iterators: iterators,
		pattern:   pattern,
		level:     lvl,
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

func (i *nonOverlappingIterator) Level() string {
	return i.level
}

func (i *nonOverlappingIterator) Err() error {
	if i.curr != nil {
		return i.curr.Err()
	}
	return nil
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
