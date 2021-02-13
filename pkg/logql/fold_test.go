package logql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Foldable(t *testing.T) {

	expr, err := ParseExpr(`sum by (cluster) (rate({job="foo"} |= "bar" | logfmt | bazz="buzz" [5m]))`)
	require.Nil(t, err)

	type audit struct {
		groupings         []*grouping
		logfmt            bool
		filter            string
		postParseMatchers string
	}

	expected := audit{
		groupings: []*grouping{
			&grouping{
				groups:  []string{"cluster"},
				without: false,
			},
		},
		logfmt:            true,
		filter:            "bar",
		postParseMatchers: `bazz="buzz"`,
	}

	got, err := expr.Fold(
		func(accum, x interface{}) (interface{}, error) {
			acc := accum.(audit)

			switch e := x.(type) {
			case *vectorAggregationExpr:
				acc.groupings = append(acc.groupings, e.grouping)
			case *lineFilterExpr:
				acc.filter = e.match
			case *labelParserExpr:
				if e.op == OpParserTypeLogfmt {
					acc.logfmt = true
				}
			case *labelFilterExpr:
				acc.postParseMatchers = e.LabelFilterer.String()

			}
			return acc, nil
		},
		audit{},
	)
	require.Nil(t, err)
	require.Equal(t, expected, got)

}
