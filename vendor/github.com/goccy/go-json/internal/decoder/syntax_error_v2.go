//go:build go1.27 && goexperiment.jsonv2

package decoder

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/goccy/go-json/internal/errors"
)

// text returns the words of the message of a syntax error which tell where the byte is, as encoding/json of Go 1.27
// has them: the ones of encoding/json/jsontext, which are made the ones of encoding/json.
func (w syntaxWhere) text() string {
	switch w {
	case whereKey:
		return "looking for beginning of object key string"
	case whereAfterKey:
		return "after object key"
	case whereAfterPair:
		return "after object key:value pair"
	case whereAfterElement:
		return "after array element"
	case whereAfterTop:
		return "after top-level value"
	case whereString:
		return "in string"
	case whereNumber, whereFraction, whereExponent:
		return "in numeric literal"
	}
	return "looking for beginning of value"
}

// invalidCharacterError returns the syntax error of the character at cursor, which the offset is after: the
// character of the UTF-8 sequence at cursor, or its byte if it is not one.
func invalidCharacterError(buf []byte, cursor int64, what string) *errors.SyntaxError {
	r, n := utf8.DecodeRune(buf[cursor : len(buf)-1])
	quoted := strconv.QuoteRune(r)
	if r == utf8.RuneError && n == 1 {
		quoted = `'\x` + strconv.FormatUint(uint64(buf[cursor]), 16) + `'`
	}
	return errors.ErrSyntax("invalid character "+quoted+" "+what, cursor+int64(n))
}

// scalarEndError returns the syntax error of the end of the input at cursor in a number, a literal or an escape,
// which starts at start: encoding/json of Go 1.27 reports it at the start of the number or of the escape, and at
// the end of the input for a literal.
func scalarEndError(_ []byte, _, start int64, _ string) error {
	return endError(start)
}

// escapeSyntaxError returns the syntax error of the byte at cursor, in the escape which the backslash at backslash
// starts: encoding/json of Go 1.27 reports the escape, which is 6 bytes for \u.
func escapeSyntaxError(buf []byte, backslash, cursor int64) error {
	end := backslash + 2
	if cursor > backslash+1 {
		end = backslash + 6
	}
	input := buf[:len(buf)-1]
	if end > int64(len(input)) {
		// the escape is cut by the end of the input: the end of the input if what it has may start an escape,
		// and else what it has
		rest := input[backslash:]
		prefix := len(rest) < 2 || rest[1] == 'u'
		for i := 2; prefix && i < len(rest); i++ {
			prefix = isHexDigit(rest[i])
		}
		if prefix {
			return endError(backslash)
		}
		end = int64(len(input))
	}
	seq := string(input[backslash:end])
	var quoted string
	if strings.ContainsFunc(seq, func(r rune) bool {
		return r == '`' || r == utf8.RuneError || unicode.IsSpace(r) || !unicode.IsPrint(r)
	}) {
		quoted = strconv.Quote(seq)
	} else {
		quoted = "`" + seq + "`"
	}
	return errors.ErrSyntax("invalid escape sequence "+quoted+" in string", end)
}

// depthSyntaxError returns the syntax error of the object or the array at cursor, which is nested deeper than the
// limit.
func depthSyntaxError(_ []byte, cursor int64) error {
	return errors.ErrSyntax("exceeded max depth", cursor+1)
}
