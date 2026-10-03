package functions

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/errors"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// This file declares the signature catalog: the single source of truth for
// which (operation, operand type) combinations have a function implementation,
// and what data type the function returns.
//
// The catalog serves two consumers:
//
//   - The physical planner uses it to type-check expressions at plan time,
//     so that invalid signatures fail before the query touches storage.
//   - The function registries in this package (and the executor, which
//     registers the cast and parse implementations) validate every
//     registration against the catalog, so that catalog and implementations
//     cannot drift apart.
//
// Signatures are keyed by [types.Type] rather than [types.DataType] because
// function dispatch at execution time is based on the underlying Arrow type:
// Loki types that share an Arrow representation (integer, duration, and bytes
// are all INT64) share a single function implementation.

// unarySignatures maps a unary operation and its operand type to the type of
// the value produced by the operation.
var unarySignatures = map[types.UnaryOp]map[types.Type]types.DataType{
	types.UnaryOpNot: {
		types.BOOL: types.Loki.Bool,
	},
	// The cast operations produce a float64 value column. At execution time
	// the implementation may additionally attach error columns for rows that
	// failed to convert, but the value itself is always a float.
	types.UnaryOpCastFloat: {
		types.STRING: types.Loki.Float,
	},
	types.UnaryOpCastBytes: {
		types.STRING: types.Loki.Float,
	},
	types.UnaryOpCastDuration: {
		types.STRING: types.Loki.Float,
	},
}

// binarySignatures maps a binary operation and its operand type (both
// operands must have the same type) to the type of the value produced by the
// operation.
var binarySignatures = map[types.BinaryOp]map[types.Type]types.DataType{
	// Arithmetic operations always produce a float64 value.
	types.BinaryOpDiv: numericArithmeticSignature(),
	types.BinaryOpAdd: numericArithmeticSignature(),
	types.BinaryOpSub: numericArithmeticSignature(),
	types.BinaryOpMul: numericArithmeticSignature(),
	types.BinaryOpMod: numericArithmeticSignature(),
	types.BinaryOpPow: numericArithmeticSignature(),

	// Comparison operations are defined for all scalar types.
	types.BinaryOpEq:  comparisonSignature(),
	types.BinaryOpNeq: comparisonSignature(),
	types.BinaryOpGt:  comparisonSignature(),
	types.BinaryOpGte: comparisonSignature(),
	types.BinaryOpLt:  comparisonSignature(),
	types.BinaryOpLte: comparisonSignature(),

	// Logical operations are only defined for booleans.
	types.BinaryOpAnd: {types.BOOL: types.Loki.Bool},
	types.BinaryOpOr:  {types.BOOL: types.Loki.Bool},
	types.BinaryOpXor: {types.BOOL: types.Loki.Bool},

	// String matching operations are only defined for strings.
	types.BinaryOpMatchSubstr:                   stringMatchSignature(),
	types.BinaryOpNotMatchSubstr:                stringMatchSignature(),
	types.BinaryOpMatchRe:                       stringMatchSignature(),
	types.BinaryOpNotMatchRe:                    stringMatchSignature(),
	types.BinaryOpEqCaseInsensitive:             stringMatchSignature(),
	types.BinaryOpNotEqCaseInsensitive:          stringMatchSignature(),
	types.BinaryOpMatchSubstrCaseInsensitive:    stringMatchSignature(),
	types.BinaryOpNotMatchSubstrCaseInsensitive: stringMatchSignature(),

	// [types.BinaryOpMatchPattern] and [types.BinaryOpNotMatchPattern] have
	// no expression function implementation and are intentionally absent.
}

// variadicSignatures is the set of variadic operations that have a function
// implementation. Variadic functions accept an arbitrary number of arguments
// and produce a set of new columns, so neither their argument types nor their
// return type are represented in the catalog.
var variadicSignatures = map[types.VariadicOp]struct{}{
	types.VariadicOpParseLogfmt:   {},
	types.VariadicOpParseJSON:     {},
	types.VariadicOpParseRegexp:   {},
	types.VariadicOpParseLabelfmt: {},
	types.VariadicOpParseLinefmt:  {},
}

func numericArithmeticSignature() map[types.Type]types.DataType {
	return map[types.Type]types.DataType{
		types.FLOAT64: types.Loki.Float,
		types.INT64:   types.Loki.Float,
	}
}

func comparisonSignature() map[types.Type]types.DataType {
	return map[types.Type]types.DataType{
		types.BOOL:      types.Loki.Bool,
		types.STRING:    types.Loki.Bool,
		types.INT64:     types.Loki.Bool,
		types.TIMESTAMP: types.Loki.Bool,
		types.FLOAT64:   types.Loki.Bool,
	}
}

func stringMatchSignature() map[types.Type]types.DataType {
	return map[types.Type]types.DataType{
		types.STRING: types.Loki.Bool,
	}
}

// UnaryReturnType returns the data type of the value produced by applying op
// to an operand of the given type. It returns [errors.ErrNotImplemented] if
// no function implementation exists for the signature.
func UnaryReturnType(op types.UnaryOp, operand types.DataType) (types.DataType, error) {
	sig, ok := unarySignatures[op]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	ret, ok := sig[operand.ID()]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	return ret, nil
}

// BinaryReturnType returns the data type of the value produced by applying op
// to operands of the given types. It returns an error if the operand types do
// not match, or [errors.ErrNotImplemented] if no function implementation
// exists for the signature.
//
// Operand types match when they share the same underlying representation:
// integer, duration, and bytes values all compare as INT64.
func BinaryReturnType(op types.BinaryOp, lhs, rhs types.DataType) (types.DataType, error) {
	// At the moment we only support functions that accept the same input types.
	if lhs.ID() != rhs.ID() {
		return nil, fmt.Errorf("types do not match")
	}
	sig, ok := binarySignatures[op]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	ret, ok := sig[lhs.ID()]
	if !ok {
		return nil, errors.ErrNotImplemented
	}
	return ret, nil
}

// HasVariadic reports whether a function implementation exists for the given
// variadic operation.
func HasVariadic(op types.VariadicOp) bool {
	_, ok := variadicSignatures[op]
	return ok
}
