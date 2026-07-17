package layout

// Kind identifies the category of a [Layout]. Each concrete Layout
// implementation returns a fixed Kind value.
//
// The zero value is an invalid Kind.
type Kind int

const (
	KindInvalid Kind = iota

	KindArray   // KindArray holds a single array.
	KindChunked // KindChunked holds a set of layout chunks.
	KindStruct  // KindStruct is a set of named layouts.
)

var kindNames = [...]string{
	KindInvalid: "invalid",
	KindArray:   "array",
	KindChunked: "chunked",
	KindStruct:  "struct",
}

// String returns the string representation of k.
func (k Kind) String() string {
	if k < 0 || k >= Kind(len(kindNames)) {
		return "unknown"
	}
	return kindNames[k]
}
