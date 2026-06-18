package expressionpb

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/engine/internal/proto/testutils"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// The tests in this file guard against silent drift between the proto enums
// declared in expressionpb.pb.go, the corresponding types.* enums declared in
// pkg/engine/internal/types, and the lookup tables in marshal_types.go /
// unmarshal_types.go.
//
// Whenever a new value is added to either the proto enum or the types.* enum
// but the lookup tables are not updated, conversions silently fail at runtime
// (MarshalType / UnmarshalType return "unsupported" errors that callers may
// ignore or surface poorly). These tests catch that drift at build time.

func TestLookupCompleteness_UnaryOp(t *testing.T) {
	testutils.CheckEnumLookup(t,
		"UnaryOp",
		UnaryOp_name,
		testutils.CountConstantsOfType(t, "../../types/operators.go", "UnaryOp"),
		nativeUnaryOpLookup,
		protoUnaryOpLookup,
		func(v int32) UnaryOp { return UnaryOp(v) },
		func(v UnaryOp) (types.UnaryOp, error) { return v.MarshalType() },
		func(v types.UnaryOp) (UnaryOp, error) {
			var p UnaryOp
			return p, p.UnmarshalType(v)
		},
	)
}

func TestLookupCompleteness_BinaryOp(t *testing.T) {
	testutils.CheckEnumLookup(t,
		"BinaryOp",
		BinaryOp_name,
		testutils.CountConstantsOfType(t, "../../types/operators.go", "BinaryOp"),
		nativeBinaryOpLookup,
		protoBinaryOpLookup,
		func(v int32) BinaryOp { return BinaryOp(v) },
		func(v BinaryOp) (types.BinaryOp, error) { return v.MarshalType() },
		func(v types.BinaryOp) (BinaryOp, error) {
			var p BinaryOp
			return p, p.UnmarshalType(v)
		},
	)
}

func TestLookupCompleteness_VariadicOp(t *testing.T) {
	testutils.CheckEnumLookup(t,
		"VariadicOp",
		VariadicOp_name,
		testutils.CountConstantsOfType(t, "../../types/operators.go", "VariadicOp"),
		nativeVariadicOpLookup,
		protoVariadicOpLookup,
		func(v int32) VariadicOp { return VariadicOp(v) },
		func(v VariadicOp) (types.VariadicOp, error) { return v.MarshalType() },
		func(v types.VariadicOp) (VariadicOp, error) {
			var p VariadicOp
			return p, p.UnmarshalType(v)
		},
	)
}

func TestLookupCompleteness_ColumnType(t *testing.T) {
	testutils.CheckEnumLookup(t,
		"ColumnType",
		ColumnType_name,
		testutils.CountConstantsOfType(t, "../../types/column.go", "ColumnType"),
		nativeColumnTypeLookup,
		protoColumnTypeLookup,
		func(v int32) ColumnType { return ColumnType(v) },
		func(v ColumnType) (types.ColumnType, error) { return v.MarshalType() },
		func(v types.ColumnType) (ColumnType, error) {
			var p ColumnType
			return p, p.UnmarshalType(v)
		},
	)
}
