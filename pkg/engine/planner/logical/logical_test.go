package logical

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestPlan_String(t *testing.T) {
	// Build a query plan for this query sorted by `age` in ascending order:
	//
	// { app="users" } | age > 21
	b := NewBuilder(
		&MakeTable{
			Selector: &BinOp{
				Left:  &ColumnRef{Column: "app", Type: types.ColumnTypeLabel},
				Right: LiteralString("users"),
				Op:    types.BinaryOpEq,
			},
		},
	).Select(
		&BinOp{
			Left:  &ColumnRef{Column: "age", Type: types.ColumnTypeMetadata},
			Right: LiteralInt64(21),
			Op:    types.BinaryOpGt,
		},
	).Sort(ColumnRef{Column: "age", Type: types.ColumnTypeMetadata}, true, false)

	// Convert to SSA
	ssaForm, err := b.ToPlan()
	require.NoError(t, err)
	require.NotNil(t, ssaForm)

	t.Logf("SSA Form:\n%s", ssaForm.String())

	// Define expected output
	exp := `
%1 = EQ label.app, "users" 
%2 = MAKE_TABLE [selector=%1] 
%3 = GT metadata.age, 21 
%4 = SELECT %2 [predicate=%3] 
%5 = SORT %4 [column=metadata.age, asc=true, nulls_first=false]
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
