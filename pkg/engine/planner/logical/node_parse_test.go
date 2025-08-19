package logical

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseInstruction(t *testing.T) {
	t.Run("Create basic Parse instruction", func(t *testing.T) {
		// Create a mock table value for input
		makeTable := &MakeTable{
			id: "make_table_1",
		}
		
		// Create Parse instruction with parser type and requested keys
		parse := &Parse{
			id:            "parse_1",
			Table:         makeTable,
			Kind:          ParserLogfmt,
			RequestedKeys: []string{"level", "status"},
		}

		// Create a logical plan
		plan := Plan{}
		
		// Add the Parse instruction to the plan
		plan.Instructions = append(plan.Instructions, parse)

		// Assert the plan contains the Parse node
		require.Len(t, plan.Instructions, 1)
		require.IsType(t, &Parse{}, plan.Instructions[0])
		
		parsedInstruction := plan.Instructions[0].(*Parse)
		require.Equal(t, ParserLogfmt, parsedInstruction.Kind)
		require.Equal(t, []string{"level", "status"}, parsedInstruction.RequestedKeys)
		require.Equal(t, "parse_1", parsedInstruction.Name())
		require.NotNil(t, parsedInstruction.Table)
	})
}