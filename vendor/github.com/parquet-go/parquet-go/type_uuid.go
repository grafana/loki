package parquet

import (
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// UUID constructs a leaf node of UUID logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#uuid
func UUID() Node { return Leaf(&uuidType{}) }

var uuidLogicaType = format.LogicalType{
	UUID: new(format.UUIDType),
}

type uuidType format.UUIDType

func (t *uuidType) String() string { return (*format.UUIDType)(t).String() }

func (t *uuidType) Kind() Kind { return be128Type{}.Kind() }

func (t *uuidType) Length() int { return be128Type{}.Length() }

func (t *uuidType) EstimateSize(n int) int { return be128Type{}.EstimateSize(n) }

func (t *uuidType) EstimateNumValues(n int) int { return be128Type{}.EstimateNumValues(n) }

func (t *uuidType) Compare(a, b Value) int { return be128Type{}.Compare(a, b) }

func (t *uuidType) ColumnOrder() *format.ColumnOrder { return &typeDefinedColumnOrder }

func (t *uuidType) PhysicalType() *format.Type { return &physicalTypes[FixedLenByteArray] }

func (t *uuidType) LogicalType() *format.LogicalType { return &uuidLogicaType }

func (t *uuidType) ConvertedType() *deprecated.ConvertedType { return nil }

func (t *uuidType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return be128Type{isUUID: true}.NewColumnIndexer(sizeLimit)
}

func (t *uuidType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return be128Type{isUUID: true}.NewDictionary(columnIndex, numValues, data)
}

func (t *uuidType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return be128Type{isUUID: true}.NewColumnBuffer(columnIndex, numValues)
}

func (t *uuidType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return be128Type{isUUID: true}.NewPage(columnIndex, numValues, data)
}

func (t *uuidType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return be128Type{isUUID: true}.NewValues(values, offsets)
}

func (t *uuidType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return be128Type{isUUID: true}.Encode(dst, src, enc)
}

func (t *uuidType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return be128Type{isUUID: true}.Decode(dst, src, enc)
}

func (t *uuidType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return be128Type{isUUID: true}.EstimateDecodeSize(numValues, src, enc)
}

func (t *uuidType) AssignValue(dst reflect.Value, src Value) error {
	return be128Type{isUUID: true}.AssignValue(dst, src)
}

func (t *uuidType) ConvertValue(val Value, typ Type) (Value, error) {
	return be128Type{isUUID: true}.ConvertValue(val, typ)
}
