package types

const (
	typeInvalid = "invalid"
)

// ValueType represents the type of a value, which can either be a literal value, or a column value.
type ValueType uint32

const (
	ValueTypeInvalid ValueType = iota // zero-value is an invalid type

	ValueTypeNull      // NULL value.
	ValueTypeBool      // Boolean value
	ValueTypeFloat     // 64bit floating point value
	ValueTypeInt       // Signed 64bit integer value
	ValueTypeTimestamp // Unsigned 64bit integer value (nanosecond timestamp)
	ValueTypeStr       // String value
	ValueTypeByteArray // Byte-slice value
	// ValueTypeBytes
	// ValueTypeDate
	// ValueTypeDuration
)

// String returns the string representation of the LiteralKind.
func (t ValueType) String() string {
	switch t {
	case ValueTypeInvalid:
		return typeInvalid
	case ValueTypeNull:
		return "null"
	case ValueTypeBool:
		return "bool"
	case ValueTypeFloat:
		return "float"
	case ValueTypeInt:
		return "int"
	case ValueTypeTimestamp:
		return "timestamp"
	case ValueTypeStr:
		return "string"
	case ValueTypeByteArray:
		return "[]byte"
	default:
		return typeInvalid
	}
}
