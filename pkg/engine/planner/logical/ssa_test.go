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

	scan := NewMakeTableNode(ds.Name(), ds.Schema())
	sel := NewSelectNode(scan, Gt("age_gt_21", Col("age"), LitI64(21)))

	// Convert to SSA
	ssaForm, err := ConvertToSSA(sel)
	require.NoError(t, err)
	require.NotNil(t, ssaForm)

	t.Logf("SSA Form:\n%s", ssaForm.String())

	// Define expected output
	exp := `
%1 = MakeTable [name=users]
%2 = ColumnRef [name=age, type=VALUE_TYPE_UINT64]
%3 = Literal [val=21, type=VALUE_TYPE_INT64]
%4 = BinaryOp [op=(>), name=age_gt_21, left=%2, right=%3]
%5 = Select [name=age_gt_21, predicate=%4, plan=%1]
RETURN %5 
`
	exp = strings.TrimSpace(exp)

	// Get the actual output without the RETURN statement
	ssaOutput := ssaForm.String()
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
		NewMakeTableNode(ds.Name(), ds.Schema()),
	).Select(
		Eq("year_2020", Col("year"), LitI64(2020)),
	).Limit(
		0,
		10,
	)

	// Convert to SSA
	ssaForm, err := ConvertToSSA(df.Node())
	require.NoError(t, err)
	require.NotNil(t, ssaForm)

	t.Logf("SSA Form:\n%s", ssaForm.String())

	// Define expected output
	exp := `
%1 = MakeTable [name=orders]
%2 = ColumnRef [name=year, type=VALUE_TYPE_UINT64]
%3 = Literal [val=2020, type=VALUE_TYPE_INT64]
%4 = BinaryOp [op=(==), name=year_2020, left=%2, right=%3]
%5 = Select [name=year_2020, predicate=%4, plan=%1]
%6 = Limit [Skip=0, Fetch=10]
RETURN %6
`
	exp = strings.TrimSpace(exp)

	ssaOutput := ssaForm.String()
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

	scan := NewMakeTableNode(ds.Name(), ds.Schema())
	sel := NewSelectNode(scan, Gt("age_gt_21", Col("age"), LitI64(21)))

	// Sort by age ascending, nulls last
	sortByAge := NewSortNode(sel, NewSortExpr("sort_by_age", Col("age"), true, false))

	ssa, err := ConvertToSSA(sortByAge)
	require.NoError(t, err)

	t.Logf("SSA Form:\n%s", ssa.String())

	// Verify the structure of the SSA form
	require.NotEmpty(t, ssa.Instructions)

	// The last node should be a return of the Sort.
	lastNode := ssa.Instructions[len(ssa.Instructions)-1]
	require.IsType(t, &Return{}, lastNode)
	require.IsType(t, &Sort{}, lastNode.(*Return).Value)
}
