package types

import (
	"fmt"
	"strconv"
)

// Literal is holds a value of [ValueType].
type Literal struct {
	Value any
}

// String returns the string representation of the literal value.
func (l *Literal) String() string {
	switch v := l.Value.(type) {
	case nil:
		return "NULL"
	case bool:
		return strconv.FormatBool(v)
	case string:
		return fmt.Sprintf(`"%s"`, v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case []byte:
		return fmt.Sprintf("%v", v)
	default:
		return "invalid"
	}
}

// ValueType returns the kind of value represented by the literal.
func (l *Literal) ValueType() ValueType {
	switch l.Value.(type) {
	case nil:
		return ValueTypeNull
	case bool:
		return ValueTypeBool
	case string:
		return ValueTypeStr
	case float64:
		return ValueTypeFloat
	case int64:
		return ValueTypeInt
	case uint64:
		return ValueTypeTimestamp
	case []byte:
		return ValueTypeByteArray
	default:
		return ValueTypeInvalid
	}
}

// IsNull returns true if lit is a [ValueTypeNull] value.
func (l Literal) IsNull() bool {
	return l.ValueType() == ValueTypeNull
}

// Str returns the value as a string. It panics if lit is not a [ValueTypeString].
func (l Literal) Str() string {
	if expect, actual := ValueTypeStr, l.ValueType(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return l.Value.(string)
}

// Int64 returns the value as an int64. It panics if lit is not a [ValueTypeFloat].
func (l Literal) Float() float64 {
	if expect, actual := ValueTypeFloat, l.ValueType(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return l.Value.(float64)
}

// Int returns the value as an int64. It panics if lit is not a [ValueTypeInt].
func (l Literal) Int() int64 {
	if expect, actual := ValueTypeInt, l.ValueType(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return l.Value.(int64)
}

// Timestamp returns the value as a uint64. It panics if lit is not a [ValueTypeTimestamp].
func (l Literal) Timestamp() uint64 {
	if expect, actual := ValueTypeTimestamp, l.ValueType(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return l.Value.(uint64)
}

// ByteArray returns the value as a byte slice. It panics if lit is not a [ValueTypeByteArray].
func (l Literal) ByteArray() []byte {
	if expect, actual := ValueTypeByteArray, l.ValueType(); expect != actual {
		panic(fmt.Sprintf("literal type is %s, not %s", actual, expect))
	}
	return l.Value.([]byte)
}

// Convenience function for creating a NULL literal.
func NullLiteral() Literal {
	return Literal{Value: nil}
}

// Convenience function for creating a bool literal.
func BoolLiteral(v bool) Literal {
	return Literal{Value: v}
}

// Convenience function for creating a string literal.
func StringLiteral(v string) Literal {
	return Literal{Value: v}
}

// Convenience function for creating a float literal.
func FloatLiteral(v float64) Literal {
	return Literal{Value: v}
}

// Convenience function for creating a timestamp literal.
func TimestampLiteral(v uint64) Literal {
	return Literal{Value: v}
}

// Convenience function for creating an integer literal.
func IntLiteral(v int64) Literal {
	return Literal{Value: v}
}

// Convenience function for creating a byte array literal.
func ByteArrayLiteral(v []byte) Literal {
	return Literal{Value: v}
}
