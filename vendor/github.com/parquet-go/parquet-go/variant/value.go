package variant

import (
	"math"

	"github.com/google/uuid"
)

// Value represents a variant value. The zero value is Null.
type Value struct {
	primitive PrimitiveType
	basic     BasicType
	scale     byte // for decimal types
	i64       int64
	f64       float64
	str       string
	bytes     []byte
	uuid      uuid.UUID
	decimal16 [16]byte
	object    Object
	array     Array
}

// Object represents a variant object (ordered set of named fields).
type Object struct {
	Fields []Field
}

// Field is a single named field within an Object.
type Field struct {
	Name  string
	Value Value
}

// Array represents a variant array.
type Array struct {
	Elements []Value
}

// Null returns a null variant value.
func Null() Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveNull}
}

// Bool returns a boolean variant value.
func Bool(v bool) Value {
	p := PrimitiveFalse
	if v {
		p = PrimitiveTrue
	}
	return Value{basic: BasicPrimitive, primitive: p}
}

// Int8 returns an int8 variant value.
func Int8(v int8) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveInt8, i64: int64(v)}
}

// Int16 returns an int16 variant value.
func Int16(v int16) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveInt16, i64: int64(v)}
}

// Int32 returns an int32 variant value.
func Int32(v int32) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveInt32, i64: int64(v)}
}

// Int64 returns an int64 variant value.
func Int64(v int64) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveInt64, i64: v}
}

// Float returns a float32 variant value.
func Float(v float32) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveFloat, f64: float64(v)}
}

// Double returns a float64 variant value.
func Double(v float64) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveDouble, f64: v}
}

// String returns a string variant value.
func String(v string) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveString, str: v}
}

// Binary returns a binary variant value.
func Binary(v []byte) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveBinary, bytes: v}
}

// Date returns a date variant value (days since epoch).
func Date(v int32) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveDate, i64: int64(v)}
}

// Timestamp returns a timestamp variant value (microseconds since epoch, UTC).
func Timestamp(v int64) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveTimestamp, i64: v}
}

// TimestampNTZ returns a timestamp without timezone variant value (microseconds since epoch).
func TimestampNTZ(v int64) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveTimestampNTZ, i64: v}
}

// Time returns a time variant value (microseconds since midnight).
func Time(v int64) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveTime, i64: v}
}

// UUID returns a UUID variant value.
func UUID(v uuid.UUID) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveUUID, uuid: v}
}

// Decimal4 returns a 4-byte decimal variant value with the given unscaled value and scale.
func Decimal4(v int32, scale byte) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveDecimal4, i64: int64(v), scale: scale}
}

// Decimal8 returns an 8-byte decimal variant value with the given unscaled value and scale.
func Decimal8(v int64, scale byte) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveDecimal8, i64: v, scale: scale}
}

// Decimal16 returns a 16-byte decimal variant value with the given unscaled value and scale.
func Decimal16(v [16]byte, scale byte) Value {
	return Value{basic: BasicPrimitive, primitive: PrimitiveDecimal16, decimal16: v, scale: scale}
}

// MakeObject creates an object variant value from a slice of fields.
func MakeObject(fields []Field) Value {
	return Value{basic: BasicObject, object: Object{Fields: fields}}
}

// MakeArray creates an array variant value from a slice of values.
func MakeArray(elements []Value) Value {
	return Value{basic: BasicArray, array: Array{Elements: elements}}
}

// Type returns the PrimitiveType of the value. For non-primitive values
// (objects, arrays, short strings), it returns -1.
func (v Value) Type() PrimitiveType {
	return v.primitive
}

// Basic returns the BasicType of the value.
func (v Value) Basic() BasicType {
	return v.basic
}

// IsNull returns true if the value is a null variant.
func (v Value) IsNull() bool {
	return v.basic == BasicPrimitive && v.primitive == PrimitiveNull
}

// BoolValue returns the boolean value. Panics if not a boolean.
func (v Value) BoolValue() bool {
	return v.primitive == PrimitiveTrue
}

// Int returns the integer value as int64. Works for Int8, Int16, Int32, Int64,
// Date, Timestamp, TimestampNTZ, and Time.
func (v Value) Int() int64 {
	return v.i64
}

// FloatValue returns the floating-point value. Works for Float and Double.
func (v Value) FloatValue() float64 {
	return v.f64
}

// isString returns true if the value is any string type (short or primitive).
func (v Value) isString() bool {
	return v.basic == BasicShortString ||
		(v.basic == BasicPrimitive && v.primitive == PrimitiveString)
}

// Str returns the string value.
func (v Value) Str() string {
	return v.str
}

// Bytes returns the binary value.
func (v Value) Bytes() []byte {
	return v.bytes
}

// UUIDValue returns the UUID value.
func (v Value) UUIDValue() uuid.UUID {
	return v.uuid
}

// Scale returns the decimal scale. Only valid for Decimal4, Decimal8, Decimal16.
func (v Value) Scale() byte {
	return v.scale
}

// Decimal16Value returns the 16-byte decimal value.
func (v Value) Decimal16Value() [16]byte {
	return v.decimal16
}

// ObjectValue returns the Object. Panics if not an object.
func (v Value) ObjectValue() Object {
	return v.object
}

// ArrayValue returns the Array. Panics if not an array.
func (v Value) ArrayValue() Array {
	return v.array
}

// GoValue converts the variant value to a natural Go type.
//
// Mapping:
//
//	Null → nil
//	True/False → bool
//	Int8 → int8, Int16 → int16, Int32 → int32, Int64 → int64
//	Float → float32, Double → float64
//	String → string, Binary → []byte
//	Date → int32 (days since epoch)
//	Timestamp/TimestampNTZ → int64 (microseconds since epoch)
//	Time → int64 (microseconds since midnight)
//	UUID → uuid.UUID
//	Decimal4 → int32 (unscaled), Decimal8 → int64 (unscaled), Decimal16 → [16]byte
//	Object → map[string]any
//	Array → []any
func (v Value) GoValue() any {
	switch v.basic {
	case BasicObject:
		m := make(map[string]any, len(v.object.Fields))
		for _, f := range v.object.Fields {
			m[f.Name] = f.Value.GoValue()
		}
		return m
	case BasicArray:
		a := make([]any, len(v.array.Elements))
		for i, e := range v.array.Elements {
			a[i] = e.GoValue()
		}
		return a
	case BasicShortString:
		return v.str
	}
	// BasicPrimitive
	switch v.primitive {
	case PrimitiveNull:
		return nil
	case PrimitiveTrue:
		return true
	case PrimitiveFalse:
		return false
	case PrimitiveInt8:
		return int8(v.i64)
	case PrimitiveInt16:
		return int16(v.i64)
	case PrimitiveInt32:
		return int32(v.i64)
	case PrimitiveInt64:
		return v.i64
	case PrimitiveFloat:
		return float32(v.f64)
	case PrimitiveDouble:
		return v.f64
	case PrimitiveString:
		return v.str
	case PrimitiveBinary:
		return v.bytes
	case PrimitiveDate:
		return int32(v.i64)
	case PrimitiveTimestamp, PrimitiveTimestampNTZ, PrimitiveTime:
		return v.i64
	case PrimitiveUUID:
		return v.uuid
	case PrimitiveDecimal4:
		return int32(v.i64)
	case PrimitiveDecimal8:
		return v.i64
	case PrimitiveDecimal16:
		return v.decimal16
	default:
		return nil
	}
}

// Equal reports whether two variant values are deeply equal.
// Short strings and primitive strings with the same content are considered equal.
func (v Value) Equal(other Value) bool {
	// Normalize: treat short strings and primitive strings as equivalent
	if v.isString() && other.isString() {
		return v.str == other.str
	}
	if v.basic != other.basic {
		return false
	}
	switch v.basic {
	case BasicObject:
		if len(v.object.Fields) != len(other.object.Fields) {
			return false
		}
		for i, f := range v.object.Fields {
			of := other.object.Fields[i]
			if f.Name != of.Name || !f.Value.Equal(of.Value) {
				return false
			}
		}
		return true
	case BasicArray:
		if len(v.array.Elements) != len(other.array.Elements) {
			return false
		}
		for i, e := range v.array.Elements {
			if !e.Equal(other.array.Elements[i]) {
				return false
			}
		}
		return true
	case BasicShortString:
		return v.str == other.str
	}
	if v.primitive != other.primitive {
		return false
	}
	switch v.primitive {
	case PrimitiveNull, PrimitiveTrue, PrimitiveFalse:
		return true
	case PrimitiveFloat:
		return math.Float32bits(float32(v.f64)) == math.Float32bits(float32(other.f64))
	case PrimitiveDouble:
		return math.Float64bits(v.f64) == math.Float64bits(other.f64)
	case PrimitiveString:
		return v.str == other.str
	case PrimitiveBinary:
		if len(v.bytes) != len(other.bytes) {
			return false
		}
		for i := range v.bytes {
			if v.bytes[i] != other.bytes[i] {
				return false
			}
		}
		return true
	case PrimitiveUUID:
		return v.uuid == other.uuid
	case PrimitiveDecimal4, PrimitiveDecimal8:
		return v.i64 == other.i64 && v.scale == other.scale
	case PrimitiveDecimal16:
		return v.decimal16 == other.decimal16 && v.scale == other.scale
	default:
		return v.i64 == other.i64
	}
}
