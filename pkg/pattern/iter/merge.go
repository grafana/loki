package iter

import (
	"math"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

type mergeIterator struct {
	tree        *loser.Tree[patternSample, Iterator]
	current     patternSample
	initialized bool
	done        bool
}

type patternSample struct {
	pattern string
	sample  logproto.PatternSample
}

var max = patternSample{
	pattern: "",
	sample:  logproto.PatternSample{Timestamp: math.MaxInt64},
}

func NewMerge(iters ...Iterator) Iterator {
	tree := loser.New(iters, max, func(s Iterator) patternSample {
		return patternSample{
			pattern: s.Pattern(),
			sample:  s.At(),
		}
	}, func(e1, e2 patternSample) bool {
		if e1.sample.Timestamp == e2.sample.Timestamp {
			return e1.pattern < e2.pattern
		}
		return e1.sample.Timestamp < e2.sample.Timestamp
	}, func(s Iterator) {
		s.Close()
	})
	return &mergeIterator{
		tree: tree,
	}
}

func (m *mergeIterator) Next() bool {
	if m.done {
		return false
	}

	if !m.initialized {
		m.initialized = true
		if !m.tree.Next() {
			m.done = true
			return false
		}
	}

	m.current.pattern = m.tree.Winner().Pattern()
	m.current.sample = m.tree.Winner().At()

	for m.tree.Next() {
		if m.current.sample.Timestamp != m.tree.Winner().At().Timestamp || m.current.pattern != m.tree.Winner().Pattern() {
			return true
		}
		m.current.sample.Value += m.tree.Winner().At().Value
	}

	m.done = true
	return true
}

func (m *mergeIterator) Pattern() string {
	return m.current.pattern
}

func (m *mergeIterator) At() logproto.PatternSample {
	return m.current.sample
}

func (m *mergeIterator) Err() error {
	return nil
}

func (m *mergeIterator) Close() error {
	m.tree.Close()
	return nil
}
