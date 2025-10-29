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
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
)

var (
	unaryFunctions  UnaryFunctionRegistry  = &unaryFuncReg{}
	binaryFunctions BinaryFunctionRegistry = &binaryFuncReg{}
)

func init() {
	// Functions for [physicalpb.BINARY_OP_DIV]
	binaryFunctions.register(physicalpb.BINARY_OP_DIV, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a / b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_DIV, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) / float64(b), nil }})
	// Functions for [physicalpb.BINARY_OP_ADD]
	binaryFunctions.register(physicalpb.BINARY_OP_ADD, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a + b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_ADD, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) + float64(b), nil }})
	// Functions for [physicalpb.BINARY_OP_SUB]
	binaryFunctions.register(physicalpb.BINARY_OP_SUB, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a - b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_SUB, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) - float64(b), nil }})
	// Functions for [physicalpb.BINARY_OP_MUL]
	binaryFunctions.register(physicalpb.BINARY_OP_MUL, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a * b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_MUL, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) * float64(b), nil }})
	// Functions for [physicalpb.BINARY_OP_MOD]
	binaryFunctions.register(physicalpb.BINARY_OP_MOD, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return math.Mod(a, b), nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_MOD, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a % b), nil }})
	// Functions for [physicalpb.BINARY_OP_POW]
	binaryFunctions.register(physicalpb.BINARY_OP_POW, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return math.Pow(a, b), nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_POW, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return math.Pow(float64(a), float64(b)), nil }})

	// Functions for [physicalpb.BINARY_OP_EQ]
	binaryFunctions.register(physicalpb.BINARY_OP_EQ, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a == b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_EQ, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a == b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_EQ, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a == b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_EQ, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a == b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_EQ, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a == b, nil }})
	// Functions for [physicalpb.BINARY_OP_NEQ]
	binaryFunctions.register(physicalpb.BINARY_OP_NEQ, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a != b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_NEQ, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a != b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_NEQ, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a != b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_NEQ, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a != b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_NEQ, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a != b, nil }})
	// Functions for [physicalpb.BINARY_OP_GT]
	binaryFunctions.register(physicalpb.BINARY_OP_GT, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) > boolToInt(b), nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_GT, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a > b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_GT, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a > b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_GT, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a > b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_GT, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a > b, nil }})
	// Functions for [physicalpb.BINARY_OP_GTE]
	binaryFunctions.register(physicalpb.BINARY_OP_GTE, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) >= boolToInt(b), nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_GTE, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a >= b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_GTE, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a >= b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_GTE, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a >= b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_GTE, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a >= b, nil }})
	// Functions for [physicalpb.BINARY_OP_LT]
	binaryFunctions.register(physicalpb.BINARY_OP_LT, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) < boolToInt(b), nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_LT, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a < b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_LT, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a < b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_LT, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a < b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_LT, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a < b, nil }})
	// Functions for [physicalpb.BINARY_OP_LTE]
	binaryFunctions.register(physicalpb.BINARY_OP_LTE, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) <= boolToInt(b), nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_LTE, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return a <= b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_LTE, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a <= b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_LTE, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a <= b, nil }})
	binaryFunctions.register(physicalpb.BINARY_OP_LTE, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a <= b, nil }})
	// Functions for [physicalpb.BINARY_OP_MATCH_SUBSTR]
	binaryFunctions.register(physicalpb.BINARY_OP_MATCH_SUBSTR, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return strings.Contains(a, b), nil }})
	// Functions for [physicalpb.BINARY_OP_NOT_MATCH_SUBSTR]
	binaryFunctions.register(physicalpb.BINARY_OP_NOT_MATCH_SUBSTR, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) { return !strings.Contains(a, b), nil }})
	// Functions for [physicalpb.BINARY_OP_MATCH_RE]
	// TODO(chaudum): Performance of regex evaluation can be improved if RHS is a Scalar,
	// because the regexp would only need to compiled once for the given scalar value.
	// TODO(chaudum): Performance of regex evaluation can be improved by simplifying the regex,
	// see pkg/logql/log/filter.go:645
	binaryFunctions.register(physicalpb.BINARY_OP_MATCH_RE, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) {
		reg, err := regexp.Compile(b)
		if err != nil {
			return false, err
		}
		return reg.Match([]byte(a)), nil
	}})
	// Functions for [physicalpb.BINARY_OP_NOT_MATCH_RE]
	binaryFunctions.register(physicalpb.BINARY_OP_NOT_MATCH_RE, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{eval: func(a, b string) (bool, error) {
		reg, err := regexp.Compile(b)
		if err != nil {
			return false, err
		}
		return !reg.Match([]byte(a)), nil
	}})

	// Cast functions
	unaryFunctions.register(physicalpb.UNARY_OP_CAST_FLOAT, arrow.BinaryTypes.String, castFn(physicalpb.UNARY_OP_CAST_FLOAT))
	unaryFunctions.register(physicalpb.UNARY_OP_CAST_BYTES, arrow.BinaryTypes.String, castFn(physicalpb.UNARY_OP_CAST_BYTES))
	unaryFunctions.register(physicalpb.UNARY_OP_CAST_DURATION, arrow.BinaryTypes.String, castFn(physicalpb.UNARY_OP_CAST_DURATION))
}

type UnaryFunctionRegistry interface {
	register(physicalpb.UnaryOp, arrow.DataType, UnaryFunction)
	GetForSignature(physicalpb.UnaryOp, arrow.DataType) (UnaryFunction, error)
}

type UnaryFunction interface {
	Evaluate(lhs arrow.Array) (arrow.Array, error)
}

type UnaryFunc func(arrow.Array) (arrow.Array, error)

func (f UnaryFunc) Evaluate(lhs arrow.Array) (arrow.Array, error) {
	return f(lhs)
}

type unaryFuncReg struct {
	reg map[physicalpb.UnaryOp]map[arrow.DataType]UnaryFunction
}

// register implements UnaryFunctionRegistry.
func (u *unaryFuncReg) register(op physicalpb.UnaryOp, ltype arrow.DataType, f UnaryFunction) {
	if u.reg == nil {
		u.reg = make(map[physicalpb.UnaryOp]map[arrow.DataType]UnaryFunction)
	}
	if _, ok := u.reg[op]; !ok {
		u.reg[op] = make(map[arrow.DataType]UnaryFunction)
	}
	// TODO(chaudum): Should the function panic when duplicate keys are registered?
	u.reg[op][ltype] = f
}

// GetForSignature implements UnaryFunctionRegistry.
func (u *unaryFuncReg) GetForSignature(op physicalpb.UnaryOp, ltype arrow.DataType) (UnaryFunction, error) {
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
	register(physicalpb.BinaryOp, arrow.DataType, BinaryFunction)
	GetForSignature(physicalpb.BinaryOp, arrow.DataType) (BinaryFunction, error)
}

type BinaryFunction interface {
	Evaluate(lhs, rhs arrow.Array) (arrow.Array, error)
}

type binaryFuncReg struct {
	reg map[physicalpb.BinaryOp]map[arrow.DataType]BinaryFunction
}

// register implements BinaryFunctionRegistry.
func (b *binaryFuncReg) register(op physicalpb.BinaryOp, ty arrow.DataType, f BinaryFunction) {
	if b.reg == nil {
		b.reg = make(map[physicalpb.BinaryOp]map[arrow.DataType]BinaryFunction)
	}
	if _, ok := b.reg[op]; !ok {
		b.reg[op] = make(map[arrow.DataType]BinaryFunction)
	}
	// TODO(chaudum): Should the function panic when duplicate keys are registered?
	b.reg[op][ty] = f
}

// GetForSignature implements BinaryFunctionRegistry.
func (b *binaryFuncReg) GetForSignature(op physicalpb.BinaryOp, ltype arrow.DataType) (BinaryFunction, error) {
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

// genericBoolFunction is a struct that implements the [BinaryFunction] interface methods
// and can be used for any array type with comparable elements.
type genericBoolFunction[E arrow.TypedArray[T], T arrow.ValueType] struct {
	eval func(a, b T) (bool, error)
}

// Evaluate implements BinaryFunction.
func (f *genericBoolFunction[E, T]) Evaluate(lhs arrow.Array, rhs arrow.Array) (arrow.Array, error) {
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
func (f *genericFloat64Function[E, T]) Evaluate(lhs arrow.Array, rhs arrow.Array) (arrow.Array, error) {
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
