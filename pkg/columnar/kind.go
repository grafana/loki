package columnar

import "strconv"

// A Kind represents the specific type of an Array. The zero Kind is a null
// value.
type Kind uint8

const (
	KindNull   Kind = iota // KindNull represents a null value.
	KindBool               // KindBool represents a boolean value.
	KindInt64              // KindInt64 represents a 64-bit integer value.
	KindUint64             // KindUint64 represents a 64-bit unsigned integer value.
	KindUTF8               // KindUTF8 represents a UTF-8 encoded string value.
)

var kindNames = [...]string{
	KindNull:   "null",
	KindBool:   "bool",
	KindInt64:  "int64",
	KindUint64: "uint64",
	KindUTF8:   "utf8",
}

// String returns the string representation of k.
func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}
