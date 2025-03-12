package physical

// Limit represents a limiting operation in the physical plan that applies
// offset and limit to the result set. The offset specifies how many rows to
// skip before starting to return results, while limit specifies the maximum
// number of rows to return.
type Limit struct {
	id string

	Offset uint32
	Limit  uint32
}

func (l *Limit) ID() string {
	return l.id
}

func (*Limit) Type() NodeType {
	return NodeTypeLimit
}

func (l *Limit) Accept(v Visitor) error {
	return v.VisitLimit(l)
}
