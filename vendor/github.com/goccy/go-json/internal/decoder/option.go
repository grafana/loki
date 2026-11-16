package decoder

import "context"

type OptionFlags uint8

const (
	FirstWinOption OptionFlags = 1 << iota
	ContextOption
	PathOption
	// UseNumberOption decodes a number into interface{} as json.Number instead of float64.
	UseNumberOption
	// DisallowUnknownFieldsOption makes an object key which matches no field of the struct an error.
	DisallowUnknownFieldsOption
	// NoCopyStringOption makes the decoded strings refer to the input instead of a copy of their bytes.
	NoCopyStringOption
)

// V2Flags are the options of a decoding by the semantics of encoding/json/v2, which the v2 json package decodes by
// ( see CompileToGetDecoderV2 ). They are zero for the decodings of v1.
type V2Flags uint32

const (
	// V2Semantics is set for every decoding of v2.
	V2Semantics V2Flags = 1 << iota
	// V2AllowInvalidUTF8 lets the strings have invalid UTF-8, which is replaced by U+FFFD.
	V2AllowInvalidUTF8
	// V2AllowDuplicateNames lets an object have a name more than once.
	V2AllowDuplicateNames
	// V2StringifyNumbers decodes the numbers of the numeric types from JSON strings, by decoders of their own.
	V2StringifyNumbers
	// V2MatchCaseInsensitiveNames matches the names of an object with the names of the fields of a struct by their
	// folded names too, unless a field has the `case:strict` option.
	V2MatchCaseInsensitiveNames
	// V2RejectUnknownMembers makes a name of an object which is the name of no field of a struct an error, unless
	// the struct has an embedded fallback.
	V2RejectUnknownMembers
	// The options of the semantics of encoding/json which the v2 json package applies when unmarshaling, named by the
	// options of the root package, whose documentation they follow, in the order of their properties in
	// internal/options, from ParseBytesWithLooseRFC4648, so that they are copied at once ( see unmarshalFlags of the
	// v2 json package ).
	V2ParseBytesWithLooseRFC4648
	V2ParseTimeWithLooseRFC3339
	V2UnmarshalArrayFromAnyLength
	V2MergeWithLegacySemantics
	V2MatchCaseSensitiveDelimiter
	V2CallMethodsWithLegacySemantics
	V2FormatByteArrayAsArray
	V2FormatBytesWithLegacySemantics
	V2FormatDurationAsNano
	V2ReportErrorsWithLegacySemantics
	V2StringifyWithLegacySemantics
)

// The options of the text which v1 accepts and encoding/json/v2 rejects by default, names twice in an object and
// invalid UTF-8 in the strings, which the decoders of v2 test together to choose their loops.
const (
	// asV1Text is both options: the text is read as v1 reads it.
	asV1Text = V2AllowDuplicateNames | V2AllowInvalidUTF8
	// strictText is neither option: the text is read as encoding/json/v2 reads it by default.
	strictText V2Flags = 0
)

type Option struct {
	Flags OptionFlags
	// V2 is in the room which Flags leaves before Context on 64-bit platforms, so that Option keeps its size there.
	V2      V2Flags
	Context context.Context
	Path    *Path
}
