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
			expr{capture("foo"), literal(' '), literal('b'), literal('a'), literal('r'), literal(' '), literal('f'), literal(' '), capture("f")},
			nil,
		},
		{
			"<foo",
			expr{literal('<'), literal('f'), literal('o'), literal('o')},
			nil,
		},
		{
			"<foo ><bar>",
			expr{literal('<'), literal('f'), literal('o'), literal('o'), literal(' '), literal('>'), capture("bar")},
			nil,
		},
		{
			"<>",
			expr{literal('<'), literal('>')},
			nil,
		},
		{
			"<_>",
			expr{capture("_")},
			nil,
		},
		{
			"<1_>",
			expr{literal('<'), literal('1'), literal('_'), literal('>')},
			nil,
		},
		{
			"<ip> - <user> [<_>] “<method> <path> <_>” <status> <size> <url> <user_agent>",
			expr{capture("ip"), literal(' '), literal('-'), literal(' '), capture("user"), literal(' '), literal('['), capture("_"), literal(']'), literal(' ')},
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
