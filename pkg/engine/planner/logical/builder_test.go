package logical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestBuilderParse(t *testing.T) {
	t.Run("Builder adds Parse after MakeTable", func(t *testing.T) {
		// Create a builder with MakeTable
		builder := NewBuilder(
			&MakeTable{
				Selector: &BinOp{
					Left:  NewColumnRef("app", types.ColumnTypeLabel),
					Right: NewLiteral("users"),
					Op:    types.BinaryOpEq,
				},
				Shard: NewShard(0, 1),
			},
		)

		// Add Parse operation
		builder = builder.Parse(ParserLogfmt, []string{"level", "status"}, nil)

		// Add Select after Parse
		builder = builder.Select(
			&BinOp{
				Left:  NewColumnRef("level", types.ColumnTypeParsed),
				Right: NewLiteral("error"),
				Op:    types.BinaryOpEq,
			},
		)

		// Convert to plan
		plan, err := builder.ToPlan()
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Verify instruction order with SSA form:
		// 0: BinOp (MakeTable selector: app="users")
		// 1: MakeTable
		// 2: Parse
		// 3: BinOp (Select predicate: level="error")
		// 4: Select
		// 5: Return
		expectedSSA := `%1 = EQ label.app "users"
%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
%3 = PARSE %2 [kind=0, keys=[level status]]
%4 = EQ parsed.level "error"
%5 = SELECT %3 [predicate=%4]
RETURN %5
`
		require.Len(t, plan.Instructions, 6)
		require.Equal(t, expectedSSA, plan.String())

		// Check instruction types in order
		parseInst, ok := plan.Instructions[2].(*Parse)
		require.True(t, ok, "Instruction 2 should be Parse")
		require.Equal(t, ParserLogfmt, parseInst.Kind)
		require.Equal(t, []string{"level", "status"}, parseInst.RequestedKeys)
	})
}
