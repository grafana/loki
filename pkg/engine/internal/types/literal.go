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
func (e *Literal) String() string {
	switch v := e.Value.(type) {
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
func (e *Literal) ValueType() ValueType {
	switch e.Value.(type) {
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
