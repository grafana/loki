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
		{"{foo=\"bar\"} |~  `\\w+`", []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING}},
		{`{foo="bar"} |~ "\\w+"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING}},
		{`{foo="bar"} |~ "\\w+" | latency > 250ms`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE, IDENTIFIER, GT, DURATION}},
		{`{foo="bar"} |~ "\\w+" | foo = 0ms`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE, IDENTIFIER, EQ, DURATION}},
		{`{foo="bar"} |~ "\\w+" | latency > 1h15m30.918273645s`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE, IDENTIFIER, GT, DURATION}},
		{`{foo="bar"} |~ "\\w+" | latency > 1h0.0m0s`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE, IDENTIFIER, GT, DURATION}},
		{`{foo="bar"} |~ "\\w+" | latency > 1h0.0m0s or foo == 4.00 and bar ="foo"`,
			[]int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING,
				PIPE, IDENTIFIER, GT, DURATION, OR, IDENTIFIER, CMP_EQ, NUMBER, AND, IDENTIFIER, EQ, STRING}},
		{`{foo="bar"} |~ "\\w+" | duration > 1h0.0m0s or avg == 4.00 and bar ="foo"`,
			[]int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING,
				PIPE, IDENTIFIER, GT, DURATION, OR, IDENTIFIER, CMP_EQ, NUMBER, AND, IDENTIFIER, EQ, STRING}},
		{`{foo="bar"} |~ "\\w+" | latency > 1h0.0m0s or foo == 4.00 and bar ="foo" | unwrap foo`,
			[]int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING,
				PIPE, IDENTIFIER, GT, DURATION, OR, IDENTIFIER, CMP_EQ, NUMBER, AND, IDENTIFIER, EQ, STRING, PIPE, UNWRAP, IDENTIFIER}},
		{`{foo="bar"} |~ "\\w+" | size > 250kB`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE, IDENTIFIER, GT, BYTES}},
		{`{foo="bar"} |~ "\\w+" | size > 250kB and latency <= 1h15m30s or bar=1`,
			[]int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE,
				IDENTIFIER, GT, BYTES, AND, IDENTIFIER, LTE, DURATION, OR, IDENTIFIER, EQ, NUMBER}},
		{`{foo="bar"} |~ "\\w+" | size > 200MiB or foo == 4.00`,
			[]int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE, IDENTIFIER, GT, BYTES, OR, IDENTIFIER, CMP_EQ, NUMBER}},
		{`{ foo = "bar" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`{ foo != "bar" }`, []int{OPEN_BRACE, IDENTIFIER, NEQ, STRING, CLOSE_BRACE}},
		{`{ foo =~ "bar" }`, []int{OPEN_BRACE, IDENTIFIER, RE, STRING, CLOSE_BRACE}},
		{`{ foo !~ "bar" }`, []int{OPEN_BRACE, IDENTIFIER, NRE, STRING, CLOSE_BRACE}},
		{`{ foo = "bar", bar != "baz" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING,
			COMMA, IDENTIFIER, NEQ, STRING, CLOSE_BRACE}},
		{`{ foo = "ba\"r" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`rate({foo="bar"}[10s])`, []int{RATE, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS}},
		{`count_over_time({foo="bar"}[5m])`, []int{COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS}},
		{`count_over_time({foo="bar"} |~ "\\w+" | unwrap foo[5m])`, []int{COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE, UNWRAP, IDENTIFIER, RANGE, CLOSE_PARENTHESIS}},
		{`sum(count_over_time({foo="bar"}[5m])) by (foo,bar)`, []int{SUM, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`topk(3,count_over_time({foo="bar"}[5m])) by (foo,bar)`, []int{TOPK, OPEN_PARENTHESIS, NUMBER, COMMA, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`bottomk(10,sum(count_over_time({foo="bar"}[5m])) by (foo,bar))`, []int{BOTTOMK, OPEN_PARENTHESIS, NUMBER, COMMA, SUM, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS}},
		{`sum(max(rate({foo="bar"}[5m])) by (foo,bar)) by (foo)`, []int{SUM, OPEN_PARENTHESIS, MAX, OPEN_PARENTHESIS, RATE, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`{foo="bar"} #|~ "\\w+"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`#{foo="bar"} |~ "\\w+"`, []int{}},
		{`{foo="#"}`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`{foo="bar"} | json | baz="#"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, JSON, PIPE, IDENTIFIER, EQ, STRING}},
		{`{foo="bar"}
					# |~ "\\w+"
					| json`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, JSON}},
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

func Test_isFunction(t *testing.T) {
	tests := []struct {
		next string
		want bool
	}{
		{"   (", true},
		{"(", true},
		{"by (", true},
		{"by(", true},
		{"by    (", true},
		{"  by (", true},
		{" by(", true},
		{"by    (", true},
		{"without (", true},
		{"without(", true},
		{"without    (", true},
		{"  without (", true},
		{" without(", true},
		{"without    (", true},
		{"   ( whatever is this", true},
		{"   (foo,bar)", true},
		{"\r\n \t\t\r\n \n   (foo,bar)", true},

		{"  foo (", false},
		{"123", false},
		{"", false},
		{"   ", false},
		{"   )(", false},
		{"byfoo", false},
		{"without foo", false},
	}
	for _, tt := range tests {
		t.Run(tt.next, func(t *testing.T) {
			sc := scanner.Scanner{}
			sc.Init(strings.NewReader(tt.next))
			if got := isFunction(sc); got != tt.want {
				t.Errorf("isFunction() = %v, want %v", got, tt.want)
			}
		})
	}
}
