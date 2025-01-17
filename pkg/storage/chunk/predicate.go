package chunk

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/querier/plan"
)

// TODO(owen-d): rename. This is not a predicate and is confusing.
type Predicate struct {
	Matchers []*labels.Matcher
	plan     *plan.QueryPlan
}

func NewPredicate(m []*labels.Matcher, p *plan.QueryPlan) Predicate {
	return Predicate{Matchers: m, plan: p}
}

func (p Predicate) Plan() plan.QueryPlan {
	if p.plan != nil {
		return *p.plan
	}
	return plan.QueryPlan{}
}
