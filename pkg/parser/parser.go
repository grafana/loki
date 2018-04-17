package parser

import (
	"fmt"
	"strconv"
	"strings"
	"text/scanner"

	"github.com/prometheus/prometheus/pkg/labels"
)

func Matchers(input string) ([]*labels.Matcher, error) {
	l := lexer{
		Scanner: scanner.Scanner{
			Mode: scanner.SkipComments | scanner.ScanStrings | scanner.ScanInts,
		},
	}
	l.Init(strings.NewReader(input))
	parser := labelsNewParser()
	e := parser.Parse(&l)
	if e != 0 {
		return nil, l.err
	}
	return l.matcher, nil
}

func mustNewMatcher(t labels.MatchType, n, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(err)
	}
	return m
}

var tokens = map[string]int{
	",":  COMMA,
	".":  DOT,
	"{":  OPEN_BRACE,
	"}":  CLOSE_BRACE,
	"=":  EQ,
	"!=": NEQ,
	"=~": RE,
	"!~": NRE,
}

type lexer struct {
	labels  labels.Labels
	matcher []*labels.Matcher
	scanner.Scanner
	err error
}

func (l *lexer) Lex(lval *labelsSymType) int {
	r := l.Scan()
	var err error
	switch r {
	case scanner.EOF:
		return 0

	case scanner.String:
		lval.str, err = strconv.Unquote(l.TokenText())
		if err != nil {
			l.err = err
			return 0
		}
		return STRING
	}

	switch l.TokenText() {
	case "=":
		if l.Peek() == '~' {
			l.Scan()
			return RE
		}
		return EQ
	case "!":
		if l.Peek() == '=' {
			l.Scan()
			return NEQ
		} else if l.Peek() == '~' {
			l.Scan()
			return NRE
		}
	}

	if token, ok := tokens[l.TokenText()]; ok {
		return token
	}

	lval.str = l.TokenText()
	return IDENTIFIER
}

func (l *lexer) Error(s string) {
	l.err = fmt.Errorf(s)
}
