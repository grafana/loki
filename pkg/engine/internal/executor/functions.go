package executor

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/errors"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	unaryFunctions    UnaryFunctionRegistry    = &unaryFuncReg{}
	binaryFunctions   BinaryFunctionRegistry   = &binaryFuncReg{}
	variadicFunctions VariadicFunctionRegistry = &variadicFuncReg{}
)

func init() {
	// Functions for [types.BinaryOpDiv]
	binaryFunctions.register(types.BinaryOpDiv, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a / b, nil }})
	binaryFunctions.register(types.BinaryOpDiv, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) / float64(b), nil }})
	// Functions for [types.BinaryOpAdd]
	binaryFunctions.register(types.BinaryOpAdd, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a + b, nil }})
	binaryFunctions.register(types.BinaryOpAdd, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) + float64(b), nil }})
	// Functions for [types.BinaryOpSub]
	binaryFunctions.register(types.BinaryOpSub, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a - b, nil }})
	binaryFunctions.register(types.BinaryOpSub, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) - float64(b), nil }})
	// Functions for [types.BinaryOpMul]
	binaryFunctions.register(types.BinaryOpMul, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a * b, nil }})
	binaryFunctions.register(types.BinaryOpMul, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) * float64(b), nil }})
	// Functions for [types.BinaryOpMod]
	binaryFunctions.register(types.BinaryOpMod, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return math.Mod(a, b), nil }})
	binaryFunctions.register(types.BinaryOpMod, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a % b), nil }})
	// Functions for [types.BinaryOpPow]
	binaryFunctions.register(types.BinaryOpPow, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return math.Pow(a, b), nil }})
	binaryFunctions.register(types.BinaryOpPow, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return math.Pow(float64(a), float64(b)), nil }})

	// Functions for [types.BinaryOpEq]
	binaryFunctions.register(types.BinaryOpEq, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a == b, nil }})
	binaryFunctions.register(types.BinaryOpEq, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a == b, nil }})
	binaryFunctions.register(types.BinaryOpEq, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a == b, nil }})
	binaryFunctions.register(types.BinaryOpEq, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a == b, nil }})
	binaryFunctions.register(types.BinaryOpEq, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a == b, nil }})
	// Functions for [types.BinaryOpNeq]
	binaryFunctions.register(types.BinaryOpNeq, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a != b, nil }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a != b, nil }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a != b, nil }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a != b, nil }})
	binaryFunctions.register(types.BinaryOpNeq, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a != b, nil }})
	// Functions for [types.BinaryOpGt]
	binaryFunctions.register(types.BinaryOpGt, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) > boolToInt(b), nil }})
	binaryFunctions.register(types.BinaryOpGt, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a > b, nil }})
	binaryFunctions.register(types.BinaryOpGt, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a > b, nil }})
	binaryFunctions.register(types.BinaryOpGt, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a > b, nil }})
	binaryFunctions.register(types.BinaryOpGt, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a > b, nil }})
	// Functions for [types.BinaryOpGte]
	binaryFunctions.register(types.BinaryOpGte, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) >= boolToInt(b), nil }})
	binaryFunctions.register(types.BinaryOpGte, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a >= b, nil }})
	binaryFunctions.register(types.BinaryOpGte, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a >= b, nil }})
	binaryFunctions.register(types.BinaryOpGte, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a >= b, nil }})
	binaryFunctions.register(types.BinaryOpGte, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a >= b, nil }})
	// Functions for [types.BinaryOpLt]
	binaryFunctions.register(types.BinaryOpLt, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) < boolToInt(b), nil }})
	binaryFunctions.register(types.BinaryOpLt, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a < b, nil }})
	binaryFunctions.register(types.BinaryOpLt, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a < b, nil }})
	binaryFunctions.register(types.BinaryOpLt, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a < b, nil }})
	binaryFunctions.register(types.BinaryOpLt, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a < b, nil }})
	// Functions for [types.BinaryOpLte]
	binaryFunctions.register(types.BinaryOpLte, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) <= boolToInt(b), nil }})
	binaryFunctions.register(types.BinaryOpLte, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a <= b, nil }})
	binaryFunctions.register(types.BinaryOpLte, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a <= b, nil }})
	binaryFunctions.register(types.BinaryOpLte, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a <= b, nil }})
	binaryFunctions.register(types.BinaryOpLte, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a <= b, nil }})
	// Functions for [types.BinaryOpAnd]
	binaryFunctions.register(types.BinaryOpAnd, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a && b, nil }})
	// Functions for [types.BinaryOpOr]
	binaryFunctions.register(types.BinaryOpOr, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a || b, nil }})
	// Functions for [types.BinaryOpXor]
	binaryFunctions.register(types.BinaryOpXor, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a != b, nil }})
	// Functions for [types.BinaryOpMatchSubstr]
	binaryFunctions.register(types.BinaryOpMatchSubstr, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return strings.Contains(a, b), nil }})
	// Functions for [types.BinaryOpNotMatchSubstr]
	binaryFunctions.register(types.BinaryOpNotMatchSubstr, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return !strings.Contains(a, b), nil }})
	// Functions for [types.BinaryOpMatchRe]
	binaryFunctions.register(types.BinaryOpMatchRe, arrow.BinaryTypes.String, &regexpFunction{eval: func(a, b string, reg *regexp.Regexp) (bool, error) {
		if reg == nil {
			// Fallback to per-row regex-compilation due to non-scalar expression
			var err error
			reg, err = regexp.Compile(b)
			if err != nil {
				return false, err
			}
		}
		return reg.Match([]byte(a)), nil
	}})
	// Functions for [types.BinaryOpNotMatchRe]
	binaryFunctions.register(types.BinaryOpNotMatchRe, arrow.BinaryTypes.String, &regexpFunction{eval: func(a, b string, reg *regexp.Regexp) (bool, error) {
		if reg == nil {
			// Fallback to per-row regex-compilation due to non-scalar expression
			var err error
			reg, err = regexp.Compile(b)
			if err != nil {
				return false, err
			}
		}
		return !reg.Match([]byte(a)), nil
	}})

	// Functions for [types.UnaryOpNot]
	unaryFunctions.register(types.UnaryOpNot, arrow.FixedWidthTypes.Boolean, UnaryFunc(func(input arrow.Array) (arrow.Array, error) {
		arr, ok := input.(*array.Boolean)
		if !ok {
			return nil, fmt.Errorf("invalid array type: expected *array.Boolean, got %T", input)
		}

		builder := array.NewBooleanBuilder(memory.DefaultAllocator)
		for i := range arr.Len() {
			if arr.IsNull(i) {
				builder.AppendNull()
				continue
			}
			builder.Append(!arr.Value(i))
		}

		return builder.NewArray(), nil
	}))

	// Cast functions
	unaryFunctions.register(types.UnaryOpCastFloat, arrow.BinaryTypes.String, castFn(types.UnaryOpCastFloat))
	unaryFunctions.register(types.UnaryOpCastBytes, arrow.BinaryTypes.String, castFn(types.UnaryOpCastBytes))
	unaryFunctions.register(types.UnaryOpCastDuration, arrow.BinaryTypes.String, castFn(types.UnaryOpCastDuration))

	// Parse functions
	variadicFunctions.register(types.VariadicOpParseLogfmt, parseFn(types.VariadicOpParseLogfmt))
	variadicFunctions.register(types.VariadicOpParseJSON, parseFn(types.VariadicOpParseJSON))
}

type UnaryFunctionRegistry interface {
	register(types.UnaryOp, arrow.DataType, UnaryFunction)
	GetForSignature(types.UnaryOp, arrow.DataType) (UnaryFunction, error)
}

type UnaryFunction interface {
	Evaluate(lhs arrow.Array) (arrow.Array, error)
}

type UnaryFunc func(arrow.Array) (arrow.Array, error)

func (f UnaryFunc) Evaluate(lhs arrow.Array) (arrow.Array, error) {
	return f(lhs)
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
func (u *unaryFuncReg) GetForSignature(op types.UnaryOp, ltype arrow.DataType) (UnaryFunction, error) {
	// Get registered functions for the specific operation
	reg, ok := u.reg[op]
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

type BinaryFunctionRegistry interface {
	register(types.BinaryOp, arrow.DataType, BinaryFunction)
	GetForSignature(types.BinaryOp, arrow.DataType) (BinaryFunction, error)
}

type BinaryFunction interface {
	Evaluate(lhs, rhs arrow.Array, lhsIsScalar, rhsIsScalar bool) (arrow.Array, error)
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

type regexpFunction struct {
	eval func(a, b string, reg *regexp.Regexp) (bool, error)
}

func (f *regexpFunction) Evaluate(lhs arrow.Array, rhs arrow.Array, _, rhsIsScalar bool) (arrow.Array, error) {
	if lhs.Len() != rhs.Len() {
		return nil, arrow.ErrIndex
	}

	lhsArr, ok := lhs.(*array.String)
	if !ok {
		return nil, fmt.Errorf("invalid array type: expected %T, got %T", new(*array.String), lhs)
	}

	rhsArr, ok := rhs.(*array.String)
	if !ok {
		return nil, fmt.Errorf("invalid array type: expected %T, got %T", new(*array.String), rhs)
	}

	builder := array.NewBooleanBuilder(memory.DefaultAllocator)
	if rhs.Len() == 0 {
		return builder.NewArray(), nil
	}

	var (
		re  *regexp.Regexp
		err error
	)

	if rhsIsScalar {
		if rhsArr.IsNull(0) {
			return nil, fmt.Errorf("invalid NULL value, expected string")
		}
		// TODO(chaudum): Performance of regex evaluation can be improved by simplifying the regex,
		// see pkg/logql/log/filter.go:645
		re, err = regexp.Compile(rhsArr.Value(0))
		if err != nil {
			return nil, fmt.Errorf("failed to compile regular expression for batch: %w", err)
		}
	}

	for i := range lhsArr.Len() {
		if lhsArr.IsNull(i) || rhsArr.IsNull(i) {
			builder.Append(false)
			continue
		}
		res, err := f.eval(lhsArr.Value(i), rhsArr.Value(i), re)
		if err != nil {
			return nil, err
		}
		builder.Append(res)
	}

	return builder.NewArray(), nil

}

// genericBoolFunction is a struct that implements the [BinaryFunction] interface methods
// and can be used for any array type with comparable elements.
type genericBoolFunction[E arrow.TypedArray[T], T arrow.ValueType] struct {
	eval func(a, b T) (bool, error)
}

// Evaluate implements BinaryFunction.
func (f *genericBoolFunction[E, T]) Evaluate(lhs arrow.Array, rhs arrow.Array, _, _ bool) (arrow.Array, error) {
	if lhs.Len() != rhs.Len() {
		return nil, arrow.ErrIndex
	}

	lhsArr, ok := lhs.(E)
	if !ok {
		return nil, fmt.Errorf("invalid array type: expected %T, got %T", new(E), lhs)
	}

	rhsArr, ok := rhs.(E)
	if !ok {
		return nil, fmt.Errorf("invalid array type: expected %T, got %T", new(E), rhs)
	}

	builder := array.NewBooleanBuilder(memory.DefaultAllocator)

	for i := range lhsArr.Len() {
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

	return builder.NewArray(), nil
}

// genericFloat64Function is a struct that implements the [BinaryFunction] interface methods
// and can be used for any array type with numeric elements.
type genericFloat64Function[E arrow.TypedArray[T], T arrow.ValueType] struct {
	eval func(a, b T) (float64, error)
}

// Evaluate implements BinaryFunction.
func (f *genericFloat64Function[E, T]) Evaluate(lhs arrow.Array, rhs arrow.Array, _, _ bool) (arrow.Array, error) {
	if lhs.Len() != rhs.Len() {
		return nil, arrow.ErrIndex
	}

	lhsArr, ok := lhs.(E)
	if !ok {
		return nil, fmt.Errorf("invalid array type: expected %T, got %T", new(E), lhs)
	}

	rhsArr, ok := rhs.(E)
	if !ok {
		return nil, fmt.Errorf("invalid array type: expected %T, got %T", new(E), rhs)
	}

	builder := array.NewFloat64Builder(memory.DefaultAllocator)

	for i := range lhsArr.Len() {
		if lhsArr.IsNull(i) || rhsArr.IsNull(i) {
			builder.AppendNull()
			continue
		}
		res, err := f.eval(lhsArr.Value(i), rhsArr.Value(i))
		if err != nil {
			return nil, err
		}
		builder.Append(res)
	}

	return builder.NewArray(), nil
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

type VariadicFunctionRegistry interface {
	register(types.VariadicOp, VariadicFunction)
	GetForSignature(types.VariadicOp) (VariadicFunction, error)
}

type VariadicFunction interface {
	Evaluate(args ...arrow.Array) (arrow.Array, error)
}

type VariadicFunctionFunc func(args ...arrow.Array) (arrow.Array, error)

func (f VariadicFunctionFunc) Evaluate(args ...arrow.Array) (arrow.Array, error) {
	return f(args...)
}

type variadicFuncReg struct {
	reg map[types.VariadicOp]VariadicFunction
}

// register implements VariadicFunctionRegistry.
func (u *variadicFuncReg) register(op types.VariadicOp, f VariadicFunction) {
	if u.reg == nil {
		u.reg = make(map[types.VariadicOp]VariadicFunction)
	}

	_, exists := u.reg[op]
	if exists {
		panic(fmt.Sprintf("duplicate variadic function registration for %s", op))
	}

	u.reg[op] = f
}

// GetForSignature implements VariadicFunctionRegistry.
func (u *variadicFuncReg) GetForSignature(op types.VariadicOp) (VariadicFunction, error) {
	// Get registered function for the specific operation
	fn, ok := u.reg[op]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	return fn, nil
}
