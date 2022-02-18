package storage

import (
	"github.com/prometheus/prometheus/model/labels"
)

// NewMatcherChunkFilterer returns a chunk filter that filters when matchers
// match. Useful for filtering on matchers other than what was queried for
func NewMatcherChunkFilterer(m []*labels.Matcher) ChunkFilterer {
	return &matcherChunkFilterer{
		matchers: m,
	}
}

type matcherChunkFilterer struct {
	matchers []*labels.Matcher
}

func (f *matcherChunkFilterer) ShouldFilter(labels labels.Labels) bool {
	for _, m := range f.matchers {
		for _, l := range labels {
			if m.Name == l.Name && m.Matches(l.Value) {
				return true
			}
		}
	}
	return false
}
