package json

import "github.com/goccy/go-json/internal/options"

// Options are the options of github.com/goccy/go-json/v2 and github.com/goccy/go-json/jsontext, which
// DefaultOptionsV1 and the options of the semantics of this package below, the ones of encoding/json, such as
// FormatDurationAsNano, make, as encoding/json makes them for encoding/json/v2: each of them makes one behavior of the
// v2 semantics the one of this package, alone or with the others, and DefaultOptionsV1 makes them all the ones of this
// package. They are not EncodeOptionFunc and DecodeOptionFunc, which are the options of the functions of this
// package. It is the same type as the Options of those packages.
//
//	jsonv2.Marshal(v, json.DefaultOptionsV1())         // as json.Marshal(v)
//	jsonv2.Unmarshal(b, &v, json.FormatDurationAsNano(true))
type Options = options.Options

// DefaultOptionsV1 sets every option whose default differs between this package and github.com/goccy/go-json/v2 to
// the default of this package: AllowDuplicateNames, AllowInvalidUTF8, EscapeForHTML, EscapeForJS and
// PreserveRawStrings of jsontext, Deterministic, FormatNilMapAsNull, FormatNilSliceAsNull and
// MatchCaseInsensitiveNames of github.com/goccy/go-json/v2, and CallMethodsWithLegacySemantics,
// FormatByteArrayAsArray, FormatBytesWithLegacySemantics, FormatDurationAsNano, MatchCaseSensitiveDelimiter,
// MergeWithLegacySemantics, OmitEmptyWithLegacySemantics, ParseBytesWithLooseRFC4648, ParseTimeWithLooseRFC3339,
// ReportErrorsWithLegacySemantics, StringifyWithLegacySemantics and UnmarshalArrayFromAnyLength of this package,
// all true. Other options are not set.
//
// Marshal and Unmarshal of github.com/goccy/go-json/v2 with it marshal and unmarshal as Marshal and Unmarshal of
// encoding/json of Go 1.27 do.
func DefaultOptionsV1() Options {
	return defaultOptionsV1
}

var defaultOptionsV1 = &options.Config{Set: options.DefaultV1Flags, Value: options.DefaultV1Flags}

// CallMethodsWithLegacySemantics specifies that the methods of marshaling and unmarshaling are called as this
// package calls them:
//
//   - When marshaling, a method of a pointer receiver is called only for a value which is addressable: a value
//     which a pointer points to, an element of a slice, and a field of a struct or an element of an array which is
//     addressable. A value held by an interface, a key or a value of a map, and a value given by itself are not.
//   - A key of a map is not marshaled or unmarshaled by MarshalJSON, MarshalJSONTo, UnmarshalJSON or
//     UnmarshalJSONFrom, but still by MarshalText, AppendText and UnmarshalText, of which a key of a string kind
//     calls none when marshaling.
//   - When marshaling, a key of a map which is a nil pointer with MarshalText or AppendText is the name "".
//   - When marshaling, a value of an interface type with a method of marshaling which holds a nil pointer is
//     marshaled by the method, not as null.
//
// This affects marshaling and unmarshaling. It is true by default in this package.
func CallMethodsWithLegacySemantics(v bool) Options {
	return options.BoolOf(options.CallMethodsWithLegacySemantics, v)
}

// FormatByteArrayAsArray specifies that a Go [N]byte is a JSON array of numbers, as any other array, rather than a
// JSON string of the base64 of its bytes.
//
// This affects marshaling and unmarshaling. It is true by default in this package.
func FormatByteArrayAsArray(v bool) Options { return options.BoolOf(options.FormatByteArrayAsArray, v) }

// FormatBytesWithLegacySemantics specifies that the slices and the arrays of bytes are handled as this package
// handles them:
//
//   - A slice or an array of a named type of bytes is a JSON string of the base64 of its bytes, as a []byte is,
//     rather than a JSON array of its elements; but it is still a JSON array of its elements when marshaling if the
//     type of the bytes has a method of marshaling, and an array is one with FormatByteArrayAsArray.
//   - When unmarshaling, a JSON array is read into a slice or an array of bytes element by element, as into any
//     other slice or array, where it is an error by default.
//
// This affects marshaling and unmarshaling. It is true by default in this package.
func FormatBytesWithLegacySemantics(v bool) Options {
	return options.BoolOf(options.FormatBytesWithLegacySemantics, v)
}

// FormatDurationAsNano specifies that a time.Duration is a JSON number of its nanoseconds, rather than an error.
//
// This affects marshaling and unmarshaling. It is true by default in this package.
func FormatDurationAsNano(v bool) Options { return options.BoolOf(options.FormatDurationAsNano, v) }

// MatchCaseSensitiveDelimiter specifies that the names which are matched case-insensitively, by
// MatchCaseInsensitiveNames of github.com/goccy/go-json/v2 or the case:ignore option of a field, are matched by
// strings.EqualFold: an underscore or a dash is not ignored.
//
// This affects marshaling and unmarshaling. It is true by default in this package.
func MatchCaseSensitiveDelimiter(v bool) Options {
	return options.BoolOf(options.MatchCaseSensitiveDelimiter, v)
}

// MergeWithLegacySemantics specifies that unmarshaling into a Go value which is not zero merges into it as this
// package does:
//
//   - A JSON null leaves a bool, a number, a string, an array, a struct and a value with UnmarshalText as they are,
//     and sets any other value to zero.
//   - A JSON value other than null is read into the value which the element of an array or a slice, even past the
//     length of the slice, a field of a struct, a pointer, or a non-nil pointer held by an interface has, rather
//     than into a zero value; a value of a map is replaced. A slice is cut at the length of the JSON array.
//
// This affects unmarshaling only. It is true by default in this package.
func MergeWithLegacySemantics(v bool) Options {
	return options.BoolOf(options.MergeWithLegacySemantics, v)
}

// OmitEmptyWithLegacySemantics specifies that the omitempty option of a field omits it if its Go value is false,
// 0, a nil pointer, a nil interface value, or an empty array, slice, map or string, rather than if it is marshaled
// as a JSON null or an empty JSON string, array or object.
//
// This affects marshaling only. It is true by default in this package.
func OmitEmptyWithLegacySemantics(v bool) Options {
	return options.BoolOf(options.OmitEmptyWithLegacySemantics, v)
}

// ParseBytesWithLooseRFC4648 specifies that the base64 of a JSON string read into bytes may have carriage returns
// and line feeds, which are skipped.
//
// This affects unmarshaling only. It is true by default in this package.
func ParseBytesWithLooseRFC4648(v bool) Options {
	return options.BoolOf(options.ParseBytesWithLooseRFC4648, v)
}

// ParseTimeWithLooseRFC3339 specifies that a time.Time may be read from a JSON string which RFC 3339 doesn't allow
// in these ways: an hour of one digit, a comma before the fraction of a second, and a timezone offset whose hour is
// 24 or whose minute is 60.
//
// This affects unmarshaling only. It is true by default in this package.
func ParseTimeWithLooseRFC3339(v bool) Options {
	return options.BoolOf(options.ParseTimeWithLooseRFC3339, v)
}

// ReportErrorsWithLegacySemantics specifies that the errors are reported as this package reports them:
//
//   - The errors are usually of the types of this package, such as SyntaxError, MarshalerError, UnsupportedTypeError,
//     UnsupportedValueError, InvalidUnmarshalError and UnmarshalTypeError, rather than SemanticError of
//     github.com/goccy/go-json/v2 and SyntacticError of github.com/goccy/go-json/jsontext.
//   - The error of a method of marshaling is wrapped in a MarshalerError, and the one of a method of unmarshaling
//     is returned as it is.
//   - A struct type with an error, such as two fields of the same name or a wrong tag, is handled as well as it
//     can be, rather than as an error.
//   - A name which matches no field exactly and more than one field case-insensitively, by
//     MatchCaseInsensitiveNames of github.com/goccy/go-json/v2 or the case:ignore option of a field, is read into
//     the first of them, rather than being an error.
//   - The whole JSON value is checked before it is unmarshaled, so that an input with a syntax error changes
//     nothing, and an error of a value doesn't stop the unmarshaling: the first such error is reported at the
//     end.
//
// This affects marshaling and unmarshaling. It is true by default in this package.
func ReportErrorsWithLegacySemantics(v bool) Options {
	return options.BoolOf(options.ReportErrorsWithLegacySemantics, v)
}

// StringifyWithLegacySemantics specifies that the string option of a field applies as in this package:
//
//   - It applies to a bool and a string too, and to a pointer to one, whose JSON value is in a JSON string.
//   - When unmarshaling, a number in a JSON string is read by the syntax of Go numbers, of which the JSON numbers
//     are a part, and a JSON string of null is a JSON null.
//
// If ReportErrorsWithLegacySemantics is false, the string option of a value which it doesn't apply to is an error,
// rather than ignored.
//
// This affects marshaling and unmarshaling. It is true by default in this package.
func StringifyWithLegacySemantics(v bool) Options {
	return options.BoolOf(options.StringifyWithLegacySemantics, v)
}

// UnmarshalArrayFromAnyLength specifies that a Go array is read from a JSON array of any length: the elements past
// the length of the Go array are skipped, and the ones which the JSON array doesn't have are zero, rather than an
// error.
//
// This affects unmarshaling only. It is true by default in this package.
func UnmarshalArrayFromAnyLength(v bool) Options {
	return options.BoolOf(options.UnmarshalArrayFromAnyLength, v)
}
