// Package logical implements logical query plan operations and expressions
package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

type LiteralType int

// loosely matches the datasetmd.ValueType enum;
// consider using that directly in the future
const (
	LiteralTypeInvalid LiteralType = iota
	LiteralTypeString
	LiteralTypeInt64
)

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

type LiteralExpr struct {
	ty  LiteralType
	val any
}

func (l LiteralExpr) ValueString() string {
	switch l.ty {
	case LiteralTypeString:
		return l.val.(string)
	}
	return fmt.Sprintf("%v", l.val)
}

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

func (l LiteralExpr) ValueType() datasetmd.ValueType {
	switch l.ty {
	case LiteralTypeString:
		return datasetmd.VALUE_TYPE_STRING
	case LiteralTypeInt64:
		return datasetmd.VALUE_TYPE_INT64
	default:
		panic(fmt.Sprintf("unsupported literal type: %d", l.ty))
	}
}

// LitStr creates a string literal expression
func LitStr(v string) Expr {
	return NewLiteralExpr(LiteralExpr{
		ty:  LiteralTypeString,
		val: v,
	})
}

func LitI64(v int64) Expr {
	return NewLiteralExpr(LiteralExpr{
		ty:  LiteralTypeInt64,
		val: v,
	})
}
