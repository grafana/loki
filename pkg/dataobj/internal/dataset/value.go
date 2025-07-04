package dataset

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// InvalidTypeError is used as a panic value when using [Value] methods with
// the incorrect type.
type InavlidTypeError struct {
	Expected datasetmd.ValueType
	Actual   datasetmd.ValueType
}

// Error returns a string representation denoting the expected and actual
// types.
func (e *InavlidTypeError) Error() string {
	return fmt.Sprintf("invalid type: expected %s, got %s", e.Expected, e.Actual)
}

// UnsupportedTypeError is used as a panic value when using [Value] methods with
// an unsupported type.
type UnsupportedTypeError struct {
	Got datasetmd.ValueType
}

// Error returns a string representation denoting the unsupported type.
func (e *UnsupportedTypeError) Error() string {
	return fmt.Sprintf("unsupported type: %s", e.Got)
}

// A Value represents a single value within a dataset. Unlike [any], Values can
// be constructed without allocations. The zero Value corresponds to nil.
type Value struct {
	// The internal representation of Value is designed to avoid allocations by
	// using a fixed-size struct that can represent all supported types without
	// needing to allocate memory for each value (such as wrapping a value into
	// an interface).
	//
	// As a side effect of this, Value is heavy on the stack, costing at least 28
	// bytes for 64-bit builds. This cost is reduced by using pointer receivers
	// wherever possible.

	_ [0]func() // Disallow equality checking of two Values

	// kind holds the type of the value.
	kind datasetmd.ValueType

	// num holds the value for numeric kinds, or the string length for string
	// kinds.
	num uint64

	// cap holds the capacity of the underlying memory in data.
	cap uint64

	// data optionally holds a pointer to the start of a byte slice. When data is
	// specified, num is the length of the byte slice, and cap is the capacity.
	//
	// data can be set even if kind is not [datasetmd.VALUE_TYPE_BYTE_ARRAY]. In
	// that case, data can still be used to access the underlying memory for
	// reuse via [Value.Buffer].
	data *byte
}

// Int64Value rerturns a [Value] for an int64.
func Int64Value(v int64) Value {
	return Value{
		kind: datasetmd.VALUE_TYPE_INT64,
		num:  uint64(v),
	}
}

// Uint64Value returns a [Value] for a uint64.
func Uint64Value(v uint64) Value {
	return Value{
		kind: datasetmd.VALUE_TYPE_UINT64,
		num:  v,
	}
}

// ByteArrayValue returns a [Value] for a byte slice representing a string.
func ByteArrayValue(v []byte) Value {
	return Value{
		kind: datasetmd.VALUE_TYPE_BYTE_ARRAY,
		num:  uint64(len(v)),
		cap:  uint64(cap(v)),
		data: unsafe.SliceData(v),
	}
}

// IsNil returns whether v is nil.
func (v *Value) IsNil() bool {
	return v.Type() == datasetmd.VALUE_TYPE_UNSPECIFIED
}

// IsZero reports whether v is the zero value.
func (v *Value) IsZero() bool {
	return v.IsNil() || v.num == 0
}

// Type returns the [datasetmd.ValueType] of v. If v is nil, Type returns
// [datasetmd.VALUE_TYPE_UNSPECIFIED].
func (v *Value) Type() datasetmd.ValueType {
	if v == nil {
		return datasetmd.VALUE_TYPE_UNSPECIFIED
	}
	return v.kind
}

// Int64 returns v's value as an int64. It panics if v is not a
// [datasetmd.VALUE_TYPE_INT64].
func (v *Value) Int64() int64 {
	if expect, actual := datasetmd.VALUE_TYPE_INT64, v.Type(); expect != actual {
		panic(&InavlidTypeError{expect, actual})
	}
	return v.int64()
}

func (v *Value) int64() int64 { return int64(v.num) }

// Uint64 returns v's value as a uint64. It panics if v is not a
// [datasetmd.VALUE_TYPE_UINT64].
func (v *Value) Uint64() uint64 {
	if expect, actual := datasetmd.VALUE_TYPE_UINT64, v.Type(); expect != actual {
		panic(&InavlidTypeError{expect, actual})
	}
	return v.uint64()
}

func (v *Value) uint64() uint64 { return v.num }

// ByteSlice returns v's value as a byte slice. If v is not a string,
// ByteSlice returns a byte slice of the form "VALUE_TYPE_T", where T is the
// underlying type of v.
func (v *Value) ByteArray() []byte {
	if expect, actual := datasetmd.VALUE_TYPE_BYTE_ARRAY, v.Type(); expect != actual {
		panic(&InavlidTypeError{expect, actual})
	}
	return v.byteArray()
}

// Buffer returns any memory that was allocated for v, even if v is currently
// null.
//
// If Value does not hold underlying memory, Buffer returns nil.
func (v *Value) Buffer() []byte {
	return v.byteArray()
}

func (v *Value) byteArray() []byte {
	if v.data == nil {
		return nil
	}

	// v.data can only be non-nil if it was previously used as a
	// [datasetmd.VALUE_TYPE_BYTE_ARRAY].
	//
	// If this is the case, it's safe to interpret v.num and v.cap as the
	// length/cap, since there's no way to change the type of a Value other than
	// from a non-NULL type to a NULL type.
	return unsafe.Slice(v.data, v.cap)[:v.num]
}

// Zero sets Value to its zero state while retaining any underlying memory if
// Value was a [datasetmd.VALUE_TYPE_BYTE_ARRAY]. After calling Zero,
// [Value.IsNil] and [Value.IsZero] will both report true.
//
// However, [Value.ByteArray] will continue to return the underlying memory.
func (v *Value) Zero() {
	v.kind = datasetmd.VALUE_TYPE_UNSPECIFIED
}

// MarshalBinary encodes v into a binary representation. Non-NULL values encode
// first with the type (encoded as uvarint), followed by an encoded value,
// where:
//
//   - [datasetmd.VALUE_TYPE_INT64] encodes as a varint.
//   - [datasetmd.VALUE_TYPE_UINT64] encodes as a uvarint.
//   - [datasetmd.VALUE_TYPE_STRING] encodes the string as a sequence of bytes.
//
// NULL values encode as nil.
func (v Value) MarshalBinary() (data []byte, err error) {
	if v.IsNil() {
		return nil, nil
	}

	buf := binary.AppendUvarint(nil, uint64(v.Type()))

	switch v.Type() {
	case datasetmd.VALUE_TYPE_INT64:
		buf = binary.AppendVarint(buf, v.Int64())
	case datasetmd.VALUE_TYPE_UINT64:
		buf = binary.AppendUvarint(buf, v.Uint64())
	case datasetmd.VALUE_TYPE_BYTE_ARRAY:
		buf = append(buf, v.ByteArray()...)
	default:
		return nil, fmt.Errorf("dataset.Value.MarshalBinary: unsupported type %s", v.Type())
	}

	return buf, nil
}

// UnmarshalBinary decodes a Value from a binary representation. See
// [Value.MarshalBinary] for the encoding format.
func (v *Value) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		*v = Value{} // NULL
		return nil
	}

	typ, n := binary.Uvarint(data)
	if n <= 0 {
		return fmt.Errorf("dataset.Value.UnmarshalBinary: invalid type")
	}

	switch vtyp := datasetmd.ValueType(typ); vtyp {
	case datasetmd.VALUE_TYPE_INT64:
		val, n := binary.Varint(data[n:])
		if n <= 0 {
			return fmt.Errorf("dataset.Value.UnmarshalBinary: invalid int64 value")
		}
		*v = Int64Value(val)
	case datasetmd.VALUE_TYPE_UINT64:
		val, n := binary.Uvarint(data[n:])
		if n <= 0 {
			return fmt.Errorf("dataset.Value.UnmarshalBinary: invalid uint64 value")
		}
		*v = Uint64Value(val)
	case datasetmd.VALUE_TYPE_BYTE_ARRAY:
		*v = ByteArrayValue(data[n:])
	default:
		return fmt.Errorf("dataset.Value.UnmarshalBinary: unsupported type %s", vtyp)
	}

	return nil
}

// Size returns the size of v in bytes when encoded.
func (v Value) Size() int {
	switch v.Type() {
	case datasetmd.VALUE_TYPE_INT64:
		return int(unsafe.Sizeof(int64(0)))
	case datasetmd.VALUE_TYPE_UINT64:
		return int(unsafe.Sizeof(uint64(0)))
	case datasetmd.VALUE_TYPE_BYTE_ARRAY:
		return int(v.num)
	case datasetmd.VALUE_TYPE_UNSPECIFIED:
		return 0
	default:
		panic(&UnsupportedTypeError{v.Type()})
	}
}

// CompareValues returns -1 if a<b, 0 if a==b, or 1 if a>b. CompareValues
// panics if a and b are not the same type.
//
// As a special case, either a or b may be nil. Two nil values are equal, and a
// nil value is always less than a non-nil value.
func CompareValues(a, b *Value) int {
	var (
		aType, bType = a.Type(), b.Type()
		aNil, bNil   = aType == datasetmd.VALUE_TYPE_UNSPECIFIED, bType == datasetmd.VALUE_TYPE_UNSPECIFIED
	)

	// Handle nil values first to avoid the panic if the types don't match.
	switch {
	case aNil && !bNil:
		return -1
	case !aNil && bNil:
		return 1
	case aNil && bNil:
		return 0

	case aType != bType:
		panic(&InavlidTypeError{aType, bType})

	case aType == datasetmd.VALUE_TYPE_INT64:
		return cmpInteger(a.int64(), b.int64())

	case aType == datasetmd.VALUE_TYPE_UINT64:
		return cmpInteger(a.uint64(), b.uint64())

	case aType == datasetmd.VALUE_TYPE_BYTE_ARRAY:
		return bytes.Compare(a.byteArray(), b.byteArray())
	}

	panic(&UnsupportedTypeError{a.Type()})
}

func cmpInteger[T int64 | uint64](a, b T) int {
	if a < b {
		return -1
	} else if a > b {
		return 1
	}
	return 0
}
