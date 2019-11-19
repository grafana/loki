package logql

import (
	"strings"
	"testing"
	"text/scanner"

	"github.com/stretchr/testify/require"
)

func TestLex(t *testing.T) {
	for _, tc := range []struct {
		input    string
		expected []int
	}{
		{`{foo="bar"}`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`{ foo = "bar" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`{ foo != "bar" }`, []int{OPEN_BRACE, IDENTIFIER, NEQ, STRING, CLOSE_BRACE}},
		{`{ foo =~ "bar" }`, []int{OPEN_BRACE, IDENTIFIER, RE, STRING, CLOSE_BRACE}},
		{`{ foo !~ "bar" }`, []int{OPEN_BRACE, IDENTIFIER, NRE, STRING, CLOSE_BRACE}},
		{`{ foo = "bar", bar != "baz" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING,
			COMMA, IDENTIFIER, NEQ, STRING, CLOSE_BRACE}},
		{`{ foo = "ba\"r" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`rate({foo="bar"}[10s])`, []int{RATE, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS}},
		{`count_over_time({foo="bar"}[5m])`, []int{COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS}},
		{`sum(count_over_time({foo="bar"}[5m])) by (foo,bar)`, []int{SUM, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`topk(3,count_over_time({foo="bar"}[5m])) by (foo,bar)`, []int{TOPK, OPEN_PARENTHESIS, IDENTIFIER, COMMA, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`bottomk(10,sum(count_over_time({foo="bar"}[5m])) by (foo,bar))`, []int{BOTTOMK, OPEN_PARENTHESIS, IDENTIFIER, COMMA, SUM, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS}},
		{`sum(max(rate({foo="bar"}[5m])) by (foo,bar)) by (foo)`, []int{SUM, OPEN_PARENTHESIS, MAX, OPEN_PARENTHESIS, RATE, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, CLOSE_PARENTHESIS}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			actual := []int{}
			l := lexer{
				Scanner: scanner.Scanner{
					Mode: scanner.SkipComments | scanner.ScanStrings,
				},
			}
			l.Init(strings.NewReader(tc.input))
			var lval exprSymType
			for {
				tok := l.Lex(&lval)
				if tok == 0 {
					break
				}
				actual = append(actual, tok)
			}
			require.Equal(t, tc.expected, actual)
		})
	}
}
