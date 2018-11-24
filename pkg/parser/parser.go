package parser

import (
	"fmt"
	"strconv"
	"strings"
	"text/scanner"

	"github.com/prometheus/prometheus/pkg/labels"
)

// Matchers parses a string and returns a set of matchers.
func Matchers(input string) ([]*labels.Matcher, error) {
	l, err := parse(MATCHERS, input)
	if err != nil {
		return nil, err
	}
	return l.matcher, nil
}

// Labels parses a string and returns a set of labels.
func Labels(input string) (labels.Labels, error) {
	l, err := parse(LABELS, input)
	if err != nil {
		return nil, err
	}
	return l.labels, nil
}

func parse(thing int, input string) (*lexer, error) {
	l := lexer{
		thing: thing,
		Scanner: scanner.Scanner{
			Mode: scanner.SkipComments | scanner.ScanStrings | scanner.ScanInts,
		},
	}
	l.Init(strings.NewReader(input))
	e := labelsParse(&l)
	if e != 0 {
		return nil, l.err
	}
	return &l, nil
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
	// What type of thing are we parsing?
	thing int
	sent  bool

	// Output
	labels  labels.Labels
	matcher []*labels.Matcher

	scanner.Scanner
	err error
}

func (l *lexer) Lex(lval *labelsSymType) int {
	if !l.sent {
		l.sent = true
		return l.thing
	}

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
