package parser

import (
	"regexp"
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
	} {
		t.Run(tc.input, func(t *testing.T) {
			actual := []int{}
			l := lexer{
				Scanner: scanner.Scanner{
					Mode: scanner.SkipComments | scanner.ScanStrings,
				},
			}
			l.Init(strings.NewReader(tc.input))
			var lval yySymType
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

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		input    string
		expected Matchers
	}{
		{`{foo="bar"}`, Matchers{eqStr{"foo", "bar"}}},
		{`{http.url=~"^/admin"}`, Matchers{re{"http.url", regexp.MustCompile("^/admin")}}},
		{`{ foo = "bar" }`, Matchers{eqStr{"foo", "bar"}}},
		{`{ foo != "bar" }`, Matchers{neStr{"foo", "bar"}}},
		{`{ foo =~ "bar" }`, Matchers{re{"foo", regexp.MustCompile("bar")}}},
		{`{ foo !~ "bar" }`, Matchers{nre{"foo", regexp.MustCompile("bar")}}},
		{`{ foo = "bar", bar != "baz" }`, Matchers{eqStr{"foo", "bar"}, neStr{"bar", "baz"}}},
		{`{http.url=~"^/admin", http.status_code!="200"}`, Matchers{re{"http.url", regexp.MustCompile("^/admin")}, neStr{"http.status_code", "200"}}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			output, err := Parse(tc.input)
			require.Nil(t, err)
			require.Equal(t, tc.expected, output)
		})
	}
}
