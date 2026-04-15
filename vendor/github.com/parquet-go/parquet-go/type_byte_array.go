package parquet

import (
	"bytes"
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

type byteArrayType struct{}

func (t byteArrayType) String() string                           { return "BYTE_ARRAY" }
func (t byteArrayType) Kind() Kind                               { return ByteArray }
func (t byteArrayType) Length() int                              { return 0 }
func (t byteArrayType) EstimateSize(n int) int                   { return estimatedSizeOfByteArrayValues * n }
func (t byteArrayType) EstimateNumValues(n int) int              { return n / estimatedSizeOfByteArrayValues }
func (t byteArrayType) Compare(a, b Value) int                   { return bytes.Compare(a.byteArray(), b.byteArray()) }
func (t byteArrayType) ColumnOrder() *format.ColumnOrder         { return &typeDefinedColumnOrder }
func (t byteArrayType) LogicalType() *format.LogicalType         { return nil }
func (t byteArrayType) ConvertedType() *deprecated.ConvertedType { return nil }
func (t byteArrayType) PhysicalType() *format.Type               { return &physicalTypes[ByteArray] }

func (t byteArrayType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return newByteArrayColumnIndexer(sizeLimit)
}

func (t byteArrayType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return newByteArrayColumnBuffer(t, makeColumnIndex(columnIndex), makeNumValues(numValues))
}

func (t byteArrayType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return newByteArrayDictionary(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t byteArrayType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return newByteArrayPage(t, makeColumnIndex(columnIndex), makeNumValues(numValues), data)
}

func (t byteArrayType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return encoding.ByteArrayValues(values, offsets)
}

func (t byteArrayType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return encoding.EncodeByteArray(dst, src, enc)
}

func (t byteArrayType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return encoding.DecodeByteArray(dst, src, enc)
}

func (t byteArrayType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return enc.EstimateDecodeByteArraySize(src)
}

func (t byteArrayType) AssignValue(dst reflect.Value, src Value) error {
	v := src.byteArray()
	switch dst.Kind() {
	case reflect.String:
		dst.SetString(string(v))
	case reflect.Slice:
		dst.SetBytes(copyBytes(v))
	case reflect.Ptr:
		// Handle pointer types like *string
		if src.IsNull() {
			dst.Set(reflect.Zero(dst.Type()))
		} else {
			// Allocate a new value of the element type
			elem := reflect.New(dst.Type().Elem())
			if err := t.AssignValue(elem.Elem(), src); err != nil {
				return err
			}
			dst.Set(elem)
		}
	default:
		val := reflect.ValueOf(string(v))
		dst.Set(val)
	}
	return nil
}

func (t byteArrayType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.Kind() {
	case Boolean:
		return convertBooleanToByteArray(val)
	case Int32:
		return convertInt32ToByteArray(val)
	case Int64:
		return convertInt64ToByteArray(val)
	case Int96:
		return convertInt96ToByteArray(val)
	case Float:
		return convertFloatToByteArray(val)
	case Double:
		return convertDoubleToByteArray(val)
	case ByteArray, FixedLenByteArray:
		return val, nil
	default:
		return makeValueKind(ByteArray), nil
	}
}
