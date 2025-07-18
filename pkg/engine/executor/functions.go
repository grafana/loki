package executor

import (
	"regexp"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/errors"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	unaryFunctions  UnaryFunctionRegistry  = &unaryFuncReg{}
	binaryFunctions BinaryFunctionRegistry = &binaryFuncReg{}
)

func init() {
	// Functions for [types.BinaryOpEq]
	binaryFunctions.register(types.BinaryOpEq, arrow.FixedWidthTypes.Boolean, &boolCompareFunction{cmp: func(a, b bool) bool { return a == b }})
	binaryFunctions.register(types.BinaryOpEq, arrow.BinaryTypes.String, &strCompareFunction{cmp: func(a, b string) bool { return a == b }})
	binaryFunctions.register(types.BinaryOpEq, arrow.PrimitiveTypes.Int64, &intCompareFunction{cmp: func(a, b int64) bool { return a == b }})
	binaryFunctions.register(types.BinaryOpEq, arrow.PrimitiveTypes.Uint64, &timestampCompareFunction{cmp: func(a, b uint64) bool { return a == b }})
	binaryFunctions.register(types.BinaryOpEq, arrow.PrimitiveTypes.Float64, &floatCompareFunction{cmp: func(a, b float64) bool { return a == b }})
	// Functions for [types.BinaryOpNeq]
	binaryFunctions.register(types.BinaryOpNeq, arrow.FixedWidthTypes.Boolean, &boolCompareFunction{cmp: func(a, b bool) bool { return a != b }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.BinaryTypes.String, &strCompareFunction{cmp: func(a, b string) bool { return a != b }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.PrimitiveTypes.Int64, &intCompareFunction{cmp: func(a, b int64) bool { return a != b }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.PrimitiveTypes.Uint64, &timestampCompareFunction{cmp: func(a, b uint64) bool { return a != b }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.PrimitiveTypes.Float64, &floatCompareFunction{cmp: func(a, b float64) bool { return a != b }})
	// Functions for [types.BinaryOpGt]
	binaryFunctions.register(types.BinaryOpGt, arrow.FixedWidthTypes.Boolean, &boolCompareFunction{cmp: func(a, b bool) bool { return boolToInt(a) > boolToInt(b) }})
	binaryFunctions.register(types.BinaryOpGt, arrow.BinaryTypes.String, &strCompareFunction{cmp: func(a, b string) bool { return a > b }})
	binaryFunctions.register(types.BinaryOpGt, arrow.PrimitiveTypes.Int64, &intCompareFunction{cmp: func(a, b int64) bool { return a > b }})
	binaryFunctions.register(types.BinaryOpGt, arrow.PrimitiveTypes.Uint64, &timestampCompareFunction{cmp: func(a, b uint64) bool { return a > b }})
	binaryFunctions.register(types.BinaryOpGt, arrow.PrimitiveTypes.Float64, &floatCompareFunction{cmp: func(a, b float64) bool { return a > b }})
	// Functions for [types.BinaryOpGte]
	binaryFunctions.register(types.BinaryOpGte, arrow.FixedWidthTypes.Boolean, &boolCompareFunction{cmp: func(a, b bool) bool { return boolToInt(a) >= boolToInt(b) }})
	binaryFunctions.register(types.BinaryOpGte, arrow.BinaryTypes.String, &strCompareFunction{cmp: func(a, b string) bool { return a >= b }})
	binaryFunctions.register(types.BinaryOpGte, arrow.PrimitiveTypes.Int64, &intCompareFunction{cmp: func(a, b int64) bool { return a >= b }})
	binaryFunctions.register(types.BinaryOpGte, arrow.PrimitiveTypes.Uint64, &timestampCompareFunction{cmp: func(a, b uint64) bool { return a >= b }})
	binaryFunctions.register(types.BinaryOpGte, arrow.PrimitiveTypes.Float64, &floatCompareFunction{cmp: func(a, b float64) bool { return a >= b }})
	// Functions for [types.BinaryOpLt]
	binaryFunctions.register(types.BinaryOpLt, arrow.FixedWidthTypes.Boolean, &boolCompareFunction{cmp: func(a, b bool) bool { return boolToInt(a) < boolToInt(b) }})
	binaryFunctions.register(types.BinaryOpLt, arrow.BinaryTypes.String, &strCompareFunction{cmp: func(a, b string) bool { return a < b }})
	binaryFunctions.register(types.BinaryOpLt, arrow.PrimitiveTypes.Int64, &intCompareFunction{cmp: func(a, b int64) bool { return a < b }})
	binaryFunctions.register(types.BinaryOpLt, arrow.PrimitiveTypes.Uint64, &timestampCompareFunction{cmp: func(a, b uint64) bool { return a < b }})
	binaryFunctions.register(types.BinaryOpLt, arrow.PrimitiveTypes.Float64, &floatCompareFunction{cmp: func(a, b float64) bool { return a < b }})
	// Functions for [types.BinaryOpLte]
	binaryFunctions.register(types.BinaryOpLte, arrow.FixedWidthTypes.Boolean, &boolCompareFunction{cmp: func(a, b bool) bool { return boolToInt(a) <= boolToInt(b) }})
	binaryFunctions.register(types.BinaryOpLte, arrow.BinaryTypes.String, &strCompareFunction{cmp: func(a, b string) bool { return a <= b }})
	binaryFunctions.register(types.BinaryOpLte, arrow.PrimitiveTypes.Int64, &intCompareFunction{cmp: func(a, b int64) bool { return a <= b }})
	binaryFunctions.register(types.BinaryOpLte, arrow.PrimitiveTypes.Uint64, &timestampCompareFunction{cmp: func(a, b uint64) bool { return a <= b }})
	binaryFunctions.register(types.BinaryOpLte, arrow.PrimitiveTypes.Float64, &floatCompareFunction{cmp: func(a, b float64) bool { return a <= b }})
	// Functions for [types.BinaryOpMatchSubstr]
	binaryFunctions.register(types.BinaryOpMatchSubstr, arrow.BinaryTypes.String, &strCompareFunction{cmp: func(a, b string) bool { return strings.Contains(a, b) }})
	// Functions for [types.BinaryOpNotMatchSubstr]
	binaryFunctions.register(types.BinaryOpNotMatchSubstr, arrow.BinaryTypes.String, &strCompareFunction{cmp: func(a, b string) bool { return !strings.Contains(a, b) }})
	// Functions for [types.BinaryOpMatchRe]
	// TODO(chaudum): Performance of regex evaluation can be improved if RHS is a Scalar,
	// because the regexp would only need to compiled once for the given scalar value.
	// TODO(chaudum): Performance of regex evaluation can be improved by simplifying the regex,
	// see pkg/logql/log/filter.go:645
	binaryFunctions.register(types.BinaryOpMatchRe, arrow.BinaryTypes.String, &strCompareFunction{cmp: func(a, b string) bool {
		reg, err := regexp.Compile(b)
		if err != nil { // TODO(chaudum): Add support for returning error when comparison fails. For now, return false.
			return false
		}
		return reg.Match([]byte(a))
	}})
	// Functions for [types.BinaryOpNotMatchRe]
	binaryFunctions.register(types.BinaryOpNotMatchRe, arrow.BinaryTypes.String, &strCompareFunction{cmp: func(a, b string) bool {
		reg, err := regexp.Compile(b)
		if err != nil { // TODO(chaudum): Add support for returning error when comparison fails. For now, return false.
			return false
		}
		return !reg.Match([]byte(a))
	}})
}

type UnaryFunctionRegistry interface {
	register(types.UnaryOp, arrow.DataType, UnaryFunction)
	GetForSignature(types.UnaryOp, arrow.DataType) (UnaryFunction, error)
}

type UnaryFunction interface {
	Evaluate(lhs ColumnVector) (ColumnVector, error)
}

type unaryFuncReg struct {
	reg map[types.UnaryOp]map[arrow.DataType]UnaryFunction
}

// register implements UnaryFunctionRegistry.
func (u *unaryFuncReg) register(op types.UnaryOp, ltype arrow.DataType, f UnaryFunction) {
	if u.reg == nil {
		u.reg = make(map[types.UnaryOp]map[arrow.DataType]UnaryFunction)
	}
	if _, ok := u.reg[op]; !ok {
		u.reg[op] = make(map[arrow.DataType]UnaryFunction)
	}
	// TODO(chaudum): Should the function panic when duplicate keys are registered?
	u.reg[op][ltype] = f
}

// GetForSignature implements UnaryFunctionRegistry.
func (u *unaryFuncReg) GetForSignature(types.UnaryOp, arrow.DataType) (UnaryFunction, error) {
	return nil, errors.ErrNotImplemented
}

type BinaryFunctionRegistry interface {
	register(types.BinaryOp, arrow.DataType, BinaryFunction)
	GetForSignature(types.BinaryOp, arrow.DataType) (BinaryFunction, error)
}

// TODO(chaudum): Make BinaryFunction typed:
//
//	type BinaryFunction[L, R arrow.DataType] interface {
//		Evaluate(lhs ColumnVector[L], rhs ColumnVector[R]) (ColumnVector[arrow.BOOL], error)
//	}
type BinaryFunction interface {
	Evaluate(lhs, rhs ColumnVector) (ColumnVector, error)
}

type binaryFuncReg struct {
	reg map[types.BinaryOp]map[arrow.DataType]BinaryFunction
}

// register implements BinaryFunctionRegistry.
func (b *binaryFuncReg) register(op types.BinaryOp, ty arrow.DataType, f BinaryFunction) {
	if b.reg == nil {
		b.reg = make(map[types.BinaryOp]map[arrow.DataType]BinaryFunction)
	}
	if _, ok := b.reg[op]; !ok {
		b.reg[op] = make(map[arrow.DataType]BinaryFunction)
	}
	// TODO(chaudum): Should the function panic when duplicate keys are registered?
	b.reg[op][ty] = f
}

// GetForSignature implements BinaryFunctionRegistry.
func (b *binaryFuncReg) GetForSignature(op types.BinaryOp, ltype arrow.DataType) (BinaryFunction, error) {
	// Get registered functions for the specific operation
	reg, ok := b.reg[op]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	// Get registered function for the specific data type
	fn, ok := reg[ltype]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	return fn, nil
}

type boolCompareFunction struct {
	cmp func(a, b bool) bool
}

// Evaluate implements BinaryFunction.
func (f *boolCompareFunction) Evaluate(lhs ColumnVector, rhs ColumnVector) (ColumnVector, error) {
	lhsArr := lhs.ToArray().(*array.Boolean)
	rhsArr := rhs.ToArray().(*array.Boolean)

	if lhsArr.Len() != rhsArr.Len() {
		return nil, errors.ErrIndex
	}

	mem := memory.NewGoAllocator()
	builder := array.NewBooleanBuilder(mem)
	defer builder.Release()

	for i := 0; i < lhs.ToArray().Len(); i++ {
		if lhsArr.IsNull(i) || rhsArr.IsNull(i) {
			builder.Append(false)
			continue
		}
		builder.Append(f.cmp(lhsArr.Value(i), rhsArr.Value(i)))
	}

	return &Array{array: builder.NewArray()}, nil
}

type strCompareFunction struct {
	cmp func(a, b string) bool
}

// Evaluate implements BinaryFunction.
func (f *strCompareFunction) Evaluate(lhs ColumnVector, rhs ColumnVector) (ColumnVector, error) {
	if lhs.Len() != rhs.Len() {
		return nil, arrow.ErrIndex
	}

	lhsArr := lhs.ToArray().(*array.String)
	rhsArr := rhs.ToArray().(*array.String)

	mem := memory.NewGoAllocator()
	builder := array.NewBooleanBuilder(mem)
	defer builder.Release()

	for i := 0; i < lhs.ToArray().Len(); i++ {
		if lhsArr.IsNull(i) || rhsArr.IsNull(i) {
			builder.Append(false)
			continue
		}
		builder.Append(f.cmp(lhsArr.Value(i), rhsArr.Value(i)))
	}

	return &Array{array: builder.NewArray()}, nil
}

type intCompareFunction struct {
	cmp func(a, b int64) bool
}

// Evaluate implements BinaryFunction.
func (f *intCompareFunction) Evaluate(lhs ColumnVector, rhs ColumnVector) (ColumnVector, error) {
	if lhs.Len() != rhs.Len() {
		return nil, arrow.ErrIndex
	}

	lhsArr := lhs.ToArray().(*array.Int64)
	rhsArr := rhs.ToArray().(*array.Int64)

	mem := memory.NewGoAllocator()
	builder := array.NewBooleanBuilder(mem)
	defer builder.Release()

	for i := 0; i < lhs.ToArray().Len(); i++ {
		if lhsArr.IsNull(i) || rhsArr.IsNull(i) {
			builder.Append(false)
			continue
		}
		builder.Append(f.cmp(lhsArr.Value(i), rhsArr.Value(i)))
	}

	return &Array{array: builder.NewArray()}, nil
}

type timestampCompareFunction struct {
	cmp func(a, b uint64) bool
}

// Evaluate implements BinaryFunction.
func (f *timestampCompareFunction) Evaluate(lhs ColumnVector, rhs ColumnVector) (ColumnVector, error) {
	if lhs.Len() != rhs.Len() {
		return nil, arrow.ErrIndex
	}

	lhsArr := lhs.ToArray().(*array.Uint64)
	rhsArr := rhs.ToArray().(*array.Uint64)

	mem := memory.NewGoAllocator()
	builder := array.NewBooleanBuilder(mem)
	defer builder.Release()

	for i := 0; i < lhs.ToArray().Len(); i++ {
		if lhsArr.IsNull(i) || rhsArr.IsNull(i) {
			builder.Append(false)
			continue
		}
		builder.Append(f.cmp(lhsArr.Value(i), rhsArr.Value(i)))
	}

	return &Array{array: builder.NewArray()}, nil
}

type floatCompareFunction struct {
	cmp func(a, b float64) bool
}

// Evaluate implements BinaryFunction.
func (f *floatCompareFunction) Evaluate(lhs ColumnVector, rhs ColumnVector) (ColumnVector, error) {
	if lhs.Len() != rhs.Len() {
		return nil, arrow.ErrIndex
	}

	lhsArr := lhs.ToArray().(*array.Float64)
	rhsArr := rhs.ToArray().(*array.Float64)

	mem := memory.NewGoAllocator()
	builder := array.NewBooleanBuilder(mem)
	defer builder.Release()

	for i := 0; i < lhs.ToArray().Len(); i++ {
		if lhsArr.IsNull(i) || rhsArr.IsNull(i) {
			builder.Append(false)
			continue
		}
		builder.Append(f.cmp(lhsArr.Value(i), rhsArr.Value(i)))
	}

	return &Array{array: builder.NewArray()}, nil
}

// Compiler optimized version of converting boolean b into an integer of value 0 or 1
// https://github.com/golang/go/issues/6011#issuecomment-323144578
func boolToInt(b bool) int {
	var i int
	if b {
		i = 1
	} else {
		i = 0
	}
	return i

}
