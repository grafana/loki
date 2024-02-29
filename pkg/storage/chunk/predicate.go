package chunk

import (
	"github.com/grafana/loki/pkg/querier/plan"
	"github.com/prometheus/prometheus/model/labels"
)

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
