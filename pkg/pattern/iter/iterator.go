package iter

import "github.com/prometheus/common/model"

type Iterator interface {
	Next() bool

	Pattern() string
	At() model.SamplePair

	Error() error
	Close() error
}

func NewSlice(pattern string, s []model.SamplePair) Iterator {
	return &sliceIterator{
		values:  s,
		pattern: pattern,
	}
}

type sliceIterator struct {
	i       int
	pattern string
	values  []model.SamplePair
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

func (s *sliceIterator) At() model.SamplePair {
	return s.values[s.i]
}

func (s *sliceIterator) Error() error {
	return nil
}

func (s *sliceIterator) Close() error {
	return nil
}
