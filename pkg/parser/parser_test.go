package parser

import (
	"strings"
	"testing"
	"text/scanner"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestLex(t *testing.T) {
	for _, tc := range []struct {
		input    string
		expected []int
	}{
		{`{foo="bar"}`, []int{MATCHERS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`{ foo = "bar" }`, []int{MATCHERS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`{ foo != "bar" }`, []int{MATCHERS, OPEN_BRACE, IDENTIFIER, NEQ, STRING, CLOSE_BRACE}},
		{`{ foo =~ "bar" }`, []int{MATCHERS, OPEN_BRACE, IDENTIFIER, RE, STRING, CLOSE_BRACE}},
		{`{ foo !~ "bar" }`, []int{MATCHERS, OPEN_BRACE, IDENTIFIER, NRE, STRING, CLOSE_BRACE}},
		{`{ foo = "bar", bar != "baz" }`, []int{MATCHERS, OPEN_BRACE, IDENTIFIER, EQ, STRING,
			COMMA, IDENTIFIER, NEQ, STRING, CLOSE_BRACE}},
		{`{ foo = "ba\"r" }`, []int{MATCHERS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			actual := []int{}
			l := lexer{
				thing: MATCHERS,
				Scanner: scanner.Scanner{
					Mode: scanner.SkipComments | scanner.ScanStrings,
				},
			}
			l.Init(strings.NewReader(tc.input))
			var lval labelsSymType
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
		expected []*labels.Matcher
	}{
		{
			`{foo="bar"}`,
			[]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
		},
		{
			`{http.url=~"^/admin"}`,
			[]*labels.Matcher{mustNewMatcher(labels.MatchRegexp, "http.url", "^/admin")},
		},
		{
			`{ foo = "bar" }`,
			[]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
		},
		{
			`{ foo != "bar" }`,
			[]*labels.Matcher{mustNewMatcher(labels.MatchNotEqual, "foo", "bar")},
		},
		{
			`{ foo =~ "bar" }`,
			[]*labels.Matcher{mustNewMatcher(labels.MatchRegexp, "foo", "bar")},
		},
		{
			`{ foo !~ "bar" }`,
			[]*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")},
		},
		{
			`{ foo = "bar", bar != "baz" }`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "foo", "bar"),
				mustNewMatcher(labels.MatchNotEqual, "bar", "baz"),
			},
		},
		{
			`{http.url=~"^/admin", http.status_code!="200"}`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchRegexp, "http.url", "^/admin"),
				mustNewMatcher(labels.MatchNotEqual, "http.status_code", "200"),
			},
		},
	} {
		t.Run(tc.input, func(t *testing.T) {
			output, err := Matchers(tc.input)
			require.Nil(t, err)
			require.Equal(t, tc.expected, output)
		})
	}
}
