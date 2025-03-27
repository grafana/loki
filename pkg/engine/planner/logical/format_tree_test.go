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

	var sb strings.Builder
	PrintTree(&sb, b.Value())

	actual := "\n" + sb.String()
	t.Logf("Actual output:\n%s", actual)

	expected := `
Select
│   └── BinOp op=GT
│       ├── ColumnRef #metadata.age
│       └── Literal value=21 kind=int
└── MakeTable
        └── BinOp op=EQ
            ├── ColumnRef #label.app
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

	var sb strings.Builder
	PrintTree(&sb, b.Value())

	actual := "\n" + sb.String()
	t.Logf("Actual output:\n%s", actual)

	expected := `
Sort direction=asc nulls=last
│   └── ColumnRef #metadata.age
└── Select
    │   └── BinOp op=GT
    │       ├── ColumnRef #metadata.age
    │       └── Literal value=21 kind=int
    └── MakeTable
            └── BinOp op=EQ
                ├── ColumnRef #label.app
                └── Literal value="users" kind=string
`
	require.Equal(t, expected, actual)
}
