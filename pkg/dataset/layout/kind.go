package layout

// Kind identifies the category of a [Layout]. Each concrete Layout
// implementation returns a fixed Kind value.
//
// The zero value is an invalid Kind.
type Kind int

const (
	KindInvalid Kind = iota

	KindArray // KindArray holds a single array.
)

var kindNames = [...]string{
	KindInvalid: "invalid",
	KindArray:   "array",
}

// String returns the string representation of k.
func (k Kind) String() string {
	if k < 0 || k >= Kind(len(kindNames)) {
		return "unknown"
	}
	return kindNames[k]
}
