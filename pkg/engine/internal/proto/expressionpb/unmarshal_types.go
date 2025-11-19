package expressionpb

import (
	fmt "fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	protoUnaryOpLookup = map[types.UnaryOp]UnaryOp{
		types.UnaryOpInvalid:      UNARY_OP_INVALID,
		types.UnaryOpNot:          UNARY_OP_NOT,
		types.UnaryOpAbs:          UNARY_OP_ABS,
		types.UnaryOpCastFloat:    UNARY_OP_CAST_FLOAT,
		types.UnaryOpCastBytes:    UNARY_OP_CAST_BYTES,
		types.UnaryOpCastDuration: UNARY_OP_CAST_DURATION,
	}

	protoBinaryOpLookup = map[types.BinaryOp]BinaryOp{
		types.BinaryOpInvalid:         BINARY_OP_INVALID,
		types.BinaryOpEq:              BINARY_OP_EQ,
		types.BinaryOpNeq:             BINARY_OP_NEQ,
		types.BinaryOpGt:              BINARY_OP_GT,
		types.BinaryOpGte:             BINARY_OP_GTE,
		types.BinaryOpLt:              BINARY_OP_LT,
		types.BinaryOpLte:             BINARY_OP_LTE,
		types.BinaryOpAnd:             BINARY_OP_AND,
		types.BinaryOpOr:              BINARY_OP_OR,
		types.BinaryOpXor:             BINARY_OP_XOR,
		types.BinaryOpAdd:             BINARY_OP_ADD,
		types.BinaryOpSub:             BINARY_OP_SUB,
		types.BinaryOpMul:             BINARY_OP_MUL,
		types.BinaryOpDiv:             BINARY_OP_DIV,
		types.BinaryOpMod:             BINARY_OP_MOD,
		types.BinaryOpPow:             BINARY_OP_POW,
		types.BinaryOpMatchSubstr:     BINARY_OP_MATCH_SUBSTR,
		types.BinaryOpNotMatchSubstr:  BINARY_OP_NOT_MATCH_SUBSTR,
		types.BinaryOpMatchRe:         BINARY_OP_MATCH_RE,
		types.BinaryOpNotMatchRe:      BINARY_OP_NOT_MATCH_RE,
		types.BinaryOpMatchPattern:    BINARY_OP_MATCH_PATTERN,
		types.BinaryOpNotMatchPattern: BINARY_OP_NOT_MATCH_PATTERN,
	}

	protoVariadicOpLookup = map[types.VariadicOp]VariadicOp{
		types.VariadicOpInvalid:     VARIADIC_OP_INVALID,
		types.VariadicOpParseLogfmt: VARIADIC_OP_PARSE_LOGFMT,
		types.VariadicOpParseJSON:   VARIADIC_OP_PARSE_JSON,
	}

	protoColumnTypeLookup = map[types.ColumnType]ColumnType{
		types.ColumnTypeInvalid:   COLUMN_TYPE_INVALID,
		types.ColumnTypeBuiltin:   COLUMN_TYPE_BUILTIN,
		types.ColumnTypeLabel:     COLUMN_TYPE_LABEL,
		types.ColumnTypeMetadata:  COLUMN_TYPE_METADATA,
		types.ColumnTypeParsed:    COLUMN_TYPE_PARSED,
		types.ColumnTypeAmbiguous: COLUMN_TYPE_AMBIGUOUS,
		types.ColumnTypeGenerated: COLUMN_TYPE_GENERATED,
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
