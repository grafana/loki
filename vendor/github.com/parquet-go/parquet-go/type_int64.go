package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

type int64Type struct{}

func (t int64Type) String() string                   { return "INT64" }
func (t int64Type) Kind() Kind                       { return Int64 }
func (t int64Type) Length() int                      { return 64 }
func (t int64Type) EstimateSize(n int) int           { return 8 * n }
func (t int64Type) EstimateNumValues(n int) int      { return n / 8 }
func (t int64Type) Compare(a, b Value) int           { return compareInt64(a.int64(), b.int64()) }
func (t int64Type) ColumnOrder() *format.ColumnOrder { return &typeDefinedColumnOrder }
func (t int64Type) LogicalType() *format.LogicalType {
	return &format.LogicalType{Integer: &format.IntType{
		BitWidth: 64,
		IsSigned: true,
	}}
}
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
