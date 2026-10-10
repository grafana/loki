package encoder

import (
	"context"
	"io"
)

type OptionFlag uint16

const (
	HTMLEscapeOption OptionFlag = 1 << iota
	IndentOption
	UnorderedMapOption
	DebugOption
	ColorizeOption
	ContextOption
	NormalizeUTF8Option
	FieldQueryOption
	// OptimizeFieldOrderOption lets the encoder order the fields of a struct as it encodes them fastest:
	// the fields of the same kind together, and a recursive field last. The opcodes of a type are compiled
	// for it apart from the ones for the order of the struct.
	OptimizeFieldOrderOption
)

type Option struct {
	Flag        OptionFlag
	ColorScheme *ColorScheme
	Context     context.Context
	DebugOut    io.Writer
	DebugDOTOut io.WriteCloser
}

type EncodeFormat struct {
	Header string
	Footer string
}

type EncodeFormatScheme struct {
	Int       EncodeFormat
	Uint      EncodeFormat
	Float     EncodeFormat
	Bool      EncodeFormat
	String    EncodeFormat
	Binary    EncodeFormat
	ObjectKey EncodeFormat
	Null      EncodeFormat
}

type (
	ColorScheme = EncodeFormatScheme
	ColorFormat = EncodeFormat
)
