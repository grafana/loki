package physical

import "github.com/grafana/loki/v3/pkg/engine/planner/logical"

// Context holds meta information needed to create a physical plan.
type Context struct{}

// Planner creates an executable physical plan from a logical plan.
type Planner struct {
	ctx Context
}

// NewPlanner creates a new planner instance with the given context.
func NewPlanner(ctx Context) *Planner {
	return &Planner{ctx: ctx}
}

// Convert transforms a logical plan into a physical plan.
func (p *Planner) Convert(_ *logical.Plan) (*Plan, error) {
	pp := &Plan{}
	// TODO: The logical plan implementation is not traversible yet.
	// Convert MAKETABLE
	// Convert SELECT
	// Convert SORT
	// Convert LIMIT
	return pp, nil
}

func (p *Planner) processMakeTable(_ *logical.MakeTable) error {
	return nil
}

func (p *Planner) processSelect(_ *logical.Filter) error {
	return nil
}

func (p *Planner) processSort(_ *logical.Sort) error {
	return nil
}

func (p *Planner) processLimit(_ *logical.Limit) error {
	return nil
}
