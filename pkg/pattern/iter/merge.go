package iter

import "github.com/grafana/loki/pkg/logproto"

type mergeIterator struct{}

func NewMerge(iters ...Iterator) Iterator {
	return &mergeIterator{}
}

func (m *mergeIterator) Next() bool {
	return false
}

func (m *mergeIterator) Pattern() string {
	return ""
}

func (m *mergeIterator) At() logproto.PatternSample {
	return logproto.PatternSample{}
}

func (m *mergeIterator) Error() error {
	return nil
}

func (m *mergeIterator) Close() error {
	return nil
}
