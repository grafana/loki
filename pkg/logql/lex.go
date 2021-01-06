package logql

import (
	"strings"
	"text/scanner"
	"time"
	"unicode"

	"github.com/dustin/go-humanize"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/util/strutil"
)

var tokens = map[string]int{
	",":            COMMA,
	".":            DOT,
	"{":            OPEN_BRACE,
	"}":            CLOSE_BRACE,
	"=":            EQ,
	OpTypeNEQ:      NEQ,
	"=~":           RE,
	"!~":           NRE,
	"|=":           PIPE_EXACT,
	"|~":           PIPE_MATCH,
	OpPipe:         PIPE,
	OpUnwrap:       UNWRAP,
	"(":            OPEN_PARENTHESIS,
	")":            CLOSE_PARENTHESIS,
	"by":           BY,
	"without":      WITHOUT,
	"bool":         BOOL,
	"[":            OPEN_BRACKET,
	"]":            CLOSE_BRACKET,
	OpLabelReplace: LABEL_REPLACE,

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

// functionTokens are tokens that needs to be suffixes with parenthesis
var functionTokens = map[string]int{
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
	OpRangeTypeQuantile:  QUANTILE_OVER_TIME,
	OpRangeTypeAbsent:    ABSENT_OVER_TIME,

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

	// conversion Op
	OpConvBytes:           BYTES_CONV,
	OpConvDuration:        DURATION_CONV,
	OpConvDurationSeconds: DURATION_SECONDS_CONV,
}

type lexer struct {
	scanner.Scanner
	errs []ParseError
}

func (l *lexer) Lex(lval *exprSymType) int {
	r := l.Scan()

	switch r {
	case '#':
		// Scan until a newline or EOF is encountered
		for next := l.Peek(); !(next == '\n' || next == scanner.EOF); next = l.Next() {
		}

		return l.Lex(lval)

	case scanner.EOF:
		return 0

	case scanner.Int, scanner.Float:
		numberText := l.TokenText()

		duration, ok := tryScanDuration(numberText, &l.Scanner)
		if ok {
			lval.duration = duration
			return DURATION
		}

		bytes, ok := tryScanBytes(numberText, &l.Scanner)
		if ok {
			lval.bytes = bytes
			return BYTES
		}

		lval.str = numberText
		return NUMBER

	case scanner.String, scanner.RawString:
		var err error
		lval.str, err = strutil.Unquote(l.TokenText())
		if err != nil {
			l.Error(err.Error())
			return 0
		}
		return STRING
	}

	// scanning duration tokens
	if r == '[' {
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

	tokenText := l.TokenText()
	tokenNext := tokenText + string(l.Peek())
	if tok, ok := functionTokens[tokenNext]; ok {
		// create a copy to advance to the entire token for testing suffix
		sc := l.Scanner
		sc.Next()
		if isFunction(sc) {
			l.Next()
			return tok
		}
	}

	if tok, ok := functionTokens[tokenText]; ok && isFunction(l.Scanner) {
		return tok
	}

	if tok, ok := tokens[tokenNext]; ok {
		l.Next()
		return tok
	}

	if tok, ok := tokens[tokenText]; ok {
		return tok
	}

	lval.str = tokenText
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

func tryScanBytes(number string, l *scanner.Scanner) (uint64, bool) {
	var sb strings.Builder
	sb.WriteString(number)
	//copy the scanner to avoid advancing it in case it's not a duration.
	s := *l
	consumed := 0
	for r := s.Peek(); r != scanner.EOF && !unicode.IsSpace(r); r = s.Peek() {
		if !unicode.IsNumber(r) && !isBytesSizeRune(r) && r != '.' {
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
	b, err := humanize.ParseBytes(sb.String())
	if err != nil {
		return 0, false
	}
	// we need to consume the scanner, now that we know this is a duration.
	for i := 0; i < consumed; i++ {
		_ = l.Next()
	}
	return b, true
}

func isBytesSizeRune(r rune) bool {
	// Accept: B, kB, MB, GB, TB, PB, KB, KiB, MiB, GiB, TiB, PiB
	// Do not accept: EB, ZB, YB, PiB, ZiB and YiB. They are not supported since the value migh not be represented in an uint64
	switch r {
	case 'B', 'i', 'k', 'K', 'M', 'G', 'T', 'P':
		return true
	default:
		return false
	}
}

// isFunction check if the next runes are either an open parenthesis
// or by/without tokens. This allows to dissociate functions and identifier correctly.
func isFunction(sc scanner.Scanner) bool {
	var sb strings.Builder
	sc = trimSpace(sc)
	for r := sc.Next(); r != scanner.EOF; r = sc.Next() {
		sb.WriteRune(r)
		switch sb.String() {
		case "(":
			return true
		case "by", "without":
			sc = trimSpace(sc)
			return sc.Next() == '('
		}
	}
	return false
}

func trimSpace(l scanner.Scanner) scanner.Scanner {
	for n := l.Peek(); n != scanner.EOF; n = l.Peek() {
		if unicode.IsSpace(n) {
			l.Next()
			continue
		}
		return l
	}
	return l
}
