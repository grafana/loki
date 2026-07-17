package expr

import "fmt"

// Walk calls fn on e and then recursively on every descendant expression in
// pre-order. Nil expressions are silently skipped.
func Walk(e Expression, fn func(Expression)) {
	if e == nil {
		return
	}
	fn(e)
	ForEach(e, func(child Expression) {
		Walk(child, fn)
	})
}

// Map returns a shallow copy of e with each direct child expression replaced
// by the result of fn. Leaf nodes (Column, Constant, Identity, Regexp,
// ValueSet) have no children and are returned unchanged.
func Map(e Expression, fn func(Expression) Expression) Expression {
	switch e := e.(type) {
	case *Column, *Constant, *Identity, *Regexp, *ValueSet:
		return e
	case *Binary:
		return &Binary{Left: fn(e.Left), Op: e.Op, Right: fn(e.Right)}
	case *Unary:
		return &Unary{Op: e.Op, Value: fn(e.Value)}
	case *Extract:
		return &Extract{Name: e.Name, Value: fn(e.Value)}
	case *Include:
		return &Include{Names: e.Names, Value: fn(e.Value)}
	case *Exclude:
		return &Exclude{Names: e.Names, Value: fn(e.Value)}
	case *MakeStruct:
		values := make([]Expression, len(e.Values))
		for i, v := range e.Values {
			values[i] = fn(v)
		}
		return &MakeStruct{Names: e.Names, Values: values}
	default:
		panic(fmt.Sprintf("Map: unexpected expression type %T", e))
	}
}

// ForEach calls fn on each direct child expression of e.
// Leaf nodes have no children, so fn is never called for them.
func ForEach(e Expression, fn func(Expression)) {
	switch e := e.(type) {
	case nil, *Column, *Constant, *Identity, *Regexp, *ValueSet:
		// Leaf nodes have no children.
	case *Binary:
		fn(e.Left)
		fn(e.Right)
	case *Unary:
		fn(e.Value)
	case *Extract:
		fn(e.Value)
	case *Include:
		fn(e.Value)
	case *Exclude:
		fn(e.Value)
	case *MakeStruct:
		for _, v := range e.Values {
			fn(v)
		}
	default:
		panic(fmt.Sprintf("ForEach: unexpected expression type %T", e))
	}
}
