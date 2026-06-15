package physicalpb_test

import (
	"reflect"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/testutils"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// TestRoundTripNodes_Reflection uses reflection to construct each supported
// physical.Node with every settable field set to a distinct non-zero value,
// runs each through the protobuf marshal/unmarshal round trip, and asserts
// that the recovered node deep-equals the input. This catches "added a struct
// field but forgot to copy it in marshal_node.go / unmarshal_node.go" silent
// bugs without requiring the test author to enumerate every field.
//
// TODO: physical.LogMerge is omitted because it has no proto representation:
// the type switches in marshal_node.go and unmarshal_node.go don't include it,
// so any plan containing it errors out at serialization time. Adding it
// here would surface that gap, but as a separate finding from this test's
// "fields not copied" focus. Once proto support lands, add it to the list.
func TestRoundTripNodes_Reflection(t *testing.T) {
	cases := []physical.Node{
		new(physical.Batching),
		new(physical.Cache),
		new(physical.ColumnCompat),
		new(physical.DataObjScan),
		new(physical.Filter),
		new(physical.IndexMerge),
		new(physical.Join),
		new(physical.Limit),
		new(physical.Merge),
		new(physical.Parallelize),
		new(physical.PointersScan),
		new(physical.Projection),
		new(physical.RangeAggregation),
		new(physical.ScanSet),
		new(physical.TopK),
		new(physical.VectorAggregation),
		//new(physical.LogMerge),
	}

	for _, n := range cases {
		name := reflect.TypeOf(n).Elem().Name()
		t.Run(name, func(t *testing.T) {
			newPhysicalFiller().Fill(reflect.ValueOf(n).Elem())

			var graph dag.Graph[physical.Node]
			graph.Add(n)
			expected := physical.FromGraph(graph)

			var protoPlan physicalpb.Plan
			require.NoError(t, protoPlan.UnmarshalPhysical(expected))
			actual, err := protoPlan.MarshalPhysical()
			require.NoError(t, err)

			actualRoot, err := actual.Root()
			require.NoError(t, err)
			require.Equal(t, n, actualRoot,
				"round-trip mismatch — a field on %s is likely not copied in marshal_node.go or unmarshal_node.go",
				name)
		})
	}
}

// newPhysicalFiller wires the engine-specific overrides on top of the
// generic testutils.Filler: interface constructors, valid enum values, and
// the ScanTarget discriminated-union fixer.
func newPhysicalFiller() *testutils.Filler {
	f := testutils.NewFiller()

	// physical.Expression and physical.ColumnExpression: pick the simplest
	// concrete implementation (*ColumnExpr) so the test stays focused on
	// node-level fields, not on the expression marshaling tree (which is
	// exercised separately in expressionpb).
	colExprCtor := func(f *testutils.Filler) reflect.Value {
		c := &physical.ColumnExpr{}
		f.Fill(reflect.ValueOf(c).Elem())
		return reflect.ValueOf(c)
	}
	f.RegisterInterface(reflect.TypeOf((*physical.Expression)(nil)).Elem(), colExprCtor)
	f.RegisterInterface(reflect.TypeOf((*physical.ColumnExpression)(nil)).Elem(), colExprCtor)

	// Enum-like types must be set to values present in the proto lookup map,
	// otherwise marshal returns an error and the round trip fails for the
	// wrong reason. TestLookupCompleteness_* separately ensures every value
	// is reachable.
	f.RegisterFixed(types.UnaryOpNot)
	f.RegisterFixed(types.BinaryOpEq)
	f.RegisterFixed(types.VariadicOpParseLogfmt)
	f.RegisterFixed(types.ColumnTypeLabel)
	f.RegisterFixed(types.RangeAggregationTypeCount)
	f.RegisterFixed(types.VectorAggregationTypeSum)

	// physical.ScanTarget is a discriminated union: Type determines whether
	// DataObject or Pointers is set (and only one at a time). The generic
	// filler would set both, which the marshal code does not support. Inside
	// a ScanTarget the inner scan's NodeID is intentionally dropped (the
	// marshal code passes ulid.Zero to MarshalPhysical because "Targets
	// aren't real nodes"), so the fixer zeros it out to keep the round trip
	// symmetric.
	f.RegisterSpecific(reflect.TypeOf(physical.ScanTarget{}), func(f *testutils.Filler) reflect.Value {
		target := physical.ScanTarget{}
		if f.Next()%2 == 0 {
			target.Type = physical.ScanTypeDataObject
			d := &physical.DataObjScan{}
			f.Fill(reflect.ValueOf(d).Elem())
			d.NodeID = ulid.ULID{}
			target.DataObject = d
		} else {
			target.Type = physical.ScanTypePointers
			p := &physical.PointersScan{}
			f.Fill(reflect.ValueOf(p).Elem())
			p.NodeID = ulid.ULID{}
			target.Pointers = p
		}
		return reflect.ValueOf(target)
	})

	return f
}
