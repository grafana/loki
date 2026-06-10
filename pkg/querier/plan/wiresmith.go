package plan

// Wiresmith customtype adapter methods; see pkg/push/wiresmith.go for the
// pattern. Delegate to the existing gogo-style implementations.

// SizeWiresmith implements the wiresmith customtype contract. A zero plan
// (nil AST) reports 0, which omits the field on marshal — matching the
// previous gogo pointer-field behavior where a nil *QueryPlan was skipped
// (Size/EncodeJSON panic on a nil AST, so they must not be reached here).
func (t *QueryPlan) SizeWiresmith() int {
	if t.AST == nil {
		return 0
	}
	return t.Size()
}

// MarshalWiresmith implements the wiresmith customtype contract.
func (t *QueryPlan) MarshalWiresmith(buf []byte) (int, error) {
	if t.AST == nil {
		return 0, nil
	}
	return t.MarshalTo(buf)
}

// UnmarshalWiresmith implements the wiresmith customtype contract.
func (t *QueryPlan) UnmarshalWiresmith(buf []byte) error { return t.Unmarshal(buf) }

// EqualWiresmith implements the wiresmith customtype contract.
func (t *QueryPlan) EqualWiresmith(other any) bool {
	o, ok := coerceQueryPlan(other)
	if !ok {
		return false
	}
	return t.Equal(o)
}

// CompareWiresmith implements the wiresmith customtype contract. Plans have
// no natural order; equality decides 0, otherwise the string forms order.
func (t *QueryPlan) CompareWiresmith(other any) int {
	o, ok := coerceQueryPlan(other)
	if !ok {
		return -1
	}
	if t.Equal(o) {
		return 0
	}
	if t.String() < o.String() {
		return -1
	}
	return 1
}

func coerceQueryPlan(other any) (QueryPlan, bool) {
	switch o := other.(type) {
	case QueryPlan:
		return o, true
	case *QueryPlan:
		if o == nil {
			return QueryPlan{}, false
		}
		return *o, true
	default:
		return QueryPlan{}, false
	}
}
