package array

// EncodingKind identifies the category of an [Encoding]. Each concrete Encoding
// implementation returns a fixed EncodingKind value.
//
// The zero value is an invalid encoding kind.
type EncodingKind int

const (
	EncodingKindInvalid EncodingKind = iota // EncodingKindInvalid is an invalid encoding.

	EncodingKindBool      // EncodingKindBool is a bit-packed encoding for boolean types.
	EncodingKindPlain     // EncodingKindPlain is plain encoding for fixed-width types (int32, etc).
	EncodingKindBinary    // EncodingKindBinary encodes variable-length binary data (like UTF8).
	EncodingKindBitpacked // EncodingKindBitpacked is a bitpacked encoding for unsigned integer types.
)

var kindNames = [...]string{
	EncodingKindInvalid:   "invalid",
	EncodingKindBool:      "bool",
	EncodingKindPlain:     "plain",
	EncodingKindBinary:    "binary",
	EncodingKindBitpacked: "bitpacked",
}

// String returns the string representation of k.
func (k EncodingKind) String() string {
	if k < 0 || k >= EncodingKind(len(kindNames)) {
		return "unknown"
	}
	return kindNames[k]
}
