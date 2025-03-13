package physical

// Filter represents a filtering operation in the physical plan.
// It contains a list of predicates (conditional expressions) that are later
// evaluated against the input columns and produce a result that only contains
// rows that match the given conditions. The list of expressions are chained
// with a logical AND.
type Filter struct {
	id string

	// Predicates is a list of filter expressions that are used to discard not
	// matching rows during execution.
	Predicates []Expression
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (f *Filter) ID() string {
	return f.id
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*Filter) Type() NodeType {
	return NodeTypeFilter
}

// Accept implements the [Node] interface.
// Dispatches itself to the provided [Visitor] v
func (f *Filter) Accept(v Visitor) error {
	return v.VisitFilter(f)
}
