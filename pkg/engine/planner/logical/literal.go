package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// LiteralType is an enum representing the type of a literal value.
// It allows for type-safe handling of different kinds of literal values.
type LiteralType int

// LiteralType constants define the supported types of literal values.
// These loosely match the datasetmd.ValueType enum;
// consider using that directly in the future.
const (
	LiteralTypeInvalid LiteralType = iota // Invalid or uninitialized literal
	LiteralTypeString                     // String literal
	LiteralTypeInt64                      // 64-bit integer literal
)

// String returns a human-readable representation of the literal type.
func (t LiteralType) String() string {
	switch t {
	case LiteralTypeString:
		return "string"
	case LiteralTypeInt64:
		return "int64"
	default:
		return "unknown"
	}
}

// LiteralExpr represents a literal value in the query plan.
// It can hold different types of values, such as strings or integers.
type LiteralExpr struct {
	ty  LiteralType // The type of the literal value
	val any         // The actual literal value
}

// ValueString returns a string representation of the literal value.
// This is used for display and debugging purposes.
func (l LiteralExpr) ValueString() string {
	switch l.ty {
	case LiteralTypeString:
		return l.val.(string)
	}
	return fmt.Sprintf("%v", l.val)
}

// ToField converts the literal to a column schema.
// The name of the column is derived from the string representation of the value.
func (l LiteralExpr) ToField(_ Plan) schema.ColumnSchema {
	switch l.ty {
	case LiteralTypeString:
		return schema.ColumnSchema{
			Name: l.val.(string),
			Type: l.ValueType(),
		}
	case LiteralTypeInt64:
		return schema.ColumnSchema{
			Name: fmt.Sprint(l.val.(int64)),
			Type: l.ValueType(),
		}
	default:
		panic(fmt.Sprintf("unsupported literal type: %d", l.ty))
	}
}

// ValueType returns the schema.ValueType corresponding to this literal type.
// This is used to determine the type of the column in the output schema.
func (l LiteralExpr) ValueType() schema.ValueType {
	switch l.ty {
	case LiteralTypeString:
		return schema.ValueTypeString
	case LiteralTypeInt64:
		return schema.ValueTypeInt64
	default:
		panic(fmt.Sprintf("unsupported literal type: %d", l.ty))
	}
}

// LitStr creates a string literal expression with the given value.
// Example: LitStr("hello") creates a string literal with value "hello".
func LitStr(v string) Expr {
	return NewLiteralExpr(LiteralExpr{
		ty:  LiteralTypeString,
		val: v,
	})
}

// LitI64 creates a 64-bit integer literal expression with the given value.
// Example: LitI64(42) creates an integer literal with value 42.
func LitI64(v int64) Expr {
	return NewLiteralExpr(LiteralExpr{
		ty:  LiteralTypeInt64,
		val: v,
	})
}
