package chunk

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logql/syntax"
)

type Predicate struct {
	Matchers []*labels.Matcher
	Filters  []syntax.LineFilter
}

func NewPredicate(m []*labels.Matcher, f []syntax.LineFilter) Predicate {
	return Predicate{Matchers: m, Filters: f}
}
