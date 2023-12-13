package chunk

import (
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
)

type Predicate struct {
	Matchers []*labels.Matcher
	Filters  []syntax.LineFilter // Should also be a slice of pointers??
}

func NewPredicate(m []*labels.Matcher, f []syntax.LineFilter) Predicate {
	return Predicate{Matchers: m, Filters: f}
}
