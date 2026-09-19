package expressionpb_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/expressionpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/testutils"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

// TestRoundTripExpressions_Reflection round-trips every concrete
// physical.Expression implementation through the protobuf encoding and
// asserts that the recovered expression deep-equals the input. It catches
// "added a field to an Expression struct but forgot to copy it in
// marshal.go / unmarshal.go" silent bugs.
//
// *UnaryExpr, *BinaryExpr, *VariadicExpr, and *ColumnExpr are filled by
// reflection so new fields are covered automatically. *LiteralExpr has an
// unexported `inner types.Literal` field that reflection cannot set, so its
// values are enumerated explicitly — once per concrete literal type
// recognized by types.NewLiteral. Adding a new literal type here goes
// hand-in-hand with adding marshal/unmarshal cases.
func TestRoundTripExpressions_Reflection(t *testing.T) {
	reflectionCases := []physical.Expression{
		new(physical.UnaryExpr),
		new(physical.BinaryExpr),
		new(physical.VariadicExpr),
		new(physical.ColumnExpr),
	}
	for _, e := range reflectionCases {
		name := reflect.TypeOf(e).Elem().Name()
		t.Run(name, func(t *testing.T) {
			newExpressionFiller().Fill(reflect.ValueOf(e).Elem())
			assertExpressionRoundTrip(t, e)
		})
	}

	literalCases := []struct {
		name  string
		value any
	}{
		{"NullLiteral", nil},
		{"BoolLiteral", true},
		{"StringLiteral", "hello"},
		{"IntegerLiteral", int64(42)},
		{"FloatLiteral", 3.14},
		{"TimestampLiteral", types.Timestamp(1_700_000_000_000_000_000)},
		{"DurationLiteral", types.Duration(5_000_000_000)},
		{"BytesLiteral", types.Bytes(1024)},
		{"StringListLiteral", []string{"a", "b", "c"}},
		{"LabelFmtListLiteral", []log.LabelFmt{{Name: "n", Value: "v", Rename: true}}},
	}
	for _, lit := range literalCases {
		t.Run(lit.name, func(t *testing.T) {
			assertExpressionRoundTrip(t, physical.NewLiteral(lit.value))
		})
	}
}

func assertExpressionRoundTrip(t *testing.T, expected physical.Expression) {
	t.Helper()
	var protoExpr expressionpb.Expression
	require.NoError(t, protoExpr.UnmarshalPhysical(expected))
	actual, err := protoExpr.MarshalPhysical()
	require.NoError(t, err)
	require.Equal(t, expected, actual,
		"round-trip mismatch — a field on %T is likely not copied in expressionpb marshal.go / unmarshal.go",
		expected)
}

// newExpressionFiller wires the engine-specific overrides for expression
// round-trip tests on top of the generic testutils.Filler.
func newExpressionFiller() *testutils.Filler {
	f := testutils.NewFiller()

	// Any physical.Expression interface field gets a *ColumnExpr — keeps each
	// expression's test focused on its own fields rather than recursing into
	// another expression's marshaling tree.
	f.RegisterInterface(reflect.TypeOf((*physical.Expression)(nil)).Elem(), func(f *testutils.Filler) reflect.Value {
		c := &physical.ColumnExpr{}
		f.Fill(reflect.ValueOf(c).Elem())
		return reflect.ValueOf(c)
	})

	// Enum-like types must be values present in the proto lookup map,
	// otherwise marshal returns an error. Coverage of all enum values is
	// handled separately by TestLookupCompleteness_*.
	f.RegisterFixed(types.UnaryOpNot)
	f.RegisterFixed(types.BinaryOpEq)
	f.RegisterFixed(types.VariadicOpParseLogfmt)
	f.RegisterFixed(types.ColumnTypeLabel)

	return f
}
