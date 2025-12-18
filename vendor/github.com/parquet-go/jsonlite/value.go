package jsonlite

import (
	"encoding/json"
	"iter"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

const (
	// kindShift is calculated based on pointer size to use the high bits
	// for the kind field. We have 7 Kind values (0-6), requiring 3 bits.
	// On 64-bit systems this is 61 (top 3 bits for kind, bottom 61 for length),
	// on 32-bit systems this is 29 (top 3 bits for kind, bottom 29 for length).
	kindShift = (unsafe.Sizeof(uintptr(0))*8 - 3)
	kindMask  = (1 << kindShift) - 1
)

// Kind represents the type of a JSON value.
type Kind int

const (
	// Null represents a JSON null value.
	Null Kind = iota
	// True represents a JSON true boolean value.
	True
	// False represents a JSON false boolean value.
	False
	// Number represents a JSON number value.
	Number
	// String represents a JSON string value.
	String
	// Object represents a JSON object value.
	Object
	// Array represents a JSON array value.
	Array
)

// Value represents a JSON value of any type.
type Value struct {
	p unsafe.Pointer
	n uintptr
}

type field struct {
	k string
	v Value
}

// Kind returns the type of the JSON value.
func (v *Value) Kind() Kind {
	return Kind(v.n >> kindShift)
}

// Len returns the length of the value.
// For strings, it returns the number of bytes.
// For arrays, it returns the number of elements.
// For objects, it returns the number of fields.
// Panics if called on other types.
func (v *Value) Len() int {
	switch v.Kind() {
	case String, Number, Array, Object:
		return int(v.n & kindMask)
	default:
		panic("jsonlite: Len called on non-string/array/object value")
	}
}

// Int returns the value as a signed 64-bit integer.
// Panics if the value is not a number or if parsing fails.
func (v *Value) Int() int64 {
	if v.Kind() != Number {
		panic("jsonlite: Int called on non-number value")
	}
	i, err := strconv.ParseInt(v.raw(), 10, 64)
	if err != nil {
		panic(err)
	}
	return i
}

// Uint returns the value as an unsigned 64-bit integer.
// Panics if the value is not a number or if parsing fails.
func (v *Value) Uint() uint64 {
	if v.Kind() != Number {
		panic("jsonlite: Uint called on non-number value")
	}
	u, err := strconv.ParseUint(v.raw(), 10, 64)
	if err != nil {
		panic(err)
	}
	return u
}

// Float returns the value as a 64-bit floating point number.
// Panics if the value is not a number or if parsing fails.
func (v *Value) Float() float64 {
	if v.Kind() != Number {
		panic("jsonlite: Float called on non-number value")
	}
	f, err := strconv.ParseFloat(v.raw(), 64)
	if err != nil {
		panic(err)
	}
	return f
}

// String returns the value as a string.
// For string and number values, returns the raw value.
// For other types, returns the JSON representation.
func (v *Value) String() string {
	switch v.Kind() {
	case String, Number:
		return v.raw()
	case Null:
		return "null"
	case True:
		return "true"
	case False:
		return "false"
	default:
		return string(v.Append(nil))
	}
}

// Array returns the value as a slice of Values.
// Panics if the value is not an array.
func (v *Value) Array() iter.Seq[*Value] {
	if v.Kind() != Array {
		panic("jsonlite: Array called on non-array value")
	}
	return func(yield func(*Value) bool) {
		elems := unsafe.Slice((*Value)(v.p), v.len())
		for i := range elems {
			if !yield(&elems[i]) {
				return
			}
		}
	}
}

// Object returns the value as a slice of key/value pairs.
// Panics if the value is not an object.
func (v *Value) Object() iter.Seq2[string, *Value] {
	if v.Kind() != Object {
		panic("jsonlite: Object called on non-object value")
	}
	return func(yield func(string, *Value) bool) {
		fields := unsafe.Slice((*field)(v.p), v.len())
		for i := range fields {
			if !yield(fields[i].k, &fields[i].v) {
				return
			}
		}
	}
}

// Lookup searches for a field by key in an object and returns a pointer to its value.
// Returns nil if the key is not found.
// Panics if the value is not an object.
func (v *Value) Lookup(k string) *Value {
	if v.Kind() != Object {
		panic("jsonlite: Lookup called on non-object value")
	}
	fields := unsafe.Slice((*field)(v.p), v.len())
	i, ok := slices.BinarySearchFunc(fields, k, func(a field, b string) int {
		return strings.Compare(a.k, b)
	})
	if ok {
		return &fields[i].v
	}
	return nil
}

// NumberType returns the classification of the number (int, uint, or float).
// Panics if the value is not a number.
func (v *Value) NumberType() NumberType {
	if v.Kind() != Number {
		panic("jsonlite: NumberType called on non-number value")
	}
	return NumberTypeOf(v.raw())
}

// Number returns the value as a json.Number.
// Panics if the value is not a number.
func (v *Value) Number() json.Number {
	if v.Kind() != Number {
		panic("jsonlite: Number called on non-number value")
	}
	return json.Number(v.raw())
}

// raw returns the underlying string data without type checking.
// This is used internally by methods that have already verified the type.
func (v *Value) raw() string {
	return unsafe.String((*byte)(v.p), v.len())
}

// len returns the length stored in the value without type checking.
func (v *Value) len() int {
	return int(v.n & kindMask)
}

// NumberType represents the classification of a JSON number.
type NumberType int

const (
	// Int indicates a signed integer number (has a leading minus sign, no decimal point or exponent).
	Int NumberType = iota
	// Uint indicates an unsigned integer number (no minus sign, no decimal point or exponent).
	Uint
	// Float indicates a floating point number (has decimal point or exponent).
	Float
)

// NumberTypeOf returns the classification of a number string.
func NumberTypeOf(s string) NumberType {
	if len(s) == 0 {
		return Float
	}
	t := Uint
	if s[0] == '-' {
		s = s[1:]
		t = Int
	}
	for i := range len(s) {
		switch s[i] {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			continue
		default:
			return Float
		}
	}
	return t
}

func makeNullValue() Value {
	return Value{n: uintptr(Null) << kindShift}
}

func makeTrueValue() Value {
	return Value{n: uintptr(True)<<kindShift | 1}
}

func makeFalseValue() Value {
	return Value{n: uintptr(False)<<kindShift | 0}
}

func makeNumberValue(s string) Value {
	return Value{
		p: unsafe.Pointer(unsafe.StringData(s)),
		n: (uintptr(Number) << kindShift) | uintptr(len(s)),
	}
}

func makeStringValue(s string) Value {
	return Value{
		p: unsafe.Pointer(unsafe.StringData(s)),
		n: (uintptr(String) << kindShift) | uintptr(len(s)),
	}
}

func makeArrayValue(elements []Value) Value {
	return Value{
		p: unsafe.Pointer(unsafe.SliceData(elements)),
		n: (uintptr(Array) << kindShift) | uintptr(len(elements)),
	}
}

func makeObjectValue(fields []field) Value {
	return Value{
		p: unsafe.Pointer(unsafe.SliceData(fields)),
		n: (uintptr(Object) << kindShift) | uintptr(len(fields)),
	}
}

// AsBool coerces the value to a boolean.
// Returns false for nil, Null, False, zero numbers, and empty strings/objects/arrays.
// Returns true for True, non-zero numbers, and non-empty strings/objects/arrays.
func AsBool(v *Value) bool {
	if v != nil {
		switch v.Kind() {
		case True:
			return true
		case Number:
			f, err := strconv.ParseFloat(v.raw(), 64)
			return err == nil && f != 0
		case String, Object, Array:
			return v.len() > 0
		}
	}
	return false
}

// AsString coerces the value to a string.
// Returns "" for nil and Null.
// Returns "true"/"false" for booleans.
// Returns the raw value for numbers and strings.
// Returns the JSON representation for objects and arrays.
func AsString(v *Value) string {
	if v != nil {
		switch v.Kind() {
		case True:
			return "true"
		case False:
			return "false"
		case Number, String:
			return v.raw()
		case Object, Array:
			return string(v.Append(nil))
		}
	}
	return ""
}

// AsInt coerces the value to a signed 64-bit integer.
// Returns 0 for nil, Null, False, objects, and arrays.
// Returns 1 for True.
// Parses numbers and strings, truncating floats. Returns 0 on parse failure.
func AsInt(v *Value) int64 {
	if v != nil {
		switch v.Kind() {
		case True:
			return 1
		case Number, String:
			if i, err := strconv.ParseInt(v.raw(), 10, 64); err == nil {
				return i
			}
			if f, err := strconv.ParseFloat(v.raw(), 64); err == nil {
				return int64(f)
			}
		}
	}
	return 0
}

// AsUint coerces the value to an unsigned 64-bit integer.
// Returns 0 for nil, Null, False, objects, arrays, and negative numbers.
// Returns 1 for True.
// Parses numbers and strings, truncating floats. Returns 0 on parse failure.
func AsUint(v *Value) uint64 {
	if v != nil {
		switch v.Kind() {
		case True:
			return 1
		case Number, String:
			if u, err := strconv.ParseUint(v.raw(), 10, 64); err == nil {
				return u
			}
			if f, err := strconv.ParseFloat(v.raw(), 64); err == nil {
				if f >= 0 {
					return uint64(f)
				}
			}
		}
	}
	return 0
}

// AsFloat coerces the value to a 64-bit floating point number.
// Returns 0 for nil, Null, False, objects, and arrays.
// Returns 1 for True.
// Parses numbers and strings. Returns 0 on parse failure.
func AsFloat(v *Value) float64 {
	if v != nil {
		switch v.Kind() {
		case True:
			return 1
		case Number, String:
			if f, err := strconv.ParseFloat(v.raw(), 64); err == nil {
				return f
			}
		}
	}
	return 0
}

// AsDuration coerces the value to a time.Duration.
// Returns 0 for nil, Null, False, objects, and arrays.
// For strings, parses using time.ParseDuration.
// For numbers, interprets the value as seconds.
func AsDuration(v *Value) time.Duration {
	if v != nil {
		switch v.Kind() {
		case True:
			return time.Second
		case Number:
			if f, err := strconv.ParseFloat(v.raw(), 64); err == nil {
				return time.Duration(f * float64(time.Second))
			}
		case String:
			if d, err := time.ParseDuration(v.raw()); err == nil {
				return d
			}
		}
	}
	return 0
}

// AsTime coerces the value to a time.Time.
// Returns the zero time for nil, Null, False, objects, and arrays.
// For strings, parses using RFC3339 format in UTC.
// For numbers, interprets the value as seconds since Unix epoch.
func AsTime(v *Value) time.Time {
	if v != nil {
		switch v.Kind() {
		case Number:
			if f, err := strconv.ParseFloat(v.raw(), 64); err == nil {
				sec, frac := math.Modf(f)
				return time.Unix(int64(sec), int64(frac*1e9)).UTC()
			}
		case String:
			if t, err := time.ParseInLocation(time.RFC3339, v.raw(), time.UTC); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// Append serializes the Value to JSON and appends it to the buffer.
// Returns the extended buffer.
func (v *Value) Append(buf []byte) []byte {
	switch v.Kind() {
	case Null:
		return append(buf, "null"...)

	case True:
		return append(buf, "true"...)

	case False:
		return append(buf, "false"...)

	case Number:
		return append(buf, v.raw()...)

	case String:
		return AppendQuote(buf, v.String())

	case Array:
		buf = append(buf, '[')
		var count int
		for elem := range v.Array() {
			if count > 0 {
				buf = append(buf, ',')
			}
			buf = elem.Append(buf)
			count++
		}
		return append(buf, ']')

	case Object:
		buf = append(buf, '{')
		var count int
		for k, v := range v.Object() {
			if count > 0 {
				buf = append(buf, ',')
			}
			buf = AppendQuote(buf, k)
			buf = append(buf, ':')
			buf = v.Append(buf)
			count++
		}
		return append(buf, '}')

	default:
		return buf
	}
}
