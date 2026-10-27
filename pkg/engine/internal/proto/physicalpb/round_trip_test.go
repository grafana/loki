package physicalpb_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/testutils"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// TestRoundTripNodes_Reflection uses reflection to construct each supported
// physical.Node with every settable field set to a distinct non-zero value,
// runs each through the protobuf marshal/unmarshal round trip, and asserts
// that the recovered node deep-equals the input. This catches "added a struct
// field but forgot to copy it in marshal_node.go / unmarshal_node.go" silent
// bugs without requiring the test author to enumerate every field.
func TestRoundTripNodes_Reflection(t *testing.T) {
	cases := []physical.Node{
		new(physical.IndexFilter),
		new(physical.IndexMerge),
		new(physical.LogMerge),
	}

	for _, n := range cases {
		name := reflect.TypeOf(n).Elem().Name()
		t.Run(name, func(t *testing.T) {
			testutils.NewFiller().Fill(reflect.ValueOf(n).Elem())

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
