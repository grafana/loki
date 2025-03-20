package logical

import (
	"testing"

	"github.com/stretchr/testify/require"

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
	// SELECT id, name FROM users WHERE age > 21
	b := NewBuilder(
		&MakeTable{
			Selector: &BinOp{
				Left:  &ColumnRef{Column: "table", Type: ColumnTypeLabel},
				Right: LiteralString("users"),
				Op:    BinOpKindEq,
			},
		},
	).Select(
		&BinOp{
			Left:  &ColumnRef{Column: "age", Type: ColumnTypeMetadata},
			Right: LiteralInt64(21),
			Op:    BinOpKindGt,
		},
	)

	var f TreeFormatter

	actual := "\n" + f.Format(b.Value())
	t.Logf("Actual output:\n%s", actual)

	expected := `
Select
│   └── BinOp op=GT
│       ├── ColumnRef #metadata.age
│       └── Literal value=21 kind=int64
└── MakeTable
        └── BinOp op=EQ
    │       ├── ColumnRef #label.table
    │       └── Literal value="users" kind=string
`

	require.Equal(t, expected, actual)
}

func TestFormatSortQuery(t *testing.T) {
	// Build a query plan with sorting:
	// SELECT id, name, age FROM users WHERE age > 21 ORDER BY age ASC
	b := NewBuilder(
		&MakeTable{
			Selector: &BinOp{
				Left:  &ColumnRef{Column: "table", Type: ColumnTypeLabel},
				Right: LiteralString("users"),
				Op:    BinOpKindEq,
			},
		},
	).Select(
		&BinOp{
			Left:  &ColumnRef{Column: "age", Type: ColumnTypeMetadata},
			Right: LiteralInt64(21),
			Op:    BinOpKindGt,
		},
	).Sort(ColumnRef{Column: "age", Type: ColumnTypeMetadata}, true, false)

	var f TreeFormatter
	actual := "\n" + f.Format(b.Value())
	t.Logf("Actual output:\n%s", actual)

	expected := `
Sort direction=asc nulls=last
│   └── ColumnRef #metadata.age
└── Select
    │   └── BinOp op=GT
    │       ├── ColumnRef #metadata.age
    │       └── Literal value=21 kind=int64
    └── MakeTable
            └── BinOp op=EQ
        │       ├── ColumnRef #label.table
        │       └── Literal value="users" kind=string
`
	require.Equal(t, expected, actual)
}
