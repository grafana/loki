package json

import (
	"io"

	"github.com/goccy/go-json/internal/decoder"
	"github.com/goccy/go-json/internal/encoder"
)

type EncodeOption = encoder.Option
type EncodeOptionFunc func(*EncodeOption)

// UnorderedMap doesn't sort when encoding map type: the entries of a map are written in an order which is not
// specified, and which may differ from the order of a range over the map.
func UnorderedMap() EncodeOptionFunc {
	return func(opt *EncodeOption) {
		opt.Flag |= encoder.UnorderedMapOption
	}
}

// OptimizeFieldOrder lets the encoder order the fields of a struct as it encodes them fastest, instead of in
// the order of the struct as encoding/json does: the fields of the same kind ( int, uint, float64, string, bool )
// are put together, in the order of the first field of each kind, and are encoded without a dispatch between
// them; and a field of the struct's own type, if there is one, is put last, so that a list of values is encoded
// without a frame for each. The keys of the JSON object are the same, only their order differs, which a JSON
// object doesn't define.
func OptimizeFieldOrder() EncodeOptionFunc {
	return func(opt *EncodeOption) {
		opt.Flag |= encoder.OptimizeFieldOrderOption
	}
}

// DisableHTMLEscape disables escaping of HTML characters ( '&', '<', '>' ) when encoding string.
func DisableHTMLEscape() EncodeOptionFunc {
	return func(opt *EncodeOption) {
		opt.Flag &= ^encoder.HTMLEscapeOption
	}
}

// DisableNormalizeUTF8
// By default, when encoding string, UTF8 characters in the range of 0x80 - 0xFF are processed by replacing invalid code with U+FFFD and escaping \u2028 and \u2029, as encoding/json does:
// the replacement character is written escaped as \ufffd before Go 1.27, and as it is by Go 1.27, whose encoding/json is made of encoding/json/v2.
// This option disables this behaviour. You can expect faster speeds by applying this option, but be careful.
// encoding/json implements here: https://github.com/golang/go/blob/6178d25fc0b28724b1b5aec2b1b74fc06d9294c7/src/encoding/json/encode.go#L1067-L1093.
func DisableNormalizeUTF8() EncodeOptionFunc {
	return func(opt *EncodeOption) {
		opt.Flag &= ^encoder.NormalizeUTF8Option
	}
}

// Debug outputs debug information when panic occurs during encoding.
func Debug() EncodeOptionFunc {
	return func(opt *EncodeOption) {
		opt.Flag |= encoder.DebugOption
	}
}

// DebugWith sets the destination to write debug messages.
func DebugWith(w io.Writer) EncodeOptionFunc {
	return func(opt *EncodeOption) {
		opt.DebugOut = w
	}
}

// DebugDOT sets the destination to write opcodes graph.
func DebugDOT(w io.WriteCloser) EncodeOptionFunc {
	return func(opt *EncodeOption) {
		opt.DebugDOTOut = w
	}
}

// Colorize add an identifier for coloring to the string of the encoded result.
func Colorize(scheme *ColorScheme) EncodeOptionFunc {
	return func(opt *EncodeOption) {
		opt.Flag |= encoder.ColorizeOption
		opt.ColorScheme = scheme
	}
}

type DecodeOption = decoder.Option
type DecodeOptionFunc func(*DecodeOption)

// DecodeFieldPriorityFirstWin
// in the default behavior, go-json, like encoding/json,
// will reflect the result of the last evaluation when a field with the same name exists.
// This option allow you to change this behavior.
// this option reflects the result of the first evaluation if a field with the same name exists.
// This behavior has a performance advantage as it allows the subsequent strings to be skipped if all fields have been evaluated.
func DecodeFieldPriorityFirstWin() DecodeOptionFunc {
	return func(opt *DecodeOption) {
		opt.Flags |= decoder.FirstWinOption
	}
}

// DecodeNoCopyString makes the decoded strings refer to the input instead of copies of their bytes,
// as sonic does by default: a string which has no escape is not copied. Then the input must not be
// modified while the decoded strings are used.
//
// By default, a decoded string is a copy, as with encoding/json: the input may be modified or reused
// after the call. The copies of the short strings share buffers of up to 16 KB, so a decoded string keeps
// at most 16 KB alive with it.
func DecodeNoCopyString() DecodeOptionFunc {
	return func(opt *DecodeOption) {
		opt.Flags |= decoder.NoCopyStringOption
	}
}
