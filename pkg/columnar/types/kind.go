package types

// Kind identifies the category of a [Type]. Each concrete type implementation
// returns a fixed Kind value.
type Kind int

const (
	KindInvalid Kind = iota // KindInvalid is the zero value, indicating an uninitialized or unknown type.
	KindNull                // KindNull represents a type with no value.
	KindUint32              // KindUint32 represents an unsigned 32-bit integer.
	KindUint64              // KindUint64 represents an unsigned 64-bit integer.
	KindInt32               // KindInt32 represents a signed 32-bit integer.
	KindInt64               // KindInt64 represents a signed 64-bit integer.
	KindBool                // KindBool represents a boolean value.
	KindUTF8                // KindUTF8 represents a UTF-8 encoded string.
	KindStruct              // KindStruct represents a composite type with named fields.
	KindList                // KindList represents a variable-length sequence of a single element type.
)

var kindNames = [...]string{
	KindInvalid: "invalid",
	KindNull:    "null",
	KindUint32:  "uint32",
	KindUint64:  "uint64",
	KindInt32:   "int32",
	KindInt64:   "int64",
	KindBool:    "bool",
	KindUTF8:    "utf8",
	KindStruct:  "struct",
	KindList:    "list",
}

// String returns the string representation of k.
func (k Kind) String() string {
	if k < 0 || k >= Kind(len(kindNames)) {
		return "unknown"
	}
	return kindNames[k]
}
