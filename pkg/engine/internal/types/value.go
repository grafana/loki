package types

// ValueType represents the type of a value, which can either be a literal value, or a column value.
type ValueType uint32

const (
	ValueTypeInvalid ValueType = iota // zero-value is an invalid type

	ValueTypeNull      // NULL value.
	ValueTypeBool      // Boolean value
	ValueTypeInt       // Signed 64bit integer value
	ValueTypeTimestamp // Unsigned 64bit integer value (nanosecond timestamp)
	ValueTypeStr       // String value
	ValueTypeBytes     // Byte-slice value
)
