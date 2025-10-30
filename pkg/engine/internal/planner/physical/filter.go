package physical

import "fmt"

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
	if f.id == "" {
		return fmt.Sprintf("%p", f)
	}
	return f.id
}

// Clone returns a deep copy of the node (minus its ID).
func (f *Filter) Clone() Node {
	return &Filter{
		Predicates: cloneExpressions(f.Predicates),
	}
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*Filter) Type() NodeType {
	return NodeTypeFilter
}
