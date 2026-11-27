package decoder

import (
	"github.com/goccy/go-json/internal/errors"
)

// The syntax errors are the ones of encoding/json of the Go version, whose messages and offsets differ between Go
// 1.27, which is made of encoding/json/v2, and the earlier versions ( see syntax_error_legacy.go and
// syntax_error_v2.go ).

// syntaxWhere is where in the grammar a byte is which the grammar doesn't have there.
type syntaxWhere uint8

const (
	whereValue syntaxWhere = iota
	whereKey
	whereAfterKey
	whereAfterPair
	whereAfterElement
	whereAfterTop
	whereString
	whereNumber
	whereFraction
	whereExponent
)

// isEnd reports whether cursor is the nul byte which ends buf, the input followed by it.
func isEnd(buf []byte, cursor int64) bool {
	return cursor == int64(len(buf))-1 && buf[cursor] == nul
}

// endError returns the error of the end of the input at the total offset at.
func endError(at int64) error {
	return errors.ErrSyntaxAtEnd("unexpected end of JSON input", at)
}

// syntaxErrorAt returns the syntax error of the byte at cursor, which the grammar doesn't have where it is: the end
// of the input if it is the nul byte which ends the buffer. It is not inlined, so that its callers keep their size.
//
//go:noinline
func syntaxErrorAt(buf []byte, cursor int64, where syntaxWhere) error {
	if isEnd(buf, cursor) {
		return endError(cursor)
	}
	err := invalidCharacterError(buf, cursor, where.text())
	if where == whereValue {
		// a stream of Go 1.27 tells a bracket there by the object or the array which Token opened
		err = errors.WithValueStartAt(err, cursor)
	}
	return err
}

// numberSyntaxError returns the syntax error of the byte at cursor in the number which starts at start.
//
//go:noinline
func numberSyntaxError(buf []byte, cursor, start int64, where syntaxWhere) error {
	if isEnd(buf, cursor) {
		return scalarEndError(buf, cursor, start, where.text())
	}
	return invalidCharacterError(buf, cursor, where.text())
}

// literalSyntaxError returns the syntax error of the literal at cursor, which is not lit: the first byte which
// differs from lit.
func literalSyntaxError(buf []byte, cursor int64, lit string) error {
	for i := 1; i < len(lit); i++ {
		at := cursor + int64(i)
		if c := buf[at]; c != lit[i] {
			what := "in literal " + lit + " (expecting " + quoteChar(lit[i]) + ")"
			if isEnd(buf, at) {
				return scalarEndError(buf, at, int64(len(buf))-1, what)
			}
			return invalidCharacterError(buf, at, what)
		}
	}
	return nil
}

// stringSyntaxError returns the syntax error of the string at cursor: a control character, an escape which is not
// one, or the end of the input.
func stringSyntaxError(buf []byte, cursor int64) error {
	for c := cursor + 1; ; c++ {
		switch b := buf[c]; {
		case b == '"':
			return nil
		case b < ' ':
			return syntaxErrorAt(buf, c, whereString)
		case b == '\\':
			switch buf[c+1] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				c++
			case 'u':
				for i := int64(2); i <= 5; i++ {
					if !isHexDigit(buf[c+i]) {
						return escapeSyntaxError(buf, c, c+i)
					}
				}
				c += 5
			default:
				return escapeSyntaxError(buf, c, c+1)
			}
		}
	}
}

func isHexDigit(c byte) bool {
	return c-'0' <= 9 || (c|0x20)-'a' <= 5
}
