package logical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/logical/format"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
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
				{Name: "id", Type: datasetmd.VALUE_TYPE_UINT64},
				{Name: "name", Type: datasetmd.VALUE_TYPE_STRING},
				{Name: "age", Type: datasetmd.VALUE_TYPE_UINT64},
			},
		},
	}

	scan := NewScan(ds, nil)
	filter := NewFilter(scan, Gt("age_gt_21", Col("age"), LitI64(21)))
	proj := NewProjection(filter, []Expr{Col("id"), Col("name")})

	f := format.NewTreeFormatter()
	proj.Format(f)

	expected := `
Projection id=VALUE_TYPE_UINT64 name=VALUE_TYPE_STRING
├── Column #id
├── Column #name
└── Filter expr=age_gt_21
    ├── BooleanCmpExpr op=(>) name=age_gt_21
    │   ├── Column #age
    │   └── LiteralI64 value=21
    └── Scan data_source=users`

	require.Equal(t, expected, "\n"+f.Format())
}

func TestFormatDataFrameQuery(t *testing.T) {
	// Calculate the sum of sales per region for the year 2020
	ds := &testDataSource{
		name: "orders",
		schema: schema.Schema{
			Columns: []schema.ColumnSchema{
				{Name: "region", Type: datasetmd.VALUE_TYPE_STRING},
				{Name: "sales", Type: datasetmd.VALUE_TYPE_UINT64},
				{Name: "year", Type: datasetmd.VALUE_TYPE_UINT64},
			},
		},
	}

	df := NewDataFrame(
		NewScan(ds, nil),
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
	)

	f := format.NewTreeFormatter()
	df.LogicalPlan().Format(f)

	expected := `
Aggregate groupings=(region) aggregates=(total_sales)
├── GroupExprs
│   └── Column #region
├── AggregateExprs
│   └── AggregateExpr op=sum
│       └── Column #sales
└── Projection region=VALUE_TYPE_STRING sales=VALUE_TYPE_UINT64 year=VALUE_TYPE_UINT64
    ├── Column #region
    ├── Column #sales
    ├── Column #year
    └── Filter expr=year_2020
        ├── BooleanCmpExpr op=(==) name=year_2020
        │   ├── Column #year
        │   └── LiteralI64 value=2020
        └── Scan data_source=orders`

	require.Equal(t, expected, "\n"+f.Format())
}
