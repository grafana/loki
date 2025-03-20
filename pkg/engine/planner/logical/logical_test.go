package logical

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlan_String(t *testing.T) {
	// Build a simple query plan:
	// SELECT id, name FROM users WHERE age > 21 SORT BY age ASC
	df := NewDataFrame(
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

	// Convert to SSA
	ssaForm, err := df.ToPlan()
	require.NoError(t, err)
	require.NotNil(t, ssaForm)

	t.Logf("SSA Form:\n%s", ssaForm.String())

	// Define expected output
	exp := `
%1 = EQ label.table, "users" 
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
