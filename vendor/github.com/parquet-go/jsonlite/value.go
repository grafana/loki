package jsonlite

import (
	"encoding/json"
	"fmt"
	"hash/maphash"
	"strconv"
	"strings"
	"unsafe"
)

const (
	// kindShift is calculated based on pointer size to use the high bits
	// for the kind field. We have 7 Kind values (0-6), requiring 3 bits.
	// On 64-bit systems this is 61 (top 3 bits for kind, bottom 61 for length),
	// on 32-bit systems this is 29 (top 3 bits for kind, bottom 29 for length).
	kindShift = (unsafe.Sizeof(uintptr(0))*8 - 3)
	kindMask  = (1 << kindShift) - 1
	// unparsedBit is set for objects/arrays that haven't been parsed yet (lazy parsing).
	// On 64-bit systems this is bit 60, on 32-bit systems this is bit 28.
	unparsedBit = uintptr(1) << (kindShift - 1)
)

var (
	// hashseed is the seed used for hashing object keys.
	hashseed = maphash.MakeSeed()
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
//
// Value instances as immutable, they can be safely accessed from multiple
// goroutines.
//
// The zero-value of Value is invalid, all Value instances must be acquired
// form Parse or from an Iterator.
type Value struct {
	p unsafe.Pointer
	n uintptr
}

type field struct {
	k string
	v Value
}

// Kind returns the type of the JSON value.
func (v *Value) Kind() Kind { return Kind(v.n >> kindShift) }

// Len returns the length of the value.
// For strings, it returns the number of bytes.
// For arrays, it returns the number of elements.
// For objects, it returns the number of fields.
// Panics if called on other types.
func (v *Value) Len() int {
	switch v.Kind() {
	case String:
		// String values now store quoted JSON - subtract 2 for quotes
		return int(v.n&kindMask) - 2
	case Number:
		return int(v.n & kindMask)
	case Array, Object:
		parsed := v
		if v.unparsed() {
			parsed = v.parse()
		}
		// First element/field is always cached JSON
		return int(parsed.n&kindMask) - 1
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
	i, err := strconv.ParseInt(v.json(), 10, 64)
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
	u, err := strconv.ParseUint(v.json(), 10, 64)
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
	f, err := strconv.ParseFloat(v.json(), 64)
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
	case Null:
		return "<nil>"
	case String:
		s, _ := Unquote(v.json())
		return s
	case Number, True, False:
		return v.json()
	case Array:
		return (*Value)(v.p).json()
	default:
		if v.unparsed() {
			return v.json()
		}
		return (*field)(v.p).v.json()
	}
}

// JSON returns the JSON representation of the value.
func (v *Value) JSON() string {
	switch v.Kind() {
	case String, Number, Null, True, False:
		return v.json()
	case Array:
		return (*Value)(v.p).json()
	default:
		if v.unparsed() {
			return v.json()
		}
		return (*field)(v.p).v.json()
	}
}

// Array iterates over the array elements.
// Panics if the value is not an array.
func (v *Value) Array(yield func(*Value) bool) {
	if v.Kind() != Array {
		panic("jsonlite: Array called on non-array value")
	}
	parsed := v
	if v.unparsed() {
		parsed = v.parse()
	}
	elems := unsafe.Slice((*Value)(parsed.p), parsed.len())[1:]
	for i := range elems {
		if !yield(&elems[i]) {
			return
		}
	}
}

// Object iterates over the object's key/value pairs.
// Panics if the value is not an object.
func (v *Value) Object(yield func(string, *Value) bool) {
	if v.Kind() != Object {
		panic("jsonlite: Object called on non-object value")
	}
	parsed := v
	if v.unparsed() {
		parsed = v.parse()
	}
	fields := unsafe.Slice((*field)(parsed.p), parsed.len())[1:]
	for i := range fields {
		if !yield(fields[i].k, &fields[i].v) {
			return
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
	parsed := v
	if v.unparsed() {
		parsed = v.parse()
	}
	fields := unsafe.Slice((*field)(parsed.p), parsed.len())
	hashes := fields[0].k
	refkey := byte(maphash.String(hashseed, k))
	offset := 0
	for {
		i := strings.IndexByte(hashes[offset:], refkey)
		if i < 0 {
			return nil
		}
		j := offset + i + 1
		f := &fields[j]
		if f.k == k {
			return &f.v
		}
		offset = j
	}
}

// LookupPath searches for a nested field by following a path of keys.
// Returns nil if any key in the path is not found.
// Panics if any intermediate value is not an object.
// If path is empty, returns the value itself.
func (v *Value) LookupPath(path ...string) *Value {
	for _, key := range path {
		if v == nil {
			return nil
		}
		v = v.Lookup(key)
	}
	return v
}

// Index returns the value at index i in an array.
// Panics if the value is not an array or if the index is out of range.
func (v *Value) Index(i int) *Value {
	if v.Kind() != Array {
		panic("jsonlite: Index called on non-array value")
	}
	parsed := v
	if v.unparsed() {
		parsed = v.parse()
	}
	elems := unsafe.Slice((*Value)(parsed.p), parsed.len())[1:]
	return &elems[i]
}

// NumberType returns the classification of the number (int, uint, or float).
// Panics if the value is not a number.
func (v *Value) NumberType() NumberType {
	if v.Kind() != Number {
		panic("jsonlite: NumberType called on non-number value")
	}
	return NumberTypeOf(v.json())
}

// Number returns the value as a json.Number.
// Panics if the value is not a number.
func (v *Value) Number() json.Number {
	if v.Kind() != Number {
		panic("jsonlite: Number called on non-number value")
	}
	return json.Number(v.json())
}

func (v *Value) json() string {
	return unsafe.String((*byte)(v.p), v.len())
}

func (v *Value) len() int {
	return int(v.n & (kindMask &^ unparsedBit))
}

func (v *Value) unparsed() bool {
	return (v.n & unparsedBit) != 0
}

func (v *Value) parse() *Value {
	parsed, err := Parse(v.JSON())
	if err != nil {
		panic(fmt.Errorf("jsonlite: lazy parse failed: %w", err))
	}
	return parsed
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

func makeValue(k Kind, s string) Value {
	return Value{
		p: unsafe.Pointer(unsafe.StringData(s)),
		n: (uintptr(k) << kindShift) | uintptr(len(s)),
	}
}

func makeNullValue(s string) Value { return makeValue(Null, s) }

func makeTrueValue(s string) Value { return makeValue(True, s) }

func makeFalseValue(s string) Value { return makeValue(False, s) }

func makeNumberValue(s string) Value { return makeValue(Number, s) }

func makeStringValue(s string) Value { return makeValue(String, s) }

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

func makeUnparsedObjectValue(json string) Value {
	return Value{
		p: unsafe.Pointer(unsafe.StringData(json)),
		n: (uintptr(Object) << kindShift) | uintptr(len(json)) | unparsedBit,
	}
}

// Append serializes the Value to JSON and appends it to the buffer.
// Returns the extended buffer.
func (v *Value) Append(buf []byte) []byte { return append(buf, v.JSON()...) }

// Compact appends a compacted JSON representation of the value to buf by recursively
// reconstructing it from the parsed structure. Unlike Append, this method does not
// use cached JSON and always regenerates the output.
func (v *Value) Compact(buf []byte) []byte {
	switch v.Kind() {
	case String, Null, True, False, Number:
		return append(buf, v.json()...)
	case Array:
		parsed := v
		if v.unparsed() {
			parsed = v.parse()
		}
		buf = append(buf, '[')
		var count int
		for elem := range parsed.Array {
			if count > 0 {
				buf = append(buf, ',')
			}
			buf = elem.Compact(buf)
			count++
		}
		return append(buf, ']')
	default:
		parsed := v
		if v.unparsed() {
			parsed = v.parse()
		}
		buf = append(buf, '{')
		var count int
		for k, v := range parsed.Object {
			if count > 0 {
				buf = append(buf, ',')
			}
			buf = AppendQuote(buf, k)
			buf = append(buf, ':')
			buf = v.Compact(buf)
			count++
		}
		return append(buf, '}')
	}
}
