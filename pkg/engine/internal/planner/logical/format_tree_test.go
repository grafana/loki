package logical

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/schema"
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
				Left:  NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
				Right: NewLiteral("users"),
				Op:    physicalpb.BINARY_OP_EQ,
			},
		},
	).Select(
		&BinOp{
			Left:  NewColumnRef("age", physicalpb.COLUMN_TYPE_METADATA),
			Right: NewLiteral(int64(21)),
			Op:    physicalpb.BINARY_OP_GT,
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
│       └── Literal value=21 kind=int64
└── MAKETABLE <%2> selector=EQ label.app "users"
        └── BinOp <%1> op=EQ left=label.app right="users"
            ├── ColumnRef column=app type=label
            └── Literal value="users" kind=utf8
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
				Left:  NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
				Right: NewLiteral("users"),
				Op:    physicalpb.BINARY_OP_EQ,
			},
		},
	).Select(
		&BinOp{
			Left:  NewColumnRef("age", physicalpb.COLUMN_TYPE_METADATA),
			Right: NewLiteral(int64(21)),
			Op:    physicalpb.BINARY_OP_GT,
		},
	).Sort(*NewColumnRef("age", physicalpb.COLUMN_TYPE_METADATA), true, false)

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
    │       └── Literal value=21 kind=int64
    └── MAKETABLE <%2> selector=EQ label.app "users"
            └── BinOp <%1> op=EQ left=label.app right="users"
                ├── ColumnRef column=app type=label
                └── Literal value="users" kind=utf8
`
	require.Equal(t, expected, actual)
}

func TestFormatRangeAggregationQuery(t *testing.T) {
	// Build a query plan for a simple range aggregation query:
	//
	// count_over_time({ app="users" } | age > 51[5m])
	b := NewBuilder(
		&MakeTable{
			Selector: &BinOp{
				Left:  NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
				Right: NewLiteral("users"),
				Op:    physicalpb.BINARY_OP_EQ,
			},
		},
	).Select(
		&BinOp{
			Left:  NewColumnRef("age", physicalpb.COLUMN_TYPE_METADATA),
			Right: NewLiteral(int64(21)),
			Op:    physicalpb.BINARY_OP_GT,
		},
	).RangeAggregation(
		[]ColumnRef{*NewColumnRef("label1", physicalpb.COLUMN_TYPE_AMBIGUOUS), *NewColumnRef("label2", physicalpb.COLUMN_TYPE_AMBIGUOUS)},
		physicalpb.AGGREGATE_RANGE_OP_COUNT,
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), // Start Time
		time.Date(1970, 1, 1, 1, 0, 0, 0, time.UTC), // End Time
		time.Minute,
		time.Minute*5, // Range
	)

	// Convert to plan so that node IDs get populated
	plan, err := b.ToPlan()
	require.NoError(t, err)

	var sb strings.Builder
	PrintTree(&sb, plan.Value())

	actual := "\n" + sb.String()
	t.Logf("Actual output:\n%s", actual)

	expected := `
RangeAggregation <%5> table=%4 operation=count start_ts=1970-01-01T00:00:00Z end_ts=1970-01-01T01:00:00Z step=1m0s range=5m0s partition_by=(ambiguous.label1, ambiguous.label2)
│   ├── ColumnRef column=label1 type=ambiguous
│   └── ColumnRef column=label2 type=ambiguous
└── SELECT <%4> table=%2 predicate=%3
    │   └── BinOp <%3> op=GT left=metadata.age right=21
    │       ├── ColumnRef column=age type=metadata
    │       └── Literal value=21 kind=int64
    └── MAKETABLE <%2> selector=EQ label.app "users"
            └── BinOp <%1> op=EQ left=label.app right="users"
                ├── ColumnRef column=app type=label
                └── Literal value="users" kind=utf8
`

	require.Equal(t, expected, actual)
}

func TestFormatVectorAggregationQuery(t *testing.T) {
	// Build a query plan for a simple vector aggregation query:
	//
	// sum by (app, env) (rate({ app="users" }[5m]))
	b := NewBuilder(
		&MakeTable{
			Selector: &BinOp{
				Left:  NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
				Right: NewLiteral("users"),
				Op:    physicalpb.BINARY_OP_EQ,
			},
		},
	).VectorAggregation(
		[]ColumnRef{
			*NewColumnRef("app", physicalpb.COLUMN_TYPE_LABEL),
			*NewColumnRef("env", physicalpb.COLUMN_TYPE_LABEL),
		},
		physicalpb.AGGREGATE_VECTOR_OP_SUM,
	)

	// Convert to plan so that node IDs get populated
	plan, err := b.ToPlan()
	require.NoError(t, err)

	var sb strings.Builder
	PrintTree(&sb, plan.Value())

	actual := "\n" + sb.String()
	t.Logf("Actual output:\n%s", actual)

	expected := `
VectorAggregation <%3> table=%2 operation=sum group_by=(label.app, label.env)
│   ├── ColumnRef column=app type=label
│   └── ColumnRef column=env type=label
└── MAKETABLE <%2> selector=EQ label.app "users"
        └── BinOp <%1> op=EQ left=label.app right="users"
            ├── ColumnRef column=app type=label
            └── Literal value="users" kind=utf8
`

	require.Equal(t, expected, actual)
}
