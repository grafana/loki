package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// Enum constructs a leaf node with a logical type representing enumerations.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#enum
func Enum() Node { return Leaf(&enumType{}) }

var enumLogicalType = format.LogicalType{
	Enum: new(format.EnumType),
}

type enumType format.EnumType

func (t *enumType) String() string { return (*format.EnumType)(t).String() }

func (t *enumType) Kind() Kind { return new(stringType).Kind() }

func (t *enumType) Length() int { return new(stringType).Length() }

func (t *enumType) EstimateSize(n int) int { return new(stringType).EstimateSize(n) }

func (t *enumType) EstimateNumValues(n int) int { return new(stringType).EstimateNumValues(n) }

func (t *enumType) Compare(a, b Value) int { return new(stringType).Compare(a, b) }

func (t *enumType) ColumnOrder() *format.ColumnOrder { return new(stringType).ColumnOrder() }

func (t *enumType) PhysicalType() *format.Type { return new(stringType).PhysicalType() }

func (t *enumType) LogicalType() *format.LogicalType { return &enumLogicalType }

func (t *enumType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Enum]
}

func (t *enumType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return new(stringType).NewColumnIndexer(sizeLimit)
}

func (t *enumType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return new(stringType).NewDictionary(columnIndex, numValues, data)
}

func (t *enumType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return new(stringType).NewColumnBuffer(columnIndex, numValues)
}

func (t *enumType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return new(stringType).NewPage(columnIndex, numValues, data)
}

func (t *enumType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return new(stringType).NewValues(values, offsets)
}

func (t *enumType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return new(stringType).Encode(dst, src, enc)
}

func (t *enumType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return new(stringType).Decode(dst, src, enc)
}

func (t *enumType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return new(stringType).EstimateDecodeSize(numValues, src, enc)
}

func (t *enumType) AssignValue(dst reflect.Value, src Value) error {
	return new(stringType).AssignValue(dst, src)
}

func (t *enumType) ConvertValue(val Value, typ Type) (Value, error) {
	switch typ.(type) {
	case byteArrayType, *stringType, *enumType:
		return val, nil
	default:
		return val, invalidConversion(val, "ENUM", typ.String())
	}
}
