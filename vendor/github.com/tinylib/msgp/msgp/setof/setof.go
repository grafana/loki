// Package setof allows serializing sets map[T]struct{} as arrays.
//
// Both msgpack and JSON encoding are supported. Nil maps are preserved
// as nil/null. A deterministic, sorted version is available for each type,
// with slightly lower performance.

package setof

// ensure 'sz' extra bytes in 'b' can be appended without reallocating
func ensure(b []byte, sz int) []byte {
	l := len(b)
	c := cap(b)
	if c-l < sz {
		o := make([]byte, l, l+sz)
		copy(o, b)
		return o
	}
	return b
}
