package parquet

import (
	"encoding/binary"
	"reflect"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// Interval represents a Parquet INTERVAL value.
//
// The physical representation is a FIXED_LEN_BYTE_ARRAY(12) containing three
// little-endian unsigned 32-bit integers: months, days, and milliseconds.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#interval
type Interval struct {
	Months       uint32
	Days         uint32
	Milliseconds uint32
}

// IntervalNode constructs a leaf node of INTERVAL logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#interval
func IntervalNode() Node { return Leaf(&intervalType{}) }

// intervalType implements the INTERVAL logical type. INTERVAL has no formal
// LogicalType entry in the Parquet Thrift spec (field 9 is reserved but
// undefined); it is expressed solely via ConvertedType = 21 (deprecated.Interval).
type intervalType struct{}

func (t *intervalType) String() string { return "INTERVAL" }

func (t *intervalType) Kind() Kind { return fixedLenByteArrayType{length: 12}.Kind() }

func (t *intervalType) Length() int { return fixedLenByteArrayType{length: 12}.Length() }

func (t *intervalType) EstimateSize(n int) int {
	return fixedLenByteArrayType{length: 12}.EstimateSize(n)
}

func (t *intervalType) EstimateNumValues(n int) int {
	return fixedLenByteArrayType{length: 12}.EstimateNumValues(n)
}

func (t *intervalType) Compare(a, b Value) int {
	return fixedLenByteArrayType{length: 12}.Compare(a, b)
}

func (t *intervalType) ColumnOrder() *format.ColumnOrder {
	return fixedLenByteArrayType{length: 12}.ColumnOrder()
}

func (t *intervalType) PhysicalType() *format.Type {
	return fixedLenByteArrayType{length: 12}.PhysicalType()
}

func (t *intervalType) LogicalType() *format.LogicalType { return nil }

func (t *intervalType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Interval]
}

func (t *intervalType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return fixedLenByteArrayType{length: 12}.NewColumnIndexer(sizeLimit)
}

func (t *intervalType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return fixedLenByteArrayType{length: 12}.NewDictionary(columnIndex, numValues, data)
}

func (t *intervalType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return fixedLenByteArrayType{length: 12}.NewColumnBuffer(columnIndex, numValues)
}

func (t *intervalType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return fixedLenByteArrayType{length: 12}.NewPage(columnIndex, numValues, data)
}

func (t *intervalType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return fixedLenByteArrayType{length: 12}.NewValues(values, offsets)
}

func (t *intervalType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return fixedLenByteArrayType{length: 12}.Encode(dst, src, enc)
}

func (t *intervalType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return fixedLenByteArrayType{length: 12}.Decode(dst, src, enc)
}

func (t *intervalType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return fixedLenByteArrayType{length: 12}.EstimateDecodeSize(numValues, src, enc)
}

func (t *intervalType) AssignValue(dst reflect.Value, src Value) error {
	if dst.Type() == reflect.TypeOf(Interval{}) {
		v := src.byteArray()
		if len(v) == 12 {
			dst.Set(reflect.ValueOf(Interval{
				Months:       binary.LittleEndian.Uint32(v[0:4]),
				Days:         binary.LittleEndian.Uint32(v[4:8]),
				Milliseconds: binary.LittleEndian.Uint32(v[8:12]),
			}))
			return nil
		}
	}
	return fixedLenByteArrayType{length: 12}.AssignValue(dst, src)
}

func (t *intervalType) ConvertValue(val Value, typ Type) (Value, error) {
	return fixedLenByteArrayType{length: 12}.ConvertValue(val, typ)
}
