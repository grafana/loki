package encoder

import (
	"bytes"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonsyntax"
	"github.com/goccy/go-json/jsontext"
)

// The outputs of the marshalers and the inputs of Compact and Indent are formatted in one pass ( see
// formatJSON ), after an output which is compact and valid already, as most are, is copied as it is ( see
// appendCompactOutput ). The syntax error of an input which is not valid is the one of the package jsontext, as
// encoding/json of Go 1.27 reports it by encoding/json/jsontext ( see jsonsyntax.LegacyError ).

// formatOptions are the options of jsontext by whether '<', '>', '&', U+2028 and U+2029 are escaped: the names of
// an object may be repeated, invalid UTF-8 is kept, and the strings are kept as they are written.
var formatOptions = [2][]jsontext.Options{
	{jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true), jsontext.PreserveRawStrings(true)},
	{jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true), jsontext.PreserveRawStrings(true),
		jsontext.EscapeForHTML(true), jsontext.EscapeForJS(true)},
}

// Compact appends src to buf with the white space between its tokens left out, and the characters for HTML
// escaped if escape is set.
func Compact(buf *bytes.Buffer, src []byte, escape bool) error {
	buf.Grow(len(src))
	ctx := TakeRuntimeContext()
	dst, err := appendCompact(ctx, buf.AvailableBuffer(), src, escape)
	ReleaseRuntimeContext(ctx)
	if err != nil {
		return err
	}
	buf.Write(dst)
	return nil
}

// appendCompact appends src to dst compacted, or returns the syntax error of src.
func appendCompact(ctx *RuntimeContext, dst, src []byte, escape bool) ([]byte, error) {
	if out, ok := appendCompactOutput(dst, src, escape, false); ok {
		return out, nil
	}
	return appendCompacted(ctx, dst, src, escape)
}

// appendCompacted is appendCompact of an input which appendCompactOutput doesn't take.
func appendCompacted(ctx *RuntimeContext, dst, src []byte, escape bool) ([]byte, error) {
	input := append(append(ctx.MarshalBuf[:0], src...), nul)
	ctx.MarshalBuf = input
	if out, _, ok := formatJSON(dst, input, escape, nil); ok {
		return out, nil
	}
	return dst, formatError(src, escape)
}

// Indent appends src to buf indented, as encoding/json does: each element of an object or an array on a line of
// its own, after prefix and one indent for each level, and the white space after the value kept.
func Indent(buf *bytes.Buffer, src []byte, prefix, indent string) error {
	buf.Grow(2 * len(src))
	ctx := TakeRuntimeContext()
	dst, err := appendIndent(ctx, buf.AvailableBuffer(), src, prefix, indent, false, true)
	ReleaseRuntimeContext(ctx)
	if err != nil {
		return err
	}
	buf.Write(dst)
	return nil
}

// appendIndent appends src to dst indented, or returns the syntax error of src. The white space after the value
// is kept if trailing is set.
func appendIndent(ctx *RuntimeContext, dst, src []byte, prefix, indent string, escape, trailing bool) ([]byte, error) {
	input := append(append(ctx.MarshalBuf[:0], src...), nul)
	ctx.MarshalBuf = input
	out, end, ok := formatJSON(dst, input, escape, &formatIndent{prefix: prefix, indent: indent})
	if !ok {
		return dst, formatError(src, escape)
	}
	if trailing {
		out = append(out, src[end:]...)
	}
	return out, nil
}

// formatError is the syntax error of src, which is not valid JSON, as jsontext reports it.
func formatError(src []byte, escape bool) error {
	escapeIndex := 0
	if escape {
		escapeIndex = 1
	}
	_, err := jsontext.AppendFormat(nil, src, formatOptions[escapeIndex]...)
	if msg, offset, ok := jsonsyntax.LegacyError(err); ok {
		return errors.ErrSyntax(msg, offset)
	}
	if err == nil {
		// the pass and jsontext disagree, which they must not: the input is reported as it is
		return errors.ErrSyntax("invalid JSON", 0)
	}
	return err
}

// nul ends the input of a pass.
const nul = byte(0)
