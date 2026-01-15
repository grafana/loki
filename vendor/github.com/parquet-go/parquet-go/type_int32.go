package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

type int32Type struct{}

func (t int32Type) String() string                   { return "INT32" }
func (t int32Type) Kind() Kind                       { return Int32 }
func (t int32Type) Length() int                      { return 32 }
func (t int32Type) EstimateSize(n int) int           { return 4 * n }
func (t int32Type) EstimateNumValues(n int) int      { return n / 4 }
func (t int32Type) Compare(a, b Value) int           { return compareInt32(a.int32(), b.int32()) }
func (t int32Type) ColumnOrder() *format.ColumnOrder { return &typeDefinedColumnOrder }
func (t int32Type) LogicalType() *format.LogicalType {
	return &format.LogicalType{Integer: &format.IntType{
		BitWidth: 32,
		IsSigned: true,
	}}
}
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
