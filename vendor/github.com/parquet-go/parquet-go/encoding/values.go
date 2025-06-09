package encoding

import (
	"fmt"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/internal/unsafecast"
)

type Kind int32

const (
	Undefined Kind = iota
	Boolean
	Int32
	Int64
	Int96
	Float
	Double
	ByteArray
	FixedLenByteArray
)

func (kind Kind) String() string {
	switch kind {
	case Boolean:
		return "BOOLEAN"
	case Int32:
		return "INT32"
	case Int64:
		return "INT64"
	case Int96:
		return "INT96"
	case Float:
		return "FLOAT"
	case Double:
		return "DOUBLE"
	case ByteArray:
		return "BYTE_ARRAY"
	case FixedLenByteArray:
		return "FIXED_LEN_BYTE_ARRAY"
	default:
		return "UNDEFINED"
	}
}

type Values struct {
	kind    Kind
	size    int32
	data    []byte
	offsets []uint32
}

func (v *Values) assertKind(kind Kind) {
	if kind != v.kind {
		panic(fmt.Sprintf("cannot convert values of type %s to type %s", v.kind, kind))
	}
}

func (v *Values) assertSize(size int) {
	if size != int(v.size) {
		panic(fmt.Sprintf("cannot convert values of size %d to size %d", v.size, size))
	}
}

func (v *Values) Size() int64 {
	return int64(len(v.data))
}

func (v *Values) Kind() Kind {
	return v.kind
}

func (v *Values) Data() (data []byte, offsets []uint32) {
	return v.data, v.offsets
}

func (v *Values) Boolean() []byte {
	v.assertKind(Boolean)
	return v.data
}

func (v *Values) Int32() []int32 {
	v.assertKind(Int32)
	return unsafecast.Slice[int32](v.data)
}

func (v *Values) Int64() []int64 {
	v.assertKind(Int64)
	return unsafecast.Slice[int64](v.data)
}

func (v *Values) Int96() []deprecated.Int96 {
	v.assertKind(Int96)
	return unsafecast.Slice[deprecated.Int96](v.data)
}

func (v *Values) Float() []float32 {
	v.assertKind(Float)
	return unsafecast.Slice[float32](v.data)
}

func (v *Values) Double() []float64 {
	v.assertKind(Double)
	return unsafecast.Slice[float64](v.data)
}

func (v *Values) ByteArray() (data []byte, offsets []uint32) {
	v.assertKind(ByteArray)
	return v.data, v.offsets
}

func (v *Values) FixedLenByteArray() (data []byte, size int) {
	v.assertKind(FixedLenByteArray)
	return v.data, int(v.size)
}

func (v *Values) Uint32() []uint32 {
	v.assertKind(Int32)
	return unsafecast.Slice[uint32](v.data)
}

func (v *Values) Uint64() []uint64 {
	v.assertKind(Int64)
	return unsafecast.Slice[uint64](v.data)
}

func (v *Values) Uint128() [][16]byte {
	v.assertKind(FixedLenByteArray)
	v.assertSize(16)
	return unsafecast.Slice[[16]byte](v.data)
}

func makeValues[T any](kind Kind, values []T) Values {
	return Values{kind: kind, data: unsafecast.Slice[byte](values)}
}

func BooleanValues(values []byte) Values {
	return makeValues(Boolean, values)
}

func Int32Values(values []int32) Values {
	return makeValues(Int32, values)
}

func Int64Values(values []int64) Values {
	return makeValues(Int64, values)
}

func Int96Values(values []deprecated.Int96) Values {
	return makeValues(Int96, values)
}

func FloatValues(values []float32) Values {
	return makeValues(Float, values)
}

func DoubleValues(values []float64) Values {
	return makeValues(Double, values)
}

func ByteArrayValues(values []byte, offsets []uint32) Values {
	return Values{kind: ByteArray, data: values, offsets: offsets}
}

func FixedLenByteArrayValues(values []byte, size int) Values {
	return Values{kind: FixedLenByteArray, size: int32(size), data: values}
}

func Uint32Values(values []uint32) Values {
	return Int32Values(unsafecast.Slice[int32](values))
}

func Uint64Values(values []uint64) Values {
	return Int64Values(unsafecast.Slice[int64](values))
}

func Uint128Values(values [][16]byte) Values {
	return FixedLenByteArrayValues(unsafecast.Slice[byte](values), 16)
}

func Int32ValuesFromBytes(values []byte) Values {
	return Values{kind: Int32, data: values}
}

func Int64ValuesFromBytes(values []byte) Values {
	return Values{kind: Int64, data: values}
}

func Int96ValuesFromBytes(values []byte) Values {
	return Values{kind: Int96, data: values}
}

func FloatValuesFromBytes(values []byte) Values {
	return Values{kind: Float, data: values}
}

func DoubleValuesFromBytes(values []byte) Values {
	return Values{kind: Double, data: values}
}

func EncodeBoolean(dst []byte, src Values, enc Encoding) ([]byte, error) {
	return enc.EncodeBoolean(dst, src.Boolean())
}

func EncodeInt32(dst []byte, src Values, enc Encoding) ([]byte, error) {
	return enc.EncodeInt32(dst, src.Int32())
}

func EncodeInt64(dst []byte, src Values, enc Encoding) ([]byte, error) {
	return enc.EncodeInt64(dst, src.Int64())
}

func EncodeInt96(dst []byte, src Values, enc Encoding) ([]byte, error) {
	return enc.EncodeInt96(dst, src.Int96())
}

func EncodeFloat(dst []byte, src Values, enc Encoding) ([]byte, error) {
	return enc.EncodeFloat(dst, src.Float())
}

func EncodeDouble(dst []byte, src Values, enc Encoding) ([]byte, error) {
	return enc.EncodeDouble(dst, src.Double())
}

func EncodeByteArray(dst []byte, src Values, enc Encoding) ([]byte, error) {
	values, offsets := src.ByteArray()
	return enc.EncodeByteArray(dst, values, offsets)
}

func EncodeFixedLenByteArray(dst []byte, src Values, enc Encoding) ([]byte, error) {
	data, size := src.FixedLenByteArray()
	return enc.EncodeFixedLenByteArray(dst, data, size)
}

func DecodeBoolean(dst Values, src []byte, enc Encoding) (Values, error) {
	values, err := enc.DecodeBoolean(dst.Boolean(), src)
	return BooleanValues(values), err
}

func DecodeInt32(dst Values, src []byte, enc Encoding) (Values, error) {
	values, err := enc.DecodeInt32(dst.Int32(), src)
	return Int32Values(values), err
}

func DecodeInt64(dst Values, src []byte, enc Encoding) (Values, error) {
	values, err := enc.DecodeInt64(dst.Int64(), src)
	return Int64Values(values), err
}

func DecodeInt96(dst Values, src []byte, enc Encoding) (Values, error) {
	values, err := enc.DecodeInt96(dst.Int96(), src)
	return Int96Values(values), err
}

func DecodeFloat(dst Values, src []byte, enc Encoding) (Values, error) {
	values, err := enc.DecodeFloat(dst.Float(), src)
	return FloatValues(values), err
}

func DecodeDouble(dst Values, src []byte, enc Encoding) (Values, error) {
	values, err := enc.DecodeDouble(dst.Double(), src)
	return DoubleValues(values), err
}

func DecodeByteArray(dst Values, src []byte, enc Encoding) (Values, error) {
	values, offsets := dst.ByteArray()
	values, offsets, err := enc.DecodeByteArray(values, src, offsets)
	return ByteArrayValues(values, offsets), err
}

func DecodeFixedLenByteArray(dst Values, src []byte, enc Encoding) (Values, error) {
	data, size := dst.FixedLenByteArray()
	values, err := enc.DecodeFixedLenByteArray(data, src, size)
	return FixedLenByteArrayValues(values, size), err
}
