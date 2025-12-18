package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

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
