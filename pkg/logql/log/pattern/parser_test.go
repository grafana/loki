package pattern

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Parse(t *testing.T) {
	for _, tc := range []struct {
		input    string
		expected expr
		err      error
	}{
		{
			"<foo> bar f <f>",
			expr{capture("foo"), literals(" bar f "), capture("f")},
			nil,
		},
		{
			"<foo",
			expr{literals("<foo")},
			nil,
		},
		{
			"<foo ><bar>",
			expr{literals("<foo >"), capture("bar")},
			nil,
		},
		{
			"<>",
			expr{literals("<>")},
			nil,
		},
		{
			"<_>",
			expr{capture("_")},
			nil,
		},
		{
			"<1_>",
			expr{literals("<1_>")},
			nil,
		},
		{
			`<ip> - <user> [<_>] "<method> <path> <_>" <status> <size> <url> <user_agent>`,
			expr{capture("ip"), literals(" - "), capture("user"), literals(" ["), capture("_"), literals(`] "`), capture("method"), literals(" "), capture("path"), literals(" "), capture('_'), literals(`" `), capture("status"), literals(" "), capture("size"), literals(" "), capture("url"), literals(" "), capture("user_agent")},
			nil,
		},
	} {
		tc := tc
		actual, err := parseExpr(tc.input)
		if tc.err != nil || err != nil {
			require.Equal(t, tc.err, err)
			return
		}
		require.Equal(t, tc.expected, actual)
	}
}
