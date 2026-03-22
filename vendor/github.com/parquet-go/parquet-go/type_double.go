package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

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
