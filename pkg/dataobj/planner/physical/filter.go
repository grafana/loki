package physical

// Filter represents a filtering operation in the physical plan.
// It contains a list of predicates (conditional expressions) that are later
// evaluated against the input columns and produce a result that only contains
// rows that match the given conditions. The list of expressions are chained
// with a logical AND.
type Filter struct {
	id string

	Predicates []Expression // filter predicate
}

func (f *Filter) ID() string {
	return f.id
}

func (*Filter) Type() NodeType {
	return NodeTypeFilter
}

func (f *Filter) Accept(v Visitor) error {
	return v.VisitFilter(f)
}
