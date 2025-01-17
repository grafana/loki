package syntax

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDepthFirstTraversalVisitor(t *testing.T) {

	visited := [][2]string{}

	visitor := &DepthFirstTraversal{
		VisitLabelParserFn: func(_ RootVisitor, e *LabelParserExpr) {
			visited = append(visited, [2]string{fmt.Sprintf("%T", e), e.String()})
		},
		VisitLineFilterFn: func(_ RootVisitor, e *LineFilterExpr) {
			visited = append(visited, [2]string{fmt.Sprintf("%T", e), e.String()})
		},
		VisitLogfmtParserFn: func(_ RootVisitor, e *LogfmtParserExpr) {
			visited = append(visited, [2]string{fmt.Sprintf("%T", e), e.String()})
		},
		VisitMatchersFn: func(_ RootVisitor, e *MatchersExpr) {
			visited = append(visited, [2]string{fmt.Sprintf("%T", e), e.String()})
		},
	}

	// Only expressions that have a Visit function defined are added to the list
	expected := [][2]string{
		{"*syntax.MatchersExpr", `{env="prod"}`},
		{"*syntax.LineFilterExpr", `|= "foo" or "bar"`},
		{"*syntax.LogfmtParserExpr", `| logfmt`},
		{"*syntax.MatchersExpr", `{env="dev"}`},
		{"*syntax.LineFilterExpr", `|~ "(foo|bar)"`},
		{"*syntax.LabelParserExpr", `| json`},
	}

	query := `
	sum by (container) (min_over_time({env="prod"} |= "foo" or "bar" | logfmt | unwrap duration [1m]))
	/
	sum by (container) (max_over_time({env="dev"} |~ "(foo|bar)" | json | unwrap duration [1m]))
	`
	expr, err := ParseExpr(query)
	require.NoError(t, err)
	expr.Accept(visitor)
	require.Equal(t, expected, visited)
}
