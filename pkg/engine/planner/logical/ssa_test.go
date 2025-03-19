package logical

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

func TestConvertSimpleQueryToSSA(t *testing.T) {
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

	// Convert to SSA
	ssaForm, err := ConvertToSSA(proj)
	require.NoError(t, err)
	require.NotNil(t, ssaForm)

	// Get the string representation for debugging
	ssaString := ssaForm.String()
	t.Logf("SSA Form:\n%s", ssaString)

	// Define expected output
	exp := `
%1 = MakeTable [name=users]
%2 = ColumnRef [name=age, type=VALUE_TYPE_UINT64]
%3 = Literal [val=21, type=VALUE_TYPE_INT64]
%4 = BinaryOp [op=(>), name=age_gt_21, left=%2, right=%3]
%5 = Filter [name=age_gt_21, predicate=%4, plan=%1]
%6 = ColumnRef [name=id, type=VALUE_TYPE_UINT64]
%7 = ColumnRef [name=name, type=VALUE_TYPE_STRING]
%8 = Project [id=%6, name=%7]
`
	exp = strings.TrimSpace(exp)

	// Get the actual output without the RETURN statement
	ssaOutput := ssaForm.Format()
	ssaLines := strings.Split(strings.TrimSpace(ssaOutput), "\n")

	expLines := strings.Split(exp, "\n")
	require.Equal(t, len(expLines), len(ssaLines), "Expected and actual SSA output line counts do not match")

	for i, line := range expLines {
		require.Equal(t, strings.TrimSpace(line), strings.TrimSpace(ssaLines[i]), fmt.Sprintf("Mismatch at line %d", i+1))
	}
}

func TestConvertComplexQueryToSSA(t *testing.T) {
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

	// Convert to SSA
	ssaForm, err := ConvertToSSA(df.LogicalPlan())
	require.NoError(t, err)
	require.NotNil(t, ssaForm)

	// Get the string representation for debugging
	ssaString := ssaForm.String()
	t.Logf("SSA Form:\n%s", ssaString)

	// Define expected output
	exp := `
%1 = MakeTable [name=orders]
%2 = ColumnRef [name=year, type=VALUE_TYPE_UINT64]
%3 = Literal [val=2020, type=VALUE_TYPE_INT64]
%4 = BinaryOp [op=(==), name=year_2020, left=%2, right=%3]
%5 = Filter [name=year_2020, predicate=%4, plan=%1]
%6 = ColumnRef [name=region, type=VALUE_TYPE_STRING]
%7 = ColumnRef [name=sales, type=VALUE_TYPE_UINT64]
%8 = ColumnRef [name=year, type=VALUE_TYPE_UINT64]
%9 = Project [region=%6, sales=%7, year=%8]
%10 = ColumnRef [name=region, type=VALUE_TYPE_STRING]
%11 = ColumnRef [name=sales, type=VALUE_TYPE_UINT64]
%12 = AggregationExpr [name=total_sales, op=sum]
%13 = AggregatePlan [aggregations=[%12], groupings=[%10]]
%14 = Limit [Skip=0, Fetch=10]
`
	exp = strings.TrimSpace(exp)

	// Get the actual output without the RETURN statement
	ssaOutput := ssaForm.Format()
	ssaLines := strings.Split(strings.TrimSpace(ssaOutput), "\n")

	expLines := strings.Split(exp, "\n")
	require.Equal(t, len(expLines), len(ssaLines), "Expected and actual SSA output line counts do not match")

	for i, line := range expLines {
		require.Equal(t, strings.TrimSpace(line), strings.TrimSpace(ssaLines[i]), fmt.Sprintf("Mismatch at line %d", i+1))
	}
}

func TestConvertSortQueryToSSA(t *testing.T) {
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

	ssa, err := ConvertToSSA(sortByAge)
	require.NoError(t, err)

	t.Logf("SSA Form:\n%s", ssa.Format())

	// Verify the structure of the SSA form
	nodes := ssa.Nodes()
	require.NotEmpty(t, nodes)

	// The last node should be the Sort node
	lastNodeID := len(ssa.nodes) - 1
	lastNode := ssa.nodes[lastNodeID]
	require.Equal(t, "Sort", lastNode.NodeType)

	// Verify the properties of the Sort node
	var exprName, direction, nulls string
	for _, tuple := range lastNode.Tuples {
		switch tuple.Key {
		case "expr":
			exprName = tuple.Value
		case "direction":
			direction = tuple.Value
		case "nulls":
			nulls = tuple.Value
		}
	}

	require.Equal(t, "sort_by_age", exprName)
	require.Equal(t, "asc", direction)
	require.Equal(t, "last", nulls)
}
