package logql

import (
	"errors"
	"fmt"
	"strings"
	"text/scanner"

	"github.com/prometheus/prometheus/pkg/labels"
)

func init() {
	// Improve the error messages coming out of yacc.
	exprErrorVerbose = true
	for str, tok := range tokens {
		exprToknames[tok-exprPrivate+1] = str
	}
}

// ParseExpr parses a string and returns an Expr.
func ParseExpr(input string) (expr Expr, err error) {
	defer func() {
		r := recover()
		if r != nil {
			var ok bool
			if err, ok = r.(error); ok {
				return
			}
		}
	}()
	l := lexer{
		parser: exprNewParser().(*exprParserImpl),
	}
	l.Init(strings.NewReader(input))
	l.Scanner.Error = func(_ *scanner.Scanner, msg string) {
		l.Error(msg)
	}
	e := l.parser.Parse(&l)
	if e != 0 || len(l.errs) > 0 {
		return nil, l.errs[0]
	}
	return l.expr, nil
}

// ParseMatchers parses a string and returns labels matchers, if the expression contains
// anything else it will return an error.
func ParseMatchers(input string) ([]*labels.Matcher, error) {
	expr, err := ParseExpr(input)
	if err != nil {
		return nil, err
	}
	matcherExpr, ok := expr.(*matchersExpr)
	if !ok {
		return nil, errors.New("only label matchers is supported")
	}
	return matcherExpr.matchers, nil
}

// ParseLogSelector parses a log selector expression `{app="foo"} |= "filter"`
func ParseLogSelector(input string) (LogSelectorExpr, error) {
	expr, err := ParseExpr(input)
	if err != nil {
		return nil, err
	}
	logSelector, ok := expr.(LogSelectorExpr)
	if !ok {
		return nil, errors.New("only log selector is supported")
	}
	return logSelector, nil
}

// ParseError is what is returned when we failed to parse.
type ParseError struct {
	msg       string
	line, col int
}

func (p ParseError) Error() string {
	if p.col == 0 && p.line == 0 {
		return fmt.Sprintf("parse error : %s", p.msg)
	}
	return fmt.Sprintf("parse error at line %d, col %d: %s", p.line, p.col, p.msg)
}

func newParseError(msg string, line, col int) ParseError {
	return ParseError{
		msg:  msg,
		line: line,
		col:  col,
	}
}
