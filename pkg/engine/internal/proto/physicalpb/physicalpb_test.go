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
			MaxTimeRange: physical.TimeRange{
				Start: time.Date(1970, 1, 1, 1, 0, 0, 0, time.UTC),
				End:   time.Date(1970, 1, 1, 2, 0, 0, 0, time.UTC),
			},
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
				Predicates:  []physical.Expression{physical.NewLiteral("test_value")},
			},
		},
		{
			name: "Filter",
			node: &physical.Filter{
				NodeID: ulid.Make(),

				Predicates: []physical.Expression{
					physical.NewLiteral(true),
					physical.NewLiteral(123),
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
					physical.NewLiteral(3.14),
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

				Grouping: physical.Grouping{
					Columns: []physical.ColumnExpression{
						&physical.ColumnExpr{Ref: types.ColumnRef{Column: "partition_col", Type: types.ColumnTypeLabel}},
					},
					Without: false,
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

				Grouping: physical.Grouping{
					Columns: []physical.ColumnExpression{
						&physical.ColumnExpr{Ref: types.ColumnRef{Column: "group_col", Type: types.ColumnTypeLabel}},
					},
					Without: false,
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
				Collisions:  []types.ColumnType{types.ColumnTypeLabel},
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
			name: "Merge",
			node: &physical.Merge{NodeID: ulid.Make()},
		},
		{
			name: "PointersScan",
			node: &physical.PointersScan{
				NodeID: ulid.Make(),

				Location: "index/0",
				Selector: &physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
					Right: physical.NewLiteral("foo"),
					Op:    types.BinaryOpEq,
				},
				Predicates: []physical.Expression{
					&physical.BinaryExpr{
						Left:  &physical.ColumnExpr{Ref: types.ColumnRef{Column: "env", Type: types.ColumnTypeLabel}},
						Right: physical.NewLiteral("prod"),
						Op:    types.BinaryOpEq,
					},
				},
				Start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				End:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "ScanSet",
			node: &physical.ScanSet{
				NodeID: ulid.Make(),

				Targets: []*physical.ScanTarget{
					{
						Type: physical.ScanTypeDataObject,
						DataObject: &physical.DataObjScan{
							NodeID:    ulid.Make(),
							Location:  "s3://bucket/target1",
							Section:   1,
							StreamIDs: []int64{10, 20},
						},
					},
					{
						Type: physical.ScanTypePointers,
						Pointers: &physical.PointersScan{
							NodeID:     ulid.Make(),
							Location:   "index/0",
							Selector:   physical.NewLiteral("selector"),
							Start:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
							End:        time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
							Predicates: []physical.Expression{physical.NewLiteral("predicate")},
						},
					},
				},
				Projections: []physical.ColumnExpression{
					&physical.ColumnExpr{Ref: types.ColumnRef{Column: "scan_col", Type: types.ColumnTypeBuiltin}},
				},
				Predicates: []physical.Expression{
					physical.NewLiteral(types.Duration(time.Second * 30)),
				},
			},
		},
		{
			name: "TableOfContentsConsolidate",
			node: &physical.TableOfContentsConsolidate{
				NodeID:           ulid.Make(),
				Tenant:           "29",
				ToCWindowStart:   1_715_000_000_000_000_000,
				RemoveIndexPaths: []string{"tenants/29/indexes/old-a.dataobj", "tenants/29/indexes/old-b.dataobj", "tenants/29/indexes/old-c.dataobj"},
				AddIndexPaths:    []string{"tenants/29/indexes/new-a.dataobj"},
				TaskTTL:          30 * time.Second,
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

// TestRoundTrip_TableOfContentsConsolidate_FieldLevel asserts every
// TableOfContentsConsolidate field round-trips by value, not by tree-printed
// equality. This catches regressions (e.g. swapping RemoveIndexPaths and
// AddIndexPaths) that the tree-equality check would miss because the printer
// renders both slices as counts only.
func TestRoundTrip_TableOfContentsConsolidate_FieldLevel(t *testing.T) {
	src := &physical.TableOfContentsConsolidate{
		NodeID:           ulid.Make(),
		Tenant:           "29",
		ToCWindowStart:   1_715_000_000_000_000_000,
		RemoveIndexPaths: []string{"remove/a", "remove/b", "remove/c"},
		AddIndexPaths:    []string{"add/x"},
		TaskTTL:          30 * time.Second,
	}

	var graph dag.Graph[physical.Node]
	graph.Add(src)
	plan := physical.FromGraph(graph)

	var protoPlan physicalpb.Plan
	require.NoError(t, protoPlan.UnmarshalPhysical(plan))

	roundTrip, err := protoPlan.MarshalPhysical()
	require.NoError(t, err)

	root, err := roundTrip.Root()
	require.NoError(t, err)
	got, ok := root.(*physical.TableOfContentsConsolidate)
	require.True(t, ok, "round-tripped root must be *physical.TableOfContentsConsolidate, got %T", root)

	require.Equal(t, src.Tenant, got.Tenant)
	require.Equal(t, src.ToCWindowStart, got.ToCWindowStart)
	require.Equal(t, src.RemoveIndexPaths, got.RemoveIndexPaths)
	require.Equal(t, src.AddIndexPaths, got.AddIndexPaths)
	require.Equal(t, src.TaskTTL, got.TaskTTL)
	// NodeID is preserved through the round-trip via the proto Node.Id
	// envelope: unmarshal_node.go stores from.ID() into Node.Id.Value, and
	// Node.MarshalPhysical passes it back to the kind-specific marshaler.
	require.Equal(t, src.NodeID, got.NodeID)
}

func Test_Expression(t *testing.T) {
	tt := []struct {
		name string
		expr physical.Expression
	}{
		{
			name: "LiteralExpr (Null)",
			expr: physical.NewLiteral(nil),
		},
		{
			name: "LiteralExpr (Bool)",
			expr: physical.NewLiteral(true),
		},
		{
			name: "LiteralExpr (String)",
			expr: physical.NewLiteral("test_string"),
		},
		{
			name: "LiteralExpr (Integer)",
			expr: physical.NewLiteral(42),
		},
		{
			name: "LiteralExpr (Float)",
			expr: physical.NewLiteral(3.14159),
		},
		{
			name: "LiteralExpr (Timestamp)",
			expr: physical.NewLiteral(types.Timestamp(1_000_000)),
		},
		{
			name: "LiteralExpr (Duration)",
			expr: physical.NewLiteral(types.Duration(100_000)),
		},
		{
			name: "LiteralExpr (Bytes)",
			expr: physical.NewLiteral(types.Bytes(1024)),
		},
		{
			name: "LiteralExpr (StringList)",
			expr: physical.NewLiteral([]string{"item1", "item2", "item3"}),
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
				Left: physical.NewLiteral(false),
			},
		},
		{
			name: "BinaryExpr",
			expr: &physical.BinaryExpr{
				Op: types.BinaryOpEq,
				Left: &physical.ColumnExpr{
					Ref: types.ColumnRef{Column: "left_col", Type: types.ColumnTypeLabel},
				},
				Right: physical.NewLiteral("comparison_value"),
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
					physical.NewLiteral("key1"),
					physical.NewLiteral("key2"),
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
