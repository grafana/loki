package parquet

import (
	"reflect"
	"time"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// Date constructs a leaf node of DATE logical type.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#date
func Date() Node { return Leaf(&dateType{}) }

var dateLogicalType = format.LogicalType{
	Date: new(format.DateType),
}

type dateType format.DateType

func (t *dateType) String() string { return (*format.DateType)(t).String() }

func (t *dateType) Kind() Kind { return int32Type{}.Kind() }

func (t *dateType) Length() int { return int32Type{}.Length() }

func (t *dateType) EstimateSize(n int) int { return int32Type{}.EstimateSize(n) }

func (t *dateType) EstimateNumValues(n int) int { return int32Type{}.EstimateNumValues(n) }

func (t *dateType) Compare(a, b Value) int { return int32Type{}.Compare(a, b) }

func (t *dateType) ColumnOrder() *format.ColumnOrder { return int32Type{}.ColumnOrder() }

func (t *dateType) PhysicalType() *format.Type { return int32Type{}.PhysicalType() }

func (t *dateType) LogicalType() *format.LogicalType { return &dateLogicalType }

func (t *dateType) ConvertedType() *deprecated.ConvertedType {
	return &convertedTypes[deprecated.Date]
}

func (t *dateType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return int32Type{}.NewColumnIndexer(sizeLimit)
}

func (t *dateType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return int32Type{}.NewDictionary(columnIndex, numValues, data)
}

func (t *dateType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return int32Type{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *dateType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return int32Type{}.NewPage(columnIndex, numValues, data)
}

func (t *dateType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return int32Type{}.NewValues(values, offsets)
}

func (t *dateType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return int32Type{}.Encode(dst, src, enc)
}

func (t *dateType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return int32Type{}.Decode(dst, src, enc)
}

func (t *dateType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return int32Type{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *dateType) AssignValue(dst reflect.Value, src Value) error {
	switch dst.Type() {
	case reflect.TypeOf(time.Time{}):
		// Check if the value is NULL - if so, assign zero time.Time
		if src.IsNull() {
			dst.Set(reflect.ValueOf(time.Time{}))
			return nil
		}

		// DATE is stored as days since Unix epoch (January 1, 1970)
		days := src.int32()
		val := time.Unix(int64(days)*86400, 0).UTC()
		dst.Set(reflect.ValueOf(val))
		return nil
	case reflect.TypeOf((*time.Time)(nil)):
		// Handle *time.Time (pointer to time.Time)
		if src.IsNull() {
			// For NULL values, set the pointer to nil
			dst.Set(reflect.Zero(dst.Type()))
			return nil
		}

		// DATE is stored as days since Unix epoch (January 1, 1970)
		days := src.int32()
		val := time.Unix(int64(days)*86400, 0).UTC()
		ptr := &val
		dst.Set(reflect.ValueOf(ptr))
		return nil
	}
	return int32Type{}.AssignValue(dst, src)
}

func (t *dateType) ConvertValue(val Value, typ Type) (Value, error) {
	switch src := typ.(type) {
	case *stringType:
		return convertStringToDate(val, time.UTC)
	case *timestampType:
		return convertTimestampToDate(val, src.Unit, src.tz())
	}
	return int32Type{}.ConvertValue(val, typ)
}
