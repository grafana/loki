package iter

import (
	"math"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/loser"
	"github.com/prometheus/prometheus/model/labels"
)

type mergeIterator struct {
	tree        *loser.Tree[patternSample, Iterator]
	current     patternSample
	initialized bool
	done        bool
}

type patternSample struct {
	pattern string
	labels  labels.Labels
	sample  logproto.PatternSample
}

var max = patternSample{
	pattern: "",
	labels:  labels.Labels{},
	sample:  logproto.PatternSample{Timestamp: math.MaxInt64},
}

func NewMerge(iters ...Iterator) Iterator {
	// TODO: I need to call next here
	tree := loser.New(iters, max, func(s Iterator) patternSample {
		return patternSample{
			pattern: s.Pattern(),
			labels:  s.Labels(),
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
	m.current.labels = m.tree.Winner().Labels()
	m.current.sample = m.tree.Winner().At()

	for m.tree.Next() {
		if m.current.sample.Timestamp != m.tree.Winner().At().Timestamp ||
			m.current.pattern != m.tree.Winner().Pattern() ||
			m.current.labels.String() != m.tree.Winner().Labels().String() {
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

func (m *mergeIterator) Labels() labels.Labels {
	return m.current.labels
}

func (m *mergeIterator) At() logproto.PatternSample {
	return m.current.sample
}

func (m *mergeIterator) Error() error {
	return nil
}

func (m *mergeIterator) Close() error {
	m.tree.Close()
	return nil
}
