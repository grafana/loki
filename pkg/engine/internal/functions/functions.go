// Package functions holds the expression function implementations of the
// query engine, together with the signature catalog that declares which
// (operation, operand type) combinations are supported and what type they
// return.
//
// The executor evaluates expressions by looking up implementations in the
// [Unary], [Binary], and [Variadic] registries. The physical planner uses
// [UnaryReturnType], [BinaryReturnType], and [HasVariadic] to type-check
// expressions at plan time without needing the implementations.
package functions

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/errors"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor/matchutil"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	// Unary is the registry of unary function implementations.
	Unary UnaryFunctionRegistry = &unaryFuncReg{}
	// Binary is the registry of binary function implementations.
	Binary BinaryFunctionRegistry = &binaryFuncReg{}
	// Variadic is the registry of variadic function implementations.
	Variadic VariadicFunctionRegistry = &variadicFuncReg{}
)

func init() {
	// Functions for [types.BinaryOpDiv]
	Binary.Register(types.BinaryOpDiv, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a / b, nil }})
	Binary.Register(types.BinaryOpDiv, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) / float64(b), nil }})
	// Functions for [types.BinaryOpAdd]
	Binary.Register(types.BinaryOpAdd, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a + b, nil }})
	Binary.Register(types.BinaryOpAdd, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) + float64(b), nil }})
	// Functions for [types.BinaryOpSub]
	Binary.Register(types.BinaryOpSub, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a - b, nil }})
	Binary.Register(types.BinaryOpSub, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) - float64(b), nil }})
	// Functions for [types.BinaryOpMul]
	Binary.Register(types.BinaryOpMul, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return a * b, nil }})
	Binary.Register(types.BinaryOpMul, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a) * float64(b), nil }})
	// Functions for [types.BinaryOpMod]
	Binary.Register(types.BinaryOpMod, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return math.Mod(a, b), nil }})
	Binary.Register(types.BinaryOpMod, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return float64(a % b), nil }})
	// Functions for [types.BinaryOpPow]
	Binary.Register(types.BinaryOpPow, arrow.PrimitiveTypes.Float64, &genericFloat64Function[*array.Float64, float64]{eval: func(a, b float64) (float64, error) { return math.Pow(a, b), nil }})
	Binary.Register(types.BinaryOpPow, arrow.PrimitiveTypes.Int64, &genericFloat64Function[*array.Int64, int64]{eval: func(a, b int64) (float64, error) { return math.Pow(float64(a), float64(b)), nil }})

	// Functions for [types.BinaryOpEq]
	Binary.Register(types.BinaryOpEq, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a == b, nil }})
	Binary.Register(types.BinaryOpEq, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) { return a == b, nil }})
	Binary.Register(types.BinaryOpEq, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a == b, nil }})
	Binary.Register(types.BinaryOpEq, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a == b, nil }})
	Binary.Register(types.BinaryOpEq, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a == b, nil }})
	// Functions for [types.BinaryOpNeq]
	Binary.Register(types.BinaryOpNeq, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a != b, nil }})
	Binary.Register(types.BinaryOpNeq, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) { return a != b, nil }})
	Binary.Register(types.BinaryOpNeq, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a != b, nil }})
	Binary.Register(types.BinaryOpNeq, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a != b, nil }})
	Binary.Register(types.BinaryOpNeq, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a != b, nil }})
	// Functions for [types.BinaryOpGt]
	Binary.Register(types.BinaryOpGt, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) > boolToInt(b), nil }})
	Binary.Register(types.BinaryOpGt, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) { return a > b, nil }})
	Binary.Register(types.BinaryOpGt, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a > b, nil }})
	Binary.Register(types.BinaryOpGt, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a > b, nil }})
	Binary.Register(types.BinaryOpGt, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a > b, nil }})
	// Functions for [types.BinaryOpGte]
	Binary.Register(types.BinaryOpGte, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) >= boolToInt(b), nil }})
	Binary.Register(types.BinaryOpGte, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) { return a >= b, nil }})
	Binary.Register(types.BinaryOpGte, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a >= b, nil }})
	Binary.Register(types.BinaryOpGte, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a >= b, nil }})
	Binary.Register(types.BinaryOpGte, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a >= b, nil }})
	// Functions for [types.BinaryOpLt]
	Binary.Register(types.BinaryOpLt, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) < boolToInt(b), nil }})
	Binary.Register(types.BinaryOpLt, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) { return a < b, nil }})
	Binary.Register(types.BinaryOpLt, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a < b, nil }})
	Binary.Register(types.BinaryOpLt, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a < b, nil }})
	Binary.Register(types.BinaryOpLt, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a < b, nil }})
	// Functions for [types.BinaryOpLte]
	Binary.Register(types.BinaryOpLte, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return boolToInt(a) <= boolToInt(b), nil }})
	Binary.Register(types.BinaryOpLte, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) { return a <= b, nil }})
	Binary.Register(types.BinaryOpLte, arrow.PrimitiveTypes.Int64, &genericBoolFunction[*array.Int64, int64]{eval: func(a, b int64) (bool, error) { return a <= b, nil }})
	Binary.Register(types.BinaryOpLte, arrow.FixedWidthTypes.Timestamp_ns, &genericBoolFunction[*array.Timestamp, arrow.Timestamp]{eval: func(a, b arrow.Timestamp) (bool, error) { return a <= b, nil }})
	Binary.Register(types.BinaryOpLte, arrow.PrimitiveTypes.Float64, &genericBoolFunction[*array.Float64, float64]{eval: func(a, b float64) (bool, error) { return a <= b, nil }})
	// Functions for [types.BinaryOpAnd]
	Binary.Register(types.BinaryOpAnd, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a && b, nil }})
	// Functions for [types.BinaryOpOr]
	Binary.Register(types.BinaryOpOr, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a || b, nil }})
	// Functions for [types.BinaryOpXor]
	Binary.Register(types.BinaryOpXor, arrow.FixedWidthTypes.Boolean, &genericBoolFunction[*array.Boolean, bool]{eval: func(a, b bool) (bool, error) { return a != b, nil }})
	// Functions for [types.BinaryOpMatchSubstr]
	Binary.Register(types.BinaryOpMatchSubstr, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) { return strings.Contains(a, b), nil }})
	// Functions for [types.BinaryOpNotMatchSubstr]
	Binary.Register(types.BinaryOpNotMatchSubstr, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) { return !strings.Contains(a, b), nil }})
	// Functions for [types.BinaryOpMatchRe]
	Binary.Register(types.BinaryOpMatchRe, arrow.BinaryTypes.String, &regexpFunction{eval: func(a, b string, reg *regexp.Regexp) (bool, error) {
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
	Binary.Register(types.BinaryOpNotMatchRe, arrow.BinaryTypes.String, &regexpFunction{eval: func(a, b string, reg *regexp.Regexp) (bool, error) {
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

	// Functions for [types.BinaryOpEqCaseInsensitive]
	Binary.Register(types.BinaryOpEqCaseInsensitive, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) {
		return matchutil.EqualUpper([]byte(a), []byte(b)), nil
	}})
	// Functions for [types.BinaryOpNotEqCaseInsensitive]
	Binary.Register(types.BinaryOpNotEqCaseInsensitive, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) {
		return !matchutil.EqualUpper([]byte(a), []byte(b)), nil
	}})
	// Functions for [types.BinaryOpMatchSubstrCaseInsensitive]
	Binary.Register(types.BinaryOpMatchSubstrCaseInsensitive, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) {
		return matchutil.ContainsUpper([]byte(a), []byte(b)), nil
	}})
	// Functions for [types.BinaryOpNotMatchSubstrCaseInsensitive]
	Binary.Register(types.BinaryOpNotMatchSubstrCaseInsensitive, arrow.BinaryTypes.String, &genericBoolFunction[*array.String, string]{nullAsZero: true, eval: func(a, b string) (bool, error) {
		return !matchutil.ContainsUpper([]byte(a), []byte(b)), nil
	}})

	// Functions for [types.UnaryOpNot]
	Unary.Register(types.UnaryOpNot, arrow.FixedWidthTypes.Boolean, UnaryFunc(func(input arrow.Array) (arrow.Array, error) {
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
}

type UnaryFunctionRegistry interface {
	// Register registers a function implementation for the given operation
	// and operand type. It panics if the signature is not declared in the
	// signature catalog.
	Register(types.UnaryOp, arrow.DataType, UnaryFunction)
	GetForSignature(types.UnaryOp, arrow.DataType) (UnaryFunction, error)
}

type UnaryFunction interface {
	Evaluate(lhs arrow.Array) (arrow.Array, error)
}

type UnaryFunc func(arrow.Array) (arrow.Array, error)

func (f UnaryFunc) Evaluate(lhs arrow.Array) (arrow.Array, error) {
	return f(lhs)
}

// Registries are keyed by [arrow.Type] (the type ID) rather than by
// [arrow.DataType] instances. Interface map keys compare by pointer identity
// for the pointer-typed Arrow data types, and while the zero-size types
// (string, int64, float64, boolean) coincidentally share a single instance,
// parameterized types like [arrow.TimestampType] do not: a timestamp column
// deserialized from IPC or built with a fresh &arrow.TimestampType{...} would
// never match the registered singleton, failing the lookup for a signature
// that exists. Keying by ID uses the same equality as the operand type check
// and the signature catalog. Timestamps are always nanosecond/UTC in the
// engine, so collapsing unit and timezone into the ID is safe.
type unaryFuncReg struct {
	reg map[types.UnaryOp]map[arrow.Type]UnaryFunction
}

// Register implements UnaryFunctionRegistry.
func (u *unaryFuncReg) Register(op types.UnaryOp, ltype arrow.DataType, f UnaryFunction) {
	if _, err := UnaryReturnType(op, dataTypeForArrowType(ltype)); err != nil {
		panic(fmt.Sprintf("unary function registration for signature %v(%v) is not declared in the signature catalog", op, ltype))
	}
	if u.reg == nil {
		u.reg = make(map[types.UnaryOp]map[arrow.Type]UnaryFunction)
	}
	if _, ok := u.reg[op]; !ok {
		u.reg[op] = make(map[arrow.Type]UnaryFunction)
	}
	// TODO(chaudum): Should the function panic when duplicate keys are registered?
	u.reg[op][ltype.ID()] = f
}

// GetForSignature implements UnaryFunctionRegistry.
func (u *unaryFuncReg) GetForSignature(op types.UnaryOp, ltype arrow.DataType) (UnaryFunction, error) {
	// Get registered functions for the specific operation
	reg, ok := u.reg[op]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	// Get registered function for the specific data type
	fn, ok := reg[ltype.ID()]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	return fn, nil
}

type BinaryFunctionRegistry interface {
	// Register registers a function implementation for the given operation
	// and operand type. It panics if the signature is not declared in the
	// signature catalog.
	Register(types.BinaryOp, arrow.DataType, BinaryFunction)
	GetForSignature(types.BinaryOp, arrow.DataType) (BinaryFunction, error)
}

type BinaryFunction interface {
	Evaluate(lhs, rhs arrow.Array, lhsIsScalar, rhsIsScalar bool) (arrow.Array, error)
}

// See the comment on [unaryFuncReg] for why the registry is keyed by
// [arrow.Type] instead of [arrow.DataType].
type binaryFuncReg struct {
	reg map[types.BinaryOp]map[arrow.Type]BinaryFunction
}

// Register implements BinaryFunctionRegistry.
func (b *binaryFuncReg) Register(op types.BinaryOp, ty arrow.DataType, f BinaryFunction) {
	if _, err := BinaryReturnType(op, dataTypeForArrowType(ty), dataTypeForArrowType(ty)); err != nil {
		panic(fmt.Sprintf("binary function registration for signature %v(%v,%v) is not declared in the signature catalog", op, ty, ty))
	}
	if b.reg == nil {
		b.reg = make(map[types.BinaryOp]map[arrow.Type]BinaryFunction)
	}
	if _, ok := b.reg[op]; !ok {
		b.reg[op] = make(map[arrow.Type]BinaryFunction)
	}
	// TODO(chaudum): Should the function panic when duplicate keys are registered?
	b.reg[op][ty.ID()] = f
}

// GetForSignature implements BinaryFunctionRegistry.
func (b *binaryFuncReg) GetForSignature(op types.BinaryOp, ltype arrow.DataType) (BinaryFunction, error) {
	// Get registered functions for the specific operation
	reg, ok := b.reg[op]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	// Get registered function for the specific data type
	fn, ok := reg[ltype.ID()]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	return fn, nil
}

// dataTypeForArrowType returns the [types.DataType] whose underlying
// representation matches the given Arrow type. It is used to validate
// registrations against the signature catalog, which is keyed by [types.Type].
func dataTypeForArrowType(ty arrow.DataType) types.DataType {
	switch types.Type(ty.ID()) {
	case types.NULL:
		return types.Loki.Null
	case types.BOOL:
		return types.Loki.Bool
	case types.STRING:
		return types.Loki.String
	case types.INT64:
		return types.Loki.Integer
	case types.FLOAT64:
		return types.Loki.Float
	case types.TIMESTAMP:
		return types.Loki.Timestamp
	default:
		return types.Loki.Null
	}
}

// coerceNullEntries returns the pair of values to compare at row i, applying
// the LogQL filter null-handling rule: when the comparison is column-vs-literal
// (one side is a scalar), a null on the non-scalar side is treated as the zero
// value of T. For strings that is "", which matches v1's behaviour where an
// absent label compares as "" — so e.g. `| foo=""` and `| foo=~".*"` both match
// rows where foo is absent. If neither side is a scalar and at least one is
// null, returns skip=true so the caller preserves the existing null
// short-circuit (no LogQL semantic applies to that shape).
//
// Only enable this via [genericBoolFunction.nullAsZero] for string-typed
// registrations. For numeric LogQL ops, an absent unwrapped value is dropped
// rather than coerced to zero, so the old null short-circuit is still correct.
func coerceNullEntries[E arrow.TypedArray[T], T arrow.ValueType](lhs, rhs E, i int, lhsIsScalar, rhsIsScalar bool) (a, b T, skip bool) {
	lhsNull := lhs.IsNull(i)
	rhsNull := rhs.IsNull(i)
	var zero T
	switch {
	case lhsNull && rhsIsScalar:
		return zero, rhs.Value(i), false
	case rhsNull && lhsIsScalar:
		return lhs.Value(i), zero, false
	case lhsNull || rhsNull:
		return zero, zero, true
	default:
		return lhs.Value(i), rhs.Value(i), false
	}
}

type regexpFunction struct {
	eval func(a, b string, reg *regexp.Regexp) (bool, error)
}

func (f *regexpFunction) Evaluate(lhs arrow.Array, rhs arrow.Array, lhsIsScalar, rhsIsScalar bool) (arrow.Array, error) {
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

	builder.Reserve(lhsArr.Len())

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
		a, b, skip := coerceNullEntries(lhsArr, rhsArr, i, lhsIsScalar, rhsIsScalar)
		if skip {
			builder.Append(false)
			continue
		}
		res, err := f.eval(a, b, re)
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
	// nullAsZero, when true, applies LogQL filter null semantics: in the
	// column-vs-literal shape, a null on the column side is treated as the
	// zero value of T (i.e. "" for strings). Set on registrations for
	// string-typed label/line filter comparators. Leave false for numeric
	// comparisons, where LogQL drops the row rather than coercing.
	nullAsZero bool
}

// Evaluate implements BinaryFunction.
func (f *genericBoolFunction[E, T]) Evaluate(lhs arrow.Array, rhs arrow.Array, lhsIsScalar, rhsIsScalar bool) (arrow.Array, error) {
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
	builder.Reserve(lhsArr.Len())

	for i := range lhsArr.Len() {
		var a, b T
		var skip bool
		if f.nullAsZero {
			a, b, skip = coerceNullEntries(lhsArr, rhsArr, i, lhsIsScalar, rhsIsScalar)
		} else if lhsArr.IsNull(i) || rhsArr.IsNull(i) {
			skip = true
		} else {
			a, b = lhsArr.Value(i), rhsArr.Value(i)
		}
		if skip {
			builder.Append(false)
			continue
		}
		res, err := f.eval(a, b)
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
	builder.Reserve(lhsArr.Len())

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
	// Register registers a function implementation for the given operation.
	// It panics if the signature is not declared in the signature catalog.
	Register(types.VariadicOp, VariadicFunction)
	GetForSignature(types.VariadicOp) (VariadicFunction, error)
}

type VariadicFunction interface {
	Evaluate(input arrow.RecordBatch, args ...arrow.Array) (arrow.Array, error)
}

type VariadicFunctionFunc func(input arrow.RecordBatch, args ...arrow.Array) (arrow.Array, error)

func (f VariadicFunctionFunc) Evaluate(input arrow.RecordBatch, args ...arrow.Array) (arrow.Array, error) {
	return f(input, args...)
}

type variadicFuncReg struct {
	reg map[types.VariadicOp]VariadicFunction
}

// Register implements VariadicFunctionRegistry.
func (u *variadicFuncReg) Register(op types.VariadicOp, f VariadicFunction) {
	if !HasVariadic(op) {
		panic(fmt.Sprintf("variadic function registration for %s is not declared in the signature catalog", op))
	}
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
