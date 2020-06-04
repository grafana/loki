package logql

import (
	"strconv"
	"text/scanner"
	"time"

	"github.com/prometheus/common/model"
)

var tokens = map[string]int{
	",":                  COMMA,
	".":                  DOT,
	"{":                  OPEN_BRACE,
	"}":                  CLOSE_BRACE,
	"=":                  EQ,
	OpTypeNEQ:            NEQ,
	"=~":                 RE,
	"!~":                 NRE,
	"|=":                 PIPE_EXACT,
	"|~":                 PIPE_MATCH,
	"(":                  OPEN_PARENTHESIS,
	")":                  CLOSE_PARENTHESIS,
	"by":                 BY,
	"without":            WITHOUT,
	"bool":               BOOL,
	"[":                  OPEN_BRACKET,
	"]":                  CLOSE_BRACKET,
	OpRangeTypeRate:      RATE,
	OpRangeTypeCount:     COUNT_OVER_TIME,
	OpRangeTypeBytesRate: BYTES_RATE,
	OpRangeTypeBytes:     BYTES_OVER_TIME,
	OpTypeSum:            SUM,
	OpTypeAvg:            AVG,
	OpTypeMax:            MAX,
	OpTypeMin:            MIN,
	OpTypeCount:          COUNT,
	OpTypeStddev:         STDDEV,
	OpTypeStdvar:         STDVAR,
	OpTypeBottomK:        BOTTOMK,
	OpTypeTopK:           TOPK,

	// binops
	OpTypeOr:     OR,
	OpTypeAnd:    AND,
	OpTypeUnless: UNLESS,
	OpTypeAdd:    ADD,
	OpTypeSub:    SUB,
	OpTypeMul:    MUL,
	OpTypeDiv:    DIV,
	OpTypeMod:    MOD,
	OpTypePow:    POW,
	// comparison binops
	OpTypeCmpEQ: CMP_EQ,
	OpTypeGT:    GT,
	OpTypeGTE:   GTE,
	OpTypeLT:    LT,
	OpTypeLTE:   LTE,
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

	case scanner.Int, scanner.Float:
		lval.str = l.TokenText()
		return NUMBER

	case scanner.String, scanner.RawString:
		var err error
		lval.str, err = strconv.Unquote(l.TokenText())
		if err != nil {
			l.Error(err.Error())
			return 0
		}
		return STRING
	}

	// scanning duration tokens
	if l.TokenText() == "[" {
		d := ""
		for r := l.Next(); r != scanner.EOF; r = l.Next() {
			if string(r) == "]" {
				i, err := model.ParseDuration(d)
				if err != nil {
					l.Error(err.Error())
					return 0
				}
				lval.duration = time.Duration(i)
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
