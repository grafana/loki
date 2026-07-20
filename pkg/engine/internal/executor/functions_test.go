package executor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/functions"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// TestFunctionRegistryMatchesSignatureCatalog verifies that the function
// registries and the signature catalog agree for every operation and operand
// type once the cast and parse implementations of this package are
// registered: a signature is declared in the catalog if and only if an
// implementation can be resolved for it. The physical planner relies on this
// equivalence to type-check expressions at plan time.
func TestFunctionRegistryMatchesSignatureCatalog(t *testing.T) {
	dataTypes := []types.DataType{
		types.Loki.Bool,
		types.Loki.String,
		types.Loki.Integer,
		types.Loki.Float,
		types.Loki.Timestamp,
		types.Loki.Duration,
		types.Loki.Bytes,
	}

	unaryOps := []types.UnaryOp{
		types.UnaryOpNot,
		types.UnaryOpAbs,
		types.UnaryOpCastFloat,
		types.UnaryOpCastBytes,
		types.UnaryOpCastDuration,
	}
	for _, op := range unaryOps {
		for _, dt := range dataTypes {
			_, catalogErr := functions.UnaryReturnType(op, dt)
			_, registryErr := functions.Unary.GetForSignature(op, dt.ArrowType())
			require.Equal(t, catalogErr == nil, registryErr == nil,
				"catalog and registry disagree for unary signature %v(%v): catalog=%v, registry=%v", op, dt, catalogErr, registryErr)
		}
	}

	binaryOps := []types.BinaryOp{
		types.BinaryOpEq, types.BinaryOpNeq,
		types.BinaryOpGt, types.BinaryOpGte, types.BinaryOpLt, types.BinaryOpLte,
		types.BinaryOpAnd, types.BinaryOpOr, types.BinaryOpXor,
		types.BinaryOpAdd, types.BinaryOpSub, types.BinaryOpMul, types.BinaryOpDiv, types.BinaryOpMod, types.BinaryOpPow,
		types.BinaryOpMatchSubstr, types.BinaryOpNotMatchSubstr,
		types.BinaryOpMatchRe, types.BinaryOpNotMatchRe,
		types.BinaryOpMatchPattern, types.BinaryOpNotMatchPattern,
		types.BinaryOpEqCaseInsensitive, types.BinaryOpNotEqCaseInsensitive,
		types.BinaryOpMatchSubstrCaseInsensitive, types.BinaryOpNotMatchSubstrCaseInsensitive,
	}
	for _, op := range binaryOps {
		for _, dt := range dataTypes {
			_, catalogErr := functions.BinaryReturnType(op, dt, dt)
			_, registryErr := functions.Binary.GetForSignature(op, dt.ArrowType())
			require.Equal(t, catalogErr == nil, registryErr == nil,
				"catalog and registry disagree for binary signature %v(%v,%v): catalog=%v, registry=%v", op, dt, dt, catalogErr, registryErr)
		}
	}

	variadicOps := []types.VariadicOp{
		types.VariadicOpParseLogfmt,
		types.VariadicOpParseJSON,
		types.VariadicOpParseRegexp,
		types.VariadicOpParseLabelfmt,
		types.VariadicOpParseLinefmt,
	}
	for _, op := range variadicOps {
		_, registryErr := functions.Variadic.GetForSignature(op)
		require.Equal(t, functions.HasVariadic(op), registryErr == nil,
			"catalog and registry disagree for variadic function %v", op)
	}
}
