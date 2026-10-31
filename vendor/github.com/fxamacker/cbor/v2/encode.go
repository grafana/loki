// Copyright (c) Faye Amacker. All rights reserved.
// Licensed under the MIT License. See LICENSE in the project root for license information.

package cbor

import (
	"bytes"
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"math/rand"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/x448/float16"
)

// Marshal returns the CBOR encoding of v using default encoding options.
// See EncOptions for encoding options.
//
// Marshal uses the following encoding rules:
//
// If value implements the Marshaler interface, Marshal calls its
// MarshalCBOR method.
//
// If value implements encoding.BinaryMarshaler, Marshal calls its
// MarshalBinary method and encode it as CBOR byte string.
//
// Boolean values encode as CBOR booleans (type 7).
//
// Positive integer values encode as CBOR positive integers (type 0).
//
// Negative integer values encode as CBOR negative integers (type 1).
//
// Floating point values encode as CBOR floating points (type 7).
//
// String values encode as CBOR text strings (type 3).
//
// []byte values encode as CBOR byte strings (type 2).
//
// Array and slice values encode as CBOR arrays (type 4).
//
// Map values encode as CBOR maps (type 5).
//
// Struct values encode as CBOR maps (type 5).  Each exported struct field
// becomes a pair with field name encoded as CBOR text string (type 3) and
// field value encoded based on its type.  See struct tag option "keyasint"
// to encode field name as CBOR integer (type 0 and 1).  Also see struct
// tag option "toarray" for special field "_" to encode struct values as
// CBOR array (type 4).
//
// Marshal supports format string stored under the "cbor" key in the struct
// field's tag.  CBOR format string can specify the name of the field,
// "omitempty", "omitzero" and "keyasint" options, and special case "-" for
// field omission. If "cbor" key is absent, Marshal uses "json" key.
// The "omitzero" option matches Go 1.24+ stdlib encoding/json behavior.
//
// Struct field name is treated as integer if it has "keyasint" option in
// its format string.  The format string must specify an integer as its
// field name.
//
// Special struct field "_" is used to specify struct level options, such as
// "toarray". "toarray" option enables Go struct to be encoded as CBOR array.
// "omitempty" and "omitzero" are disabled by "toarray" to ensure that the
// same number of elements are encoded every time.
//
// Anonymous struct fields are marshaled as if their exported fields
// were fields in the outer struct.  Marshal follows the same struct fields
// visibility rules used by JSON encoding package.
//
// time.Time values encode as text strings specified in RFC3339 or numerical
// representation of seconds since January 1, 1970 UTC depending on
// EncOptions.Time setting.  Also See EncOptions.TimeTag to encode
// time.Time as CBOR tag with tag number 0 or 1.
//
// big.Int values encode as CBOR integers (type 0 and 1) if values fit.
// Otherwise, big.Int values encode as CBOR bignums (tag 2 and 3).  See
// EncOptions.BigIntConvert to always encode big.Int values as CBOR
// bignums.
//
// Pointer values encode as the value pointed to.
//
// Interface values encode as the value stored in the interface.
//
// Nil slice/map/pointer/interface values encode as CBOR nulls (type 7).
//
// Values of other types cannot be encoded in CBOR.  Attempting
// to encode such a value causes Marshal to return an UnsupportedTypeError.
func Marshal(v any) ([]byte, error) {
	return defaultEncMode.Marshal(v)
}

// MarshalToBuffer encodes v into provided buffer (instead of using built-in buffer pool)
// and uses default encoding options.
//
// NOTE: Unlike Marshal, the buffer provided to MarshalToBuffer can contain
// partially encoded data if error is returned.
//
// See Marshal for more details.
func MarshalToBuffer(v any, buf *bytes.Buffer) error {
	return defaultEncMode.MarshalToBuffer(v, buf)
}

// Marshaler is the interface implemented by types that can marshal themselves
// into valid CBOR.
type Marshaler interface {
	MarshalCBOR() ([]byte, error)
}

// MarshalerError represents error from checking encoded CBOR data item
// returned from MarshalCBOR for well-formedness and some very limited tag validation.
type MarshalerError struct {
	typ reflect.Type
	err error
}

func (e *MarshalerError) Error() string {
	return "cbor: error calling MarshalCBOR for type " +
		e.typ.String() +
		": " + e.err.Error()
}

func (e *MarshalerError) Unwrap() error {
	return e.err
}

type TranscodeError struct {
	err                        error
	rtype                      reflect.Type
	sourceFormat, targetFormat string
}

func (e TranscodeError) Error() string {
	return "cbor: cannot transcode from " + e.sourceFormat + " to " + e.targetFormat + ": " + e.err.Error()
}

func (e TranscodeError) Unwrap() error {
	return e.err
}

// UnsupportedTypeError is returned by Marshal when attempting to encode value
// of an unsupported type.
type UnsupportedTypeError struct {
	Type reflect.Type
}

func (e *UnsupportedTypeError) Error() string {
	return "cbor: unsupported type: " + e.Type.String()
}

// UnsupportedValueError is returned by Marshal when attempting to encode an
// unsupported value.
type UnsupportedValueError struct {
	msg string
}

func (e *UnsupportedValueError) Error() string {
	return "cbor: unsupported value: " + e.msg
}

// SortMode identifies supported sorting order.
type SortMode int

const (
	// SortNone encodes map pairs and struct fields in an arbitrary order.
	SortNone SortMode = 0

	// SortLengthFirst causes map keys or struct fields to be sorted such that:
	//     - If two keys have different lengths, the shorter one sorts earlier;
	//     - If two keys have the same length, the one with the lower value in
	//       (byte-wise) lexical order sorts earlier.
	// It is used in "Canonical CBOR" encoding in RFC 7049 3.9.
	SortLengthFirst SortMode = 1

	// SortBytewiseLexical causes map keys or struct fields to be sorted in the
	// bytewise lexicographic order of their deterministic CBOR encodings.
	// It is used in "CTAP2 Canonical CBOR" and "Core Deterministic Encoding"
	// in RFC 7049bis.
	SortBytewiseLexical SortMode = 2

	// SortFastShuffle encodes map pairs and struct fields in a shuffled
	// order. This mode does not guarantee an unbiased permutation, but it
	// does guarantee that the runtime of the shuffle algorithm used will be
	// constant.
	SortFastShuffle SortMode = 3

	// SortCanonical is used in "Canonical CBOR" encoding in RFC 7049 3.9.
	SortCanonical SortMode = SortLengthFirst

	// SortCTAP2 is used in "CTAP2 Canonical CBOR".
	SortCTAP2 SortMode = SortBytewiseLexical

	// SortCoreDeterministic is used in "Core Deterministic Encoding" in RFC 7049bis.
	SortCoreDeterministic SortMode = SortBytewiseLexical

	maxSortMode SortMode = 4
)

func (sm SortMode) valid() bool {
	return sm >= 0 && sm < maxSortMode
}

// StringMode specifies how to encode Go string values.
type StringMode int

const (
	// StringToTextString encodes Go string to CBOR text string (major type 3).
	StringToTextString StringMode = iota

	// StringToByteString encodes Go string to CBOR byte string (major type 2).
	StringToByteString
)

func (st StringMode) cborType() (cborType, bool) {
	switch st {
	case StringToTextString:
		return cborTypeTextString, true

	case StringToByteString:
		return cborTypeByteString, true
	}
	return 0, false
}

// ShortestFloatMode specifies which floating-point format should
// be used as the shortest possible format for CBOR encoding.
// It is not used for encoding Infinity and NaN values.
type ShortestFloatMode int

const (
	// ShortestFloatNone makes float values encode without any conversion.
	// This is the default for ShortestFloatMode in v1.
	// E.g. a float32 in Go will encode to CBOR float32.  And
	// a float64 in Go will encode to CBOR float64.
	ShortestFloatNone ShortestFloatMode = iota

	// ShortestFloat16 specifies float16 as the shortest form that preserves value.
	// E.g. if float64 can convert to float32 while preserving value, then
	// encoding will also try to convert float32 to float16.  So a float64 might
	// encode as CBOR float64, float32 or float16 depending on the value.
	ShortestFloat16

	maxShortestFloat
)

func (sfm ShortestFloatMode) valid() bool {
	return sfm >= 0 && sfm < maxShortestFloat
}

// NaNConvertMode specifies how to encode NaN and overrides ShortestFloatMode.
// ShortestFloatMode is not used for encoding Infinity and NaN values.
type NaNConvertMode int

const (
	// NaNConvert7e00 always encodes NaN to 0xf97e00 (CBOR float16 = 0x7e00).
	NaNConvert7e00 NaNConvertMode = iota

	// NaNConvertNone never modifies or converts NaN to other representations
	// (float64 NaN stays float64, etc. even if it can use float16 without losing
	// any bits).
	NaNConvertNone

	// NaNConvertPreserveSignal converts NaN to the smallest form that preserves
	// value (quiet bit + payload) as described in RFC 7049bis Draft 12.
	NaNConvertPreserveSignal

	// NaNConvertQuiet always forces quiet bit = 1 and shortest form that preserves
	// NaN payload.
	NaNConvertQuiet

	// NaNConvertReject returns UnsupportedValueError on attempts to encode a NaN value.
	NaNConvertReject

	maxNaNConvert
)

func (ncm NaNConvertMode) valid() bool {
	return ncm >= 0 && ncm < maxNaNConvert
}

// InfConvertMode specifies how to encode Infinity and overrides ShortestFloatMode.
// ShortestFloatMode is not used for encoding Infinity and NaN values.
type InfConvertMode int

const (
	// InfConvertFloat16 always converts Inf to lossless IEEE binary16 (float16).
	InfConvertFloat16 InfConvertMode = iota

	// InfConvertNone never converts (used by CTAP2 Canonical CBOR).
	InfConvertNone

	// InfConvertReject returns UnsupportedValueError on attempts to encode an infinite value.
	InfConvertReject

	maxInfConvert
)

func (icm InfConvertMode) valid() bool {
	return icm >= 0 && icm < maxInfConvert
}

// TimeMode selects the RFC 8949 (CBOR) format to use when encoding time.Time values:
//   - Section 3.4.1: Standard Date/Time String (CBOR tag 0)
//   - Section 3.4.2: Epoch-Based Date/Time (CBOR tag 1, integer or floating-point)
//
// To emit CBOR tag number 0 or 1, use EncOptions{TimeTag: EncTagRequired}.
//
// RFC 8949 specifies fractional seconds as text or floating-point, but not as integers.
// User-defined types can use integer-based format for fractional seconds by implementing
// cbor.Marshaler and cbor.Unmarshaler (without CBOR tag number 0 or 1).
type TimeMode int

const (
	// TimeUnix encodes to the integer format specified by RFC 8949 for whole seconds,
	// with 1-second precision.  For fractional seconds, use other TimeMode options or
	// a custom CBOR time format (see TimeMode and cbor.Marshaler).
	TimeUnix TimeMode = iota

	// TimeUnixMicro encodes to the floating-point format specified by RFC 8949 for
	// fractional seconds, with up to 1-microsecond precision that decreases for times
	// far from 1970.  Like all binary floating-point formats, IEEE 754 binary64
	// (float64) cannot represent most decimal fractions exactly.
	// For more precision, use TimeRFC3339NanoUTC or a custom CBOR time format (see
	// TimeMode and cbor.Marshaler) to store fractional seconds in an integer.
	TimeUnixMicro

	// TimeUnixDynamic encodes to the integer or floating-point format specified by
	// RFC 8949.  It encodes to an integer if the time rounded to microseconds is a
	// whole second, and otherwise to the floating-point format.  Like all binary
	// floating-point formats, IEEE 754 binary64 (float64) cannot represent most decimal
	// fractions exactly.  For precision details and alternative formats, see TimeMode
	// and TimeUnixMicro.
	TimeUnixDynamic

	// TimeRFC3339 encodes to the text format specified by RFC 8949, with 1-second
	// precision and the UTC offset of the time.Time.  To always encode "Z" (zero
	// offset), use UTC time.Time values or TimeRFC3339NanoUTC.
	// For fractional seconds, use other TimeMode options or a custom CBOR time format
	// (see TimeMode and cbor.Marshaler).
	// Marshal returns UnsupportedValueError if the year is outside 0000-9999 or the
	// UTC offset is 24 hours or more.
	TimeRFC3339

	// TimeRFC3339Nano encodes to the text format specified by RFC 8949, with
	// 1-nanosecond precision and the UTC offset of the time.Time.  To always encode
	// "Z" (zero offset), use UTC time.Time values or TimeRFC3339NanoUTC.
	// Marshal returns UnsupportedValueError if the year is outside 0000-9999 or the
	// UTC offset is 24 hours or more.
	TimeRFC3339Nano

	// TimeRFC3339NanoUTC encodes to the text format specified by RFC 8949, with
	// 1-nanosecond precision and the time converted to UTC ("Z").
	// Marshal returns UnsupportedValueError if the UTC year is outside 0000-9999.
	TimeRFC3339NanoUTC

	maxTimeMode
)

func (tm TimeMode) valid() bool {
	return tm >= 0 && tm < maxTimeMode
}

// BigIntConvertMode specifies how to encode big.Int values.
type BigIntConvertMode int

const (
	// BigIntConvertShortest makes big.Int encode to CBOR integer if value fits.
	// E.g. if big.Int value can be converted to CBOR integer while preserving
	// value, encoder will encode it to CBOR integer (major type 0 or 1).
	BigIntConvertShortest BigIntConvertMode = iota

	// BigIntConvertNone makes big.Int encode to CBOR bignum (tag 2 or 3) without
	// converting it to another CBOR type.
	BigIntConvertNone

	// BigIntConvertReject returns an UnsupportedTypeError instead of marshaling a big.Int.
	BigIntConvertReject

	maxBigIntConvert
)

func (bim BigIntConvertMode) valid() bool {
	return bim >= 0 && bim < maxBigIntConvert
}

// NilContainersMode specifies how to encode nil slices and maps.
type NilContainersMode int

const (
	// NilContainerAsNull encodes nil slices and maps as CBOR null.
	// This is the default.
	NilContainerAsNull NilContainersMode = iota

	// NilContainerAsEmpty encodes nil slices and maps as
	// empty container (CBOR bytestring, array, or map).
	NilContainerAsEmpty

	maxNilContainersMode
)

func (m NilContainersMode) valid() bool {
	return m >= 0 && m < maxNilContainersMode
}

// OmitEmptyMode specifies how to encode struct fields with omitempty tag.
// The default behavior omits if field value would encode as empty CBOR value.
type OmitEmptyMode int

const (
	// OmitEmptyCBORValue specifies that struct fields tagged with "omitempty"
	// should be omitted from encoding if the field would be encoded as an empty
	// CBOR value, such as CBOR false, 0, 0.0, nil, empty byte, empty string,
	// empty array, or empty map.
	OmitEmptyCBORValue OmitEmptyMode = iota

	// OmitEmptyGoValue specifies that struct fields tagged with "omitempty"
	// should be omitted from encoding if the field has an empty Go value,
	// defined as false, 0, 0.0, a nil pointer, a nil interface value, and
	// any empty array, slice, map, or string.
	// This behavior is the same as the current (aka v1) encoding/json package
	// included in Go.
	OmitEmptyGoValue

	maxOmitEmptyMode
)

func (om OmitEmptyMode) valid() bool {
	return om >= 0 && om < maxOmitEmptyMode
}

// FieldNameMode specifies the CBOR type to use when encoding struct field names.
type FieldNameMode int

const (
	// FieldNameToTextString encodes struct fields to CBOR text string (major type 3).
	FieldNameToTextString FieldNameMode = iota

	// FieldNameToByteString encodes struct fields to CBOR byte string (major type 2).
	FieldNameToByteString

	maxFieldNameMode
)

func (fnm FieldNameMode) valid() bool {
	return fnm >= 0 && fnm < maxFieldNameMode
}

// ByteSliceLaterFormatMode specifies which later format conversion hint (CBOR tag 21-23)
// to include (if any) when encoding Go byte slice to CBOR byte string. The encoder will
// always encode unmodified bytes from the byte slice and just wrap it within
// CBOR tag 21, 22, or 23 if specified.
// See "Expected Later Encoding for CBOR-to-JSON Converters" in RFC 8949 Section 3.4.5.2.
type ByteSliceLaterFormatMode int

const (
	// ByteSliceLaterFormatNone encodes unmodified bytes from Go byte slice to CBOR byte string (major type 2)
	// without adding CBOR tag 21, 22, or 23.
	ByteSliceLaterFormatNone ByteSliceLaterFormatMode = iota

	// ByteSliceLaterFormatBase64URL encodes unmodified bytes from Go byte slice to CBOR byte string (major type 2)
	// inside CBOR tag 21 (expected later conversion to base64url encoding, see RFC 8949 Section 3.4.5.2).
	ByteSliceLaterFormatBase64URL

	// ByteSliceLaterFormatBase64 encodes unmodified bytes from Go byte slice to CBOR byte string (major type 2)
	// inside CBOR tag 22 (expected later conversion to base64 encoding, see RFC 8949 Section 3.4.5.2).
	ByteSliceLaterFormatBase64

	// ByteSliceLaterFormatBase16 encodes unmodified bytes from Go byte slice to CBOR byte string (major type 2)
	// inside CBOR tag 23 (expected later conversion to base16 encoding, see RFC 8949 Section 3.4.5.2).
	ByteSliceLaterFormatBase16
)

func (bsefm ByteSliceLaterFormatMode) encodingTag() (uint64, bool) {
	switch bsefm {
	case ByteSliceLaterFormatNone:
		return 0, true

	case ByteSliceLaterFormatBase64URL:
		return tagNumExpectedLaterEncodingBase64URL, true

	case ByteSliceLaterFormatBase64:
		return tagNumExpectedLaterEncodingBase64, true

	case ByteSliceLaterFormatBase16:
		return tagNumExpectedLaterEncodingBase16, true
	}
	return 0, false
}

// ByteArrayMode specifies how to encode byte arrays.
type ByteArrayMode int

const (
	// ByteArrayToByteSlice encodes byte arrays the same way that a byte slice with identical
	// length and contents is encoded.
	ByteArrayToByteSlice ByteArrayMode = iota

	// ByteArrayToArray encodes byte arrays to the CBOR array type with one unsigned integer
	// item for each byte in the array.
	ByteArrayToArray

	maxByteArrayMode
)

func (bam ByteArrayMode) valid() bool {
	return bam >= 0 && bam < maxByteArrayMode
}

// BinaryMarshalerMode specifies how to encode types that implement encoding.BinaryMarshaler.
type BinaryMarshalerMode int

const (
	// BinaryMarshalerByteString encodes the output of MarshalBinary to a CBOR byte string.
	BinaryMarshalerByteString BinaryMarshalerMode = iota

	// BinaryMarshalerNone does not recognize BinaryMarshaler implementations during encode.
	BinaryMarshalerNone

	maxBinaryMarshalerMode
)

func (bmm BinaryMarshalerMode) valid() bool {
	return bmm >= 0 && bmm < maxBinaryMarshalerMode
}

// TextMarshalerMode specifies how to encode types that implement encoding.TextMarshaler.
type TextMarshalerMode int

const (
	// TextMarshalerNone does not recognize TextMarshaler implementations during encode.
	// This is the default behavior.
	TextMarshalerNone TextMarshalerMode = iota

	// TextMarshalerTextString encodes the output of MarshalText to a CBOR text string.
	TextMarshalerTextString

	maxTextMarshalerMode
)

func (tmm TextMarshalerMode) valid() bool {
	return tmm >= 0 && tmm < maxTextMarshalerMode
}

// ToIndefArrayStructTagMode specifies whether to honor the `toindefarray`
// struct tag option. Indefinite-length encoding via the streaming API is
// governed separately by IndefLength.
type ToIndefArrayStructTagMode int

const (
	// ToIndefArrayStructTagForbidden rejects encoding of structs tagged with `toindefarray`.
	ToIndefArrayStructTagForbidden ToIndefArrayStructTagMode = iota

	// ToIndefArrayStructTagAllowed encodes structs tagged with `toindefarray` as indefinite-length arrays.
	ToIndefArrayStructTagAllowed

	maxToIndefArrayStructTagMode
)

func (m ToIndefArrayStructTagMode) valid() bool {
	return m >= 0 && m < maxToIndefArrayStructTagMode
}

// EncOptions specifies encoding options.
type EncOptions struct {
	// Sort specifies sorting order.
	Sort SortMode

	// ShortestFloat specifies the shortest floating-point encoding that preserves
	// the value being encoded.
	ShortestFloat ShortestFloatMode

	// NaNConvert specifies how to encode NaN and it overrides ShortestFloatMode.
	NaNConvert NaNConvertMode

	// InfConvert specifies how to encode Inf and it overrides ShortestFloatMode.
	InfConvert InfConvertMode

	// BigIntConvert specifies how to encode big.Int values.
	BigIntConvert BigIntConvertMode

	// Time specifies how to encode time.Time.
	Time TimeMode

	// TimeTag allows time.Time to be encoded with a tag number.
	// RFC3339 format gets tag number 0, and numeric epoch time tag number 1.
	TimeTag EncTagMode

	// IndefLength specifies whether to allow indefinite-length CBOR items.
	IndefLength IndefLengthMode

	// NilContainers specifies how to encode nil slices and maps.
	NilContainers NilContainersMode

	// TagsMd specifies whether to allow CBOR tags (major type 6).
	TagsMd TagsMode

	// OmitEmptyMode specifies how to encode struct fields with omitempty tag.
	OmitEmpty OmitEmptyMode

	// String specifies which CBOR type to use when encoding Go strings.
	// - CBOR text string (major type 3) is default
	// - CBOR byte string (major type 2)
	String StringMode

	// FieldName specifies the CBOR type to use when encoding struct field names.
	FieldName FieldNameMode

	// ByteSliceLaterFormat specifies which later format conversion hint (CBOR tag 21-23)
	// to include (if any) when encoding Go byte slice to CBOR byte string. The encoder will
	// always encode unmodified bytes from the byte slice and just wrap it within
	// CBOR tag 21, 22, or 23 if specified.
	// See "Expected Later Encoding for CBOR-to-JSON Converters" in RFC 8949 Section 3.4.5.2.
	ByteSliceLaterFormat ByteSliceLaterFormatMode

	// ByteArray specifies how to encode byte arrays.
	ByteArray ByteArrayMode

	// BinaryMarshaler specifies how to encode types that implement encoding.BinaryMarshaler.
	BinaryMarshaler BinaryMarshalerMode

	// TextMarshaler specifies how to encode types that implement encoding.TextMarshaler.
	TextMarshaler TextMarshalerMode

	// JSONMarshalerTranscoder sets the transcoding scheme used to marshal types that implement
	// json.Marshaler but do not also implement cbor.Marshaler. If nil, encoding behavior is not
	// influenced by whether or not a type implements json.Marshaler.
	JSONMarshalerTranscoder Transcoder

	// ToIndefArrayStructTag specifies whether the `toindefarray` struct tag option is honored.
	ToIndefArrayStructTag ToIndefArrayStructTagMode
}

// CanonicalEncOptions returns EncOptions for "Canonical CBOR" encoding,
// defined in RFC 7049 Section 3.9 with the following rules:
//
//  1. "Integers must be as small as possible."
//  2. "The expression of lengths in major types 2 through 5 must be as short as possible."
//  3. The keys in every map must be sorted in length-first sorting order.
//     See SortLengthFirst for details.
//  4. "Indefinite-length items must be made into definite-length items."
//  5. "If a protocol allows for IEEE floats, then additional canonicalization rules might
//     need to be added.  One example rule might be to have all floats start as a 64-bit
//     float, then do a test conversion to a 32-bit float; if the result is the same numeric
//     value, use the shorter value and repeat the process with a test conversion to a
//     16-bit float.  (This rule selects 16-bit float for positive and negative Infinity
//     as well.)  Also, there are many representations for NaN.  If NaN is an allowed value,
//     it must always be represented as 0xf97e00."
func CanonicalEncOptions() EncOptions {
	return EncOptions{
		Sort:          SortCanonical,
		ShortestFloat: ShortestFloat16,
		NaNConvert:    NaNConvert7e00,
		InfConvert:    InfConvertFloat16,
		IndefLength:   IndefLengthForbidden,
	}
}

// CTAP2EncOptions returns EncOptions for "CTAP2 Canonical CBOR" encoding,
// defined in CTAP specification, with the following rules:
//
//  1. "Integers must be encoded as small as possible."
//  2. "The representations of any floating-point values are not changed."
//  3. "The expression of lengths in major types 2 through 5 must be as short as possible."
//  4. "Indefinite-length items must be made into definite-length items.""
//  5. The keys in every map must be sorted in bytewise lexicographic order.
//     See SortBytewiseLexical for details.
//  6. "Tags as defined in Section 2.4 in [RFC7049] MUST NOT be present."
func CTAP2EncOptions() EncOptions {
	return EncOptions{
		Sort:          SortCTAP2,
		ShortestFloat: ShortestFloatNone,
		NaNConvert:    NaNConvertNone,
		InfConvert:    InfConvertNone,
		IndefLength:   IndefLengthForbidden,
		TagsMd:        TagsForbidden,
	}
}

// CoreDetEncOptions returns EncOptions for "Core Deterministic" encoding,
// defined in RFC 7049bis with the following rules:
//
//  1. "Preferred serialization MUST be used. In particular, this means that arguments
//     (see Section 3) for integers, lengths in major types 2 through 5, and tags MUST
//     be as short as possible"
//     "Floating point values also MUST use the shortest form that preserves the value"
//  2. "Indefinite-length items MUST NOT appear."
//  3. "The keys in every map MUST be sorted in the bytewise lexicographic order of
//     their deterministic encodings."
func CoreDetEncOptions() EncOptions {
	return EncOptions{
		Sort:          SortCoreDeterministic,
		ShortestFloat: ShortestFloat16,
		NaNConvert:    NaNConvert7e00,
		InfConvert:    InfConvertFloat16,
		IndefLength:   IndefLengthForbidden,
	}
}

// PreferredUnsortedEncOptions returns EncOptions for "Preferred Serialization" encoding,
// defined in RFC 7049bis with the following rules:
//
//  1. "The preferred serialization always uses the shortest form of representing the argument
//     (Section 3);"
//  2. "it also uses the shortest floating-point encoding that preserves the value being
//     encoded (see Section 5.5)."
//     "The preferred encoding for a floating-point value is the shortest floating-point encoding
//     that preserves its value, e.g., 0xf94580 for the number 5.5, and 0xfa45ad9c00 for the
//     number 5555.5, unless the CBOR-based protocol specifically excludes the use of the shorter
//     floating-point encodings. For NaN values, a shorter encoding is preferred if zero-padding
//     the shorter significand towards the right reconstitutes the original NaN value (for many
//     applications, the single NaN encoding 0xf97e00 will suffice)."
//  3. "Definite length encoding is preferred whenever the length is known at the time the
//     serialization of the item starts."
func PreferredUnsortedEncOptions() EncOptions {
	return EncOptions{
		Sort:          SortNone,
		ShortestFloat: ShortestFloat16,
		NaNConvert:    NaNConvert7e00,
		InfConvert:    InfConvertFloat16,
	}
}

// EncMode returns EncMode with immutable options and no tags (safe for concurrency).
func (opts EncOptions) EncMode() (EncMode, error) { //nolint:gocritic // ignore hugeParam
	return opts.encMode()
}

// UserBufferEncMode returns UserBufferEncMode with immutable options and no tags (safe for concurrency).
func (opts EncOptions) UserBufferEncMode() (UserBufferEncMode, error) { //nolint:gocritic // ignore hugeParam
	return opts.encMode()
}

// EncModeWithTags returns EncMode with options and tags that are both immutable (safe for concurrency).
func (opts EncOptions) EncModeWithTags(tags TagSet) (EncMode, error) { //nolint:gocritic // ignore hugeParam
	return opts.UserBufferEncModeWithTags(tags)
}

// UserBufferEncModeWithTags returns UserBufferEncMode with options and tags that are both immutable (safe for concurrency).
func (opts EncOptions) UserBufferEncModeWithTags(tags TagSet) (UserBufferEncMode, error) { //nolint:gocritic // ignore hugeParam
	if opts.TagsMd == TagsForbidden {
		return nil, errors.New("cbor: cannot create EncMode with TagSet when TagsMd is TagsForbidden")
	}
	if tags == nil {
		return nil, errors.New("cbor: cannot create EncMode with nil value as TagSet")
	}
	em, err := opts.encMode()
	if err != nil {
		return nil, err
	}
	// Copy tags
	ts := tagSet(make(map[reflect.Type]*tagItem))
	syncTags := tags.(*syncTagSet)
	syncTags.RLock()
	for contentType, tag := range syncTags.t {
		if tag.opts.EncTag != EncTagNone {
			ts[contentType] = tag
		}
	}
	syncTags.RUnlock()
	if len(ts) > 0 {
		em.tags = ts
	}
	return em, nil
}

// EncModeWithSharedTags returns EncMode with immutable options and mutable shared tags (safe for concurrency).
func (opts EncOptions) EncModeWithSharedTags(tags TagSet) (EncMode, error) { //nolint:gocritic // ignore hugeParam
	return opts.UserBufferEncModeWithSharedTags(tags)
}

// UserBufferEncModeWithSharedTags returns UserBufferEncMode with immutable options and mutable shared tags (safe for concurrency).
func (opts EncOptions) UserBufferEncModeWithSharedTags(tags TagSet) (UserBufferEncMode, error) { //nolint:gocritic // ignore hugeParam
	if opts.TagsMd == TagsForbidden {
		return nil, errors.New("cbor: cannot create EncMode with TagSet when TagsMd is TagsForbidden")
	}
	if tags == nil {
		return nil, errors.New("cbor: cannot create EncMode with nil value as TagSet")
	}
	em, err := opts.encMode()
	if err != nil {
		return nil, err
	}
	em.tags = tags
	return em, nil
}

func (opts EncOptions) encMode() (*encMode, error) { //nolint:gocritic // ignore hugeParam
	if !opts.Sort.valid() {
		return nil, invalidEncOptionError("Sort", int(opts.Sort))
	}
	if !opts.ShortestFloat.valid() {
		return nil, invalidEncOptionError("ShortestFloat", int(opts.ShortestFloat))
	}
	if !opts.NaNConvert.valid() {
		return nil, invalidEncOptionError("NaNConvert", int(opts.NaNConvert))
	}
	if !opts.InfConvert.valid() {
		return nil, invalidEncOptionError("InfConvert", int(opts.InfConvert))
	}
	if !opts.BigIntConvert.valid() {
		return nil, invalidEncOptionError("BigIntConvert", int(opts.BigIntConvert))
	}
	if opts.TagsMd == TagsForbidden && opts.BigIntConvert == BigIntConvertNone {
		return nil, tagsForbiddenEncOptionError("BigIntConvert is BigIntConvertNone")
	}
	if !opts.Time.valid() {
		return nil, invalidEncOptionError("Time", int(opts.Time))
	}
	if !opts.TimeTag.valid() {
		return nil, invalidEncOptionError("TimeTag", int(opts.TimeTag))
	}
	if !opts.IndefLength.valid() {
		return nil, invalidEncOptionError("IndefLength", int(opts.IndefLength))
	}
	if !opts.NilContainers.valid() {
		return nil, invalidEncOptionError("NilContainers", int(opts.NilContainers))
	}
	if !opts.TagsMd.valid() {
		return nil, invalidEncOptionError("TagsMd", int(opts.TagsMd))
	}
	if opts.TagsMd == TagsForbidden && opts.TimeTag == EncTagRequired {
		return nil, tagsForbiddenEncOptionError("TimeTag is EncTagRequired")
	}
	if !opts.OmitEmpty.valid() {
		return nil, invalidEncOptionError("OmitEmpty", int(opts.OmitEmpty))
	}
	stringMajorType, ok := opts.String.cborType()
	if !ok {
		return nil, invalidEncOptionError("String", int(opts.String))
	}
	if !opts.FieldName.valid() {
		return nil, invalidEncOptionError("FieldName", int(opts.FieldName))
	}
	byteSliceLaterEncodingTag, ok := opts.ByteSliceLaterFormat.encodingTag()
	if !ok {
		return nil, invalidEncOptionError("ByteSliceLaterFormat", int(opts.ByteSliceLaterFormat))
	}
	if opts.TagsMd == TagsForbidden && opts.ByteSliceLaterFormat != ByteSliceLaterFormatNone {
		return nil, tagsForbiddenEncOptionError("ByteSliceLaterFormat is not ByteSliceLaterFormatNone")
	}
	if !opts.ByteArray.valid() {
		return nil, invalidEncOptionError("ByteArray", int(opts.ByteArray))
	}
	if !opts.BinaryMarshaler.valid() {
		return nil, invalidEncOptionError("BinaryMarshaler", int(opts.BinaryMarshaler))
	}
	if !opts.TextMarshaler.valid() {
		return nil, invalidEncOptionError("TextMarshaler", int(opts.TextMarshaler))
	}
	if !opts.ToIndefArrayStructTag.valid() {
		return nil, invalidEncOptionError("ToIndefArrayStructTag", int(opts.ToIndefArrayStructTag))
	}
	if opts.IndefLength == IndefLengthForbidden && opts.ToIndefArrayStructTag == ToIndefArrayStructTagAllowed {
		return nil, conflictingEncOptionsError("ToIndefArrayStructTag", "ToIndefArrayStructTagAllowed", "IndefLength is IndefLengthForbidden")
	}
	em := encMode{
		sort:                      opts.Sort,
		shortestFloat:             opts.ShortestFloat,
		nanConvert:                opts.NaNConvert,
		infConvert:                opts.InfConvert,
		bigIntConvert:             opts.BigIntConvert,
		time:                      opts.Time,
		timeTag:                   opts.TimeTag,
		indefLength:               opts.IndefLength,
		nilContainers:             opts.NilContainers,
		tagsMd:                    opts.TagsMd,
		omitEmpty:                 opts.OmitEmpty,
		stringType:                opts.String,
		stringMajorType:           stringMajorType,
		fieldName:                 opts.FieldName,
		byteSliceLaterFormat:      opts.ByteSliceLaterFormat,
		byteSliceLaterEncodingTag: byteSliceLaterEncodingTag,
		byteArray:                 opts.ByteArray,
		binaryMarshaler:           opts.BinaryMarshaler,
		textMarshaler:             opts.TextMarshaler,
		jsonMarshalerTranscoder:   opts.JSONMarshalerTranscoder,
		toIndefArrayStructTag:     opts.ToIndefArrayStructTag,
	}
	return &em, nil
}

// invalidEncOptionError returns an error for an invalid EncOptions field value.
// The option parameter must be the EncOptions field name (e.g., "Sort").
func invalidEncOptionError(option string, value int) error {
	return errors.New("cbor: invalid " + option + " " + strconv.Itoa(value))
}

// conflictingEncOptionsError returns an error for an EncOptions field value
// that conflicts with another field.
// The option parameter must be the EncOptions field name (e.g., "TagsMd").
func conflictingEncOptionsError(option, value, conflict string) error {
	return errors.New("cbor: cannot set " + option + " to " + value + " when " + conflict)
}

// tagsForbiddenEncOptionError returns an error for an EncOptions field value that
// conflicts with TagsForbidden.
func tagsForbiddenEncOptionError(conflict string) error {
	return conflictingEncOptionsError("TagsMd", "TagsForbidden", conflict)
}

// EncMode is the main interface for CBOR encoding.
type EncMode interface {
	Marshal(v any) ([]byte, error)
	NewEncoder(w io.Writer) *Encoder
	EncOptions() EncOptions
}

// UserBufferEncMode is an interface for CBOR encoding, which extends EncMode by
// adding MarshalToBuffer to support user specified buffer rather than encoding
// into the built-in buffer pool.
type UserBufferEncMode interface {
	EncMode
	MarshalToBuffer(v any, buf *bytes.Buffer) error

	// This private method is to prevent users implementing
	// this interface and so future additions to it will
	// not be breaking changes.
	// See https://go.dev/blog/module-compatibility
	unexport()
}

type encMode struct {
	tags                      tagProvider
	sort                      SortMode
	shortestFloat             ShortestFloatMode
	nanConvert                NaNConvertMode
	infConvert                InfConvertMode
	bigIntConvert             BigIntConvertMode
	time                      TimeMode
	timeTag                   EncTagMode
	indefLength               IndefLengthMode
	nilContainers             NilContainersMode
	tagsMd                    TagsMode
	omitEmpty                 OmitEmptyMode
	stringType                StringMode
	stringMajorType           cborType
	fieldName                 FieldNameMode
	byteSliceLaterFormat      ByteSliceLaterFormatMode
	byteSliceLaterEncodingTag uint64
	byteArray                 ByteArrayMode
	binaryMarshaler           BinaryMarshalerMode
	textMarshaler             TextMarshalerMode
	jsonMarshalerTranscoder   Transcoder
	toIndefArrayStructTag     ToIndefArrayStructTagMode
}

var defaultEncMode, _ = EncOptions{}.encMode()

// These four decoding modes are used by getMarshalerDecMode.
// maxNestedLevels, maxArrayElements, and maxMapPairs are
// set to max allowed limits to avoid rejecting Marshaler
// output that would have been the allowable output of a
// non-Marshaler object that exceeds default limits.
var (
	marshalerForbidIndefLengthForbidTagsDecMode = decMode{
		maxNestedLevels:  maxMaxNestedLevels,
		maxArrayElements: maxMaxArrayElements,
		maxMapPairs:      maxMaxMapPairs,
		indefLength:      IndefLengthForbidden,
		tagsMd:           TagsForbidden,
	}

	marshalerAllowIndefLengthForbidTagsDecMode = decMode{
		maxNestedLevels:  maxMaxNestedLevels,
		maxArrayElements: maxMaxArrayElements,
		maxMapPairs:      maxMaxMapPairs,
		indefLength:      IndefLengthAllowed,
		tagsMd:           TagsForbidden,
	}

	marshalerForbidIndefLengthAllowTagsDecMode = decMode{
		maxNestedLevels:  maxMaxNestedLevels,
		maxArrayElements: maxMaxArrayElements,
		maxMapPairs:      maxMaxMapPairs,
		indefLength:      IndefLengthForbidden,
		tagsMd:           TagsAllowed,
	}

	marshalerAllowIndefLengthAllowTagsDecMode = decMode{
		maxNestedLevels:  maxMaxNestedLevels,
		maxArrayElements: maxMaxArrayElements,
		maxMapPairs:      maxMaxMapPairs,
		indefLength:      IndefLengthAllowed,
		tagsMd:           TagsAllowed,
	}
)

// getMarshalerDecMode returns one of four existing decoding modes
// which can be reused (safe for parallel use) for the purpose of
// checking if data returned by Marshaler is well-formed.
func getMarshalerDecMode(indefLength IndefLengthMode, tagsMd TagsMode) *decMode {
	switch {
	case indefLength == IndefLengthAllowed && tagsMd == TagsAllowed:
		return &marshalerAllowIndefLengthAllowTagsDecMode

	case indefLength == IndefLengthAllowed && tagsMd == TagsForbidden:
		return &marshalerAllowIndefLengthForbidTagsDecMode

	case indefLength == IndefLengthForbidden && tagsMd == TagsAllowed:
		return &marshalerForbidIndefLengthAllowTagsDecMode

	case indefLength == IndefLengthForbidden && tagsMd == TagsForbidden:
		return &marshalerForbidIndefLengthForbidTagsDecMode

	default:
		// This should never happen, unless we add new options to
		// IndefLengthMode or TagsMode without updating this function.
		return &decMode{
			maxNestedLevels:  maxMaxNestedLevels,
			maxArrayElements: maxMaxArrayElements,
			maxMapPairs:      maxMaxMapPairs,
			indefLength:      indefLength,
			tagsMd:           tagsMd,
		}
	}
}

// EncOptions returns user specified options used to create this EncMode.
func (em *encMode) EncOptions() EncOptions {
	return EncOptions{
		Sort:                    em.sort,
		ShortestFloat:           em.shortestFloat,
		NaNConvert:              em.nanConvert,
		InfConvert:              em.infConvert,
		BigIntConvert:           em.bigIntConvert,
		Time:                    em.time,
		TimeTag:                 em.timeTag,
		IndefLength:             em.indefLength,
		NilContainers:           em.nilContainers,
		TagsMd:                  em.tagsMd,
		OmitEmpty:               em.omitEmpty,
		String:                  em.stringType,
		FieldName:               em.fieldName,
		ByteSliceLaterFormat:    em.byteSliceLaterFormat,
		ByteArray:               em.byteArray,
		BinaryMarshaler:         em.binaryMarshaler,
		TextMarshaler:           em.textMarshaler,
		JSONMarshalerTranscoder: em.jsonMarshalerTranscoder,
		ToIndefArrayStructTag:   em.toIndefArrayStructTag,
	}
}

func (em *encMode) unexport() {}

func (em *encMode) appendTagBytes(dst []byte, v reflect.Value) []byte {
	if em.tags != nil {
		return em.appendTagBytesSlow(dst, v)
	}
	return dst
}

func (em *encMode) appendTagBytesSlow(dst []byte, v reflect.Value) []byte {
	if tagItem := em.tags.getTagItemFromType(v.Type()); tagItem != nil {
		return append(dst, tagItem.cborTagNum...)
	}
	return dst
}

// Marshal returns the CBOR encoding of v using em encoding mode.
//
// See the documentation for Marshal for details.
func (em *encMode) Marshal(v any) ([]byte, error) {
	bp := getEncodeBuffer()

	rv := reflect.ValueOf(v)

	var b []byte
	var handled bool
	var err error
	if tryFastPath(rv) {
		b, handled, err = fastEncode(*bp, em, v)
	}
	if !handled {
		b, err = encode(*bp, em, rv)
	}
	*bp = b[:0]
	if err != nil {
		putEncodeBuffer(bp)
		return nil, err
	}

	buf := make([]byte, len(b))
	copy(buf, b)

	putEncodeBuffer(bp)
	return buf, nil
}

// MarshalToBuffer encodes v into provided buffer (instead of using built-in buffer pool)
// and uses em encoding mode.
//
// NOTE: Unlike Marshal, the buffer provided to MarshalToBuffer can contain
// partially encoded data if error is returned.
//
// See Marshal for more details.
func (em *encMode) MarshalToBuffer(v any, buf *bytes.Buffer) error {
	if buf == nil {
		return errors.New("cbor: encoding buffer provided by user is nil")
	}

	rv := reflect.ValueOf(v)

	var b []byte
	var handled bool
	var err error
	if tryFastPath(rv) {
		b, handled, err = fastEncode(buf.AvailableBuffer(), em, v)
	}
	if !handled {
		b, err = encode(buf.AvailableBuffer(), em, rv)
	}
	if err != nil {
		return err
	}
	buf.Write(b)
	return nil
}

// NewEncoder returns a new encoder that writes to w using em EncMode.
func (em *encMode) NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w, em: em}
}

const encodeBufferInitialLength = 32 // TODO: make this configurable

// encodeBufferPool caches unused []byte for later reuse.
var encodeBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, encodeBufferInitialLength)
		return &b
	},
}

func getEncodeBuffer() *[]byte {
	bp := encodeBufferPool.Get().(*[]byte)
	*bp = (*bp)[:0]
	return bp
}

func putEncodeBuffer(bp *[]byte) {
	encodeBufferPool.Put(bp)
}

type encodeFunc func(dst []byte, em *encMode, v reflect.Value) ([]byte, error)
type isEmptyFunc func(em *encMode, v reflect.Value) (empty bool, err error)
type isZeroFunc func(em *encMode, v reflect.Value) (zero bool, err error)

func encode(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if !v.IsValid() {
		// v is zero value
		dst = append(dst, cborNil)
		return dst, nil
	}
	vt := v.Type()
	f, _, _ := getEncodeFunc(vt)
	if f == nil {
		return dst, &UnsupportedTypeError{vt}
	}

	return f(dst, em, v)
}

func encodeBool(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	dst = em.appendTagBytes(dst, v)
	return appendBool(dst, v.Bool()), nil
}

func appendBool(dst []byte, b bool) []byte {
	// NOTE: this function is written to be inlinable.
	bs := cborFalse
	if b {
		bs = cborTrue
	}
	return append(dst, bs)
}

func encodeInt(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	dst = em.appendTagBytes(dst, v)
	i := v.Int()
	if i >= 0 {
		return appendPositiveInt(dst, uint64(i)), nil
	}
	return appendNegativeInt(dst, i), nil
}

func encodeUint(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	dst = em.appendTagBytes(dst, v)
	return appendPositiveInt(dst, v.Uint()), nil
}

func appendPositiveInt(dst []byte, i uint64) []byte {
	// NOTE: this function is written to be inlinable.
	return appendHead(dst, byte(cborTypePositiveInt), i)
}

func appendNegativeInt(dst []byte, i int64) []byte {
	// NOTE: this function is written to be inlinable.
	return appendHead(dst, byte(cborTypeNegativeInt), uint64(^i)) //nolint:gosec
}

// NOTE: encodeFloat must be in sync with appendFloat64WithEM and produce the same encoded data.
func encodeFloat(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	dst = em.appendTagBytes(dst, v)
	f64 := v.Float()
	if math.IsNaN(f64) {
		return encodeNaN(dst, em, v)
	}
	if math.IsInf(f64, 0) {
		return encodeInf(dst, em, v)
	}
	if em.shortestFloat == ShortestFloatNone {
		if v.Kind() == reflect.Float64 {
			return appendFloat64(dst, f64), nil
		}
		return appendFloat32(dst, float32(f64)), nil
	}

	return appendShortestFloat64(dst, f64)
}

func appendShortestFloat64(dst []byte, f64 float64) ([]byte, error) {
	if cannotFitFloat32(f64) {
		return appendFloat64(dst, f64), nil
	}

	f32 := float32(f64)

	var f16 float16.Float16
	p := float16.PrecisionFromfloat32(f32)
	switch p {
	case float16.PrecisionExact:
		// Roundtrip float32->float16->float32 test isn't needed.
		f16 = float16.Fromfloat32(f32)
	case float16.PrecisionUnknown:
		// Try roundtrip float32->float16->float32 to determine if float32 can fit into float16.
		f16 = float16.Fromfloat32(f32)
		if f16.Float32() == f32 {
			p = float16.PrecisionExact
		}
	}
	if p == float16.PrecisionExact {
		return appendFloat16(dst, f16), nil
	}
	return appendFloat32(dst, f32), nil
}

func encodeInf(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	f64 := v.Float()
	switch em.infConvert {
	case InfConvertReject:
		return dst, &UnsupportedValueError{msg: "floating-point infinity"}

	case InfConvertFloat16:
		if f64 > 0 {
			dst = append(dst, cborPositiveInfinity...)
		} else {
			dst = append(dst, cborNegativeInfinity...)
		}
		return dst, nil
	}
	if v.Kind() == reflect.Float64 {
		return appendFloat64(dst, f64), nil
	}
	return appendFloat32(dst, float32(f64)), nil
}

func encodeNaN(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	switch em.nanConvert {
	case NaNConvert7e00:
		dst = append(dst, cborNaN...)
		return dst, nil

	case NaNConvertNone:
		if v.Kind() == reflect.Float64 {
			return appendFloat64(dst, v.Float()), nil
		}
		f32 := float32NaNFromReflectValue(v)
		return appendFloat32(dst, f32), nil

	case NaNConvertReject:
		return dst, &UnsupportedValueError{msg: "floating-point NaN"}

	default: // NaNConvertPreserveSignal, NaNConvertQuiet
		if v.Kind() == reflect.Float64 {
			f64 := v.Float()
			f64bits := math.Float64bits(f64)
			if em.nanConvert == NaNConvertQuiet && f64bits&(1<<51) == 0 {
				f64bits |= 1 << 51 // Set quiet bit = 1
				f64 = math.Float64frombits(f64bits)
			}
			// The lower 29 bits are dropped when converting from float64 to float32.
			if f64bits&0x1fffffff != 0 {
				// Encode NaN as float64 because dropped coef bits from float64 to float32 are not all 0s.
				return appendFloat64(dst, f64), nil
			}
			// Create float32 from float64 manually because float32(f64) always turns on NaN's quiet bits.
			sign := uint32(f64bits>>32) & (1 << 31)
			exp := uint32(0x7f800000)
			coef := uint32((f64bits & 0xfffffffffffff) >> 29)
			f32bits := sign | exp | coef
			f32 := math.Float32frombits(f32bits)
			// The lower 13 bits are dropped when converting from float32 to float16.
			if f32bits&0x1fff != 0 {
				// Encode NaN as float32 because dropped coef bits from float32 to float16 are not all 0s.
				return appendFloat32(dst, f32), nil
			}
			// Encode NaN as float16
			f16, _ := float16.FromNaN32ps(f32) // Ignore err because it only returns error when f32 is not a NaN.
			return appendFloat16(dst, f16), nil
		}

		f32 := float32NaNFromReflectValue(v)
		f32bits := math.Float32bits(f32)
		if em.nanConvert == NaNConvertQuiet && f32bits&(1<<22) == 0 {
			f32bits |= 1 << 22 // Set quiet bit = 1
			f32 = math.Float32frombits(f32bits)
		}
		// The lower 13 bits are dropped coef bits when converting from float32 to float16.
		if f32bits&0x1fff != 0 {
			// Encode NaN as float32 because dropped coef bits from float32 to float16 are not all 0s.
			return appendFloat32(dst, f32), nil
		}
		f16, _ := float16.FromNaN32ps(f32) // Ignore err because it only returns error when f32 is not a NaN.
		return appendFloat16(dst, f16), nil
	}
}

func appendFloat16(dst []byte, f16 float16.Float16) []byte {
	// NOTE: this function is written to be inlinable.
	return binary.BigEndian.AppendUint16(
		append(dst, byte(cborTypePrimitives)|additionalInformationAsFloat16),
		uint16(f16))
}

func appendFloat32(dst []byte, f32 float32) []byte {
	// NOTE: this function is written to be inlinable.
	return binary.BigEndian.AppendUint32(
		append(dst, byte(cborTypePrimitives)|additionalInformationAsFloat32),
		math.Float32bits(f32))
}

func appendFloat64(dst []byte, f64 float64) []byte {
	// NOTE: this function is written to be inlinable.
	return binary.BigEndian.AppendUint64(
		append(dst, byte(cborTypePrimitives)|additionalInformationAsFloat64),
		math.Float64bits(f64),
	)
}

// NOTE: encodeByteSlice must be in sync with appendByteSliceWithEM and produce the same encoded data.
func encodeByteSlice(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if v.IsNil() && em.nilContainers == NilContainerAsNull {
		return append(dst, cborNil), nil
	}
	if em.byteSliceLaterEncodingTag != 0 {
		dst = appendHead(dst, byte(cborTypeTag), em.byteSliceLaterEncodingTag)
	}
	dst = em.appendTagBytes(dst, v)
	return appendByteString(dst, v.Bytes()), nil
}

func appendByteString(dst []byte, bs []byte) []byte {
	// NOTE: this function is written to be inlinable.
	dst = appendHead(dst, byte(cborTypeByteString), uint64(len(bs)))
	return append(dst, bs...)
}

func encodeByteArrayAsByteString(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	dst = em.appendTagBytes(dst, v)
	slen := v.Len()
	dst = appendHead(dst, byte(cborTypeByteString), uint64(slen)) //nolint:gosec
	dst = slices.Grow(dst, slen)
	for i := range slen {
		dst = append(dst, byte(v.Index(i).Uint())) //nolint:gosec
	}
	return dst, nil
}

func encodeString(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	dst = em.appendTagBytes(dst, v)
	s := v.String()
	return appendStringWithEM(dst, em, s), nil
}

func appendStringWithEM(dst []byte, em *encMode, s string) []byte {
	// NOTE: this function is written to be inlinable.
	dst = appendHead(dst, byte(em.stringMajorType), uint64(len(s)))
	return append(dst, s...)
}

type arrayEncodeFunc struct {
	f encodeFunc
}

// NOTE: arrayEncodeFunc.encode must be in sync with appendSliceAnyWithEM and produce the same encoded data.
func (ae arrayEncodeFunc) encode(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if em.byteArray == ByteArrayToByteSlice && v.Type().Elem().Kind() == reflect.Uint8 {
		return encodeByteArrayAsByteString(dst, em, v)
	}
	if v.Kind() == reflect.Slice && v.IsNil() && em.nilContainers == NilContainerAsNull {
		dst = append(dst, cborNil)
		return dst, nil
	}
	dst = em.appendTagBytes(dst, v)
	alen := v.Len()
	if alen == 0 {
		dst = append(dst, byte(cborTypeArray))
		return dst, nil
	}
	dst = appendHead(dst, byte(cborTypeArray), uint64(alen)) //nolint:gosec
	for i := range alen {
		var err error
		if dst, err = ae.f(dst, em, v.Index(i)); err != nil {
			return dst, err
		}
	}
	return dst, nil
}

// encodeKeyValueFunc encodes key/value pairs in map (v).
// If kvs is provided (having the same length as v), length of encoded key and value are stored in kvs.
// kvs is used for canonical encoding of map.
type encodeKeyValueFunc func(dst []byte, em *encMode, v reflect.Value, kvs []keyValue) ([]byte, error)

type mapEncodeFunc struct {
	e encodeKeyValueFunc
}

// NOTE: mapEncodeFunc.encode must be in sync with appendMapAnyAnyWithEM, appendMapStringAnyWithEM,
// appendMapStringStringWithEM and produce the same encoded data.
func (me mapEncodeFunc) encode(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if v.IsNil() && em.nilContainers == NilContainerAsNull {
		dst = append(dst, cborNil)
		return dst, nil
	}
	dst = em.appendTagBytes(dst, v)
	mlen := v.Len()
	if mlen == 0 {
		dst = append(dst, byte(cborTypeMap))
		return dst, nil
	}

	dst = appendHead(dst, byte(cborTypeMap), uint64(mlen)) //nolint:gosec
	if em.sort == SortNone || em.sort == SortFastShuffle || mlen <= 1 {
		return me.e(dst, em, v, nil)
	}

	kvsp := getKeyValues(v.Len()) // for sorting keys
	defer putKeyValues(kvsp)
	kvs := *kvsp

	var err error

	kvBeginOffset := len(dst)
	if dst, err = me.e(dst, em, v, kvs); err != nil {
		return dst, err
	}

	return sortEncodedKeyValues(dst, em, kvs, kvBeginOffset), nil
}

func sortEncodedKeyValues(dst []byte, em *encMode, kvs []keyValue, kvBeginOffset int) []byte {
	kvTotalLen := len(dst) - kvBeginOffset

	// Use the capacity at the tail of the encode buffer as a staging area to rearrange the
	// encoded pairs into sorted order.
	dst = slices.Grow(dst, kvTotalLen)
	tmp := dst[len(dst) : len(dst)+kvTotalLen]
	kvb := dst[kvBeginOffset:]

	if em.sort == SortBytewiseLexical {
		// Sort kvs by map keys in bytewise lexical order.
		slices.SortFunc(kvs, func(kvi, kvj keyValue) int {
			return bytes.Compare(kvb[kvi.offset:kvi.valueOffset], kvb[kvj.offset:kvj.valueOffset])
		})
	} else {
		// Sort kvs by map keys in length first order.
		slices.SortFunc(kvs, func(kvi, kvj keyValue) int {
			if keyLengthDifference := (kvi.valueOffset - kvi.offset) - (kvj.valueOffset - kvj.offset); keyLengthDifference != 0 {
				return keyLengthDifference
			}
			return bytes.Compare(kvb[kvi.offset:kvi.valueOffset], kvb[kvj.offset:kvj.valueOffset])
		})
	}

	// This is where the encoded bytes are actually rearranged in the output buffer to reflect
	// the desired order.
	sortedOffset := 0
	for _, kv := range kvs {
		copy(tmp[sortedOffset:], kvb[kv.offset:kv.nextOffset])
		sortedOffset += kv.nextOffset - kv.offset
	}
	copy(kvb, tmp)

	return dst
}

// keyValue is the position of an encoded pair in a buffer. All offsets are zero-based and relative
// to the first byte of the first encoded pair.
type keyValue struct {
	offset      int
	valueOffset int
	nextOffset  int
}

// maxPooledKeyValues is the max capacity of []keyValue in keyValuePool.
const maxPooledKeyValues = 1024

var keyValuePool = sync.Pool{
	New: func() any {
		return new([]keyValue)
	},
}

func getKeyValues(length int) *[]keyValue {
	v := keyValuePool.Get().(*[]keyValue)
	if cap(*v) < length {
		*v = make([]keyValue, length)
	} else {
		*v = (*v)[:length]
	}
	return v
}

func putKeyValues(x *[]keyValue) {
	if cap(*x) > maxPooledKeyValues {
		*x = nil
	} else {
		*x = (*x)[:0]
	}
	keyValuePool.Put(x)
}

func encodeStructToArray(dst []byte, em *encMode, v reflect.Value) (_ []byte, err error) {
	structType, err := getEncodingStructType(v.Type())
	if err != nil {
		return dst, err
	}

	if structType.toIndefArray && em.toIndefArrayStructTag != ToIndefArrayStructTagAllowed {
		return dst, errors.New("cbor: cannot encode struct " + v.Type().String() +
			" with `toindefarray` when ToIndefArrayStructTag is not ToIndefArrayStructTagAllowed")
	}

	dst = em.appendTagBytes(dst, v)

	flds := structType.fields

	if structType.toIndefArray {
		dst = append(dst, cborArrayWithIndefiniteLengthHead)
	} else {
		dst = appendHead(dst, byte(cborTypeArray), uint64(len(flds)))
	}
	for i := range flds {
		f := flds[i]

		var fv reflect.Value
		if len(f.idx) == 1 {
			fv = v.Field(f.idx[0])
		} else {
			// Get embedded field value.  No error is expected.
			fv, _ = getFieldValue(v, f.idx, func(reflect.Value) (reflect.Value, error) {
				// Write CBOR nil for null pointer to embedded struct
				dst = append(dst, cborNil)
				return reflect.Value{}, nil
			})
			if !fv.IsValid() {
				continue
			}
		}

		if dst, err = f.ef(dst, em, fv); err != nil {
			return dst, err
		}
	}
	if structType.toIndefArray {
		dst = append(dst, cborBreakFlag)
	}
	return dst, nil
}

func encodeStruct(dst []byte, em *encMode, v reflect.Value) (_ []byte, err error) {
	structType, err := getEncodingStructType(v.Type())
	if err != nil {
		return dst, err
	}

	flds := structType.getFields(em)

	start := 0
	if em.sort == SortFastShuffle && len(flds) > 0 {
		start = rand.Intn(len(flds)) //nolint:gosec // Don't need a CSPRNG for deck cutting.
	}

	dst = em.appendTagBytes(dst, v)

	// Encode head with struct field count.
	// Head is rewritten later if actual encoded field count is different from struct field count.
	headBeginOffset := len(dst)
	dst = appendHead(dst, byte(cborTypeMap), uint64(len(flds)))
	encodedHeadLen := len(dst) - headBeginOffset

	kvBeginOffset := len(dst)
	kvCount := 0
	for offset := range flds {
		f := flds[(start+offset)%len(flds)]

		var fv reflect.Value
		if len(f.idx) == 1 {
			fv = v.Field(f.idx[0])
		} else {
			// Get embedded field value.  No error is expected.
			fv, _ = getFieldValue(v, f.idx, func(reflect.Value) (reflect.Value, error) {
				// Skip null pointer to embedded struct
				return reflect.Value{}, nil
			})
			if !fv.IsValid() {
				continue
			}
		}
		if f.omitEmpty {
			empty, emptyErr := f.ief(em, fv)
			if emptyErr != nil {
				return dst, emptyErr
			}
			if empty {
				continue
			}
		}
		if f.omitZero {
			zero, zeroErr := f.izf(em, fv)
			if zeroErr != nil {
				return dst, zeroErr
			}
			if zero {
				continue
			}
		}

		if !f.keyAsInt && em.fieldName == FieldNameToByteString {
			dst = append(dst, f.cborNameByteString...)
		} else { // int or text string
			dst = append(dst, f.cborName...)
		}

		if dst, err = f.ef(dst, em, fv); err != nil {
			return dst, err
		}

		kvCount++
	}

	if len(flds) == kvCount {
		// Encoded element count in head is the same as actual element count.
		return dst, nil
	}

	// Overwrite the bytes that were reserved for the head before encoding the map entries.
	var actualHeadLen int
	{
		headbuf := dst[headBeginOffset : headBeginOffset : headBeginOffset+encodedHeadLen]
		headbuf = appendHead(headbuf, byte(cborTypeMap), uint64(kvCount))
		actualHeadLen = len(headbuf)
	}

	if actualHeadLen == encodedHeadLen {
		// The bytes reserved for the encoded head were exactly the right size, so the
		// encoded entries are already in their final positions.
		return dst, nil
	}

	// We reserved more bytes than needed for the encoded head, based on the number of fields
	// encoded. The encoded entries are offset to the right by the number of excess reserved
	// bytes. Shift the entries left to remove the gap.
	excessReservedBytes := encodedHeadLen - actualHeadLen
	copy(dst[kvBeginOffset-excessReservedBytes:len(dst)-excessReservedBytes], dst[kvBeginOffset:])

	// After shifting, the excess bytes are at the end of the output buffer and they are
	// garbage.
	dst = dst[:len(dst)-excessReservedBytes]
	return dst, nil
}

func encodeInterface(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if v.IsNil() {
		dst = append(dst, cborNil)
		return dst, nil
	}
	return encode(dst, em, v.Elem())
}

// Inclusive range of Unix seconds within which Time.UnixNano is defined
// for any nanosecond fraction.
const (
	minUnixNanoSecs = math.MinInt64 / 1_000_000_000
	maxUnixNanoSecs = math.MaxInt64/1_000_000_000 - 1
)

func encodeTime(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	var t time.Time
	if v.CanAddr() {
		t = *v.Addr().Interface().(*time.Time)
	} else {
		t = v.Interface().(time.Time)
	}

	if t.IsZero() {
		dst = append(dst, cborNil) // Even if tag is required, encode as CBOR null.
		return dst, nil
	}
	if em.timeTag == EncTagRequired {
		tagNumber := 1
		if em.time == TimeRFC3339 || em.time == TimeRFC3339Nano || em.time == TimeRFC3339NanoUTC {
			tagNumber = 0
		}
		dst = appendHead(dst, byte(cborTypeTag), uint64(tagNumber))
	}
	var s string
	switch em.time {
	case TimeUnix:
		secs := t.Unix()
		return encodeInt(dst, em, reflect.ValueOf(secs))

	case TimeUnixMicro:
		t = t.UTC().Round(time.Microsecond)
		var f float64
		if secs := t.Unix(); secs >= minUnixNanoSecs && secs <= maxUnixNanoSecs {
			// Unix seconds within [minUnixNanoSecs, maxUnixNanoSecs]
			// are encoded from time.UnixNano to keep encoded data unchanged.
			f = float64(t.UnixNano()) / 1e9
		} else {
			// Unix seconds outside [minUnixNanoSecs, maxUnixNanoSecs]
			// are encoded from Unix seconds and nanoseconds.
			nsecs := t.Nanosecond()
			f = float64(secs) + float64(nsecs)/1e9
		}
		return appendFloat64WithEM(dst, em, f)

	case TimeUnixDynamic:
		t = t.UTC().Round(time.Microsecond)
		secs, nsecs := t.Unix(), uint64(t.Nanosecond()) //nolint:gosec
		if nsecs == 0 {
			return encodeInt(dst, em, reflect.ValueOf(secs))
		}
		f := float64(secs) + float64(nsecs)/1e9
		return appendFloat64WithEM(dst, em, f)

	case TimeRFC3339:
		s = t.Format(time.RFC3339)

	case TimeRFC3339NanoUTC:
		s = t.UTC().Format(time.RFC3339Nano)

	default: // TimeRFC3339Nano
		s = t.Format(time.RFC3339Nano)
	}
	if err := checkRFC3339Limits(s); err != nil {
		return dst, err
	}
	return encodeString(dst, em, reflect.ValueOf(s))
}

// checkRFC3339Limits returns an error if s, text formatted by Go using the
// time.RFC3339 or time.RFC3339Nano layout, exceeds the limits of RFC 3339:
// 4-digit years (0000-9999) and UTC offset hours 00-23.
func checkRFC3339Limits(s string) error {
	// NOTE: this function is written to be inlinable.
	const rfc3339YearLength = len("9999")
	const rfc3339UTCOffsetLength = len("+07:00")

	if s[rfc3339YearLength] != '-' {
		return &UnsupportedValueError{msg: "time.Time year outside of range [0,9999] for RFC 3339"}
	}
	if s[len(s)-1] != 'Z' {
		// Go formats the UTC offset as "+hh:mm" or "-hh:mm", with more hour digits
		// for offsets of 100 hours or more.
		zone := s[len(s)-rfc3339UTCOffsetLength:]
		if (zone[0] != '+' && zone[0] != '-') || zone[1] > '2' || (zone[1] == '2' && zone[2] >= '4') {
			return &UnsupportedValueError{msg: "time.Time UTC offset outside of range [-23:59,+23:59] for RFC 3339"}
		}
	}
	return nil
}

func encodeBigInt(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if em.bigIntConvert == BigIntConvertReject {
		return dst, &UnsupportedTypeError{Type: typeBigInt}
	}

	var vbi big.Int
	if v.CanAddr() {
		vbi = *v.Addr().Interface().(*big.Int)
	} else {
		vbi = v.Interface().(big.Int)
	}

	sign := vbi.Sign()
	bi := new(big.Int).SetBytes(vbi.Bytes()) // bi is absolute value of v
	if sign < 0 {
		// For negative number, convert to CBOR encoded number (-v-1).
		bi.Sub(bi, big.NewInt(1))
	}

	if em.bigIntConvert == BigIntConvertShortest {
		if bi.IsUint64() {
			if sign >= 0 {
				// Encode as CBOR pos int (major type 0)
				dst = appendHead(dst, byte(cborTypePositiveInt), bi.Uint64())
				return dst, nil
			}
			// Encode as CBOR neg int (major type 1)
			dst = appendHead(dst, byte(cborTypeNegativeInt), bi.Uint64())
			return dst, nil
		}
	}

	if em.tagsMd == TagsForbidden {
		return dst, errors.New("cbor: cannot encode big.Int outside CBOR integer range when TagsMd is TagsForbidden")
	}

	tagNum := 2
	if sign < 0 {
		tagNum = 3
	}
	// Write tag number
	dst = appendHead(dst, byte(cborTypeTag), uint64(tagNum))
	// Write bignum byte string
	b := bi.Bytes()
	dst = appendHead(dst, byte(cborTypeByteString), uint64(len(b)))
	dst = append(dst, b...)
	return dst, nil
}

type binaryMarshalerEncoder struct {
	alternateEncode  encodeFunc
	alternateIsEmpty isEmptyFunc
	alternateIsZero  isZeroFunc
	isZeroFunc       isZeroFunc
}

func (bme binaryMarshalerEncoder) encode(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if em.binaryMarshaler != BinaryMarshalerByteString {
		if bme.alternateEncode == nil {
			return dst, &UnsupportedTypeError{Type: v.Type()}
		}
		return bme.alternateEncode(dst, em, v)
	}

	bmv := v
	if bmv.CanAddr() {
		bmv = bmv.Addr()
	}
	m, ok := bmv.Interface().(encoding.BinaryMarshaler)
	if !ok {
		pv := reflect.New(v.Type())
		pv.Elem().Set(v)
		m = pv.Interface().(encoding.BinaryMarshaler)
	}
	data, err := m.MarshalBinary()
	if err != nil {
		return dst, err
	}
	dst = em.appendTagBytes(dst, v)
	dst = appendHead(dst, byte(cborTypeByteString), uint64(len(data)))
	dst = append(dst, data...)
	return dst, nil
}

func (bme binaryMarshalerEncoder) isEmpty(em *encMode, v reflect.Value) (bool, error) {
	if em.binaryMarshaler != BinaryMarshalerByteString {
		if bme.alternateIsEmpty == nil {
			return false, &UnsupportedTypeError{Type: v.Type()}
		}
		return bme.alternateIsEmpty(em, v)
	}

	bmv := v
	if bmv.CanAddr() {
		bmv = bmv.Addr()
	}
	m, ok := bmv.Interface().(encoding.BinaryMarshaler)
	if !ok {
		pv := reflect.New(v.Type())
		pv.Elem().Set(v)
		m = pv.Interface().(encoding.BinaryMarshaler)
	}
	data, err := m.MarshalBinary()
	if err != nil {
		return false, err
	}
	return len(data) == 0, nil
}

func (bme binaryMarshalerEncoder) isZero(em *encMode, v reflect.Value) (bool, error) {
	if em.binaryMarshaler != BinaryMarshalerByteString {
		if bme.alternateEncode == nil {
			return false, &UnsupportedTypeError{Type: v.Type()}
		}
		return bme.alternateIsZero(em, v)
	}
	return bme.isZeroFunc(em, v)
}

type textMarshalerEncoder struct {
	alternateEncode  encodeFunc
	alternateIsEmpty isEmptyFunc
	alternateIsZero  isZeroFunc
	isZeroFunc       isZeroFunc
}

func (tme textMarshalerEncoder) encode(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if em.textMarshaler == TextMarshalerNone {
		if tme.alternateEncode == nil {
			return dst, &UnsupportedTypeError{Type: v.Type()}
		}
		return tme.alternateEncode(dst, em, v)
	}

	tmv := v
	if tmv.CanAddr() {
		tmv = tmv.Addr()
	}
	m, ok := tmv.Interface().(encoding.TextMarshaler)
	if !ok {
		pv := reflect.New(v.Type())
		pv.Elem().Set(v)
		m = pv.Interface().(encoding.TextMarshaler)
	}
	data, err := m.MarshalText()
	if err != nil {
		return dst, fmt.Errorf("cbor: cannot marshal text for %s: %w", v.Type(), err)
	}
	dst = em.appendTagBytes(dst, v)

	dst = appendHead(dst, byte(cborTypeTextString), uint64(len(data)))
	dst = append(dst, data...)
	return dst, nil
}

func (tme textMarshalerEncoder) isEmpty(em *encMode, v reflect.Value) (bool, error) {
	if em.textMarshaler == TextMarshalerNone {
		if tme.alternateIsEmpty == nil {
			return false, &UnsupportedTypeError{Type: v.Type()}
		}
		return tme.alternateIsEmpty(em, v)
	}

	tmv := v
	if tmv.CanAddr() {
		tmv = tmv.Addr()
	}
	m, ok := tmv.Interface().(encoding.TextMarshaler)
	if !ok {
		pv := reflect.New(v.Type())
		pv.Elem().Set(v)
		m = pv.Interface().(encoding.TextMarshaler)
	}
	data, err := m.MarshalText()
	if err != nil {
		return false, fmt.Errorf("cbor: cannot marshal text for %s: %w", v.Type(), err)
	}
	return len(data) == 0, nil
}

func (tme textMarshalerEncoder) isZero(em *encMode, v reflect.Value) (bool, error) {
	if em.textMarshaler == TextMarshalerNone {
		if tme.alternateEncode == nil {
			return false, &UnsupportedTypeError{Type: v.Type()}
		}
		return tme.alternateIsZero(em, v)
	}
	return tme.isZeroFunc(em, v)
}

// transcodeBufferPool caches unused bytes.Buffer for transcoding.
var transcodeBufferPool = sync.Pool{
	New: func() any {
		e := new(bytes.Buffer)
		e.Grow(encodeBufferInitialLength)
		return e
	},
}

func getTranscodeBuffer() *bytes.Buffer {
	e := transcodeBufferPool.Get().(*bytes.Buffer)
	e.Reset()
	return e
}

func putTranscodeBuffer(e *bytes.Buffer) {
	transcodeBufferPool.Put(e)
}

type jsonMarshalerEncoder struct {
	alternateEncode  encodeFunc
	alternateIsEmpty isEmptyFunc
	alternateIsZero  isZeroFunc
	isZeroFunc       isZeroFunc
}

func (jme jsonMarshalerEncoder) encode(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if em.jsonMarshalerTranscoder == nil {
		if jme.alternateEncode == nil {
			return dst, &UnsupportedTypeError{Type: v.Type()}
		}
		return jme.alternateEncode(dst, em, v)
	}

	jmv := v
	if jmv.CanAddr() {
		jmv = jmv.Addr()
	}
	m, ok := jmv.Interface().(jsonMarshaler)
	if !ok {
		pv := reflect.New(v.Type())
		pv.Elem().Set(v)
		m = pv.Interface().(jsonMarshaler)
	}

	json, err := m.MarshalJSON()
	if err != nil {
		return dst, err
	}

	e := getTranscodeBuffer()
	defer putTranscodeBuffer(e)

	encodedTag := em.appendTagBytes(e.AvailableBuffer(), v)
	e.Write(encodedTag)

	err = em.jsonMarshalerTranscoder.Transcode(e, bytes.NewReader(json))
	if err != nil {
		return dst, &TranscodeError{err: err, rtype: v.Type(), sourceFormat: "json", targetFormat: "cbor"}
	}

	b := e.Bytes()

	// Validate that the transcode function has written exactly one well-formed data item.
	d := decoder{data: b, dm: getMarshalerDecMode(em.indefLength, em.tagsMd)}
	if err := d.wellformed(false, true); err != nil {
		return dst, &TranscodeError{err: err, rtype: v.Type(), sourceFormat: "json", targetFormat: "cbor"}
	}

	dst = append(dst, b...)
	return dst, nil
}

func (jme jsonMarshalerEncoder) isEmpty(em *encMode, v reflect.Value) (bool, error) {
	if em.jsonMarshalerTranscoder == nil {
		if jme.alternateIsEmpty == nil {
			return false, &UnsupportedTypeError{Type: v.Type()}
		}
		return jme.alternateIsEmpty(em, v)
	}

	// As with types implementing cbor.Marshaler, transcoded json.Marshaler values always encode
	// as exactly one complete CBOR data item.
	return false, nil
}

func (jme jsonMarshalerEncoder) isZero(em *encMode, v reflect.Value) (bool, error) {
	if em.jsonMarshalerTranscoder == nil {
		if jme.alternateEncode == nil {
			return false, &UnsupportedTypeError{Type: v.Type()}
		}
		return jme.alternateIsZero(em, v)
	}
	return jme.isZeroFunc(em, v)
}

func encodeMarshalerType(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if em.tagsMd == TagsForbidden && v.Type() == typeRawTag {
		return dst, errors.New("cbor: cannot encode cbor.RawTag when TagsMd is TagsForbidden")
	}
	mv := v
	if mv.CanAddr() {
		mv = mv.Addr()
	}
	m, ok := mv.Interface().(Marshaler)
	if !ok {
		pv := reflect.New(v.Type())
		pv.Elem().Set(v)
		m = pv.Interface().(Marshaler)
	}
	data, err := m.MarshalCBOR()
	if err != nil {
		return dst, err
	}

	// Verify returned CBOR data item from MarshalCBOR() is well-formed and passes tag validity for builtin tags 0-3.
	d := decoder{data: data, dm: getMarshalerDecMode(em.indefLength, em.tagsMd)}
	err = d.wellformed(false, true)
	if err != nil {
		return dst, &MarshalerError{typ: v.Type(), err: err}
	}

	dst = append(dst, data...)
	return dst, nil
}

func encodeTag(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if em.tagsMd == TagsForbidden {
		return dst, errors.New("cbor: cannot encode cbor.Tag when TagsMd is TagsForbidden")
	}

	var t Tag
	if v.CanAddr() {
		t = *v.Addr().Interface().(*Tag)
	} else {
		t = v.Interface().(Tag)
	}

	if t.Number == 0 && t.Content == nil {
		// Marshal uninitialized cbor.Tag
		dst = append(dst, cborNil)
		return dst, nil
	}

	// Marshal tag number
	dst = appendHead(dst, byte(cborTypeTag), t.Number)

	vem := *em // shallow copy

	// For built-in tags, disable settings that may introduce tag validity errors when
	// marshaling certain Content values.
	switch t.Number {
	case tagNumRFC3339Time:
		vem.stringType = StringToTextString
		vem.stringMajorType = cborTypeTextString
	case tagNumUnsignedBignum, tagNumNegativeBignum:
		vem.byteSliceLaterFormat = ByteSliceLaterFormatNone
		vem.byteSliceLaterEncodingTag = 0
	}

	// Marshal tag content
	if enableEncodingFastPath {
		return appendAnyWithEM(dst, &vem, t.Content)
	}
	return encode(dst, &vem, reflect.ValueOf(t.Content))
}

// encodedHeadLength returns the number of bytes for CBOR head with argument n.
func encodedHeadLength(n uint64) int {
	// NOTE: this function is written to be inlinable.
	const initialByteSize = 1

	if n <= maxAdditionalInformationWithoutArgument {
		return initialByteSize
	}
	if n <= math.MaxUint8 {
		const argumentSize = 1
		return initialByteSize + argumentSize
	}
	if n <= math.MaxUint16 {
		const argumentSize = 2
		return initialByteSize + argumentSize
	}
	if n <= math.MaxUint32 {
		const argumentSize = 4
		return initialByteSize + argumentSize
	}
	const argumentSize = 8
	return initialByteSize + argumentSize
}

// appendHead appends CBOR head of type t and argument n to the given byte buffer.
func appendHead(dst []byte, t byte, n uint64) []byte {
	// NOTE: this function is written to be inlinable.

	if n <= maxAdditionalInformationWithoutArgument {
		return append(dst, t|byte(n))
	}

	if n <= math.MaxUint8 {
		return append(dst,
			t|byte(additionalInformationWith1ByteArgument),
			byte(n),
		)
	}

	if n <= math.MaxUint16 {
		return binary.BigEndian.AppendUint16(
			append(dst, t|byte(additionalInformationWith2ByteArgument)),
			uint16(n))
	}

	if n <= math.MaxUint32 {
		return binary.BigEndian.AppendUint32(
			append(dst, t|byte(additionalInformationWith4ByteArgument)),
			uint32(n))
	}

	return binary.BigEndian.AppendUint64(
		append(dst, t|byte(additionalInformationWith8ByteArgument)),
		n)
}

type jsonMarshaler interface{ MarshalJSON() ([]byte, error) }

var (
	typeMarshaler       = reflect.TypeFor[Marshaler]()
	typeBinaryMarshaler = reflect.TypeFor[encoding.BinaryMarshaler]()
	typeTextMarshaler   = reflect.TypeFor[encoding.TextMarshaler]()
	typeJSONMarshaler   = reflect.TypeFor[jsonMarshaler]()
	typeRawMessage      = reflect.TypeFor[RawMessage]()
	typeByteString      = reflect.TypeFor[ByteString]()
	typeAny             = reflect.TypeFor[any]()
	typeSliceAny        = reflect.TypeFor[[]any]()
	typeMapAnyAny       = reflect.TypeFor[map[any]any]()
	typeMapStringAny    = reflect.TypeFor[map[string]any]()
	typeMapStringString = reflect.TypeFor[map[string]string]()
)

//nolint:gocyclo
func newEncodeFunc(t reflect.Type, newEncodeFuncs map[reflect.Type]*inProgressEncodeFuncs) (ef encodeFunc, ief isEmptyFunc, izf isZeroFunc) {
	// baseIzf stores the original izf before marshaler encoders unwinding.
	var baseIzf isZeroFunc

	fs := &inProgressEncodeFuncs{}
	newEncodeFuncs[t] = fs
	defer func() {
		fs.ef = ef
		fs.ief = ief
		fs.izf = izf
		fs.complete = true
	}()

	k := t.Kind()
	if k == reflect.Pointer {
		return getEncodeIndirectValueFunc(t, newEncodeFuncs), isEmptyPtr, getIsZeroFunc(t)
	}
	switch t {
	case typeSimpleValue:
		return encodeMarshalerType, isEmptyUint, getIsZeroFunc(t)

	case typeTag:
		return encodeTag, alwaysNotEmpty, getIsZeroFunc(t)

	case typeTime:
		return encodeTime, alwaysNotEmpty, getIsZeroFunc(t)

	case typeBigInt:
		return encodeBigInt, alwaysNotEmpty, getIsZeroFunc(t)

	case typeRawMessage:
		return encodeMarshalerType, isEmptySlice, getIsZeroFunc(t)

	case typeByteString:
		return encodeMarshalerType, isEmptyString, getIsZeroFunc(t)
	}
	if enableEncodingFastPath {
		switch t {
		case typeAny:
			return encodeAny, isEmptyInterface, getIsZeroFunc(t)

		case typeSliceAny:
			return encodeSliceAny, isEmptySlice, getIsZeroFunc(t)

		case typeMapAnyAny:
			return encodeMapAnyAny, isEmptyMap, getIsZeroFunc(t)

		case typeMapStringAny:
			return encodeMapStringAny, isEmptyMap, getIsZeroFunc(t)

		case typeMapStringString:
			return encodeMapStringString, isEmptyMap, getIsZeroFunc(t)
		}
	}
	if reflect.PointerTo(t).Implements(typeMarshaler) {
		return encodeMarshalerType, alwaysNotEmpty, getIsZeroFunc(t)
	}
	if reflect.PointerTo(t).Implements(typeBinaryMarshaler) {
		defer func() {
			// capture encoding method used for modes that disable BinaryMarshaler
			bme := binaryMarshalerEncoder{
				alternateEncode:  ef,
				alternateIsEmpty: ief,
				alternateIsZero:  izfOrDefault(t, izf),
				isZeroFunc:       baseIzf,
			}
			ef = bme.encode
			ief = bme.isEmpty
			izf = bme.isZero
		}()
	}
	if reflect.PointerTo(t).Implements(typeTextMarshaler) {
		defer func() {
			// capture encoding method used for modes that disable TextMarshaler
			tme := textMarshalerEncoder{
				alternateEncode:  ef,
				alternateIsEmpty: ief,
				alternateIsZero:  izfOrDefault(t, izf),
				isZeroFunc:       baseIzf,
			}
			ef = tme.encode
			ief = tme.isEmpty
			izf = tme.isZero
		}()
	}
	if reflect.PointerTo(t).Implements(typeJSONMarshaler) {
		defer func() {
			// capture encoding method used for modes that don't support transcoding
			// from types that implement json.Marshaler.
			jme := jsonMarshalerEncoder{
				alternateEncode:  ef,
				alternateIsEmpty: ief,
				alternateIsZero:  izfOrDefault(t, izf),
				isZeroFunc:       baseIzf,
			}
			ef = jme.encode
			ief = jme.isEmpty
			izf = jme.isZero
		}()
	}

	// Capture base izf before marshaler encoder unwinding.
	defer func() {
		baseIzf = izfOrDefault(t, izf)
	}()

	switch k {
	case reflect.Bool:
		return encodeBool, isEmptyBool, getIsZeroFunc(t)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return encodeInt, isEmptyInt, getIsZeroFunc(t)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return encodeUint, isEmptyUint, getIsZeroFunc(t)

	case reflect.Float32, reflect.Float64:
		return encodeFloat, isEmptyFloat, getIsZeroFunc(t)

	case reflect.String:
		return encodeString, isEmptyString, getIsZeroFunc(t)

	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return encodeByteSlice, isEmptySlice, getIsZeroFunc(t)
		}
		fallthrough

	case reflect.Array:
		f := getEncodeFuncWithNewEncodeFuncs(t.Elem(), newEncodeFuncs)
		if f == nil {
			return nil, nil, nil
		}
		return arrayEncodeFunc{f: f}.encode, isEmptySlice, getIsZeroFunc(t)

	case reflect.Map:
		f := getEncodeMapFunc(t, newEncodeFuncs)
		if f == nil {
			return nil, nil, nil
		}
		return f, isEmptyMap, getIsZeroFunc(t)

	case reflect.Struct:
		// Get struct's special field "_" tag options
		if f, ok := t.FieldByName("_"); ok {
			tag := f.Tag.Get("cbor")
			if tag != "-" {
				if hasToArrayOption(tag) || hasToIndefArrayOption(tag) {
					return encodeStructToArray, isEmptyStruct, isZeroFieldStruct
				}
			}
		}
		return encodeStruct, isEmptyStruct, getIsZeroFunc(t)

	case reflect.Interface:
		return encodeInterface, isEmptyInterface, getIsZeroFunc(t)
	}
	return nil, nil, nil
}

func getEncodeIndirectValueFunc(t reflect.Type, newEncodeFuncs map[reflect.Type]*inProgressEncodeFuncs) encodeFunc {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	f := getEncodeFuncWithNewEncodeFuncs(t, newEncodeFuncs)
	if f == nil {
		return nil
	}
	return func(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
		for v.Kind() == reflect.Pointer && !v.IsNil() {
			v = v.Elem()
		}
		if v.Kind() == reflect.Pointer && v.IsNil() {
			dst = append(dst, cborNil)
			return dst, nil
		}
		return f(dst, em, v)
	}
}

func alwaysNotEmpty(_ *encMode, _ reflect.Value) (empty bool, err error) {
	return false, nil
}

func isEmptyBool(_ *encMode, v reflect.Value) (bool, error) {
	return !v.Bool(), nil
}

func isEmptyInt(_ *encMode, v reflect.Value) (bool, error) {
	return v.Int() == 0, nil
}

func isEmptyUint(_ *encMode, v reflect.Value) (bool, error) {
	return v.Uint() == 0, nil
}

func isEmptyFloat(_ *encMode, v reflect.Value) (bool, error) {
	return v.Float() == 0.0, nil
}

func isEmptyString(_ *encMode, v reflect.Value) (bool, error) {
	return v.Len() == 0, nil
}

func isEmptySlice(_ *encMode, v reflect.Value) (bool, error) {
	return v.Len() == 0, nil
}

func isEmptyMap(_ *encMode, v reflect.Value) (bool, error) {
	return v.Len() == 0, nil
}

func isEmptyPtr(_ *encMode, v reflect.Value) (bool, error) {
	return v.IsNil(), nil
}

func isEmptyInterface(_ *encMode, v reflect.Value) (bool, error) {
	return v.IsNil(), nil
}

func isEmptyStruct(em *encMode, v reflect.Value) (bool, error) {
	structType, err := getEncodingStructType(v.Type())
	if err != nil {
		return false, err
	}

	if em.omitEmpty == OmitEmptyGoValue {
		return false, nil
	}

	if structType.toArray || structType.toIndefArray {
		return len(structType.fields) == 0, nil
	}

	if len(structType.fields) > len(structType.omitEmptyFieldsIdx) {
		return false, nil
	}

	for _, i := range structType.omitEmptyFieldsIdx {
		f := structType.fields[i]

		// Get field value
		var fv reflect.Value
		if len(f.idx) == 1 {
			fv = v.Field(f.idx[0])
		} else {
			// Get embedded field value.  No error is expected.
			fv, _ = getFieldValue(v, f.idx, func(reflect.Value) (reflect.Value, error) {
				// Skip null pointer to embedded struct
				return reflect.Value{}, nil
			})
			if !fv.IsValid() {
				continue
			}
		}

		empty, err := f.ief(em, fv)
		if err != nil {
			return false, err
		}
		if !empty {
			return false, nil
		}
	}
	return true, nil
}

func cannotFitFloat32(f64 float64) bool {
	f32 := float32(f64)
	return float64(f32) != f64
}

// float32NaNFromReflectValue extracts float32 NaN from reflect.Value while preserving NaN's quiet bit.
func float32NaNFromReflectValue(v reflect.Value) float32 {
	// Keith Randall's workaround for issue https://github.com/golang/go/issues/36400
	p := reflect.New(v.Type())
	p.Elem().Set(v)
	f32 := p.Convert(reflect.TypeFor[*float32]()).Elem().Interface().(float32)
	return f32
}

type isZeroer interface {
	IsZero() bool
}

var isZeroerType = reflect.TypeFor[isZeroer]()

// getIsZeroFunc returns a function for the given type that can be called to determine if a given value is zero.
// Types that implement `IsZero() bool` are delegated to for non-nil values.
// Types that do not implement `IsZero() bool` use the reflect.Value#IsZero() implementation.
// The returned function matches behavior of stdlib encoding/json behavior in Go 1.24+.
func getIsZeroFunc(t reflect.Type) isZeroFunc {
	// Provide a function that uses a type's IsZero method if defined.
	switch {
	case t == nil:
		return isZeroDefault
	case t.Kind() == reflect.Interface && t.Implements(isZeroerType):
		return isZeroInterfaceCustom
	case t.Kind() == reflect.Pointer && t.Implements(isZeroerType):
		return isZeroPointerCustom
	case t.Implements(isZeroerType):
		return isZeroCustom
	case reflect.PointerTo(t).Implements(isZeroerType):
		return isZeroAddrCustom
	default:
		return isZeroDefault
	}
}

// isZeroInterfaceCustom returns true for nil or pointer-to-nil values,
// and delegates to the custom IsZero() implementation otherwise.
func isZeroInterfaceCustom(_ *encMode, v reflect.Value) (bool, error) {
	kind := v.Kind()

	switch kind {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Interface, reflect.Slice:
		if v.IsNil() {
			return true, nil
		}
	}

	switch kind {
	case reflect.Interface, reflect.Pointer:
		if elem := v.Elem(); elem.Kind() == reflect.Pointer && elem.IsNil() {
			return true, nil
		}
	}

	return v.Interface().(isZeroer).IsZero(), nil
}

// isZeroPointerCustom returns true for nil values,
// and delegates to the custom IsZero() implementation otherwise.
func isZeroPointerCustom(_ *encMode, v reflect.Value) (bool, error) {
	if v.IsNil() {
		return true, nil
	}
	return v.Interface().(isZeroer).IsZero(), nil
}

// isZeroCustom delegates to the custom IsZero() implementation.
func isZeroCustom(_ *encMode, v reflect.Value) (bool, error) {
	zv := v
	if zv.CanAddr() {
		zv = zv.Addr()
	}
	return zv.Interface().(isZeroer).IsZero(), nil
}

// isZeroAddrCustom delegates to the custom IsZero() implementation of the addr of the value.
func isZeroAddrCustom(_ *encMode, v reflect.Value) (bool, error) {
	if !v.CanAddr() {
		// Temporarily box v so we can take the address.
		v2 := reflect.New(v.Type()).Elem()
		v2.Set(v)
		v = v2
	}
	return v.Addr().Interface().(isZeroer).IsZero(), nil
}

// isZeroDefault calls reflect.Value#IsZero()
func isZeroDefault(_ *encMode, v reflect.Value) (bool, error) {
	if !v.IsValid() {
		// v is zero value
		return true, nil
	}
	return v.IsZero(), nil
}

// isZeroFieldStruct is used to determine whether to omit toarray structs
func isZeroFieldStruct(_ *encMode, v reflect.Value) (bool, error) {
	structType, err := getEncodingStructType(v.Type())
	if err != nil {
		return false, err
	}
	return len(structType.fields) == 0, nil
}

func izfOrDefault(t reflect.Type, izf isZeroFunc) isZeroFunc {
	if izf == nil {
		return getIsZeroFunc(t)
	}
	return izf
}

func encodeAny(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if v.IsNil() {
		return append(dst, cborNil), nil
	}

	e := v.Elem()

	var handled bool
	var err error
	if tryFastPath(e) {
		dst, handled, err = fastEncode(dst, em, v.Interface())
	}
	if !handled {
		dst, err = encode(dst, em, e)
	}
	return dst, err
}

func encodeSliceAny(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	if v.CanAddr() {
		return appendSliceAnyWithEM(dst, em, *v.Addr().Interface().(*[]any))
	}
	return appendSliceAnyWithEM(dst, em, v.Interface().([]any))
}

func encodeMapAnyAny(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	return appendMapAnyAnyWithEM(dst, em, v.Interface().(map[any]any))
}

func encodeMapStringAny(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	return appendMapStringAnyWithEM(dst, em, v.Interface().(map[string]any))
}

func encodeMapStringString(dst []byte, em *encMode, v reflect.Value) ([]byte, error) {
	return appendMapStringStringWithEM(dst, em, v.Interface().(map[string]string))
}
