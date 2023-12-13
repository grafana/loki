package chunk

import (
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/model/labels"
)

type Predicate struct {
	Matchers []*labels.Matcher
	Filters  []*logproto.LineFilterExpression
}

func NewPredicate(m []*labels.Matcher, f []*logproto.LineFilterExpression) Predicate {
	return Predicate{Matchers: m, Filters: f}
}
