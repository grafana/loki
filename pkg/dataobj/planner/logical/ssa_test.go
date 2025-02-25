package logical

import (
	"strings"
	"testing"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

func TestConvertToSSA(t *testing.T) {
	// Create a test schema
	testSchema := schema.Schema{
		Columns: []schema.ColumnSchema{
			{Name: "region", Type: datasetmd.VALUE_TYPE_STRING},
			{Name: "sales", Type: datasetmd.VALUE_TYPE_INT64},
			{Name: "year", Type: datasetmd.VALUE_TYPE_INT64},
		},
	}

	// Create a simple logical plan:
	// SELECT region, SUM(sales) AS total_sales
	// FROM orders
	// WHERE year = 2020
	// GROUP BY region

	// Create a scan node
	table := NewScan("orders", testSchema)

	// Create a comparison expression for year = 2020
	yearExpr := Col("year")
	year2020Literal := LitI64(2020)
	year2020Expr := Eq("year_2020", yearExpr, year2020Literal)

	// Filter for year = 2020
	filtered := NewFilter(table, year2020Expr)

	// Project the columns we need
	projected := NewProjection(filtered, []Expr{
		Col("region"),
		Col("sales"),
	})

	// Aggregate by region with sum(sales)
	aggregated := NewAggregate(projected, []Expr{Col("region")}, []AggregateExpr{Sum("total_sales", Col("sales"))})

	// Convert to SSA
	ssa, err := ConvertToSSA(aggregated)
	if err != nil {
		t.Fatalf("Failed to convert to SSA: %v", err)
	}

	// Basic validation
	if len(ssa.Nodes) != 4 {
		t.Errorf("Expected 4 nodes in SSA form, got %d", len(ssa.Nodes))
	}

	// Verify the order: table -> filter -> projection -> aggregate
	if ssa.Nodes[0].NodeType != PlanTypeTable {
		t.Errorf("First node should be Table, got %v", ssa.Nodes[0].NodeType)
	}
	if ssa.Nodes[1].NodeType != PlanTypeFilter {
		t.Errorf("Second node should be Filter, got %v", ssa.Nodes[1].NodeType)
	}
	if ssa.Nodes[2].NodeType != PlanTypeProjection {
		t.Errorf("Third node should be Projection, got %v", ssa.Nodes[2].NodeType)
	}
	if ssa.Nodes[3].NodeType != PlanTypeAggregate {
		t.Errorf("Fourth node should be Aggregate, got %v", ssa.Nodes[3].NodeType)
	}

	// Verify the references
	if len(ssa.Nodes[1].Refs) != 1 || ssa.Nodes[1].Refs[0] != ssa.Nodes[0].ID {
		t.Errorf("Filter should reference Table node")
	}
	if len(ssa.Nodes[2].Refs) != 1 || ssa.Nodes[2].Refs[0] != ssa.Nodes[1].ID {
		t.Errorf("Projection should reference Filter node")
	}
	if len(ssa.Nodes[3].Refs) != 1 || ssa.Nodes[3].Refs[0] != ssa.Nodes[2].ID {
		t.Errorf("Aggregate should reference Projection node")
	}

	// Check the root reference
	if ssa.RootRef != ssa.Nodes[3].ID {
		t.Errorf("Root reference should point to the Aggregate node")
	}

	// Print the SSA form for debugging
	ssaString := ssa.String()
	t.Logf("SSA Form:\n%s", ssaString)

	// Ensure the SSA string contains expected elements
	expected := []string{
		"Table",
		"Filter",
		"Projection",
		"Aggregate",
		"RETURN",
	}

	for _, e := range expected {
		if !strings.Contains(ssaString, e) {
			t.Errorf("SSA string should contain '%s'", e)
		}
	}
}
