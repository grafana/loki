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
	binaryFunctions.register(types.BinaryOpEq, arrow.FixedWidthTypes.Boolean, &genericFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a == b, nil }})
	binaryFunctions.register(types.BinaryOpEq, arrow.BinaryTypes.String, &genericFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a == b, nil }})
	binaryFunctions.register(types.BinaryOpEq, arrow.PrimitiveTypes.Int64, &genericFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a == b, nil }})
	binaryFunctions.register(types.BinaryOpEq, arrow.FixedWidthTypes.Timestamp_ns, &genericFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a == b, nil }})
	binaryFunctions.register(types.BinaryOpEq, arrow.PrimitiveTypes.Float64, &genericFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a == b, nil }})
	// Functions for [types.BinaryOpNeq]
	binaryFunctions.register(types.BinaryOpNeq, arrow.FixedWidthTypes.Boolean, &genericFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a != b, nil }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.BinaryTypes.String, &genericFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a != b, nil }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.PrimitiveTypes.Int64, &genericFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a != b, nil }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.FixedWidthTypes.Timestamp_ns, &genericFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a != b, nil }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.PrimitiveTypes.Float64, &genericFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a != b, nil }})
	// Functions for [types.BinaryOpGt]
	binaryFunctions.register(types.BinaryOpGt, arrow.FixedWidthTypes.Boolean, &genericFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) > boolToInt(b), nil }})
	binaryFunctions.register(types.BinaryOpGt, arrow.BinaryTypes.String, &genericFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a > b, nil }})
	binaryFunctions.register(types.BinaryOpGt, arrow.PrimitiveTypes.Int64, &genericFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a > b, nil }})
	binaryFunctions.register(types.BinaryOpGt, arrow.FixedWidthTypes.Timestamp_ns, &genericFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a > b, nil }})
	binaryFunctions.register(types.BinaryOpGt, arrow.PrimitiveTypes.Float64, &genericFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a > b, nil }})
	// Functions for [types.BinaryOpGte]
	binaryFunctions.register(types.BinaryOpGte, arrow.FixedWidthTypes.Boolean, &genericFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) >= boolToInt(b), nil }})
	binaryFunctions.register(types.BinaryOpGte, arrow.BinaryTypes.String, &genericFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a >= b, nil }})
	binaryFunctions.register(types.BinaryOpGte, arrow.PrimitiveTypes.Int64, &genericFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a >= b, nil }})
	binaryFunctions.register(types.BinaryOpGte, arrow.FixedWidthTypes.Timestamp_ns, &genericFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a >= b, nil }})
	binaryFunctions.register(types.BinaryOpGte, arrow.PrimitiveTypes.Float64, &genericFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a >= b, nil }})
	// Functions for [types.BinaryOpLt]
	binaryFunctions.register(types.BinaryOpLt, arrow.FixedWidthTypes.Boolean, &genericFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) < boolToInt(b), nil }})
	binaryFunctions.register(types.BinaryOpLt, arrow.BinaryTypes.String, &genericFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a < b, nil }})
	binaryFunctions.register(types.BinaryOpLt, arrow.PrimitiveTypes.Int64, &genericFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a < b, nil }})
	binaryFunctions.register(types.BinaryOpLt, arrow.FixedWidthTypes.Timestamp_ns, &genericFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a < b, nil }})
	binaryFunctions.register(types.BinaryOpLt, arrow.PrimitiveTypes.Float64, &genericFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a < b, nil }})
	// Functions for [types.BinaryOpLte]
	binaryFunctions.register(types.BinaryOpLte, arrow.FixedWidthTypes.Boolean, &genericFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) <= boolToInt(b), nil }})
	binaryFunctions.register(types.BinaryOpLte, arrow.BinaryTypes.String, &genericFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a <= b, nil }})
	binaryFunctions.register(types.BinaryOpLte, arrow.PrimitiveTypes.Int64, &genericFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a <= b, nil }})
	binaryFunctions.register(types.BinaryOpLte, arrow.FixedWidthTypes.Timestamp_ns, &genericFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a <= b, nil }})
	binaryFunctions.register(types.BinaryOpLte, arrow.PrimitiveTypes.Float64, &genericFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a <= b, nil }})
	// Functions for [types.BinaryOpMatchSubstr]
	binaryFunctions.register(types.BinaryOpMatchSubstr, arrow.BinaryTypes.String, &genericFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return strings.Contains(a, b), nil }})
	// Functions for [types.BinaryOpNotMatchSubstr]
	binaryFunctions.register(types.BinaryOpNotMatchSubstr, arrow.BinaryTypes.String, &genericFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return !strings.Contains(a, b), nil }})
	// Functions for [types.BinaryOpMatchRe]
	// TODO(chaudum): Performance of regex evaluation can be improved if RHS is a Scalar,
	// because the regexp would only need to compiled once for the given scalar value.
	// TODO(chaudum): Performance of regex evaluation can be improved by simplifying the regex,
	// see pkg/logql/log/filter.go:645
	binaryFunctions.register(types.BinaryOpMatchRe, arrow.BinaryTypes.String, &genericFunction[*array.String, string]{eval: func(a, b string) (bool, error) {
		reg, err := regexp.Compile(b)
		if err != nil {
			return false, err
		}
		return reg.Match([]byte(a)), nil
	}})
	// Functions for [types.BinaryOpNotMatchRe]
	binaryFunctions.register(types.BinaryOpNotMatchRe, arrow.BinaryTypes.String, &genericFunction[*array.String, string]{eval: func(a, b string) (bool, error) {
		reg, err := regexp.Compile(b)
		if err != nil {
			return false, err
		}
		return !reg.Match([]byte(a)), nil
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

// arrayType combines the IsNull function from the [arrow.Array] interface with the
// type specific function Value from the concrete types, such as [array.String].
type arrayType[T comparable] interface {
	IsNull(int) bool
	Value(int) T
}

// genericFunction is a struct that implements the [BinaryFunction] interface methods
// and can be used for any array type with compareable elements.
type genericFunction[E arrayType[T], T comparable] struct {
	eval func(a, b T) (bool, error)
}

// Evaluate implements BinaryFunction.
func (f *genericFunction[E, T]) Evaluate(lhs ColumnVector, rhs ColumnVector) (ColumnVector, error) {
	if lhs.Len() != rhs.Len() {
		return nil, arrow.ErrIndex
	}

	lhsArr, ok := lhs.ToArray().(E)
	if !ok {
		return nil, arrow.ErrType
	}

	rhsArr, ok := rhs.ToArray().(E)
	if !ok {
		return nil, arrow.ErrType
	}

	mem := memory.NewGoAllocator()
	builder := array.NewBooleanBuilder(mem)
	defer builder.Release()

	for i := 0; i < lhs.ToArray().Len(); i++ {
		if lhsArr.IsNull(i) || rhsArr.IsNull(i) {
			builder.Append(false)
			continue
		}
		res, err := f.eval(lhsArr.Value(i), rhsArr.Value(i))
		if err != nil {
			return nil, err
		}
		builder.Append(res)
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
