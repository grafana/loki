package physical

// Projection represents a column selection operation in the physical plan.
// It contains a list of columns (column expressions) that are later
// evaluated against the input columns to remove unnecessary colums from the
// intermediate result.
type Projection struct {
	id string

	Columns []ColumnExpression
}

func (p *Projection) ID() string {
	return p.id
}

func (*Projection) Type() NodeType {
	return NodeTypeProjection
}

func (p *Projection) Accept(v Visitor) error {
	return v.VisitProjection(p)
}
