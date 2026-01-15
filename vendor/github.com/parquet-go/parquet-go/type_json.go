package parquet

import (
	"encoding/json"
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// JSON constructs a leaf node of JSON logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#json
func JSON() Node { return Leaf(&jsonType{}) }

var jsonLogicalType = format.LogicalType{
	Json: new(format.JsonType),
}

type jsonType format.JsonType

func (t *jsonType) String() string { return (*format.JsonType)(t).String() }

func (t *jsonType) Kind() Kind { return byteArrayType{}.Kind() }

func (t *jsonType) Length() int { return byteArrayType{}.Length() }

func (t *jsonType) EstimateSize(n int) int { return byteArrayType{}.EstimateSize(n) }

func (t *jsonType) EstimateNumValues(n int) int { return byteArrayType{}.EstimateNumValues(n) }

func (t *jsonType) Compare(a, b Value) int { return byteArrayType{}.Compare(a, b) }

func (t *jsonType) ColumnOrder() *format.ColumnOrder { return byteArrayType{}.ColumnOrder() }

func (t *jsonType) PhysicalType() *format.Type { return byteArrayType{}.PhysicalType() }

func (t *jsonType) LogicalType() *format.LogicalType { return &jsonLogicalType }

func (t *jsonType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Json]
}

func (t *jsonType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return byteArrayType{}.NewColumnIndexer(sizeLimit)
}

func (t *jsonType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return byteArrayType{}.NewDictionary(columnIndex, numValues, data)
}

func (t *jsonType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return byteArrayType{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *jsonType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return byteArrayType{}.NewPage(columnIndex, numValues, data)
}

func (t *jsonType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return byteArrayType{}.NewValues(values, offsets)
}

func (t *jsonType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return byteArrayType{}.Encode(dst, src, enc)
}

func (t *jsonType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return byteArrayType{}.Decode(dst, src, enc)
}

func (t *jsonType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return byteArrayType{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *jsonType) AssignValue(dst reflect.Value, src Value) error {
	// Assign value using ByteArrayType for BC...
	switch dst.Kind() {
	case reflect.String:
		return byteArrayType{}.AssignValue(dst, src)
	case reflect.Slice:
		if dst.Type().Elem().Kind() == reflect.Uint8 {
			return byteArrayType{}.AssignValue(dst, src)
		}
	}

	// Otherwise handle with json.Unmarshal
	b := src.byteArray()
	val := reflect.New(dst.Type()).Elem()
	err := json.Unmarshal(b, val.Addr().Interface())
	if err != nil {
		return err
	}
	dst.Set(val)
	return nil
}

func (t *jsonType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case byteArrayType, *stringType, *jsonType:
		return val, nil
	default:
		return val, invalidConversion(val, "JSON", typ.String())
	}
}
