package logql

import (
	"strconv"
	"strings"
	"text/scanner"
	"time"
	"unicode"

	"github.com/prometheus/common/model"
)

var tokens = map[string]int{
	",":       COMMA,
	".":       DOT,
	"{":       OPEN_BRACE,
	"}":       CLOSE_BRACE,
	"=":       EQ,
	OpTypeNEQ: NEQ,
	"=~":      RE,
	"!~":      NRE,
	"|=":      PIPE_EXACT,
	"|~":      PIPE_MATCH,
	OpPipe:    PIPE,
	OpUnwrap:  UNWRAP,
	"(":       OPEN_PARENTHESIS,
	")":       CLOSE_PARENTHESIS,
	"by":      BY,
	"without": WITHOUT,
	"bool":    BOOL,
	"[":       OPEN_BRACKET,
	"]":       CLOSE_BRACKET,

	// range vec ops
	OpRangeTypeRate:      RATE,
	OpRangeTypeCount:     COUNT_OVER_TIME,
	OpRangeTypeBytesRate: BYTES_RATE,
	OpRangeTypeBytes:     BYTES_OVER_TIME,
	OpRangeTypeAvg:       AVG_OVER_TIME,
	OpRangeTypeSum:       SUM_OVER_TIME,
	OpRangeTypeMin:       MIN_OVER_TIME,
	OpRangeTypeMax:       MAX_OVER_TIME,
	OpRangeTypeStdvar:    STDVAR_OVER_TIME,
	OpRangeTypeStddev:    STDDEV_OVER_TIME,

	// vec ops
	OpTypeSum:     SUM,
	OpTypeAvg:     AVG,
	OpTypeMax:     MAX,
	OpTypeMin:     MIN,
	OpTypeCount:   COUNT,
	OpTypeStddev:  STDDEV,
	OpTypeStdvar:  STDVAR,
	OpTypeBottomK: BOTTOMK,
	OpTypeTopK:    TOPK,

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

	// parsers
	OpParserTypeJSON:   JSON,
	OpParserTypeRegexp: REGEXP,
	OpParserTypeLogfmt: LOGFMT,

	// fmt
	OpFmtLabel: LABEL_FMT,
	OpFmtLine:  LINE_FMT,
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
		numberText := l.TokenText()
		duration, ok := tryScanDuration(numberText, &l.Scanner)
		if !ok {
			lval.str = numberText
			return NUMBER
		}
		lval.duration = duration
		return DURATION

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
				return RANGE
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

func tryScanDuration(number string, l *scanner.Scanner) (time.Duration, bool) {
	var sb strings.Builder
	sb.WriteString(number)
	//copy the scanner to avoid advancing it in case it's not a duration.
	s := *l
	consumed := 0
	for r := s.Peek(); r != scanner.EOF && !unicode.IsSpace(r); r = s.Peek() {
		if !unicode.IsNumber(r) && !isDurationRune(r) && r != '.' {
			break
		}
		_, _ = sb.WriteRune(r)
		_ = s.Next()
		consumed++
	}

	if consumed == 0 {
		return 0, false
	}
	// we've found more characters before a whitespace or the end
	d, err := time.ParseDuration(sb.String())
	if err != nil {
		return 0, false
	}
	// we need to consume the scanner, now that we know this is a duration.
	for i := 0; i < consumed; i++ {
		_ = l.Next()
	}
	return d, true
}

func isDurationRune(r rune) bool {
	// "ns", "us" (or "µs"), "ms", "s", "m", "h".
	switch r {
	case 'n', 's', 'u', 'm', 'h', 'µ':
		return true
	default:
		return false
	}
}
