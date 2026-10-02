//go:build !go1.27 || !goexperiment.jsonv2

package decoder

import (
	"github.com/goccy/go-json/internal/errors"
)

// text returns the words of the message of a syntax error which tell where the byte is, as encoding/json before
// Go 1.27 has them.
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
		return "in string literal"
	case whereNumber:
		return "in numeric literal"
	case whereFraction:
		return "after decimal point in numeric literal"
	case whereExponent:
		return "in exponent of numeric literal"
	}
	return "looking for beginning of value"
}

// invalidCharacterError returns the syntax error of the byte at cursor, which the offset is after.
func invalidCharacterError(buf []byte, cursor int64, what string) *errors.SyntaxError {
	return errors.ErrSyntax("invalid character "+quoteChar(buf[cursor])+" "+what, cursor+1)
}

// scalarEndError returns the syntax error of the end of the input at cursor in a number, a literal or an escape,
// which starts at start: encoding/json before Go 1.27 reads a white space at the end of the input, which is the
// byte of the error.
func scalarEndError(_ []byte, cursor, _ int64, what string) error {
	return errors.ErrSyntaxAtEnd("invalid character ' ' "+what, cursor)
}

// escapeSyntaxError returns the syntax error of the byte at cursor, in the escape which the backslash at backslash
// starts.
func escapeSyntaxError(buf []byte, backslash, cursor int64) error {
	what := "in string escape code"
	if cursor > backslash+1 {
		what = `in \u hexadecimal character escape`
	}
	if isEnd(buf, cursor) {
		return scalarEndError(buf, cursor, backslash, what)
	}
	return invalidCharacterError(buf, cursor, what)
}

// depthSyntaxError returns the syntax error of the object or the array at cursor, which is nested deeper than the
// limit.
func depthSyntaxError(buf []byte, cursor int64) error {
	return invalidCharacterError(buf, cursor, "exceeded max depth")
}
