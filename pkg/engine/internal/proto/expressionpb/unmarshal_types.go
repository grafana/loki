package expressionpb

import (
	fmt "fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	protoUnaryOpLookup = map[types.UnaryOp]UnaryOp{
		types.UnaryOpInvalid:      UnaryOp_UNARY_OP_INVALID,
		types.UnaryOpNot:          UnaryOp_UNARY_OP_NOT,
		types.UnaryOpAbs:          UnaryOp_UNARY_OP_ABS,
		types.UnaryOpCastFloat:    UnaryOp_UNARY_OP_CAST_FLOAT,
		types.UnaryOpCastBytes:    UnaryOp_UNARY_OP_CAST_BYTES,
		types.UnaryOpCastDuration: UnaryOp_UNARY_OP_CAST_DURATION,
	}

	protoBinaryOpLookup = map[types.BinaryOp]BinaryOp{
		types.BinaryOpInvalid:                       BinaryOp_BINARY_OP_INVALID,
		types.BinaryOpEq:                            BinaryOp_BINARY_OP_EQ,
		types.BinaryOpNeq:                           BinaryOp_BINARY_OP_NEQ,
		types.BinaryOpGt:                            BinaryOp_BINARY_OP_GT,
		types.BinaryOpGte:                           BinaryOp_BINARY_OP_GTE,
		types.BinaryOpLt:                            BinaryOp_BINARY_OP_LT,
		types.BinaryOpLte:                           BinaryOp_BINARY_OP_LTE,
		types.BinaryOpAnd:                           BinaryOp_BINARY_OP_AND,
		types.BinaryOpOr:                            BinaryOp_BINARY_OP_OR,
		types.BinaryOpXor:                           BinaryOp_BINARY_OP_XOR,
		types.BinaryOpAdd:                           BinaryOp_BINARY_OP_ADD,
		types.BinaryOpSub:                           BinaryOp_BINARY_OP_SUB,
		types.BinaryOpMul:                           BinaryOp_BINARY_OP_MUL,
		types.BinaryOpDiv:                           BinaryOp_BINARY_OP_DIV,
		types.BinaryOpMod:                           BinaryOp_BINARY_OP_MOD,
		types.BinaryOpPow:                           BinaryOp_BINARY_OP_POW,
		types.BinaryOpMatchSubstr:                   BinaryOp_BINARY_OP_MATCH_SUBSTR,
		types.BinaryOpNotMatchSubstr:                BinaryOp_BINARY_OP_NOT_MATCH_SUBSTR,
		types.BinaryOpMatchRe:                       BinaryOp_BINARY_OP_MATCH_RE,
		types.BinaryOpNotMatchRe:                    BinaryOp_BINARY_OP_NOT_MATCH_RE,
		types.BinaryOpMatchPattern:                  BinaryOp_BINARY_OP_MATCH_PATTERN,
		types.BinaryOpNotMatchPattern:               BinaryOp_BINARY_OP_NOT_MATCH_PATTERN,
		types.BinaryOpEqCaseInsensitive:             BinaryOp_BINARY_OP_EQ_CASE_INSENSITIVE,
		types.BinaryOpNotEqCaseInsensitive:          BinaryOp_BINARY_OP_NEQ_CASE_INSENSITIVE,
		types.BinaryOpMatchSubstrCaseInsensitive:    BinaryOp_BINARY_OP_MATCH_SUBSTR_CASE_INSENSITIVE,
		types.BinaryOpNotMatchSubstrCaseInsensitive: BinaryOp_BINARY_OP_NOT_MATCH_SUBSTR_CASE_INSENSITIVE,
	}

	protoVariadicOpLookup = map[types.VariadicOp]VariadicOp{
		types.VariadicOpInvalid:       VariadicOp_VARIADIC_OP_INVALID,
		types.VariadicOpParseLogfmt:   VariadicOp_VARIADIC_OP_PARSE_LOGFMT,
		types.VariadicOpParseJSON:     VariadicOp_VARIADIC_OP_PARSE_JSON,
		types.VariadicOpParseRegexp:   VariadicOp_VARIADIC_OP_PARSE_REGEXP,
		types.VariadicOpParseLabelfmt: VariadicOp_VARIADIC_OP_PARSE_LABELFMT,
		types.VariadicOpParseLinefmt:  VariadicOp_VARIADIC_OP_PARSE_LINEFMT,
	}

	protoColumnTypeLookup = map[types.ColumnType]ColumnType{
		types.ColumnTypeInvalid:   ColumnType_COLUMN_TYPE_INVALID,
		types.ColumnTypeBuiltin:   ColumnType_COLUMN_TYPE_BUILTIN,
		types.ColumnTypeLabel:     ColumnType_COLUMN_TYPE_LABEL,
		types.ColumnTypeMetadata:  ColumnType_COLUMN_TYPE_METADATA,
		types.ColumnTypeParsed:    ColumnType_COLUMN_TYPE_PARSED,
		types.ColumnTypeAmbiguous: ColumnType_COLUMN_TYPE_AMBIGUOUS,
		types.ColumnTypeGenerated: ColumnType_COLUMN_TYPE_GENERATED,
	}
)

// UnmarshalType converts an internal types.UnaryOp to its protobuf UnaryOp representation.
// Returns an error if the conversion fails or the operator is unsupported.
func (o *UnaryOp) UnmarshalType(from types.UnaryOp) error {
	out, ok := protoUnaryOpLookup[from]
	if !ok {
		return fmt.Errorf("unsupported unary operator %s", from)
	}
	*o = out
	return nil
}

// UnmarshalType converts an internal types.BinaryOp to its protobuf BinaryOp representation.
// Returns an error if the conversion fails or the operator is unsupported.
func (o *BinaryOp) UnmarshalType(from types.BinaryOp) error {
	out, ok := protoBinaryOpLookup[from]
	if !ok {
		return fmt.Errorf("unsupported binary operator %s", from)
	}
	*o = out
	return nil
}

// UnmarshalType converts an internal types.VariadicOp to its protobuf VariadicOp representation.
// Returns an error if the conversion fails or the operator is unsupported.
func (o *VariadicOp) UnmarshalType(from types.VariadicOp) error {
	out, ok := protoVariadicOpLookup[from]
	if !ok {
		return fmt.Errorf("unsupported variadic operator %s", from)
	}
	*o = out
	return nil
}

// UnmarshalType converts an internal types.ColumnType to its protobuf ColumnType representation.
// Returns an error if the conversion fails or the column type is unsupported.
func (t *ColumnType) UnmarshalType(from types.ColumnType) error {
	out, ok := protoColumnTypeLookup[from]
	if !ok {
		return fmt.Errorf("unsupported column type %s", from)
	}
	*t = out
	return nil
}
