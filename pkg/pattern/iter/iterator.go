package iter

import (
	"github.com/grafana/loki/pkg/logproto"
)

type Iterator interface {
	Next() bool

	Pattern() string
	At() logproto.PatternSample

	Error() error
	Close() error
}

func NewSlice(pattern string, s []logproto.PatternSample) Iterator {
	return &sliceIterator{
		values:  s,
		pattern: pattern,
	}
}

type sliceIterator struct {
	i       int
	pattern string
	values  []logproto.PatternSample
}

func (s *sliceIterator) Next() bool {
	if s.i >= len(s.values) {
		return false
	}
	s.i++
	return true
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
