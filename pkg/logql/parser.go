package logql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/scanner"

	"github.com/grafana/loki/pkg/iter"
	"github.com/prometheus/prometheus/pkg/labels"
)

// ParseExpr parses a string and returns an Expr.
func ParseExpr(input string) (Expr, error) {
	var l lexer
	l.Init(strings.NewReader(input))
	//l.Scanner.Mode = scanner.SkipComments | scanner.ScanStrings | scanner.ScanInts
	l.Scanner.Error = func(_ *scanner.Scanner, msg string) {
		l.Error(msg)
	}

	e := exprParse(&l)
	if e != 0 || l.err != nil {
		return nil, l.err
	}
	return l.expr, nil
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
	err  error
	expr Expr
}

func (l *lexer) Lex(lval *exprSymType) int {
	r := l.Scan()

	switch r {
	case scanner.EOF:
		return 0

	case scanner.String:
		var err error
		lval.str, err = strconv.Unquote(l.TokenText())
		if err != nil {
			l.Error(err.Error())
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

func (l *lexer) Error(msg string) {
	// We want to return the first error (from the lexer), and ignore subsequent ones.
	if l.err != nil {
		return
	}

	l.err = ParseError{
		msg:  msg,
		line: l.Line,
		col:  l.Column,
	}
}

// Expr is a LogQL expression.
type Expr interface {
	Eval(Querier) (iter.EntryIterator, error)
}

type Querier interface {
	Query([]*labels.Matcher) (iter.EntryIterator, error)
}

type matchersExpr struct {
	matchers []*labels.Matcher
}

func (e *matchersExpr) Eval(q Querier) (iter.EntryIterator, error) {
	return q.Query(e.matchers)
}

type matchExpr struct {
	left  Expr
	ty    labels.MatchType
	match string
}

func (e *matchExpr) Eval(q Querier) (iter.EntryIterator, error) {
	var f func(string) bool
	switch e.ty {
	case labels.MatchRegexp:
		re, err := regexp.Compile(e.match)
		if err != nil {
			return nil, err
		}
		f = re.MatchString

	case labels.MatchNotRegexp:
		re, err := regexp.Compile(e.match)
		if err != nil {
			return nil, err
		}
		f = func(line string) bool {
			return !re.MatchString(line)
		}

	case labels.MatchEqual:
		f = func(line string) bool {
			return strings.Contains(line, e.match)
		}

	case labels.MatchNotEqual:
		f = func(line string) bool {
			return !strings.Contains(line, e.match)
		}

	default:
		return nil, fmt.Errorf("unknow matcher: %v", e.match)
	}

	left, err := e.left.Eval(q)
	if err != nil {
		return nil, err
	}

	return iter.NewFilter(f, left), nil
}

func mustNewMatcher(t labels.MatchType, n, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(err)
	}
	return m
}

type ParseError struct {
	msg       string
	line, col int
}

func (p ParseError) Error() string {
	return fmt.Sprintf("parse error at line %d, col %d: %s", p.line, p.col, p.msg)
}
