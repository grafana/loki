package logql

import (
	"fmt"
	"strconv"
	"strings"
	"text/scanner"

	"github.com/prometheus/prometheus/pkg/labels"
)

// ParseExpr parses a string and returns an Expr.
func ParseExpr(input string) (Expr, error) {
	l := lexer{
		Scanner: scanner.Scanner{
			Mode: scanner.SkipComments | scanner.ScanStrings | scanner.ScanInts,
		},
	}
	l.Init(strings.NewReader(input))
	e := exprParse(&l)
	if e != 0 {
		return nil, l.err
	}
	return l.expr, nil
}

// Expr is a LogQL expression.
type Expr interface {
	Eval()
	Walk(func(Expr) error) error
}

type matchersExpr struct {
	matchers []*labels.Matcher
}

func (e *matchersExpr) Eval() {}

func (e *matchersExpr) Walk(f func(Expr) error) error {
	return f(e)
}

type matchExpr struct {
	left  Expr
	ty    labels.MatchType
	match string
}

func (e *matchExpr) Eval() {}

func (e *matchExpr) Walk(f func(Expr) error) error {
	if err := f(e); err != nil {
		return err
	}
	return e.left.Walk(f)
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
	"|=": PIPE_EXACT,
	"|~": PIPE_MATCH,
}

type lexer struct {
	scanner.Scanner
	err error

	expr Expr
}

func (l *lexer) Lex(lval *exprSymType) int {
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

	if tok, ok := tokens[l.TokenText()+string(l.Peek())]; ok {
		l.Next()
		return tok
	}

	if tok, ok := tokens[l.TokenText()]; ok {
		return tok
	}

	lval.str = l.TokenText()
	return IDENTIFIER
}

func (l *lexer) Error(s string) {
	l.err = fmt.Errorf(s)
}
