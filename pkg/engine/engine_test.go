package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestCanExecuteWithNewEngine(t *testing.T) {
	for _, tt := range []struct {
		statement string
		expected  bool
	}{
		{
			statement: `{env="prod"}`,
			expected:  true,
		},
		{
			statement: `{env="prod"} |= "metrics.go"`,
			expected:  true,
		},
		{
			statement: `{env="prod"} |= "metrics.go"`,
			expected:  true,
		},
		{
			statement: `{env="prod"} | tenant="loki"`,
			expected:  true,
		},
		{
			statement: `{env="prod"} | tenant="loki" != "foo"`,
			expected:  true,
		},
		{
			statement: `{env="prod"} | json`,
			expected:  false,
		},
		{
			statement: `{env="prod"} | json foo="bar"`,
			expected:  false,
		},
		{
			statement: `{env="prod"} | logfmt`,
			expected:  false,
		},
		{
			statement: `{env="prod"} | logfmt foo="bar"`,
			expected:  false,
		},
		{
			statement: `{env="prod"} | pattern "<_> foo=<foo> <_>"`,
			expected:  false,
		},
		{
			statement: `{env="prod"} | regexp ".* foo=(?P<foo>.+) .*"`,
			expected:  false,
		},
		{
			statement: `{env="prod"} | unpack`,
			expected:  false,
		},
		{
			statement: `{env="prod"} |= "metrics.go" | logfmt`,
			expected:  false,
		},
		{
			statement: `{env="prod"} | line_format "{.cluster}"`,
			expected:  false,
		},
		{
			statement: `{env="prod"} | label_format cluster="us"`,
			expected:  false,
		},
		{
			statement: `sum(rate({env="prod"}[1m]))`,
			expected:  false,
		},
	} {
		t.Run(tt.statement, func(t *testing.T) {
			expr := syntax.MustParseExpr(tt.statement)
			canExecute := canExecuteWithNewEngine(expr)
			require.Equal(t, tt.expected, canExecute)
		})
	}
}
