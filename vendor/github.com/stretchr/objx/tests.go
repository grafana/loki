package objx

// Has reports whether the specified selector exists, even when the
// value at the selector is nil.
//
// If m is nil, Has will always return false.
func (m Map) Has(selector string) bool {
	if m == nil {
		return false
	}
	_, ok := access(m, selector, nil, actionGet)
	return ok
}

// IsNil gets whether the data is nil or not.
func (v *Value) IsNil() bool {
	return v == nil || v.data == nil
}
