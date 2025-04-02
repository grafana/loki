package logical

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

type testDataSource struct {
	name   string
	schema schema.Schema
}

func (t *testDataSource) Schema() schema.Schema { return t.schema }
func (t *testDataSource) Name() string          { return t.name }

func TestFormatSimpleQuery(t *testing.T) {
	// Build a simple query plan:
	// { app="users" } | age > 21
	b := NewBuilder(
		&MakeTable{
			Selector: &BinOp{
				Left:  NewColumnRef("app", types.ColumnTypeLabel),
				Right: NewLiteral("users"),
				Op:    types.BinaryOpEq,
			},
		},
	).Select(
		&BinOp{
			Left:  NewColumnRef("age", types.ColumnTypeMetadata),
			Right: NewLiteral[int64](21),
			Op:    types.BinaryOpGt,
		},
	)

	// Convert to plan so that node IDs get populated
	plan, _ := b.ToPlan()

	var sb strings.Builder
	PrintTree(&sb, plan.Value())

	actual := "\n" + sb.String()
	t.Logf("Actual output:\n%s", actual)

	expected := `
SELECT <%4> table=%2 predicate=%3
│   └── BinOp <%3> op=GT left=metadata.age right=21
│       ├── ColumnRef column=age type=metadata
│       └── Literal value=21 kind=int
└── MAKETABLE <%2> selector=EQ label.app "users"
        └── BinOp <%1> op=EQ left=label.app right="users"
            ├── ColumnRef column=app type=label
            └── Literal value="users" kind=string
`

	require.Equal(t, expected, actual)
}

func TestFormatSortQuery(t *testing.T) {
	// Build a query plan for this query sorted by `age` in ascending order:
	//
	// { app="users" } | age > 21
	b := NewBuilder(
		&MakeTable{
			Selector: &BinOp{
				Left:  NewColumnRef("app", types.ColumnTypeLabel),
				Right: NewLiteral("users"),
				Op:    types.BinaryOpEq,
			},
		},
	).Select(
		&BinOp{
			Left:  NewColumnRef("age", types.ColumnTypeMetadata),
			Right: NewLiteral[int64](21),
			Op:    types.BinaryOpGt,
		},
	).Sort(*NewColumnRef("age", types.ColumnTypeMetadata), true, false)

	// Convert to plan so that node IDs get populated
	plan, _ := b.ToPlan()

	var sb strings.Builder
	PrintTree(&sb, plan.Value())

	actual := "\n" + sb.String()
	t.Logf("Actual output:\n%s", actual)

	expected := `
SORT <%5> table=%4 column=metadata.age direction=asc nulls=last
│   └── ColumnRef column=age type=metadata
└── SELECT <%4> table=%2 predicate=%3
    │   └── BinOp <%3> op=GT left=metadata.age right=21
    │       ├── ColumnRef column=age type=metadata
    │       └── Literal value=21 kind=int
    └── MAKETABLE <%2> selector=EQ label.app "users"
            └── BinOp <%1> op=EQ left=label.app right="users"
                ├── ColumnRef column=app type=label
                └── Literal value="users" kind=string
`
	require.Equal(t, expected, actual)
}
