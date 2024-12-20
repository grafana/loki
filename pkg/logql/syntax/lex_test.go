package syntax

import (
	"strings"
	"testing"
	"text/scanner"
	"time"

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
		{
			`{foo="bar"} |~ "\\w+" | latency > 1h0.0m0s or foo == 4.00 and bar ="foo"`,
			[]int{
				OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING,
				PIPE, IDENTIFIER, GT, DURATION, OR, IDENTIFIER, CMP_EQ, NUMBER, AND, IDENTIFIER, EQ, STRING,
			},
		},
		{
			`{foo="bar"} |~ "\\w+" | duration > 1h0.0m0s or avg == 4.00 and bar ="foo"`,
			[]int{
				OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING,
				PIPE, IDENTIFIER, GT, DURATION, OR, IDENTIFIER, CMP_EQ, NUMBER, AND, IDENTIFIER, EQ, STRING,
			},
		},
		{
			`{foo="bar"} |~ "\\w+" | latency > 1h0.0m0s or foo == 4.00 and bar ="foo" | unwrap foo`,
			[]int{
				OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING,
				PIPE, IDENTIFIER, GT, DURATION, OR, IDENTIFIER, CMP_EQ, NUMBER, AND, IDENTIFIER, EQ, STRING, PIPE, UNWRAP, IDENTIFIER,
			},
		},
		{`{foo="bar"} |~ "\\w+" | size > 250kB`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE, IDENTIFIER, GT, BYTES}},
		{
			`{foo="bar"} |~ "\\w+" | size > 250kB and latency <= 1h15m30s or bar=1`,
			[]int{
				OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE,
				IDENTIFIER, GT, BYTES, AND, IDENTIFIER, LTE, DURATION, OR, IDENTIFIER, EQ, NUMBER,
			},
		},
		{
			`{foo="bar"} |~ "\\w+" | size > 200MiB or foo == 4.00`,
			[]int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE, IDENTIFIER, GT, BYTES, OR, IDENTIFIER, CMP_EQ, NUMBER},
		},
		{`{ foo = "bar" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`{ foo != "bar" }`, []int{OPEN_BRACE, IDENTIFIER, NEQ, STRING, CLOSE_BRACE}},
		{`{ foo =~ "bar" }`, []int{OPEN_BRACE, IDENTIFIER, RE, STRING, CLOSE_BRACE}},
		{`{ foo !~ "bar" }`, []int{OPEN_BRACE, IDENTIFIER, NRE, STRING, CLOSE_BRACE}},
		{`{ foo = "bar", bar != "baz" }`, []int{
			OPEN_BRACE, IDENTIFIER, EQ, STRING,
			COMMA, IDENTIFIER, NEQ, STRING, CLOSE_BRACE,
		}},
		{`{ foo = "ba\"r" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`rate({foo="bar"}[10s])`, []int{RATE, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS}},
		{`rate_counter({foo="bar"} | unwrap foo[10s])`, []int{RATE_COUNTER, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, UNWRAP, IDENTIFIER, RANGE, CLOSE_PARENTHESIS}},
		{`count_over_time({foo="bar"}[5m])`, []int{COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS}},
		{`count_over_time({foo="bar"} |~ "\\w+" | unwrap foo[5m])`, []int{COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE_MATCH, STRING, PIPE, UNWRAP, IDENTIFIER, RANGE, CLOSE_PARENTHESIS}},
		{`sum(count_over_time({foo="bar"}[5m])) by (foo,bar)`, []int{SUM, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`SUM(Count_Over_Time({foo="bar"}[5m])) BY (foo,bar)`, []int{SUM, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`SUM BY (foo, bar) (Count_Over_Time({foo="bar"}[5m]))`, []int{SUM, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS}},
		{`topk(3,count_over_time({foo="bar"}[5m])) by (foo,bar)`, []int{TOPK, OPEN_PARENTHESIS, NUMBER, COMMA, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`bottomk(10,sum(count_over_time({foo="bar"}[5m])) by (foo,bar))`, []int{BOTTOMK, OPEN_PARENTHESIS, NUMBER, COMMA, SUM, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS}},
		{`BOTTOMK(10,Sum(COUNT_OVER_TIME({foo="bar"}[5m])) By (foo,bar))`, []int{BOTTOMK, OPEN_PARENTHESIS, NUMBER, COMMA, SUM, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS}},
		{`sum(max(rate({foo="bar"}[5m])) by (foo,bar)) by (foo)`, []int{SUM, OPEN_PARENTHESIS, MAX, OPEN_PARENTHESIS, RATE, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`{foo="bar"} #|~ "\\w+"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`#{foo="bar"} |~ "\\w+"`, []int{}},
		{`{foo="#"}`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`{foo="bar"}|logfmt --strict"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PARSER_FLAG}},
		{`{foo="bar"}|LOGFMT --strict"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PARSER_FLAG}},
		{`{foo="bar"}|logfmt|ip="b"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PIPE, IDENTIFIER, EQ, STRING}},
		{`{foo="bar"}|logfmt|rate="b"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PIPE, IDENTIFIER, EQ, STRING}},
		{`{foo="bar"}|logfmt|b=ip("b")`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PIPE, IDENTIFIER, EQ, IP, OPEN_PARENTHESIS, STRING, CLOSE_PARENTHESIS}},
		{`{foo="bar"}|logfmt|=ip("b")`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PIPE_EXACT, IP, OPEN_PARENTHESIS, STRING, CLOSE_PARENTHESIS}},
		{`{foo="bar"}|logfmt --strict --keep-empty|=ip("b")`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PARSER_FLAG, PARSER_FLAG, PIPE_EXACT, IP, OPEN_PARENTHESIS, STRING, CLOSE_PARENTHESIS}},
		{`ip`, []int{IDENTIFIER}},
		{`rate`, []int{IDENTIFIER}},
		{`{foo="bar"} | json | baz="#"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, JSON, PIPE, IDENTIFIER, EQ, STRING}},
		{`{foo="bar"}
					# |~ "\\w+"
					| json`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, JSON}},
		{`{foo="bar"} | json code="response.code", param="request.params[0]"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, JSON, IDENTIFIER, EQ, STRING, COMMA, IDENTIFIER, EQ, STRING}},
		{`{foo="bar"} | logfmt code="response.code", IPAddress="host"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, IDENTIFIER, EQ, STRING, COMMA, IDENTIFIER, EQ, STRING}},
		{`{foo="bar"} | logfmt --strict code"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PARSER_FLAG, IDENTIFIER}},
		{`{foo="bar"} | logfmt --keep-empty --strict code="response.code", IPAddress="host"`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PARSER_FLAG, PARSER_FLAG, IDENTIFIER, EQ, STRING, COMMA, IDENTIFIER, EQ, STRING}},
		{`decolorize`, []int{DECOLORIZE}},
		{`123`, []int{NUMBER}},
		{`-123`, []int{SUB, NUMBER}},
		{`123.45`, []int{NUMBER}},
		{`-123.45`, []int{SUB, NUMBER}},
		{`123KB`, []int{BYTES}},
		// Skip -123KB: Negative bytes are explicitly not supported in the Lexer.
		{`123ms`, []int{DURATION}},
		{`-123ms`, []int{DURATION}},
		{`34 + - 123`, []int{NUMBER, ADD, SUB, NUMBER}},
		{`34 + -123`, []int{NUMBER, ADD, SUB, NUMBER}},
		{`34+-123`, []int{NUMBER, ADD, SUB, NUMBER}},
		{`34-123`, []int{NUMBER, SUB, NUMBER}},
		{`sum(rate({foo="bar"}[5m])-1 > 30`, []int{SUM, OPEN_PARENTHESIS, RATE, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, RANGE, CLOSE_PARENTHESIS, SUB, NUMBER, GT, NUMBER}},
		{`{foo="bar"} | logfmt | bytes  < 0B`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PIPE, IDENTIFIER, LT, BYTES}},
		{`{foo="bar"} | logfmt | bytes  < 1B`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, PIPE, LOGFMT, PIPE, IDENTIFIER, LT, BYTES}},
		{`0b01`, []int{NUMBER}},
		{`0b10`, []int{NUMBER}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			actual := []int{}
			l := lexer{
				Scanner: Scanner{
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
			sc := Scanner{}
			sc.Init(strings.NewReader(tt.next))
			if got := isFunction(sc); got != tt.want {
				t.Errorf("isFunction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseDuration(t *testing.T) {
	const MICROSECOND = 1000 * time.Nanosecond
	const DAY = 24 * time.Hour
	const WEEK = 7 * DAY
	const YEAR = 365 * DAY

	for _, tc := range []struct {
		input    string
		expected time.Duration
	}{
		{"1ns", time.Nanosecond},
		{"1s", time.Second},
		{"1us", MICROSECOND},
		{"1m", time.Minute},
		{"1h", time.Hour},
		{"1µs", MICROSECOND},
		{"1y", YEAR},
		{"1w", WEEK},
		{"1d", DAY},
		{"1h15m30.918273645s", time.Hour + 15*time.Minute + 30*time.Second + 918273645*time.Nanosecond},
		{"-1s", -1 * time.Second},
	} {
		actual, err := parseDuration(tc.input)

		require.Equal(t, err, nil)
		require.Equal(t, tc.expected, actual)
	}
}
