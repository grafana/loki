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
	ds := &testDataSource{
		name: "users",
		schema: schema.Schema{
			Columns: []schema.ColumnSchema{
				{Name: "id", Type: schema.ValueTypeUint64},
				{Name: "name", Type: schema.ValueTypeString},
				{Name: "age", Type: schema.ValueTypeUint64},
			},
		},
	}

	scan := NewScan(ds.Name(), ds.Schema())
	filter := NewFilter(scan, Gt("age_gt_21", Col("age"), LitI64(21)))
	proj := NewProjection(filter, []Expr{Col("id"), Col("name")})

	var f TreeFormatter

	actual := "\n" + f.Format(proj)
	t.Logf("Actual output:\n%s", actual)

	expected := `
Projection id=VALUE_TYPE_UINT64 name=VALUE_TYPE_STRING
│   ├── Column #id
│   └── Column #name
└── Filter expr=age_gt_21
    │   └── BinaryOp type=cmp op=">" name=age_gt_21
    │       ├── Column #age
    │       └── Literal value=21 type=VALUE_TYPE_INT64
    └── MakeTable name=users
`

	require.Equal(t, expected, actual)
}

func TestFormatDataFrameQuery(t *testing.T) {
	// Calculate the sum of sales per region for the year 2020
	ds := &testDataSource{
		name: "orders",
		schema: schema.Schema{
			Columns: []schema.ColumnSchema{
				{Name: "region", Type: schema.ValueTypeString},
				{Name: "sales", Type: schema.ValueTypeUint64},
				{Name: "year", Type: schema.ValueTypeUint64},
			},
		},
	}

	df := NewDataFrame(
		NewScan(ds.Name(), ds.Schema()),
	).Filter(
		Eq("year_2020", Col("year"), LitI64(2020)),
	).Project(
		[]Expr{
			Col("region"),
			Col("sales"),
			Col("year"),
		},
	).Aggregate(
		[]Expr{Col("region")},
		[]AggregateExpr{
			Sum("total_sales", Col("sales")),
		},
	).Limit(
		0,
		10,
	)

	var f TreeFormatter

	actual := "\n" + f.Format(df.LogicalPlan())
	t.Logf("Actual output:\n%s", actual)

	expected := `
Limit offset=0 fetch=10
└── Aggregate groupings=([region]) aggregates=([total_sales])
    │   ├── GroupExpr
    │   │   └── Column #region
    │   └── AggregateExpr
    │       └── Aggregate op=sum
    │           └── Column #sales
    └── Projection region=VALUE_TYPE_STRING sales=VALUE_TYPE_UINT64 year=VALUE_TYPE_UINT64
        │   ├── Column #region
        │   ├── Column #sales
        │   └── Column #year
        └── Filter expr=year_2020
            │   └── BinaryOp type=cmp op="==" name=year_2020
            │       ├── Column #year
            │       └── Literal value=2020 type=VALUE_TYPE_INT64
            └── MakeTable name=orders
`
	require.Equal(t, expected, actual)
}

func TestFormatSortQuery(t *testing.T) {
	// Build a query plan with sorting:
	// SELECT id, name, age FROM users WHERE age > 21 ORDER BY age ASC, name DESC
	ds := &testDataSource{
		name: "users",
		schema: schema.Schema{
			Columns: []schema.ColumnSchema{
				{Name: "id", Type: schema.ValueTypeUint64},
				{Name: "name", Type: schema.ValueTypeString},
				{Name: "age", Type: schema.ValueTypeUint64},
			},
		},
	}

	scan := NewScan(ds.Name(), ds.Schema())
	filter := NewFilter(scan, Gt("age_gt_21", Col("age"), LitI64(21)))
	proj := NewProjection(filter, []Expr{Col("id"), Col("name"), Col("age")})

	// Sort by age ascending, nulls last
	sortByAge := NewSort(proj, NewSortExpr("sort_by_age", Col("age"), true, false))

	var f TreeFormatter

	actual := "\n" + f.Format(sortByAge)
	t.Logf("Actual output:\n%s", actual)

	expected := `
Sort expr=sort_by_age direction=asc nulls=last
│   └── Column #age
└── Projection id=VALUE_TYPE_UINT64 name=VALUE_TYPE_STRING age=VALUE_TYPE_UINT64
    │   ├── Column #id
    │   ├── Column #name
    │   └── Column #age
    └── Filter expr=age_gt_21
        │   └── BinaryOp type=cmp op=">" name=age_gt_21
        │       ├── Column #age
        │       └── Literal value=21 type=VALUE_TYPE_INT64
        └── MakeTable name=users
`
	require.Equal(t, expected, actual)
}

func TestFormatDataFrameWithSortQuery(t *testing.T) {
	// Calculate the sum of sales per region for the year 2020, sorted by total sales descending
	ds := &testDataSource{
		name: "orders",
		schema: schema.Schema{
			Columns: []schema.ColumnSchema{
				{Name: "region", Type: schema.ValueTypeString},
				{Name: "sales", Type: schema.ValueTypeUint64},
				{Name: "year", Type: schema.ValueTypeUint64},
			},
		},
	}

	df := NewDataFrame(
		NewScan(ds.Name(), ds.Schema()),
	).Filter(
		Eq("year_2020", Col("year"), LitI64(2020)),
	).Project(
		[]Expr{
			Col("region"),
			Col("sales"),
			Col("year"),
		},
	).Aggregate(
		[]Expr{Col("region")},
		[]AggregateExpr{
			Sum("total_sales", Col("sales")),
		},
	).Sort(
		NewSortExpr("sort_by_sales", Col("total_sales"), false, true), // Sort by total_sales descending, nulls first
	).Limit(
		0,
		10,
	)

	var f TreeFormatter

	actual := "\n" + f.Format(df.LogicalPlan())
	t.Logf("Actual output:\n%s", actual)

	expected := `
Limit offset=0 fetch=10
└── Sort expr=sort_by_sales direction=desc nulls=first
    │   └── Column #total_sales
    └── Aggregate groupings=([region]) aggregates=([total_sales])
        │   ├── GroupExpr
        │   │   └── Column #region
        │   └── AggregateExpr
        │       └── Aggregate op=sum
        │           └── Column #sales
        └── Projection region=VALUE_TYPE_STRING sales=VALUE_TYPE_UINT64 year=VALUE_TYPE_UINT64
            │   ├── Column #region
            │   ├── Column #sales
            │   └── Column #year
            └── Filter expr=year_2020
                │   └── BinaryOp type=cmp op="==" name=year_2020
                │       ├── Column #year
                │       └── Literal value=2020 type=VALUE_TYPE_INT64
                └── MakeTable name=orders
`
	require.Equal(t, expected, actual)
}
