package logql

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/scanner"
	"time"

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

var tokens = map[string]int{
	",":                 COMMA,
	".":                 DOT,
	"{":                 OPEN_BRACE,
	"}":                 CLOSE_BRACE,
	"=":                 EQ,
	"!=":                NEQ,
	"=~":                RE,
	"!~":                NRE,
	"|=":                PIPE_EXACT,
	"|~":                PIPE_MATCH,
	"(":                 OPEN_PARENTHESIS,
	")":                 CLOSE_PARENTHESIS,
	"by":                BY,
	"without":           WITHOUT,
	OpTypeCountOverTime: COUNT_OVER_TIME,
	"[":                 OPEN_BRACKET,
	"]":                 CLOSE_BRACKET,
	OpTypeRate:          RATE,
	OpTypeSum:           SUM,
	OpTypeAvg:           AVG,
	OpTypeMax:           MAX,
	OpTypeMin:           MIN,
	OpTypeCount:         COUNT,
	OpTypeStddev:        STDDEV,
	OpTypeStdvar:        STDVAR,
	OpTypeBottomK:       BOTTOMK,
	OpTypeTopK:          TOPK,
}

type lexer struct {
	scanner.Scanner
	errs   []ParseError
	expr   Expr
	parser *exprParserImpl
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

	// scaning duration tokens
	if l.TokenText() == "[" {
		d := ""
		for r := l.Next(); r != scanner.EOF; r = l.Next() {
			if string(r) == "]" {
				i, err := time.ParseDuration(d)
				if err != nil {
					l.Error(err.Error())
					return 0
				}
				lval.duration = i
				return DURATION
			}
			d += string(r)
		}
		l.Error("missing closing ']' in duration")
		return 0
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
	l.errs = append(l.errs, newParseError(msg, l.Line, l.Column))
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
