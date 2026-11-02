package jsontext

import (
	"io"

	"github.com/goccy/go-json/internal/jsonsyntax"
)

func init() {
	jsonsyntax.LegacyError = legacySyntaxError
}

// legacySyntaxError is the syntax error err as encoding/json reports it, which formats JSON by this package as
// encoding/json of Go 1.27 does by encoding/json/jsontext: the message in its words, and the offset after the
// invalid text, or after the first byte of a token which doesn't fit the grammar where it is.
func legacySyntaxError(err error) (string, int64, bool) {
	serr, ok := err.(*SyntacticError)
	if !ok {
		return "", 0, false
	}
	switch e := serr.Err.(type) {
	case *textError:
		return e.message(true), serr.ByteOffset + int64(len(e.what)), true
	}
	switch serr.Err {
	case io.ErrUnexpectedEOF:
		return "unexpected end of JSON input", serr.ByteOffset, true
	case errMissingValue:
		return "missing value after object key", serr.ByteOffset + 1, true
	}
	return serr.Err.Error(), serr.ByteOffset + 1, true
}
