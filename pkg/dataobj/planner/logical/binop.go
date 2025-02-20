package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

type BinaryOpMath string

const (
	BinaryOpAdd      BinaryOpMath = "+"
	BinaryOpSubtract BinaryOpMath = "-"
	BinaryOpMultiply BinaryOpMath = "*"
	BinaryOpDivide   BinaryOpMath = "/"
	BinaryOpModulo   BinaryOpMath = "%"
)

var (
	Add      = newBinaryMathExprConstructor(BinaryOpAdd)
	Subtract = newBinaryMathExprConstructor(BinaryOpSubtract)
	Multiply = newBinaryMathExprConstructor(BinaryOpMultiply)
	Divide   = newBinaryMathExprConstructor(BinaryOpDivide)
	Modulo   = newBinaryMathExprConstructor(BinaryOpModulo)
)

type BinaryOpCmp string

const (
	BinaryOpEq  BinaryOpCmp = "=="
	BinaryOpNeq BinaryOpCmp = "!="
	BinaryOpLt  BinaryOpCmp = "<"
	BinaryOpLte BinaryOpCmp = "<="
	BinaryOpGt  BinaryOpCmp = ">"
	BinaryOpGte BinaryOpCmp = ">="
)

var (
	Eq  = newBinaryCompareExprConstructor(BinaryOpEq)
	Neq = newBinaryCompareExprConstructor(BinaryOpNeq)
	Lt  = newBinaryCompareExprConstructor(BinaryOpLt)
	Lte = newBinaryCompareExprConstructor(BinaryOpLte)
	Gt  = newBinaryCompareExprConstructor(BinaryOpGt)
	Gte = newBinaryCompareExprConstructor(BinaryOpGte)
)

type BinaryOpSet string

const (
	BinaryOpAnd BinaryOpSet = "and"
	BinaryOpOr  BinaryOpSet = "or"
	BinaryOpNot BinaryOpSet = "not" // "unless"
	BinaryOpXor BinaryOpSet = "xor"
)

var (
	And = newBinarySetExprConstructor(BinaryOpAnd)
	Or  = newBinarySetExprConstructor(BinaryOpOr)
	Not = newBinarySetExprConstructor(BinaryOpNot)
	Xor = newBinarySetExprConstructor(BinaryOpXor)
)

type BinaryMathExpr struct {
	name string
	op   BinaryOpMath
	l    Expr
	r    Expr
}

func newBinaryMathExprConstructor(op BinaryOpMath) func(name string, l Expr, r Expr) BinaryMathExpr {
	return func(name string, l Expr, r Expr) BinaryMathExpr {
		return BinaryMathExpr{
			name: name,
			op:   op,
			l:    l,
			r:    r,
		}
	}
}

func (b BinaryMathExpr) ToField(p Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: b.name,
		Type: b.l.ToField(p).Type,
	}
}

type BooleanCmpExpr struct {
	name string
	op   BinaryOpCmp
	l    Expr
	r    Expr
}

func newBinaryCompareExprConstructor(op BinaryOpCmp) func(name string, l Expr, r Expr) BooleanCmpExpr {
	return func(name string, l Expr, r Expr) BooleanCmpExpr {
		return BooleanCmpExpr{
			name: name,
			op:   op,
			l:    l,
			r:    r,
		}
	}
}

func (b BooleanCmpExpr) ToField(_ Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: b.name,
		// TODO: bool type
		Type: datasetmd.VALUE_TYPE_UINT64,
	}
}

type BooleanSetExpr struct {
	name string
	op   BinaryOpSet
	l    Expr
	r    Expr
}

func newBinarySetExprConstructor(op BinaryOpSet) func(name string, l Expr, r Expr) BooleanSetExpr {
	return func(name string, l Expr, r Expr) BooleanSetExpr {
		return BooleanSetExpr{
			name: name,
			op:   op,
			l:    l,
			r:    r,
		}
	}
}

func (b BooleanSetExpr) ToField(p Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: b.name,
		Type: b.l.ToField(p).Type,
	}
}
