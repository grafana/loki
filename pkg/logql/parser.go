package logql

import (
	"fmt"
	"strconv"
	"strings"
	"text/scanner"
)

func init() {
	// Improve the error messages coming out of yacc.
	exprErrorVerbose = true
	for str, tok := range tokens {
		exprToknames[tok-exprPrivate+1] = str
	}
}

// ParseExpr parses a string and returns an Expr.
func ParseExpr(input string) (Expr, error) {
	var l = lexer{
		parser: exprNewParser().(*exprParserImpl),
	}
	l.Init(strings.NewReader(input))
	//l.Scanner.Mode = scanner.SkipComments | scanner.ScanStrings | scanner.ScanInts
	l.Scanner.Error = func(_ *scanner.Scanner, msg string) {
		l.Error(msg)
	}

	e := l.parser.Parse(&l)
	if e != 0 || len(l.errs) > 0 {
		return nil, l.errs[0]
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
	l.errs = append(l.errs, ParseError{
		msg:  msg,
		line: l.Line,
		col:  l.Column,
	})
}

// ParseError is what is returned when we failed to parse.
type ParseError struct {
	msg       string
	line, col int
}

func (p ParseError) Error() string {
	return fmt.Sprintf("parse error at line %d, col %d: %s", p.line, p.col, p.msg)
}
