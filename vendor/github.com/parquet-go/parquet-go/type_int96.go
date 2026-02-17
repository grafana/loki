package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

type int96Type struct{}

func (t int96Type) String() string { return "INT96" }

func (t int96Type) Kind() Kind                               { return Int96 }
func (t int96Type) Length() int                              { return 96 }
func (t int96Type) EstimateSize(n int) int                   { return 12 * n }
func (t int96Type) EstimateNumValues(n int) int              { return n / 12 }
func (t int96Type) Compare(a, b Value) int                   { return compareInt96(a.int96(), b.int96()) }
func (t int96Type) ColumnOrder() *format.ColumnOrder         { return &typeDefinedColumnOrder }
func (t int96Type) LogicalType() *format.LogicalType         { return nil }
func (t int96Type) ConvertedType() *deprecated.ConvertedType { return nil }
func (t int96Type) PhysicalType() *format.Type               { return &physicalTypes[Int96] }

func (t int96Type) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newInt96ColumnIndexer()
}

func (t int96Type) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newInt96ColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t int96Type) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newInt96Dictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t int96Type) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newInt96Page(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t int96Type) NewValues(values []byte, _ []uint32) encoding.Values {
	return encoding.Int96ValuesFromBytes(values)
}

func (t int96Type) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeInt96(dst, src, enc)
}

func (t int96Type) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeInt96(dst, src, enc)
}

func (t int96Type) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.EstimateSize(numValues)
}

func (t int96Type) AssignValue(dst reflect.Value, src Value) error {
	v := src.Int96()
	dst.Set(reflect.ValueOf(v))
	return nil
}

func (t int96Type) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case *stringType:
		return convertStringToInt96(val)
	}
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToInt96(val)
	case Int32:
		return convertInt32ToInt96(val)
	case Int64:
		return convertInt64ToInt96(val)
	case Int96:
		return val, nil
	case Float:
		return convertFloatToInt96(val)
	case Double:
		return convertDoubleToInt96(val)
	case ByteArray, FixedLenByteArray:
		return convertByteArrayToInt96(val)
	default:
		return makeValueKind(Int96), nil
	}
}
