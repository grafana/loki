package logical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlannerCreatesParseFromLogfmt(t *testing.T) {
	t.Run("Planner creates Parse instruction from LogfmtParserExpr in metric query", func(t *testing.T) {
		// Query with logfmt parser followed by label filter in an instant metric query
		q := &query{
			statement: `sum by (level) (count_over_time({app="test"} | logfmt | level="error" [5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := BuildPlan(q)
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Find the Parse instruction in the plan
		var parseInst *Parse
		for _, inst := range plan.Instructions {
			if p, ok := inst.(*Parse); ok {
				parseInst = p
				break
			}
		}

		// Assert Parse instruction was created
		require.NotNil(t, parseInst, "Parse instruction should be created for logfmt")
		require.Equal(t, ParserLogfmt, parseInst.Kind)
		// The key collector should have identified that "level" needs to be extracted
		require.Contains(t, parseInst.RequestedKeys, "level")
	})
}
