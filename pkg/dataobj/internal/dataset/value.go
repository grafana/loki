package dataset

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// Helper types
type (
	stringptr *byte
	bytearray *byte
)

// A Value represents a single value within a dataset. Unlike [any], Values can
// be constructed without allocations. The zero Value corresponds to nil.
type Value struct {
	// The internal representation of Value is based on log/slog.Value, which is
	// also designed to avoid allocations.
	//
	// While usage of any typically causes an allocation (due to any being a fat
	// pointer), our usage avoids it:
	//
	// * Go will avoid allocating integer values that can be stored in a single
	//   byte, which applies to datasetmd.ValueType.
	//
	// * If any is referring to a pointer, then wrapping the poitner in an any
	//   does not cause an allocation. This is why we use stringptr instead of a
	//   string.

	_ [0]func() // Disallow equality checking of two Values

	// num holds the value for numeric types, or the string length for string
	// types.
	num uint64

	// If any is of type [datasetmd.ValueType], then the value is in num as
	// described above.
	//
	// If any is of type stringptr, then the value is of type
	// [datasetmd.VALUE_TYPE_STRING] and the string value consists of the length
	// in num and the pointer in any.
	any any
}

// Int64Value rerturns a [Value] for an int64.
func Int64Value(v int64) Value {
	return Value{
		num: uint64(v),
		any: datasetmd.VALUE_TYPE_INT64,
	}
}

// Uint64Value returns a [Value] for a uint64.
func Uint64Value(v uint64) Value {
	return Value{
		num: v,
		any: datasetmd.VALUE_TYPE_UINT64,
	}
}

// StringValue returns a [Value] for a string.
func StringValue(v string) Value {
	return Value{
		num: uint64(len(v)),
		any: (stringptr)(unsafe.StringData(v)),
	}
}

// ByteArrayValue returns a [Value] for a byte slice representing a string.
func ByteArrayValue(v []byte) Value {
	return Value{
		num: uint64(len(v)),
		any: (bytearray)(unsafe.SliceData(v)),
	}
}

// IsNil returns whether v is nil.
func (v Value) IsNil() bool {
	return v.any == nil
}

// IsZero reports whether v is the zero value.
func (v Value) IsZero() bool {
	// If Value is a numeric type, v.num == 0 checks if it's the zero value. For
	// string types, v.num == 0 means the string is empty.
	return v.num == 0
}

// Type returns the [datasetmd.ValueType] of v. If v is nil, Type returns
// [datasetmd.VALUE_TYPE_UNSPECIFIED].
func (v Value) Type() datasetmd.ValueType {
	if v.IsNil() {
		return datasetmd.VALUE_TYPE_UNSPECIFIED
	}

	switch v := v.any.(type) {
	case datasetmd.ValueType:
		return v
	case stringptr:
		return datasetmd.VALUE_TYPE_STRING
	case bytearray:
		return datasetmd.VALUE_TYPE_BYTE_ARRAY
	default:
		panic(fmt.Sprintf("dataset.Value has unexpected type %T", v))
	}
}

// Int64 returns v's value as an int64. It panics if v is not a
// [datasetmd.VALUE_TYPE_INT64].
func (v Value) Int64() int64 {
	if expect, actual := datasetmd.VALUE_TYPE_INT64, v.Type(); expect != actual {
		panic(fmt.Sprintf("dataset.Value type is %s, not %s", actual, expect))
	}
	return int64(v.num)
}

// Uint64 returns v's value as a uint64. It panics if v is not a
// [datasetmd.VALUE_TYPE_UINT64].
func (v Value) Uint64() uint64 {
	if expect, actual := datasetmd.VALUE_TYPE_UINT64, v.Type(); expect != actual {
		panic(fmt.Sprintf("dataset.Value type is %s, not %s", actual, expect))
	}
	return v.num
}

// String returns v's value as a string. Because of Go's String method
// convention, if v is not a string, String returns a string of the form
// "VALUE_TYPE_T", where T is the underlying type of v.
func (v Value) String() string {
	if sp, ok := v.any.(stringptr); ok {
		return unsafe.String(sp, v.num)
	}
	return v.Type().String()
}

// ByteSlice returns v's value as a byte slice. If v is not a string,
// ByteSlice returns a byte slice of the form "VALUE_TYPE_T", where T is the
// underlying type of v.
func (v Value) ByteArray() []byte {
	if ba, ok := v.any.(bytearray); ok {
		return unsafe.Slice(ba, v.num)
	}
	panic(fmt.Sprintf("dataset.Value type is %s, not %s", v.Type(), datasetmd.VALUE_TYPE_BYTE_ARRAY))
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
	case datasetmd.VALUE_TYPE_STRING:
		str := v.String()
		buf = append(buf, unsafe.Slice(unsafe.StringData(str), len(str))...)
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
	case datasetmd.VALUE_TYPE_STRING:
		str := string(data[n:])
		*v = StringValue(str)
	case datasetmd.VALUE_TYPE_BYTE_ARRAY:
		*v = ByteArrayValue(data[n:])
	default:
		return fmt.Errorf("dataset.Value.UnmarshalBinary: unsupported type %s", vtyp)
	}

	return nil
}

// CompareValues returns -1 if a<b, 0 if a==b, or 1 if a>b. CompareValues
// panics if a and b are not the same type.
//
// As a special case, either a or b may be nil. Two nil values are equal, and a
// nil value is always less than a non-nil value.
func CompareValues(a, b Value) int {
	// nil handling. This must be done before the typecheck since nil has a
	// special type.
	switch {
	case a.IsNil() && !b.IsNil():
		return -1
	case !a.IsNil() && b.IsNil():
		return 1
	case a.IsNil() && b.IsNil():
		return 0
	}

	if a.Type() != b.Type() {
		panic(fmt.Sprintf("page.CompareValues: cannot compare values of type %s and %s", a.Type(), b.Type()))
	}

	switch a.Type() {
	case datasetmd.VALUE_TYPE_INT64:
		return cmp.Compare(a.Int64(), b.Int64())
	case datasetmd.VALUE_TYPE_UINT64:
		return cmp.Compare(a.Uint64(), b.Uint64())
	case datasetmd.VALUE_TYPE_STRING:
		return cmp.Compare(a.String(), b.String())
	case datasetmd.VALUE_TYPE_BYTE_ARRAY:
		return bytes.Compare(a.ByteArray(), b.ByteArray())
	default:
		panic(fmt.Sprintf("page.CompareValues: unsupported type %s", a.Type()))
	}
}
