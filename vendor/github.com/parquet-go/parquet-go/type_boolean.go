package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

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
