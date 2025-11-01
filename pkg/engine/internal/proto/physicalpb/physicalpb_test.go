package physicalpb_test

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// Test performs a basic end-to-end test ensuring that we can convert from a
// [physical.Plan] into a [Plan] and back.
func Test(t *testing.T) {
	var expectedPlan *physical.Plan
	{
		var graph dag.Graph[physical.Node]

		scanNode := graph.Add(&physical.DataObjScan{
			NodeID: ulid.Make(),

			Location:  "test-location",
			Section:   1,
			StreamIDs: []int64{100, 200, 300},
		})

		limitNode := graph.Add(&physical.Limit{
			NodeID: ulid.Make(),

			Skip:  10,
			Fetch: 100,
		})

		err := graph.AddEdge(dag.Edge[physical.Node]{Parent: limitNode, Child: scanNode})
		require.NoError(t, err, "Failed to add edge")

		expectedPlan = physical.FromGraph(graph)
	}

	var protoPlan physicalpb.Plan
	require.NoError(t, protoPlan.UnmarshalPhysical(expectedPlan), "Failed to unmarshal physical plan")

	actualPlan, err := protoPlan.MarshalPhysical()
	require.NoError(t, err, "Failed to marshal protobuf plan")

	expectedOutput := physical.PrintAsTree(expectedPlan)
	actualOutput := physical.PrintAsTree(actualPlan)
	require.Equal(t, expectedOutput, actualOutput, "Unmarshaled plan from protobuf does not match origianl")
}

func Test_Node(t *testing.T) {
	tt := []struct {
		name string
		node physical.Node
	}{
		{
			name: "DataObjScan",
			node: &physical.DataObjScan{
				NodeID: ulid.Make(),

				Location:    "object",
				Section:     42,
				StreamIDs:   []int64{1, 2, 3, 4, 5},
				Projections: []physical.ColumnExpression{&physical.ColumnExpr{Ref: types.ColumnRef{Column: "test_col", Type: types.ColumnTypeLabel}}},
				Predicates:  []physical.Expression{&physical.LiteralExpr{Literal: types.StringLiteral("test_value")}},
			},
		},
		{
			name: "Filter",
			node: &physical.Filter{
				NodeID: ulid.Make(),

				Predicates: []physical.Expression{
					&physical.LiteralExpr{Literal: types.BoolLiteral(true)},
					&physical.LiteralExpr{Literal: types.IntegerLiteral(123)},
				},
			},
		},
		{
			name: "Limit",
			node: &physical.Limit{
				NodeID: ulid.Make(),

				Skip:  25,
				Fetch: 100,
			},
		},
		{
			name: "Projection",
			node: &physical.Projection{
				NodeID: ulid.Make(),

				Expressions: []physical.Expression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "col1", Type: types.ColumnTypeLabel}},
					&physical.LiteralExpr{Literal: types.FloatLiteral(3.14)},
				},
				All:    true,
				Expand: true,
				Drop:   false,
			},
		},
		{
			name: "RangeAggregation",
			node: &physical.RangeAggregation{
				NodeID: ulid.Make(),

				PartitionBy: []physical.ColumnExpression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "partition_col", Type: types.ColumnTypeLabel}},
				},
				Operation: types.RangeAggregationTypeCount,
				Start:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				End:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
				Step:      time.Minute * 5,
				Range:     time.Minute * 1,
			},
		},
		{
			name: "VectorAggregation",
			node: &physical.VectorAggregation{
				NodeID: ulid.Make(),

				GroupBy: []physical.ColumnExpression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "group_col", Type: types.ColumnTypeLabel}},
				},
				Operation: types.VectorAggregationTypeSum,
			},
		},
		{
			name: "ColumnCompat",
			node: &physical.ColumnCompat{
				NodeID: ulid.Make(),

				Source:      types.ColumnTypeMetadata,
				Destination: types.ColumnTypeMetadata,
				Collision:   types.ColumnTypeLabel,
			},
		},
		{
			name: "TopK",
			node: &physical.TopK{
				NodeID: ulid.Make(),

				SortBy:     &physical.ColumnExpr{Ref: types.ColumnRef{Column: "sort_col", Type: types.ColumnTypeBuiltin}},
				Ascending:  true,
				NullsFirst: true,
				K:          10,
			},
		},
		{
			name: "Parallelize",
			node: &physical.Parallelize{NodeID: ulid.Make()},
		},
		{
			name: "Join",
			node: &physical.Join{NodeID: ulid.Make()},
		},
		{
			name: "ScanSet",
			node: &physical.ScanSet{
				NodeID: ulid.Make(),

				Targets: []*physical.ScanTarget{{
					Type: physical.ScanTypeDataObject,
					DataObject: &physical.DataObjScan{
						NodeID:    ulid.Make(),
						Location:  "s3://bucket/target1",
						Section:   1,
						StreamIDs: []int64{10, 20},
					},
				}},
				Projections: []physical.ColumnExpression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "scan_col", Type: types.ColumnTypeBuiltin}},
				},
				Predicates: []physical.Expression{
					&physical.LiteralExpr{Literal: types.DurationLiteral(time.Second * 30)},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			var expectedPlan *physical.Plan
			{
				var graph dag.Graph[physical.Node]
				graph.Add(tc.node)
				expectedPlan = physical.FromGraph(graph)
			}

			var protoPlan physicalpb.Plan
			require.NoError(t, protoPlan.UnmarshalPhysical(expectedPlan), "Failed to unmarshal physical plan")

			actualPlan, err := protoPlan.MarshalPhysical()
			require.NoError(t, err, "Failed to marshal protobuf plan")

			expectedOutput := physical.PrintAsTree(expectedPlan)
			actualOutput := physical.PrintAsTree(actualPlan)
			require.Equal(t, expectedOutput, actualOutput, "Unmarshaled plan from protobuf does not match origianl")
		})
	}
}

func Test_Expression(t *testing.T) {
	tt := []struct {
		name string
		expr physical.Expression
	}{
		{
			name: "LiteralExpr (Null)",
			expr: &physical.LiteralExpr{Literal: types.NullLiteral{}},
		},
		{
			name: "LiteralExpr (Bool)",
			expr: &physical.LiteralExpr{Literal: types.BoolLiteral(true)},
		},
		{
			name: "LiteralExpr (String)",
			expr: &physical.LiteralExpr{Literal: types.StringLiteral("test_string")},
		},
		{
			name: "LiteralExpr (Integer)",
			expr: &physical.LiteralExpr{Literal: types.IntegerLiteral(42)},
		},
		{
			name: "LiteralExpr (Float)",
			expr: &physical.LiteralExpr{Literal: types.FloatLiteral(3.14159)},
		},
		{
			name: "LiteralExpr (Timestamp)",
			expr: &physical.LiteralExpr{Literal: types.TimestampLiteral(1_000_000)},
		},
		{
			name: "LiteralExpr (Duration)",
			expr: &physical.LiteralExpr{Literal: types.DurationLiteral(100_000)},
		},
		{
			name: "LiteralExpr (Bytes)",
			expr: &physical.LiteralExpr{Literal: types.BytesLiteral(1024)},
		},
		{
			name: "LiteralExpr (StringList)",
			expr: &physical.LiteralExpr{Literal: types.StringListLiteral([]string{"item1", "item2", "item3"})},
		},
		{
			name: "ColumnExpr",
			expr: &physical.ColumnExpr{
				Ref: types.ColumnRef{Column: "test_column", Type: types.ColumnTypeLabel},
			},
		},
		{
			name: "UnaryExpr",
			expr: &physical.UnaryExpr{
				Op:   types.UnaryOpNot,
				Left: &physical.LiteralExpr{Literal: types.BoolLiteral(false)},
			},
		},
		{
			name: "BinaryExpr",
			expr: &physical.BinaryExpr{
				Op: types.BinaryOpEq,
				Left: &physical.ColumnExpr{
					Ref: types.ColumnRef{Column: "left_col", Type: types.ColumnTypeLabel},
				},
				Right: &physical.LiteralExpr{
					Literal: types.StringLiteral("comparison_value"),
				},
			},
		},
		{
			name: "VariadicExpr",
			expr: &physical.VariadicExpr{
				Op: types.VariadicOpParseJSON,
				Expressions: []physical.Expression{
					&physical.ColumnExpr{
						Ref: types.ColumnRef{Column: "json_column", Type: types.ColumnTypeBuiltin},
					},
					&physical.LiteralExpr{Literal: types.StringLiteral("key1")},
					&physical.LiteralExpr{Literal: types.StringLiteral("key2")},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			var expectedPlan *physical.Plan
			{
				var graph dag.Graph[physical.Node]
				graph.Add(&physical.Filter{
					NodeID: ulid.Make(),

					Predicates: []physical.Expression{tc.expr},
				})

				expectedPlan = physical.FromGraph(graph)
			}

			var protoPlan physicalpb.Plan
			require.NoError(t, protoPlan.UnmarshalPhysical(expectedPlan), "Failed to unmarshal physical plan")

			actualPlan, err := protoPlan.MarshalPhysical()
			require.NoError(t, err, "Failed to marshal protobuf plan")

			expectedOutput := physical.PrintAsTree(expectedPlan)
			actualOutput := physical.PrintAsTree(actualPlan)
			require.Equal(t, expectedOutput, actualOutput, "Unmarshaled plan from protobuf does not match origianl")
		})
	}
}
