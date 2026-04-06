package parquet

import (
	"reflect"

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

// EqualTypes returns true if type1 and type2 are equal.
//
// Types are considered equal if they have the same Kind, Length, and LogicalType.
// The comparison uses reflect.DeepEqual for LogicalType comparison.
//
// Note: This function is designed for leaf types. For complex group types like
// MAP and LIST, use EqualNodes instead, as those types require structural comparison
// of their nested fields.
func EqualTypes(type1, type2 Type) bool {
	return equalKind(type1, type2) && equalLength(type1, type2) && equalLogicalTypes(type1, type2)
}

func equalKind(type1, type2 Type) bool {
	return type1.Kind() == type2.Kind()
}

func equalLength(type1, type2 Type) bool {
	return type1.Length() == type2.Length()
}

func equalLogicalTypes(type1, type2 Type) bool {
	return reflect.DeepEqual(type1.LogicalType(), type2.LogicalType())
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
