package expressionpb

import (
	fmt "fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	nativeUnaryOpLookup = map[UnaryOp]types.UnaryOp{
		UnaryOp_UNARY_OP_INVALID:       types.UnaryOpInvalid,
		UnaryOp_UNARY_OP_NOT:           types.UnaryOpNot,
		UnaryOp_UNARY_OP_ABS:           types.UnaryOpAbs,
		UnaryOp_UNARY_OP_CAST_FLOAT:    types.UnaryOpCastFloat,
		UnaryOp_UNARY_OP_CAST_BYTES:    types.UnaryOpCastBytes,
		UnaryOp_UNARY_OP_CAST_DURATION: types.UnaryOpCastDuration,
	}

	nativeBinaryOpLookup = map[BinaryOp]types.BinaryOp{
		BinaryOp_BINARY_OP_INVALID:                           types.BinaryOpInvalid,
		BinaryOp_BINARY_OP_EQ:                                types.BinaryOpEq,
		BinaryOp_BINARY_OP_NEQ:                               types.BinaryOpNeq,
		BinaryOp_BINARY_OP_GT:                                types.BinaryOpGt,
		BinaryOp_BINARY_OP_GTE:                               types.BinaryOpGte,
		BinaryOp_BINARY_OP_LT:                                types.BinaryOpLt,
		BinaryOp_BINARY_OP_LTE:                               types.BinaryOpLte,
		BinaryOp_BINARY_OP_AND:                               types.BinaryOpAnd,
		BinaryOp_BINARY_OP_OR:                                types.BinaryOpOr,
		BinaryOp_BINARY_OP_XOR:                               types.BinaryOpXor,
		BinaryOp_BINARY_OP_ADD:                               types.BinaryOpAdd,
		BinaryOp_BINARY_OP_SUB:                               types.BinaryOpSub,
		BinaryOp_BINARY_OP_MUL:                               types.BinaryOpMul,
		BinaryOp_BINARY_OP_DIV:                               types.BinaryOpDiv,
		BinaryOp_BINARY_OP_MOD:                               types.BinaryOpMod,
		BinaryOp_BINARY_OP_POW:                               types.BinaryOpPow,
		BinaryOp_BINARY_OP_MATCH_SUBSTR:                      types.BinaryOpMatchSubstr,
		BinaryOp_BINARY_OP_NOT_MATCH_SUBSTR:                  types.BinaryOpNotMatchSubstr,
		BinaryOp_BINARY_OP_MATCH_RE:                          types.BinaryOpMatchRe,
		BinaryOp_BINARY_OP_NOT_MATCH_RE:                      types.BinaryOpNotMatchRe,
		BinaryOp_BINARY_OP_MATCH_PATTERN:                     types.BinaryOpMatchPattern,
		BinaryOp_BINARY_OP_NOT_MATCH_PATTERN:                 types.BinaryOpNotMatchPattern,
		BinaryOp_BINARY_OP_EQ_CASE_INSENSITIVE:               types.BinaryOpEqCaseInsensitive,
		BinaryOp_BINARY_OP_NEQ_CASE_INSENSITIVE:              types.BinaryOpNotEqCaseInsensitive,
		BinaryOp_BINARY_OP_MATCH_SUBSTR_CASE_INSENSITIVE:     types.BinaryOpMatchSubstrCaseInsensitive,
		BinaryOp_BINARY_OP_NOT_MATCH_SUBSTR_CASE_INSENSITIVE: types.BinaryOpNotMatchSubstrCaseInsensitive,
	}

	nativeVariadicOpLookup = map[VariadicOp]types.VariadicOp{
		VariadicOp_VARIADIC_OP_INVALID:        types.VariadicOpInvalid,
		VariadicOp_VARIADIC_OP_PARSE_LOGFMT:   types.VariadicOpParseLogfmt,
		VariadicOp_VARIADIC_OP_PARSE_JSON:     types.VariadicOpParseJSON,
		VariadicOp_VARIADIC_OP_PARSE_LABELFMT: types.VariadicOpParseLabelfmt,
		VariadicOp_VARIADIC_OP_PARSE_LINEFMT:  types.VariadicOpParseLinefmt,
	}

	nativeColumnTypeLookup = map[ColumnType]types.ColumnType{
		ColumnType_COLUMN_TYPE_INVALID:   types.ColumnTypeInvalid,
		ColumnType_COLUMN_TYPE_BUILTIN:   types.ColumnTypeBuiltin,
		ColumnType_COLUMN_TYPE_LABEL:     types.ColumnTypeLabel,
		ColumnType_COLUMN_TYPE_METADATA:  types.ColumnTypeMetadata,
		ColumnType_COLUMN_TYPE_PARSED:    types.ColumnTypeParsed,
		ColumnType_COLUMN_TYPE_AMBIGUOUS: types.ColumnTypeAmbiguous,
		ColumnType_COLUMN_TYPE_GENERATED: types.ColumnTypeGenerated,
	}
)

// MarshalType converts a protobuf UnaryOp to its internal types.UnaryOp representation.
// Returns an error if the conversion fails or the operator is unsupported.
func (o UnaryOp) MarshalType() (types.UnaryOp, error) {
	out, ok := nativeUnaryOpLookup[o]
	if !ok {
		return types.UnaryOpInvalid, fmt.Errorf("unsupported unary operator %s", o)
	}
	return out, nil
}

// MarshalType converts a protobuf BinaryOp to its internal types.BinaryOp representation.
// Returns an error if the conversion fails or the operator is unsupported.
func (o BinaryOp) MarshalType() (types.BinaryOp, error) {
	out, ok := nativeBinaryOpLookup[o]
	if !ok {
		return types.BinaryOpInvalid, fmt.Errorf("unsupported binary operator %s", o)
	}
	return out, nil
}

// MarshalType converts a protobuf VariadicOp to its internal types.VariadicOp representation.
// Returns an error if the conversion fails or the operator is unsupported.
func (o VariadicOp) MarshalType() (types.VariadicOp, error) {
	out, ok := nativeVariadicOpLookup[o]
	if !ok {
		return types.VariadicOpInvalid, fmt.Errorf("unsupported variadic operator %s", o)
	}
	return out, nil
}

// MarshalType converts a protobuf ColumnType to its internal types.ColumnType representation.
// Returns an error if the conversion fails or the column type is unsupported.
func (t ColumnType) MarshalType() (types.ColumnType, error) {
	out, ok := nativeColumnTypeLookup[t]
	if !ok {
		return types.ColumnTypeInvalid, fmt.Errorf("unsupported column type %s", t)
	}
	return out, nil
}
