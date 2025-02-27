package parquet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/bits"
	"reflect"
	"time"
	"unsafe"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// Kind is an enumeration type representing the physical types supported by the
// parquet type system.
type Kind int8

const (
	Boolean           Kind = Kind(format.Boolean)
	Int32             Kind = Kind(format.Int32)
	Int64             Kind = Kind(format.Int64)
	Int96             Kind = Kind(format.Int96)
	Float             Kind = Kind(format.Float)
	Double            Kind = Kind(format.Double)
	ByteArray         Kind = Kind(format.ByteArray)
	FixedLenByteArray Kind = Kind(format.FixedLenByteArray)
)

// String returns a human-readable representation of the physical type.
func (k Kind) String() string { return format.Type(k).String() }

// Value constructs a value from k and v.
//
// The method panics if the data is not a valid representation of the value
// kind; for example, if the kind is Int32 but the data is not 4 bytes long.
func (k Kind) Value(v []byte) Value {
	x, err := parseValue(k, v)
	if err != nil {
		panic(err)
	}
	return x
}

// The Type interface represents logical types of the parquet type system.
//
// Types are immutable and therefore safe to access from multiple goroutines.
type Type interface {
	// Returns a human-readable representation of the parquet type.
	String() string

	// Returns the Kind value representing the underlying physical type.
	//
	// The method panics if it is called on a group type.
	Kind() Kind

	// For integer and floating point physical types, the method returns the
	// size of values in bits.
	//
	// For fixed-length byte arrays, the method returns the size of elements
	// in bytes.
	//
	// For other types, the value is zero.
	Length() int

	// Returns an estimation of the number of bytes required to hold the given
	// number of values of this type in memory.
	//
	// The method returns zero for group types.
	EstimateSize(numValues int) int

	// Returns an estimation of the number of values of this type that can be
	// held in the given byte size.
	//
	// The method returns zero for group types.
	EstimateNumValues(size int) int

	// Compares two values and returns a negative integer if a < b, positive if
	// a > b, or zero if a == b.
	//
	// The values' Kind must match the type, otherwise the result is undefined.
	//
	// The method panics if it is called on a group type.
	Compare(a, b Value) int

	// ColumnOrder returns the type's column order. For group types, this method
	// returns nil.
	//
	// The order describes the comparison logic implemented by the Less method.
	//
	// As an optimization, the method may return the same pointer across
	// multiple calls. Applications must treat the returned value as immutable,
	// mutating the value will result in undefined behavior.
	ColumnOrder() *format.ColumnOrder

	// Returns the physical type as a *format.Type value. For group types, this
	// method returns nil.
	//
	// As an optimization, the method may return the same pointer across
	// multiple calls. Applications must treat the returned value as immutable,
	// mutating the value will result in undefined behavior.
	PhysicalType() *format.Type

	// Returns the logical type as a *format.LogicalType value. When the logical
	// type is unknown, the method returns nil.
	//
	// As an optimization, the method may return the same pointer across
	// multiple calls. Applications must treat the returned value as immutable,
	// mutating the value will result in undefined behavior.
	LogicalType() *format.LogicalType

	// Returns the logical type's equivalent converted type. When there are
	// no equivalent converted type, the method returns nil.
	//
	// As an optimization, the method may return the same pointer across
	// multiple calls. Applications must treat the returned value as immutable,
	// mutating the value will result in undefined behavior.
	ConvertedType() *deprecated.ConvertedType

	// Creates a column indexer for values of this type.
	//
	// The size limit is a hint to the column indexer that it is allowed to
	// truncate the page boundaries to the given size. Only BYTE_ARRAY and
	// FIXED_LEN_BYTE_ARRAY types currently take this value into account.
	//
	// A value of zero or less means no limits.
	//
	// The method panics if it is called on a group type.
	NewColumnIndexer(sizeLimit int) ColumnIndexer

	// Creates a row group buffer column for values of this type.
	//
	// Column buffers are created using the index of the column they are
	// accumulating values in memory for (relative to the parent schema),
	// and the size of their memory buffer.
	//
	// The application may give an estimate of the number of values it expects
	// to write to the buffer as second argument. This estimate helps set the
	// initialize buffer capacity but is not a hard limit, the underlying memory
	// buffer will grown as needed to allow more values to be written. Programs
	// may use the Size method of the column buffer (or the parent row group,
	// when relevant) to determine how many bytes are being used, and perform a
	// flush of the buffers to a storage layer.
	//
	// The method panics if it is called on a group type.
	NewColumnBuffer(columnIndex, numValues int) ColumnBuffer

	// Creates a dictionary holding values of this type.
	//
	// The dictionary retains the data buffer, it does not make a copy of it.
	// If the application needs to share ownership of the memory buffer, it must
	// ensure that it will not be modified while the page is in use, or it must
	// make a copy of it prior to creating the dictionary.
	//
	// The method panics if the data type does not correspond to the parquet
	// type it is called on.
	NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary

	// Creates a page belonging to a column at the given index, backed by the
	// data buffer.
	//
	// The page retains the data buffer, it does not make a copy of it. If the
	// application needs to share ownership of the memory buffer, it must ensure
	// that it will not be modified while the page is in use, or it must make a
	// copy of it prior to creating the page.
	//
	// The method panics if the data type does not correspond to the parquet
	// type it is called on.
	NewPage(columnIndex, numValues int, data encoding.Values) Page

	// Creates an encoding.Values instance backed by the given buffers.
	//
	// The offsets is only used by BYTE_ARRAY types, where it represents the
	// positions of each variable length value in the values buffer.
	//
	// The following expression creates an empty instance for any type:
	//
	//		values := typ.NewValues(nil, nil)
	//
	// The method panics if it is called on group types.
	NewValues(values []byte, offsets []uint32) encoding.Values

	// Assuming the src buffer contains PLAIN encoded values of the type it is
	// called on, applies the given encoding and produces the output to the dst
	// buffer passed as first argument by dispatching the call to one of the
	// encoding methods.
	Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error)

	// Assuming the src buffer contains values encoding in the given encoding,
	// decodes the input and produces the encoded values into the dst output
	// buffer passed as first argument by dispatching the call to one of the
	// encoding methods.
	Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error)

	// Returns an estimation of the output size after decoding the values passed
	// as first argument with the given encoding.
	//
	// For most types, this is similar to calling EstimateSize with the known
	// number of encoded values. For variable size types, using this method may
	// provide a more precise result since it can inspect the input buffer.
	EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int

	// Assigns a Parquet value to a Go value. Returns an error if assignment is
	// not possible. The source Value must be an expected logical type for the
	// receiver. This can be accomplished using ConvertValue.
	AssignValue(dst reflect.Value, src Value) error

	// Convert a Parquet Value of the given Type into a Parquet Value that is
	// compatible with the receiver. The returned Value is suitable to be passed
	// to AssignValue.
	ConvertValue(val Value, typ Type) (Value, error)
}

var (
	BooleanType   Type = booleanType{}
	Int32Type     Type = int32Type{}
	Int64Type     Type = int64Type{}
	Int96Type     Type = int96Type{}
	FloatType     Type = floatType{}
	DoubleType    Type = doubleType{}
	ByteArrayType Type = byteArrayType{}
)

// In the current parquet version supported by this library, only type-defined
// orders are supported.
var typeDefinedColumnOrder = format.ColumnOrder{
	TypeOrder: new(format.TypeDefinedOrder),
}

var physicalTypes = [...]format.Type{
	0: format.Boolean,
	1: format.Int32,
	2: format.Int64,
	3: format.Int96,
	4: format.Float,
	5: format.Double,
	6: format.ByteArray,
	7: format.FixedLenByteArray,
}

var convertedTypes = [...]deprecated.ConvertedType{
	0:  deprecated.UTF8,
	1:  deprecated.Map,
	2:  deprecated.MapKeyValue,
	3:  deprecated.List,
	4:  deprecated.Enum,
	5:  deprecated.Decimal,
	6:  deprecated.Date,
	7:  deprecated.TimeMillis,
	8:  deprecated.TimeMicros,
	9:  deprecated.TimestampMillis,
	10: deprecated.TimestampMicros,
	11: deprecated.Uint8,
	12: deprecated.Uint16,
	13: deprecated.Uint32,
	14: deprecated.Uint64,
	15: deprecated.Int8,
	16: deprecated.Int16,
	17: deprecated.Int32,
	18: deprecated.Int64,
	19: deprecated.Json,
	20: deprecated.Bson,
	21: deprecated.Interval,
}

type booleanType struct{}

func (t booleanType) String() string                           { return "BOOLEAN" }
func (t booleanType) Kind() Kind                               { return Boolean }
func (t booleanType) Length() int                              { return 1 }
func (t booleanType) EstimateSize(n int) int                   { return (n + 7) / 8 }
func (t booleanType) EstimateNumValues(n int) int              { return 8 * n }
func (t booleanType) Compare(a, b Value) int                   { return compareBool(a.boolean(), b.boolean()) }
func (t booleanType) ColumnOrder() *format.ColumnOrder         { return &typeDefinedColumnOrder }
func (t booleanType) LogicalType() *format.LogicalType         { return nil }
func (t booleanType) ConvertedType() *deprecated.ConvertedType { return nil }
func (t booleanType) PhysicalType() *format.Type               { return &physicalTypes[Boolean] }

func (t booleanType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newBooleanColumnIndexer()
}

func (t booleanType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newBooleanColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t booleanType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newBooleanDictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t booleanType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newBooleanPage(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t booleanType) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.BooleanValues(values)
}

func (t booleanType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeBoolean(dst, src, enc)
}

func (t booleanType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeBoolean(dst, src, enc)
}

func (t booleanType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t booleanType) AssignValue(dst reflect.Value, src Value) error {
	v := src.boolean()
	switch dst.Kind() {
	case reflect.Bool:
		dst.SetBool(v)
	default:
		dst.Set(reflect.ValueOf(v))
	}
	return nil
}

func (t booleanType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *stringType:
		return convertStringToBoolean(val)
	}
	switch typ.Kind() {
	case Boolean:
		return val, nil
	case Int32:
		return convertInt32ToBoolean(val)
	case Int64:
		return convertInt64ToBoolean(val)
	case Int96:
		return convertInt96ToBoolean(val)
	case Float:
		return convertFloatToBoolean(val)
	case Double:
		return convertDoubleToBoolean(val)
	case ByteArray, FixedLenByteArray:
		return convertByteArrayToBoolean(val)
	default:
		return makeValueKind(Boolean), nil
	}
}

type int32Type struct{}

func (t int32Type) String() string                           { return "INT32" }
func (t int32Type) Kind() Kind                               { return Int32 }
func (t int32Type) Length() int                              { return 32 }
func (t int32Type) EstimateSize(n int) int                   { return 4 * n }
func (t int32Type) EstimateNumValues(n int) int              { return n / 4 }
func (t int32Type) Compare(a, b Value) int                   { return compareInt32(a.int32(), b.int32()) }
func (t int32Type) ColumnOrder() *format.ColumnOrder         { return &typeDefinedColumnOrder }
func (t int32Type) LogicalType() *format.LogicalType         { return nil }
func (t int32Type) ConvertedType() *deprecated.ConvertedType { return nil }
func (t int32Type) PhysicalType() *format.Type               { return &physicalTypes[Int32] }

func (t int32Type) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newInt32ColumnIndexer()
}

func (t int32Type) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newInt32ColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t int32Type) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newInt32Dictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t int32Type) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newInt32Page(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t int32Type) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.Int32ValuesFromBytes(values)
}

func (t int32Type) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeInt32(dst, src, enc)
}

func (t int32Type) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeInt32(dst, src, enc)
}

func (t int32Type) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t int32Type) AssignValue(dst reflect.Value, src Value) error {
	v := src.int32()
	switch dst.Kind() {
	case reflect.Int8, reflect.Int16, reflect.Int32:
		dst.SetInt(int64(v))
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		dst.SetUint(uint64(v))
	default:
		dst.Set(reflect.ValueOf(v))
	}
	return nil
}

func (t int32Type) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *stringType:
		return convertStringToInt32(val)
	}
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToInt32(val)
	case Int32:
		return val, nil
	case Int64:
		return convertInt64ToInt32(val)
	case Int96:
		return convertInt96ToInt32(val)
	case Float:
		return convertFloatToInt32(val)
	case Double:
		return convertDoubleToInt32(val)
	case ByteArray, FixedLenByteArray:
		return convertByteArrayToInt32(val)
	default:
		return makeValueKind(Int32), nil
	}
}

type int64Type struct{}

func (t int64Type) String() string                           { return "INT64" }
func (t int64Type) Kind() Kind                               { return Int64 }
func (t int64Type) Length() int                              { return 64 }
func (t int64Type) EstimateSize(n int) int                   { return 8 * n }
func (t int64Type) EstimateNumValues(n int) int              { return n / 8 }
func (t int64Type) Compare(a, b Value) int                   { return compareInt64(a.int64(), b.int64()) }
func (t int64Type) ColumnOrder() *format.ColumnOrder         { return &typeDefinedColumnOrder }
func (t int64Type) LogicalType() *format.LogicalType         { return nil }
func (t int64Type) ConvertedType() *deprecated.ConvertedType { return nil }
func (t int64Type) PhysicalType() *format.Type               { return &physicalTypes[Int64] }

func (t int64Type) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newInt64ColumnIndexer()
}

func (t int64Type) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newInt64ColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t int64Type) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newInt64Dictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t int64Type) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newInt64Page(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t int64Type) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.Int64ValuesFromBytes(values)
}

func (t int64Type) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeInt64(dst, src, enc)
}

func (t int64Type) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeInt64(dst, src, enc)
}

func (t int64Type) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t int64Type) AssignValue(dst reflect.Value, src Value) error {
	v := src.int64()
	switch dst.Kind() {
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Int:
		dst.SetInt(v)
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uint, reflect.Uintptr:
		dst.SetUint(uint64(v))
	default:
		dst.Set(reflect.ValueOf(v))
	}
	return nil
}

func (t int64Type) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *stringType:
		return convertStringToInt64(val)
	}
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToInt64(val)
	case Int32:
		return convertInt32ToInt64(val)
	case Int64:
		return val, nil
	case Int96:
		return convertInt96ToInt64(val)
	case Float:
		return convertFloatToInt64(val)
	case Double:
		return convertDoubleToInt64(val)
	case ByteArray, FixedLenByteArray:
		return convertByteArrayToInt64(val)
	default:
		return makeValueKind(Int64), nil
	}
}

type int96Type struct{}

func (t int96Type) String() string { return "INT96" }

func (t int96Type) Kind() Kind                               { return Int96 }
func (t int96Type) Length() int                              { return 96 }
func (t int96Type) EstimateSize(n int) int                   { return 12 * n }
func (t int96Type) EstimateNumValues(n int) int              { return n / 12 }
func (t int96Type) Compare(a, b Value) int                   { return compareInt96(a.int96(), b.int96()) }
func (t int96Type) ColumnOrder() *format.ColumnOrder         { return &typeDefinedColumnOrder }
func (t int96Type) LogicalType() *format.LogicalType         { return nil }
func (t int96Type) ConvertedType() *deprecated.ConvertedType { return nil }
func (t int96Type) PhysicalType() *format.Type               { return &physicalTypes[Int96] }

func (t int96Type) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newInt96ColumnIndexer()
}

func (t int96Type) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newInt96ColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t int96Type) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newInt96Dictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t int96Type) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newInt96Page(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t int96Type) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.Int96ValuesFromBytes(values)
}

func (t int96Type) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeInt96(dst, src, enc)
}

func (t int96Type) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeInt96(dst, src, enc)
}

func (t int96Type) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t int96Type) AssignValue(dst reflect.Value, src Value) error {
	v := src.Int96()
	dst.Set(reflect.ValueOf(v))
	return nil
}

func (t int96Type) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *stringType:
		return convertStringToInt96(val)
	}
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToInt96(val)
	case Int32:
		return convertInt32ToInt96(val)
	case Int64:
		return convertInt64ToInt96(val)
	case Int96:
		return val, nil
	case Float:
		return convertFloatToInt96(val)
	case Double:
		return convertDoubleToInt96(val)
	case ByteArray, FixedLenByteArray:
		return convertByteArrayToInt96(val)
	default:
		return makeValueKind(Int96), nil
	}
}

type floatType struct{}

func (t floatType) String() string                           { return "FLOAT" }
func (t floatType) Kind() Kind                               { return Float }
func (t floatType) Length() int                              { return 32 }
func (t floatType) EstimateSize(n int) int                   { return 4 * n }
func (t floatType) EstimateNumValues(n int) int              { return n / 4 }
func (t floatType) Compare(a, b Value) int                   { return compareFloat32(a.float(), b.float()) }
func (t floatType) ColumnOrder() *format.ColumnOrder         { return &typeDefinedColumnOrder }
func (t floatType) LogicalType() *format.LogicalType         { return nil }
func (t floatType) ConvertedType() *deprecated.ConvertedType { return nil }
func (t floatType) PhysicalType() *format.Type               { return &physicalTypes[Float] }

func (t floatType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newFloatColumnIndexer()
}

func (t floatType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newFloatColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t floatType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newFloatDictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t floatType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newFloatPage(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t floatType) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.FloatValuesFromBytes(values)
}

func (t floatType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeFloat(dst, src, enc)
}

func (t floatType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeFloat(dst, src, enc)
}

func (t floatType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t floatType) AssignValue(dst reflect.Value, src Value) error {
	v := src.float()
	switch dst.Kind() {
	case reflect.Float32, reflect.Float64:
		dst.SetFloat(float64(v))
	default:
		dst.Set(reflect.ValueOf(v))
	}
	return nil
}

func (t floatType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *stringType:
		return convertStringToFloat(val)
	}
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToFloat(val)
	case Int32:
		return convertInt32ToFloat(val)
	case Int64:
		return convertInt64ToFloat(val)
	case Int96:
		return convertInt96ToFloat(val)
	case Float:
		return val, nil
	case Double:
		return convertDoubleToFloat(val)
	case ByteArray, FixedLenByteArray:
		return convertByteArrayToFloat(val)
	default:
		return makeValueKind(Float), nil
	}
}

type doubleType struct{}

func (t doubleType) String() string                           { return "DOUBLE" }
func (t doubleType) Kind() Kind                               { return Double }
func (t doubleType) Length() int                              { return 64 }
func (t doubleType) EstimateSize(n int) int                   { return 8 * n }
func (t doubleType) EstimateNumValues(n int) int              { return n / 8 }
func (t doubleType) Compare(a, b Value) int                   { return compareFloat64(a.double(), b.double()) }
func (t doubleType) ColumnOrder() *format.ColumnOrder         { return &typeDefinedColumnOrder }
func (t doubleType) LogicalType() *format.LogicalType         { return nil }
func (t doubleType) ConvertedType() *deprecated.ConvertedType { return nil }
func (t doubleType) PhysicalType() *format.Type               { return &physicalTypes[Double] }

func (t doubleType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newDoubleColumnIndexer()
}

func (t doubleType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newDoubleColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t doubleType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newDoubleDictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t doubleType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newDoublePage(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t doubleType) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.DoubleValuesFromBytes(values)
}

func (t doubleType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeDouble(dst, src, enc)
}

func (t doubleType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeDouble(dst, src, enc)
}

func (t doubleType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t doubleType) AssignValue(dst reflect.Value, src Value) error {
	v := src.double()
	switch dst.Kind() {
	case reflect.Float32, reflect.Float64:
		dst.SetFloat(v)
	default:
		dst.Set(reflect.ValueOf(v))
	}
	return nil
}

func (t doubleType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *stringType:
		return convertStringToDouble(val)
	}
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToDouble(val)
	case Int32:
		return convertInt32ToDouble(val)
	case Int64:
		return convertInt64ToDouble(val)
	case Int96:
		return convertInt96ToDouble(val)
	case Float:
		return convertFloatToDouble(val)
	case Double:
		return val, nil
	case ByteArray, FixedLenByteArray:
		return convertByteArrayToDouble(val)
	default:
		return makeValueKind(Double), nil
	}
}

type byteArrayType struct{}

func (t byteArrayType) String() string                           { return "BYTE_ARRAY" }
func (t byteArrayType) Kind() Kind                               { return ByteArray }
func (t byteArrayType) Length() int                              { return 0 }
func (t byteArrayType) EstimateSize(n int) int                   { return estimatedSizeOfByteArrayValues * n }
func (t byteArrayType) EstimateNumValues(n int) int              { return n / estimatedSizeOfByteArrayValues }
func (t byteArrayType) Compare(a, b Value) int                   { return bytes.Compare(a.byteArray(), b.byteArray()) }
func (t byteArrayType) ColumnOrder() *format.ColumnOrder         { return &typeDefinedColumnOrder }
func (t byteArrayType) LogicalType() *format.LogicalType         { return nil }
func (t byteArrayType) ConvertedType() *deprecated.ConvertedType { return nil }
func (t byteArrayType) PhysicalType() *format.Type               { return &physicalTypes[ByteArray] }

func (t byteArrayType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newByteArrayColumnIndexer(sizeLimit)
}

func (t byteArrayType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newByteArrayColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t byteArrayType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newByteArrayDictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t byteArrayType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newByteArrayPage(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t byteArrayType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return encoding.ByteArrayValues(values, offsets)
}

func (t byteArrayType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeByteArray(dst, src, enc)
}

func (t byteArrayType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeByteArray(dst, src, enc)
}

func (t byteArrayType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return enc.EstimateDecodeByteArraySize(src)
}

func (t byteArrayType) AssignValue(dst reflect.Value, src Value) error {
	v := src.byteArray()
	switch dst.Kind() {
	case reflect.String:
		dst.SetString(string(v))
	case reflect.Slice:
		dst.SetBytes(copyBytes(v))
	default:
		val := reflect.ValueOf(string(v))
		dst.Set(val)
	}
	return nil
}

func (t byteArrayType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToByteArray(val)
	case Int32:
		return convertInt32ToByteArray(val)
	case Int64:
		return convertInt64ToByteArray(val)
	case Int96:
		return convertInt96ToByteArray(val)
	case Float:
		return convertFloatToByteArray(val)
	case Double:
		return convertDoubleToByteArray(val)
	case ByteArray, FixedLenByteArray:
		return val, nil
	default:
		return makeValueKind(ByteArray), nil
	}
}

type fixedLenByteArrayType struct{ length int }

func (t fixedLenByteArrayType) String() string {
	return fmt.Sprintf("FIXED_LEN_BYTE_ARRAY(%d)", t.length)
}

func (t fixedLenByteArrayType) Kind() Kind { return FixedLenByteArray }

func (t fixedLenByteArrayType) Length() int { return t.length }

func (t fixedLenByteArrayType) EstimateSize(n int) int { return t.length * n }

func (t fixedLenByteArrayType) EstimateNumValues(n int) int { return n / t.length }

func (t fixedLenByteArrayType) Compare(a, b Value) int {
	return bytes.Compare(a.byteArray(), b.byteArray())
}

func (t fixedLenByteArrayType) ColumnOrder() *format.ColumnOrder { return &typeDefinedColumnOrder }

func (t fixedLenByteArrayType) LogicalType() *format.LogicalType { return nil }

func (t fixedLenByteArrayType) ConvertedType() *deprecated.ConvertedType { return nil }

func (t fixedLenByteArrayType) PhysicalType() *format.Type { return &physicalTypes[FixedLenByteArray] }

func (t fixedLenByteArrayType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newFixedLenByteArrayColumnIndexer(t.length, sizeLimit)
}

func (t fixedLenByteArrayType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newFixedLenByteArrayColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t fixedLenByteArrayType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newFixedLenByteArrayDictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t fixedLenByteArrayType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newFixedLenByteArrayPage(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t fixedLenByteArrayType) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.FixedLenByteArrayValues(values, t.length)
}

func (t fixedLenByteArrayType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeFixedLenByteArray(dst, src, enc)
}

func (t fixedLenByteArrayType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeFixedLenByteArray(dst, src, enc)
}

func (t fixedLenByteArrayType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t fixedLenByteArrayType) AssignValue(dst reflect.Value, src Value) error {
	v := src.byteArray()
	switch dst.Kind() {
	case reflect.Array:
		if dst.Type().Elem().Kind() == reflect.Uint8 && dst.Len() == len(v) {
			// This code could be implemented as a call to reflect.Copy but
			// it would require creating a reflect.Value from v which causes
			// the heap allocation to pack the []byte value. To avoid this
			// overhead we instead convert the reflect.Value holding the
			// destination array into a byte slice which allows us to use
			// a more efficient call to copy.
			d := unsafe.Slice((*byte)(reflectValueData(dst)), len(v))
			copy(d, v)
			return nil
		}
	case reflect.Slice:
		dst.SetBytes(copyBytes(v))
		return nil
	}

	val := reflect.ValueOf(copyBytes(v))
	dst.Set(val)
	return nil
}

func reflectValueData(v reflect.Value) unsafe.Pointer {
	return (*[2]unsafe.Pointer)(unsafe.Pointer(&v))[1]
}

func (t fixedLenByteArrayType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *stringType:
		return convertStringToFixedLenByteArray(val, t.length)
	}
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToFixedLenByteArray(val, t.length)
	case Int32:
		return convertInt32ToFixedLenByteArray(val, t.length)
	case Int64:
		return convertInt64ToFixedLenByteArray(val, t.length)
	case Int96:
		return convertInt96ToFixedLenByteArray(val, t.length)
	case Float:
		return convertFloatToFixedLenByteArray(val, t.length)
	case Double:
		return convertDoubleToFixedLenByteArray(val, t.length)
	case ByteArray, FixedLenByteArray:
		return convertByteArrayToFixedLenByteArray(val, t.length)
	default:
		return makeValueBytes(FixedLenByteArray, make([]byte, t.length)), nil
	}
}

type uint32Type struct{ int32Type }

func (t uint32Type) Compare(a, b Value) int {
	return compareUint32(a.uint32(), b.uint32())
}

func (t uint32Type) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newUint32ColumnIndexer()
}

func (t uint32Type) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newUint32ColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t uint32Type) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newUint32Dictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t uint32Type) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newUint32Page(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

type uint64Type struct{ int64Type }

func (t uint64Type) Compare(a, b Value) int {
	return compareUint64(a.uint64(), b.uint64())
}

func (t uint64Type) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newUint64ColumnIndexer()
}

func (t uint64Type) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newUint64ColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t uint64Type) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newUint64Dictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t uint64Type) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newUint64Page(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

// BE128 stands for "big-endian 128 bits". This type is used as a special case
// for fixed-length byte arrays of 16 bytes, which are commonly used to
// represent columns of random unique identifiers such as UUIDs.
//
// Comparisons of BE128 values use the natural byte order, the zeroth byte is
// the most significant byte.
//
// The special case is intended to provide optimizations based on the knowledge
// that the values are 16 bytes long. Stronger type checking can also be applied
// by the compiler when using [16]byte values rather than []byte, reducing the
// risk of errors on these common code paths.
type be128Type struct{}

func (t be128Type) String() string { return "FIXED_LEN_BYTE_ARRAY(16)" }

func (t be128Type) Kind() Kind { return FixedLenByteArray }

func (t be128Type) Length() int { return 16 }

func (t be128Type) EstimateSize(n int) int { return 16 * n }

func (t be128Type) EstimateNumValues(n int) int { return n / 16 }

func (t be128Type) Compare(a, b Value) int { return compareBE128(a.be128(), b.be128()) }

func (t be128Type) ColumnOrder() *format.ColumnOrder { return &typeDefinedColumnOrder }

func (t be128Type) LogicalType() *format.LogicalType { return nil }

func (t be128Type) ConvertedType() *deprecated.ConvertedType { return nil }

func (t be128Type) PhysicalType() *format.Type { return &physicalTypes[FixedLenByteArray] }

func (t be128Type) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newBE128ColumnIndexer()
}

func (t be128Type) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newBE128ColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t be128Type) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newBE128Dictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t be128Type) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newBE128Page(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t be128Type) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.FixedLenByteArrayValues(values, 16)
}

func (t be128Type) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeFixedLenByteArray(dst, src, enc)
}

func (t be128Type) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeFixedLenByteArray(dst, src, enc)
}

func (t be128Type) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t be128Type) AssignValue(dst reflect.Value, src Value) error {
	return fixedLenByteArrayType{length: 16}.AssignValue(dst, src)
}

func (t be128Type) ConvertValue(val Value, typ Type) (Value, error) {
	return fixedLenByteArrayType{length: 16}.ConvertValue(val, typ)
}

// FixedLenByteArrayType constructs a type for fixed-length values of the given
// size (in bytes).
func FixedLenByteArrayType(length int) Type {
	switch length {
	case 16:
		return be128Type{}
	default:
		return fixedLenByteArrayType{length: length}
	}
}

// Int constructs a leaf node of signed integer logical type of the given bit
// width.
//
// The bit width must be one of 8, 16, 32, 64, or the function will panic.
func Int(bitWidth int) Node {
	return Leaf(integerType(bitWidth, &signedIntTypes))
}

// Uint constructs a leaf node of unsigned integer logical type of the given
// bit width.
//
// The bit width must be one of 8, 16, 32, 64, or the function will panic.
func Uint(bitWidth int) Node {
	return Leaf(integerType(bitWidth, &unsignedIntTypes))
}

func integerType(bitWidth int, types *[4]intType) *intType {
	switch bitWidth {
	case 8:
		return &types[0]
	case 16:
		return &types[1]
	case 32:
		return &types[2]
	case 64:
		return &types[3]
	default:
		panic(fmt.Sprintf("cannot create a %d bits parquet integer node", bitWidth))
	}
}

var signedIntTypes = [...]intType{
	{BitWidth: 8, IsSigned: true},
	{BitWidth: 16, IsSigned: true},
	{BitWidth: 32, IsSigned: true},
	{BitWidth: 64, IsSigned: true},
}

var unsignedIntTypes = [...]intType{
	{BitWidth: 8, IsSigned: false},
	{BitWidth: 16, IsSigned: false},
	{BitWidth: 32, IsSigned: false},
	{BitWidth: 64, IsSigned: false},
}

type intType format.IntType

func (t *intType) baseType() Type {
	if t.IsSigned {
		if t.BitWidth == 64 {
			return int64Type{}
		} else {
			return int32Type{}
		}
	} else {
		if t.BitWidth == 64 {
			return uint64Type{}
		} else {
			return uint32Type{}
		}
	}
}

func (t *intType) String() string { return (*format.IntType)(t).String() }

func (t *intType) Kind() Kind { return t.baseType().Kind() }

func (t *intType) Length() int { return int(t.BitWidth) }

func (t *intType) EstimateSize(n int) int { return (int(t.BitWidth) / 8) * n }

func (t *intType) EstimateNumValues(n int) int { return n / (int(t.BitWidth) / 8) }

func (t *intType) Compare(a, b Value) int {
	// This code is similar to t.baseType().Compare(a,b) but comparison methods
	// tend to be invoked a lot (e.g. when sorting) so avoiding the interface
	// indirection in this case yields much better throughput in some cases.
	if t.BitWidth == 64 {
		i1 := a.int64()
		i2 := b.int64()
		if t.IsSigned {
			return compareInt64(i1, i2)
		} else {
			return compareUint64(uint64(i1), uint64(i2))
		}
	} else {
		i1 := a.int32()
		i2 := b.int32()
		if t.IsSigned {
			return compareInt32(i1, i2)
		} else {
			return compareUint32(uint32(i1), uint32(i2))
		}
	}
}

func (t *intType) ColumnOrder() *format.ColumnOrder { return t.baseType().ColumnOrder() }

func (t *intType) PhysicalType() *format.Type { return t.baseType().PhysicalType() }

func (t *intType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Integer: (*format.IntType)(t)}
}

func (t *intType) ConvertedType() *deprecated.ConvertedType {
	convertedType := bits.Len8(uint8(t.BitWidth)/8) - 1 // 8=>0, 16=>1, 32=>2, 64=>4
	if t.IsSigned {
		convertedType += int(deprecated.Int8)
	} else {
		convertedType += int(deprecated.Uint8)
	}
	return &convertedTypes[convertedType]
}

func (t *intType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return t.baseType().NewColumnIndexer(sizeLimit)
}

func (t *intType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return t.baseType().NewColumnBuffer(columnIndex, numValues)
}

func (t *intType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return t.baseType().NewDictionary(columnIndex, numValues, data)
}

func (t *intType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return t.baseType().NewPage(columnIndex, numValues, data)
}

func (t *intType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return t.baseType().NewValues(values, offsets)
}

func (t *intType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return t.baseType().Encode(dst, src, enc)
}

func (t *intType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return t.baseType().Decode(dst, src, enc)
}

func (t *intType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.baseType().EstimateDecodeSize(numValues, src, enc)
}

func (t *intType) AssignValue(dst reflect.Value, src Value) error {
	if t.BitWidth == 64 {
		return int64Type{}.AssignValue(dst, src)
	} else {
		return int32Type{}.AssignValue(dst, src)
	}
}

func (t *intType) ConvertValue(val Value, typ Type) (Value, error) {
	if t.BitWidth == 64 {
		return int64Type{}.ConvertValue(val, typ)
	} else {
		return int32Type{}.ConvertValue(val, typ)
	}
}

// Decimal constructs a leaf node of decimal logical type with the given
// scale, precision, and underlying type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#decimal
func Decimal(scale, precision int, typ Type) Node {
	switch typ.Kind() {
	case Int32, Int64, FixedLenByteArray:
	default:
		panic("DECIMAL node must annotate Int32, Int64 or FixedLenByteArray but got " + typ.String())
	}
	return Leaf(&decimalType{
		decimal: format.DecimalType{
			Scale:     int32(scale),
			Precision: int32(precision),
		},
		Type: typ,
	})
}

type decimalType struct {
	decimal format.DecimalType
	Type
}

func (t *decimalType) String() string { return t.decimal.String() }

func (t *decimalType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Decimal: &t.decimal}
}

func (t *decimalType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Decimal]
}

// String constructs a leaf node of UTF8 logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#string
func String() Node { return Leaf(&stringType{}) }

type stringType format.StringType

func (t *stringType) String() string { return (*format.StringType)(t).String() }

func (t *stringType) Kind() Kind { return ByteArray }

func (t *stringType) Length() int { return 0 }

func (t *stringType) EstimateSize(n int) int { return byteArrayType{}.EstimateSize(n) }

func (t *stringType) EstimateNumValues(n int) int { return byteArrayType{}.EstimateNumValues(n) }

func (t *stringType) Compare(a, b Value) int {
	return bytes.Compare(a.byteArray(), b.byteArray())
}

func (t *stringType) ColumnOrder() *format.ColumnOrder {
	return &typeDefinedColumnOrder
}

func (t *stringType) PhysicalType() *format.Type {
	return &physicalTypes[ByteArray]
}

func (t *stringType) LogicalType() *format.LogicalType {
	return &format.LogicalType{UTF8: (*format.StringType)(t)}
}

func (t *stringType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.UTF8]
}

func (t *stringType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newByteArrayColumnIndexer(sizeLimit)
}

func (t *stringType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newByteArrayDictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t *stringType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newByteArrayColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t *stringType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newByteArrayPage(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t *stringType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return encoding.ByteArrayValues(values, offsets)
}

func (t *stringType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeByteArray(dst, src, enc)
}

func (t *stringType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeByteArray(dst, src, enc)
}

func (t *stringType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return byteArrayType{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *stringType) AssignValue(dst reflect.Value, src Value) error {
	return byteArrayType{}.AssignValue(dst, src)
}

func (t *stringType) ConvertValue(val Value, typ Type) (Value, error) {
	switch t2 := typ.(type) {
	case *dateType:
		return convertDateToString(val)
	case *timeType:
		tz := t2.tz()
		if t2.Unit.Micros != nil {
			return convertTimeMicrosToString(val, tz)
		} else {
			return convertTimeMillisToString(val, tz)
		}
	}
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToString(val)
	case Int32:
		return convertInt32ToString(val)
	case Int64:
		return convertInt64ToString(val)
	case Int96:
		return convertInt96ToString(val)
	case Float:
		return convertFloatToString(val)
	case Double:
		return convertDoubleToString(val)
	case ByteArray:
		return val, nil
	case FixedLenByteArray:
		return convertFixedLenByteArrayToString(val)
	default:
		return makeValueKind(ByteArray), nil
	}
}

// UUID constructs a leaf node of UUID logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#uuid
func UUID() Node { return Leaf(&uuidType{}) }

type uuidType format.UUIDType

func (t *uuidType) String() string { return (*format.UUIDType)(t).String() }

func (t *uuidType) Kind() Kind { return be128Type{}.Kind() }

func (t *uuidType) Length() int { return be128Type{}.Length() }

func (t *uuidType) EstimateSize(n int) int { return be128Type{}.EstimateSize(n) }

func (t *uuidType) EstimateNumValues(n int) int { return be128Type{}.EstimateNumValues(n) }

func (t *uuidType) Compare(a, b Value) int { return be128Type{}.Compare(a, b) }

func (t *uuidType) ColumnOrder() *format.ColumnOrder { return &typeDefinedColumnOrder }

func (t *uuidType) PhysicalType() *format.Type { return &physicalTypes[FixedLenByteArray] }

func (t *uuidType) LogicalType() *format.LogicalType {
	return &format.LogicalType{UUID: (*format.UUIDType)(t)}
}

func (t *uuidType) ConvertedType() *deprecated.ConvertedType { return nil }

func (t *uuidType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return be128Type{}.NewColumnIndexer(sizeLimit)
}

func (t *uuidType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return be128Type{}.NewDictionary(columnIndex, numValues, data)
}

func (t *uuidType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return be128Type{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *uuidType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return be128Type{}.NewPage(columnIndex, numValues, data)
}

func (t *uuidType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return be128Type{}.NewValues(values, offsets)
}

func (t *uuidType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return be128Type{}.Encode(dst, src, enc)
}

func (t *uuidType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return be128Type{}.Decode(dst, src, enc)
}

func (t *uuidType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return be128Type{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *uuidType) AssignValue(dst reflect.Value, src Value) error {
	return be128Type{}.AssignValue(dst, src)
}

func (t *uuidType) ConvertValue(val Value, typ Type) (Value, error) {
	return be128Type{}.ConvertValue(val, typ)
}

// Enum constructs a leaf node with a logical type representing enumerations.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#enum
func Enum() Node { return Leaf(&enumType{}) }

type enumType format.EnumType

func (t *enumType) String() string { return (*format.EnumType)(t).String() }

func (t *enumType) Kind() Kind { return new(stringType).Kind() }

func (t *enumType) Length() int { return new(stringType).Length() }

func (t *enumType) EstimateSize(n int) int { return new(stringType).EstimateSize(n) }

func (t *enumType) EstimateNumValues(n int) int { return new(stringType).EstimateNumValues(n) }

func (t *enumType) Compare(a, b Value) int { return new(stringType).Compare(a, b) }

func (t *enumType) ColumnOrder() *format.ColumnOrder { return new(stringType).ColumnOrder() }

func (t *enumType) PhysicalType() *format.Type { return new(stringType).PhysicalType() }

func (t *enumType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Enum: (*format.EnumType)(t)}
}

func (t *enumType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Enum]
}

func (t *enumType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return new(stringType).NewColumnIndexer(sizeLimit)
}

func (t *enumType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return new(stringType).NewDictionary(columnIndex, numValues, data)
}

func (t *enumType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return new(stringType).NewColumnBuffer(columnIndex, numValues)
}

func (t *enumType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return new(stringType).NewPage(columnIndex, numValues, data)
}

func (t *enumType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return new(stringType).NewValues(values, offsets)
}

func (t *enumType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return new(stringType).Encode(dst, src, enc)
}

func (t *enumType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return new(stringType).Decode(dst, src, enc)
}

func (t *enumType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return new(stringType).EstimateDecodeSize(numValues, src, enc)
}

func (t *enumType) AssignValue(dst reflect.Value, src Value) error {
	return new(stringType).AssignValue(dst, src)
}

func (t *enumType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *byteArrayType, *stringType, *enumType:
		return val, nil
	default:
		return val, invalidConversion(val, "ENUM", typ.String())
	}
}

// JSON constructs a leaf node of JSON logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#json
func JSON() Node { return Leaf(&jsonType{}) }

type jsonType format.JsonType

func (t *jsonType) String() string { return (*format.JsonType)(t).String() }

func (t *jsonType) Kind() Kind { return byteArrayType{}.Kind() }

func (t *jsonType) Length() int { return byteArrayType{}.Length() }

func (t *jsonType) EstimateSize(n int) int { return byteArrayType{}.EstimateSize(n) }

func (t *jsonType) EstimateNumValues(n int) int { return byteArrayType{}.EstimateNumValues(n) }

func (t *jsonType) Compare(a, b Value) int { return byteArrayType{}.Compare(a, b) }

func (t *jsonType) ColumnOrder() *format.ColumnOrder { return byteArrayType{}.ColumnOrder() }

func (t *jsonType) PhysicalType() *format.Type { return byteArrayType{}.PhysicalType() }

func (t *jsonType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Json: (*format.JsonType)(t)}
}

func (t *jsonType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Json]
}

func (t *jsonType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return byteArrayType{}.NewColumnIndexer(sizeLimit)
}

func (t *jsonType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return byteArrayType{}.NewDictionary(columnIndex, numValues, data)
}

func (t *jsonType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return byteArrayType{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *jsonType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return byteArrayType{}.NewPage(columnIndex, numValues, data)
}

func (t *jsonType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return byteArrayType{}.NewValues(values, offsets)
}

func (t *jsonType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return byteArrayType{}.Encode(dst, src, enc)
}

func (t *jsonType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return byteArrayType{}.Decode(dst, src, enc)
}

func (t *jsonType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return byteArrayType{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *jsonType) AssignValue(dst reflect.Value, src Value) error {
	// Assign value using ByteArrayType for BC...
	switch dst.Kind() {
	case reflect.String:
		return byteArrayType{}.AssignValue(dst, src)
	case reflect.Slice:
		if dst.Type().Elem().Kind() == reflect.Uint8 {
			return byteArrayType{}.AssignValue(dst, src)
		}
	}

	// Otherwise handle with json.Unmarshal
	b := src.byteArray()
	val := reflect.New(dst.Type()).Elem()
	err := json.Unmarshal(b, val.Addr().Interface())
	if err != nil {
		return err
	}
	dst.Set(val)
	return nil
}

func (t *jsonType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *byteArrayType, *stringType, *jsonType:
		return val, nil
	default:
		return val, invalidConversion(val, "JSON", typ.String())
	}
}

// BSON constructs a leaf node of BSON logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#bson
func BSON() Node { return Leaf(&bsonType{}) }

type bsonType format.BsonType

func (t *bsonType) String() string { return (*format.BsonType)(t).String() }

func (t *bsonType) Kind() Kind { return byteArrayType{}.Kind() }

func (t *bsonType) Length() int { return byteArrayType{}.Length() }

func (t *bsonType) EstimateSize(n int) int { return byteArrayType{}.EstimateSize(n) }

func (t *bsonType) EstimateNumValues(n int) int { return byteArrayType{}.EstimateNumValues(n) }

func (t *bsonType) Compare(a, b Value) int { return byteArrayType{}.Compare(a, b) }

func (t *bsonType) ColumnOrder() *format.ColumnOrder { return byteArrayType{}.ColumnOrder() }

func (t *bsonType) PhysicalType() *format.Type { return byteArrayType{}.PhysicalType() }

func (t *bsonType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Bson: (*format.BsonType)(t)}
}

func (t *bsonType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Bson]
}

func (t *bsonType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return byteArrayType{}.NewColumnIndexer(sizeLimit)
}

func (t *bsonType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return byteArrayType{}.NewDictionary(columnIndex, numValues, data)
}

func (t *bsonType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return byteArrayType{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *bsonType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return byteArrayType{}.NewPage(columnIndex, numValues, data)
}

func (t *bsonType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return byteArrayType{}.NewValues(values, offsets)
}

func (t *bsonType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return byteArrayType{}.Encode(dst, src, enc)
}

func (t *bsonType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return byteArrayType{}.Decode(dst, src, enc)
}

func (t *bsonType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return byteArrayType{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *bsonType) AssignValue(dst reflect.Value, src Value) error {
	return byteArrayType{}.AssignValue(dst, src)
}

func (t *bsonType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *byteArrayType, *bsonType:
		return val, nil
	default:
		return val, invalidConversion(val, "BSON", typ.String())
	}
}

// Date constructs a leaf node of DATE logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#date
func Date() Node { return Leaf(&dateType{}) }

type dateType format.DateType

func (t *dateType) String() string { return (*format.DateType)(t).String() }

func (t *dateType) Kind() Kind { return int32Type{}.Kind() }

func (t *dateType) Length() int { return int32Type{}.Length() }

func (t *dateType) EstimateSize(n int) int { return int32Type{}.EstimateSize(n) }

func (t *dateType) EstimateNumValues(n int) int { return int32Type{}.EstimateNumValues(n) }

func (t *dateType) Compare(a, b Value) int { return int32Type{}.Compare(a, b) }

func (t *dateType) ColumnOrder() *format.ColumnOrder { return int32Type{}.ColumnOrder() }

func (t *dateType) PhysicalType() *format.Type { return int32Type{}.PhysicalType() }

func (t *dateType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Date: (*format.DateType)(t)}
}

func (t *dateType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Date]
}

func (t *dateType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return int32Type{}.NewColumnIndexer(sizeLimit)
}

func (t *dateType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return int32Type{}.NewDictionary(columnIndex, numValues, data)
}

func (t *dateType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return int32Type{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *dateType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return int32Type{}.NewPage(columnIndex, numValues, data)
}

func (t *dateType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return int32Type{}.NewValues(values, offsets)
}

func (t *dateType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return int32Type{}.Encode(dst, src, enc)
}

func (t *dateType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return int32Type{}.Decode(dst, src, enc)
}

func (t *dateType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return int32Type{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *dateType) AssignValue(dst reflect.Value, src Value) error {
	return int32Type{}.AssignValue(dst, src)
}

func (t *dateType) ConvertValue(val Value, typ Type) (Value, error) {
	switch src := typ.(type) {
	case *stringType:
		return convertStringToDate(val, time.UTC)
	case *timestampType:
		return convertTimestampToDate(val, src.Unit, src.tz())
	}
	return int32Type{}.ConvertValue(val, typ)
}

// TimeUnit represents units of time in the parquet type system.
type TimeUnit interface {
	// Returns the precision of the time unit as a time.Duration value.
	Duration() time.Duration
	// Converts the TimeUnit value to its representation in the parquet thrift
	// format.
	TimeUnit() format.TimeUnit
}

var (
	Millisecond TimeUnit = &millisecond{}
	Microsecond TimeUnit = &microsecond{}
	Nanosecond  TimeUnit = &nanosecond{}
)

type millisecond format.MilliSeconds

func (u *millisecond) Duration() time.Duration { return time.Millisecond }
func (u *millisecond) TimeUnit() format.TimeUnit {
	return format.TimeUnit{Millis: (*format.MilliSeconds)(u)}
}

type microsecond format.MicroSeconds

func (u *microsecond) Duration() time.Duration { return time.Microsecond }
func (u *microsecond) TimeUnit() format.TimeUnit {
	return format.TimeUnit{Micros: (*format.MicroSeconds)(u)}
}

type nanosecond format.NanoSeconds

func (u *nanosecond) Duration() time.Duration { return time.Nanosecond }
func (u *nanosecond) TimeUnit() format.TimeUnit {
	return format.TimeUnit{Nanos: (*format.NanoSeconds)(u)}
}

// Time constructs a leaf node of TIME logical type.
// IsAdjustedToUTC is true by default.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#time
func Time(unit TimeUnit) Node {
	return TimeAdjusted(unit, true)
}

// TimeAdjusted constructs a leaf node of TIME logical type
// with the IsAdjustedToUTC property explicitly set.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#time
func TimeAdjusted(unit TimeUnit, isAdjustedToUTC bool) Node {
	return Leaf(&timeType{IsAdjustedToUTC: isAdjustedToUTC, Unit: unit.TimeUnit()})
}

type timeType format.TimeType

func (t *timeType) tz() *time.Location {
	if t.IsAdjustedToUTC {
		return time.UTC
	} else {
		return time.Local
	}
}

func (t *timeType) baseType() Type {
	if t.useInt32() {
		return int32Type{}
	} else {
		return int64Type{}
	}
}

func (t *timeType) useInt32() bool { return t.Unit.Millis != nil }

func (t *timeType) useInt64() bool { return t.Unit.Micros != nil }

func (t *timeType) String() string { return (*format.TimeType)(t).String() }

func (t *timeType) Kind() Kind { return t.baseType().Kind() }

func (t *timeType) Length() int { return t.baseType().Length() }

func (t *timeType) EstimateSize(n int) int { return t.baseType().EstimateSize(n) }

func (t *timeType) EstimateNumValues(n int) int { return t.baseType().EstimateNumValues(n) }

func (t *timeType) Compare(a, b Value) int { return t.baseType().Compare(a, b) }

func (t *timeType) ColumnOrder() *format.ColumnOrder { return t.baseType().ColumnOrder() }

func (t *timeType) PhysicalType() *format.Type { return t.baseType().PhysicalType() }

func (t *timeType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Time: (*format.TimeType)(t)}
}

func (t *timeType) ConvertedType() *deprecated.ConvertedType {
	switch {
	case t.useInt32():
		return &convertedTypes[deprecated.TimeMillis]
	case t.useInt64():
		return &convertedTypes[deprecated.TimeMicros]
	default:
		return nil
	}
}

func (t *timeType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return t.baseType().NewColumnIndexer(sizeLimit)
}

func (t *timeType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return t.baseType().NewColumnBuffer(columnIndex, numValues)
}

func (t *timeType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return t.baseType().NewDictionary(columnIndex, numValues, data)
}

func (t *timeType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return t.baseType().NewPage(columnIndex, numValues, data)
}

func (t *timeType) NewValues(values []byte, offset []uint32) encoding.Values {
	return t.baseType().NewValues(values, offset)
}

func (t *timeType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return t.baseType().Encode(dst, src, enc)
}

func (t *timeType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return t.baseType().Decode(dst, src, enc)
}

func (t *timeType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.baseType().EstimateDecodeSize(numValues, src, enc)
}

func (t *timeType) AssignValue(dst reflect.Value, src Value) error {
	return t.baseType().AssignValue(dst, src)
}

func (t *timeType) ConvertValue(val Value, typ Type) (Value, error) {
	switch src := typ.(type) {
	case *stringType:
		tz := t.tz()
		if t.Unit.Micros != nil {
			return convertStringToTimeMicros(val, tz)
		} else {
			return convertStringToTimeMillis(val, tz)
		}
	case *timestampType:
		tz := t.tz()
		if t.Unit.Micros != nil {
			return convertTimestampToTimeMicros(val, src.Unit, src.tz(), tz)
		} else {
			return convertTimestampToTimeMillis(val, src.Unit, src.tz(), tz)
		}
	}
	return t.baseType().ConvertValue(val, typ)
}

// Timestamp constructs of leaf node of TIMESTAMP logical type.
// IsAdjustedToUTC is true by default.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#timestamp
func Timestamp(unit TimeUnit) Node {
	return TimestampAdjusted(unit, true)
}

// TimestampAdjusted constructs a leaf node of TIMESTAMP logical type
// with the IsAdjustedToUTC property explicitly set.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#time
func TimestampAdjusted(unit TimeUnit, isAdjustedToUTC bool) Node {
	return Leaf(&timestampType{IsAdjustedToUTC: isAdjustedToUTC, Unit: unit.TimeUnit()})
}

type timestampType format.TimestampType

func (t *timestampType) tz() *time.Location {
	if t.IsAdjustedToUTC {
		return time.UTC
	} else {
		return time.Local
	}
}

func (t *timestampType) String() string { return (*format.TimestampType)(t).String() }

func (t *timestampType) Kind() Kind { return int64Type{}.Kind() }

func (t *timestampType) Length() int { return int64Type{}.Length() }

func (t *timestampType) EstimateSize(n int) int { return int64Type{}.EstimateSize(n) }

func (t *timestampType) EstimateNumValues(n int) int { return int64Type{}.EstimateNumValues(n) }

func (t *timestampType) Compare(a, b Value) int { return int64Type{}.Compare(a, b) }

func (t *timestampType) ColumnOrder() *format.ColumnOrder { return int64Type{}.ColumnOrder() }

func (t *timestampType) PhysicalType() *format.Type { return int64Type{}.PhysicalType() }

func (t *timestampType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Timestamp: (*format.TimestampType)(t)}
}

func (t *timestampType) ConvertedType() *deprecated.ConvertedType {
	switch {
	case t.Unit.Millis != nil:
		return &convertedTypes[deprecated.TimestampMillis]
	case t.Unit.Micros != nil:
		return &convertedTypes[deprecated.TimestampMicros]
	default:
		return nil
	}
}

func (t *timestampType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return int64Type{}.NewColumnIndexer(sizeLimit)
}

func (t *timestampType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return int64Type{}.NewDictionary(columnIndex, numValues, data)
}

func (t *timestampType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return int64Type{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *timestampType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return int64Type{}.NewPage(columnIndex, numValues, data)
}

func (t *timestampType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return int64Type{}.NewValues(values, offsets)
}

func (t *timestampType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return int64Type{}.Encode(dst, src, enc)
}

func (t *timestampType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return int64Type{}.Decode(dst, src, enc)
}

func (t *timestampType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return int64Type{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *timestampType) AssignValue(dst reflect.Value, src Value) error {
	switch dst.Type() {
	case reflect.TypeOf(time.Time{}):
		unit := Nanosecond.TimeUnit()
		lt := t.LogicalType()
		if lt != nil && lt.Timestamp != nil {
			unit = lt.Timestamp.Unit
		}

		nanos := src.int64()
		switch {
		case unit.Millis != nil:
			nanos = nanos * 1e6
		case unit.Micros != nil:
			nanos = nanos * 1e3
		}

		val := time.Unix(0, nanos).UTC()
		dst.Set(reflect.ValueOf(val))
		return nil
	default:
		return int64Type{}.AssignValue(dst, src)
	}
}

func (t *timestampType) ConvertValue(val Value, typ Type) (Value, error) {
	switch src := typ.(type) {
	case *timestampType:
		return convertTimestampToTimestamp(val, src.Unit, t.Unit)
	case *dateType:
		return convertDateToTimestamp(val, t.Unit, t.tz())
	}
	return int64Type{}.ConvertValue(val, typ)
}

// List constructs a node of LIST logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#lists
func List(of Node) Node {
	return listNode{Group{"list": Repeated(Group{"element": of})}}
}

type listNode struct{ Group }

func (listNode) Type() Type { return &listType{} }

type listType format.ListType

func (t *listType) String() string { return (*format.ListType)(t).String() }

func (t *listType) Kind() Kind { panic("cannot call Kind on parquet LIST type") }

func (t *listType) Length() int { return 0 }

func (t *listType) EstimateSize(int) int { return 0 }

func (t *listType) EstimateNumValues(int) int { return 0 }

func (t *listType) Compare(Value, Value) int { panic("cannot compare values on parquet LIST type") }

func (t *listType) ColumnOrder() *format.ColumnOrder { return nil }

func (t *listType) PhysicalType() *format.Type { return nil }

func (t *listType) LogicalType() *format.LogicalType {
	return &format.LogicalType{List: (*format.ListType)(t)}
}

func (t *listType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.List]
}

func (t *listType) NewColumnIndexer(int) ColumnIndexer {
	panic("create create column indexer from parquet LIST type")
}

func (t *listType) NewDictionary(int, int, encoding.Values) Dictionary {
	panic("cannot create dictionary from parquet LIST type")
}

func (t *listType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet LIST type")
}

func (t *listType) NewPage(int, int, encoding.Values) Page {
	panic("cannot create page from parquet LIST type")
}

func (t *listType) NewValues(values []byte, _ []uint32) encoding.Values {
	panic("cannot create values from parquet LIST type")
}

func (t *listType) Encode(_ []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	panic("cannot encode parquet LIST type")
}

func (t *listType) Decode(_ encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	panic("cannot decode parquet LIST type")
}

func (t *listType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	panic("cannot estimate decode size of parquet LIST type")
}

func (t *listType) AssignValue(reflect.Value, Value) error {
	panic("cannot assign value to a parquet LIST type")
}

func (t *listType) ConvertValue(Value, Type) (Value, error) {
	panic("cannot convert value to a parquet LIST type")
}

// Map constructs a node of MAP logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#maps
func Map(key, value Node) Node {
	return mapNode{Group{
		"key_value": Repeated(Group{
			"key":   Required(key),
			"value": value,
		}),
	}}
}

type mapNode struct{ Group }

func (mapNode) Type() Type { return &mapType{} }

type mapType format.MapType

func (t *mapType) String() string { return (*format.MapType)(t).String() }

func (t *mapType) Kind() Kind { panic("cannot call Kind on parquet MAP type") }

func (t *mapType) Length() int { return 0 }

func (t *mapType) EstimateSize(int) int { return 0 }

func (t *mapType) EstimateNumValues(int) int { return 0 }

func (t *mapType) Compare(Value, Value) int { panic("cannot compare values on parquet MAP type") }

func (t *mapType) ColumnOrder() *format.ColumnOrder { return nil }

func (t *mapType) PhysicalType() *format.Type { return nil }

func (t *mapType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Map: (*format.MapType)(t)}
}

func (t *mapType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Map]
}

func (t *mapType) NewColumnIndexer(int) ColumnIndexer {
	panic("create create column indexer from parquet MAP type")
}

func (t *mapType) NewDictionary(int, int, encoding.Values) Dictionary {
	panic("cannot create dictionary from parquet MAP type")
}

func (t *mapType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet MAP type")
}

func (t *mapType) NewPage(int, int, encoding.Values) Page {
	panic("cannot create page from parquet MAP type")
}

func (t *mapType) NewValues(values []byte, _ []uint32) encoding.Values {
	panic("cannot create values from parquet MAP type")
}

func (t *mapType) Encode(_ []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	panic("cannot encode parquet MAP type")
}

func (t *mapType) Decode(_ encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	panic("cannot decode parquet MAP type")
}

func (t *mapType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	panic("cannot estimate decode size of parquet MAP type")
}

func (t *mapType) AssignValue(reflect.Value, Value) error {
	panic("cannot assign value to a parquet MAP type")
}

func (t *mapType) ConvertValue(Value, Type) (Value, error) {
	panic("cannot convert value to a parquet MAP type")
}

type nullType format.NullType

func (t *nullType) String() string { return (*format.NullType)(t).String() }

func (t *nullType) Kind() Kind { return -1 }

func (t *nullType) Length() int { return 0 }

func (t *nullType) EstimateSize(int) int { return 0 }

func (t *nullType) EstimateNumValues(int) int { return 0 }

func (t *nullType) Compare(Value, Value) int { panic("cannot compare values on parquet NULL type") }

func (t *nullType) ColumnOrder() *format.ColumnOrder { return nil }

func (t *nullType) PhysicalType() *format.Type { return nil }

func (t *nullType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Unknown: (*format.NullType)(t)}
}

func (t *nullType) ConvertedType() *deprecated.ConvertedType { return nil }

func (t *nullType) NewColumnIndexer(int) ColumnIndexer {
	panic("create create column indexer from parquet NULL type")
}

func (t *nullType) NewDictionary(int, int, encoding.Values) Dictionary {
	panic("cannot create dictionary from parquet NULL type")
}

func (t *nullType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet NULL type")
}

func (t *nullType) NewPage(columnIndex, numValues int, _ encoding.Values) Page {
	return newNullPage(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t *nullType) NewValues(_ []byte, _ []uint32) encoding.Values {
	return encoding.Values{}
}

func (t *nullType) Encode(dst []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	return dst[:0], nil
}

func (t *nullType) Decode(dst encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	return dst, nil
}

func (t *nullType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	return 0
}

func (t *nullType) AssignValue(reflect.Value, Value) error {
	return nil
}

func (t *nullType) ConvertValue(val Value, _ Type) (Value, error) {
	return val, nil
}

// Variant constructs a node of unshredded VARIANT logical type. It is a group with
// two required fields, "metadata" and "value", both byte arrays.
//
// Experimental: The specification for variants is still being developed and the type
// is not fully adopted. Support for this type is subject to change.
//
// Initial support does not attempt to process the variant data. So reading and writing
// data of this type behaves as if it were just a group with two byte array fields, as
// if the logical type annotation were absent. This may change in the future.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#variant
func Variant() Node {
	return variantNode{Group{"metadata": Required(Leaf(ByteArrayType)), "value": Required(Leaf(ByteArrayType))}}
}

// TODO: add ShreddedVariant(Node) function, to create a shredded variant
//  where the argument defines the type/structure of the shredded value(s).

type variantNode struct{ Group }

func (variantNode) Type() Type { return &variantType{} }

type variantType format.VariantType

func (t *variantType) String() string { return (*format.VariantType)(t).String() }

func (t *variantType) Kind() Kind { panic("cannot call Kind on parquet VARIANT type") }

func (t *variantType) Length() int { return 0 }

func (t *variantType) EstimateSize(int) int { return 0 }

func (t *variantType) EstimateNumValues(int) int { return 0 }

func (t *variantType) Compare(Value, Value) int {
	panic("cannot compare values on parquet VARIANT type")
}

func (t *variantType) ColumnOrder() *format.ColumnOrder { return nil }

func (t *variantType) PhysicalType() *format.Type { return nil }

func (t *variantType) LogicalType() *format.LogicalType {
	return &format.LogicalType{Variant: (*format.VariantType)(t)}
}

func (t *variantType) ConvertedType() *deprecated.ConvertedType { return nil }

func (t *variantType) NewColumnIndexer(int) ColumnIndexer {
	panic("create create column indexer from parquet VARIANT type")
}

func (t *variantType) NewDictionary(int, int, encoding.Values) Dictionary {
	panic("cannot create dictionary from parquet VARIANT type")
}

func (t *variantType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet VARIANT type")
}

func (t *variantType) NewPage(int, int, encoding.Values) Page {
	panic("cannot create page from parquet VARIANT type")
}

func (t *variantType) NewValues(values []byte, _ []uint32) encoding.Values {
	panic("cannot create values from parquet VARIANT type")
}

func (t *variantType) Encode(_ []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	panic("cannot encode parquet VARIANT type")
}

func (t *variantType) Decode(_ encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	panic("cannot decode parquet VARIANT type")
}

func (t *variantType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	panic("cannot estimate decode size of parquet VARIANT type")
}

func (t *variantType) AssignValue(reflect.Value, Value) error {
	panic("cannot assign value to a parquet VARIANT type")
}

func (t *variantType) ConvertValue(Value, Type) (Value, error) {
	panic("cannot convert value to a parquet VARIANT type")
}

type groupType struct{}

func (groupType) String() string { return "group" }

func (groupType) Kind() Kind {
	panic("cannot call Kind on parquet group")
}

func (groupType) Compare(Value, Value) int {
	panic("cannot compare values on parquet group")
}

func (groupType) NewColumnIndexer(int) ColumnIndexer {
	panic("cannot create column indexer from parquet group")
}

func (groupType) NewDictionary(int, int, encoding.Values) Dictionary {
	panic("cannot create dictionary from parquet group")
}

func (t groupType) NewColumnBuffer(int, int) ColumnBuffer {
	panic("cannot create column buffer from parquet group")
}

func (t groupType) NewPage(int, int, encoding.Values) Page {
	panic("cannot create page from parquet group")
}

func (t groupType) NewValues(_ []byte, _ []uint32) encoding.Values {
	panic("cannot create values from parquet group")
}

func (groupType) Encode(_ []byte, _ encoding.Values, _ encoding.Encoding) ([]byte, error) {
	panic("cannot encode parquet group")
}

func (groupType) Decode(_ encoding.Values, _ []byte, _ encoding.Encoding) (encoding.Values, error) {
	panic("cannot decode parquet group")
}

func (groupType) EstimateDecodeSize(_ int, _ []byte, _ encoding.Encoding) int {
	panic("cannot estimate decode size of parquet group")
}

func (groupType) AssignValue(reflect.Value, Value) error {
	panic("cannot assign value to a parquet group")
}

func (t groupType) ConvertValue(Value, Type) (Value, error) {
	panic("cannot convert value to a parquet group")
}

func (groupType) Length() int { return 0 }

func (groupType) EstimateSize(int) int { return 0 }

func (groupType) EstimateNumValues(int) int { return 0 }

func (groupType) ColumnOrder() *format.ColumnOrder { return nil }

func (groupType) PhysicalType() *format.Type { return nil }

func (groupType) LogicalType() *format.LogicalType { return nil }

func (groupType) ConvertedType() *deprecated.ConvertedType { return nil }

func checkTypeKindEqual(to, from Type) error {
	if to.Kind() != from.Kind() {
		return fmt.Errorf("cannot convert from parquet value of type %s to %s", from, to)
	}
	return nil
}
