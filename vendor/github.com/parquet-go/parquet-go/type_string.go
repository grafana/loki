package parquet

import (
	"bytes"
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// String constructs a leaf node of UTF8 logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#string
func String() Node { return Leaf(&stringType{}) }

var stringLogicalType = format.LogicalType{
	UTF8: new(format.StringType),
}

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
	return &stringLogicalType
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
