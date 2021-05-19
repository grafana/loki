package pattern

import "fmt"

const underscore = "_"

var tokens = map[int]string{
	LESS_THAN:  "<",
	MORE_THAN:  ">",
	UNDERSCORE: underscore,
}

func init() {
	// Improve the error messages coming out of yacc.
	exprErrorVerbose = true
	for tok, str := range tokens {
		exprToknames[tok-exprPrivate+1] = str
	}
}

func parseExpr(input string) (expr, error) {
	l := newLexer()
	l.setData([]byte(input))
	e := exprNewParser().Parse(l)
	if e != 0 || len(l.errs) > 0 {
		return nil, l.errs[0]
	}
	return l.expr, nil
}

// parseError is what is returned when we failed to parse.
type parseError struct {
	msg       string
	line, col int
}

func (p parseError) Error() string {
	if p.col == 0 && p.line == 0 {
		return p.msg
	}
	return fmt.Sprintf("parse error at line %d, col %d: %s", p.line, p.col, p.msg)
}

func newParseError(msg string, line, col int) parseError {
	return parseError{
		msg:  msg,
		line: line,
		col:  col,
	}
}
