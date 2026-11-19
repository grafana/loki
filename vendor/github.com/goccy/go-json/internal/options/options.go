// Package options is the state of the options of jsontext and of the v2 json package: both packages take the same
// Options, so that the options of one may be given to the functions of the other, as encoding/json/v2 and
// encoding/json/jsontext allow. An Options can't be made outside this module: its method takes a type of this
// internal package.
package options

import "math/bits"

// Options is an option, or a set of them, which sets properties of a Config.
type Options interface {
	ApplyTo(c *Config)
}

// Flags are properties which are true or false, and, for the properties whose values are not booleans, the bits
// which show that they are set.
type Flags uint64

// The properties of the encoders and the decoders of jsontext.
const (
	AllowDuplicateNames Flags = 1 << iota
	AllowInvalidUTF8
	EscapeForHTML
	EscapeForJS
	PreserveRawStrings
	CanonicalizeRawInts
	CanonicalizeRawFloats
	ReorderRawObjects
	SpaceAfterColon
	SpaceAfterComma
	Multiline
	IndentSet // WithIndent was given: Config.Indent
	PrefixSet // WithIndentPrefix was given: Config.Prefix
	// OmitTopLevelNewline is set by the functions and methods which format a single value, whose output has no
	// line feed after the value, as an Encoder writes.
	OmitTopLevelNewline

	maxCoderFlag
)

// The properties of marshaling and unmarshaling Go values, of the v2 json package.
const (
	StringifyNumbers Flags = maxCoderFlag << iota
	Deterministic
	FormatNilMapAsNull
	FormatNilSliceAsNull
	MatchCaseInsensitiveNames
	OmitZeroStructFields
	RejectUnknownMembers
	MarshalersSet   // WithMarshalers was given: Config.Marshalers
	UnmarshalersSet // WithUnmarshalers was given: Config.Unmarshalers
	// StringTag is set by no option: the encoder which a method or a function of a value of a field of the `string`
	// option is given has it, by which MarshalEncode of the encoder encodes the value as such a value, as
	// encoding/json/v2 has it, until an object or an array begins.
	StringTag
	// UnmarshalAnyWithRawNumber is set by no option: the decoder which a Decoder of the root package gives
	// UnmarshalJSONFrom after UseNumber has it, by which UnmarshalDecode of the decoder decodes a number into an empty
	// interface as a json.Number, as encoding/json/v2 has it for encoding/json.
	UnmarshalAnyWithRawNumber

	maxArshalFlag
)

// The properties of marshaling and unmarshaling Go values by the semantics of encoding/json ( v1 ), which the options
// of the root package set, as the ones of encoding/json do: the v2 json package applies each of them, alone or with
// the others.
const (
	ParseBytesWithLooseRFC4648 Flags = maxArshalFlag << iota
	ParseTimeWithLooseRFC3339
	UnmarshalArrayFromAnyLength
	MergeWithLegacySemantics
	MatchCaseSensitiveDelimiter
	CallMethodsWithLegacySemantics
	FormatByteArrayAsArray
	FormatBytesWithLegacySemantics
	FormatDurationAsNano
	ReportErrorsWithLegacySemantics
	StringifyWithLegacySemantics
	OmitEmptyWithLegacySemantics

	maxV1OptionFlag
)

// V1OptionFlags are the properties of the semantics of encoding/json: the flags from ParseBytesWithLooseRFC4648 to
// the last one, which the flag after it less the first one sets.
const V1OptionFlags = maxV1OptionFlag - ParseBytesWithLooseRFC4648

// DefaultV1Flags are the properties which DefaultOptionsV1 of the root package sets to true, and DefaultOptionsV2 of
// the v2 json package to false: the ones whose defaults differ between encoding/json and encoding/json/v2.
const DefaultV1Flags = AllowDuplicateNames | AllowInvalidUTF8 | EscapeForHTML | EscapeForJS | PreserveRawStrings |
	Deterministic | FormatNilMapAsNull | FormatNilSliceAsNull | MatchCaseInsensitiveNames | V1OptionFlags

// Config is the state of the options: the properties which were set, and their values.
type Config struct {
	Set   Flags // the properties which an option set
	Value Flags // the values of the properties which were set; the others are false
	// the properties which are not booleans, valid when their bits are in Set
	Indent       string
	Prefix       string
	Marshalers   any // the *Marshalers of the v2 json package
	Unmarshalers any // the *Unmarshalers of the v2 json package
}

// ApplyTo sets the properties of c in dst: a Config is itself an option of the properties it sets.
func (c *Config) ApplyTo(dst *Config) {
	dst.Set |= c.Set
	dst.Value = dst.Value&^c.Set | c.Value
	if c.Set&IndentSet != 0 {
		dst.Indent = c.Indent
	}
	if c.Set&PrefixSet != 0 {
		dst.Prefix = c.Prefix
	}
	if c.Set&MarshalersSet != 0 {
		dst.Marshalers = c.Marshalers
	}
	if c.Set&UnmarshalersSet != 0 {
		dst.Unmarshalers = c.Unmarshalers
	}
}

// Apply sets the options in order: an option set later overrides a property set by an earlier one. A Bool, as most
// options are, is applied without a call of its method.
func (c *Config) Apply(opts []Options) {
	for _, o := range opts {
		switch o := o.(type) {
		case Bool:
			o.ApplyTo(c)
		case nil:
		default:
			o.ApplyTo(c)
		}
	}
}

// Has reports whether the property f is true.
func (c *Config) Has(f Flags) bool { return c.Value&f != 0 }

// Same reports whether c sets the same options as d. The properties which are not booleans are compared only if
// they are set; the marshalers and unmarshalers by identity.
func (c *Config) Same(d *Config) bool {
	return c.Set == d.Set && c.Value == d.Value &&
		(c.Set&IndentSet == 0 || c.Indent == d.Indent) && (c.Set&PrefixSet == 0 || c.Prefix == d.Prefix) &&
		(c.Set&MarshalersSet == 0 || c.Marshalers == d.Marshalers) &&
		(c.Set&UnmarshalersSet == 0 || c.Unmarshalers == d.Unmarshalers)
}

// Bool is the option of a property which is true or false: the index of the bit of its flag shifted by one bit, with
// the value as the lowest bit. It is a byte, which an Options holds without an allocation, as the runtime has the
// values of the bytes: an option made for each call, as encoding/json/v2's are, costs nothing more.
type Bool uint8

// ApplyTo sets the property of o in c.
func (o Bool) ApplyTo(c *Config) {
	f := Flags(1) << (o >> 1)
	c.Set |= f
	c.Value &^= f
	if o&1 != 0 {
		c.Value |= f
	}
}

// BoolOf returns the option which sets the property f, a single flag, to v.
func BoolOf(f Flags, v bool) Options {
	o := Bool(bits.TrailingZeros64(uint64(f)) << 1)
	if v {
		o |= 1
	}
	return o
}

// Indent is the option of WithIndent or WithIndentPrefix of jsontext, which implies Multiline.
type Indent struct {
	S      string
	Prefix bool
}

// ApplyTo sets the indentation or the prefix of o in c, and Multiline.
func (o Indent) ApplyTo(c *Config) {
	c.Set |= Multiline
	c.Value |= Multiline
	if o.Prefix {
		c.Set |= PrefixSet
		c.Prefix = o.S
	} else {
		c.Set |= IndentSet
		c.Indent = o.S
	}
}
