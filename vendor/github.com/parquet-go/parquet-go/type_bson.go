package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// BSON constructs a leaf node of BSON logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#bson
func BSON() Node { return Leaf(&bsonType{}) }

var bsonLogicalType = format.LogicalType{
	Bson: new(format.BsonType),
}

type bsonType format.BsonType

func (t *bsonType) String() string { return (*format.BsonType)(t).String() }

func (t *bsonType) Kind() Kind { return byteArrayType{}.Kind() }

func (t *bsonType) Length() int { return byteArrayType{}.Length() }

func (t *bsonType) EstimateSize(n int) int { return byteArrayType{}.EstimateSize(n) }

func (t *bsonType) EstimateNumValues(n int) int { return byteArrayType{}.EstimateNumValues(n) }

func (t *bsonType) Compare(a, b Value) int { return byteArrayType{}.Compare(a, b) }

func (t *bsonType) ColumnOrder() *format.ColumnOrder { return byteArrayType{}.ColumnOrder() }

func (t *bsonType) PhysicalType() *format.Type { return byteArrayType{}.PhysicalType() }

func (t *bsonType) LogicalType() *format.LogicalType { return &bsonLogicalType }

func (t *bsonType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Bson]
}

func (t *bsonType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return byteArrayType{}.NewColumnIndexer(sizeLimit)
}

func (t *bsonType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return byteArrayType{}.NewDictionary(columnIndex, numValues, data)
}

func (t *bsonType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return byteArrayType{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *bsonType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return byteArrayType{}.NewPage(columnIndex, numValues, data)
}

func (t *bsonType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return byteArrayType{}.NewValues(values, offsets)
}

func (t *bsonType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return byteArrayType{}.Encode(dst, src, enc)
}

func (t *bsonType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return byteArrayType{}.Decode(dst, src, enc)
}

func (t *bsonType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return byteArrayType{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *bsonType) AssignValue(dst reflect.Value, src Value) error {
	return byteArrayType{}.AssignValue(dst, src)
}

func (t *bsonType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case byteArrayType, *bsonType:
		return val, nil
	default:
		return val, invalidConversion(val, "BSON", typ.String())
	}
}
