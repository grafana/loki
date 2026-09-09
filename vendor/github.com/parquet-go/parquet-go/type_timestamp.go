package parquet

import (
	"reflect"
	"time"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// Timestamp constructs of leaf node of TIMESTAMP logical type.
// IsAdjustedToUTC is true by default.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#timestamp
func Timestamp(unit TimeUnit) Node {
	return TimestampAdjusted(unit, true)
}

// TimestampAdjusted constructs a leaf node of TIMESTAMP logical type
// with the IsAdjustedToUTC property explicitly set.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#time
func TimestampAdjusted(unit TimeUnit, isAdjustedToUTC bool) Node {
	// Use pre-allocated instances for common cases
	timeUnit := unit.TimeUnit()
	if isAdjustedToUTC {
		switch timeUnit.Value.(type) {
		case *format.MilliSeconds:
			return Leaf(&timestampMilliAdjustedToUTC)
		case *format.MicroSeconds:
			return Leaf(&timestampMicroAdjustedToUTC)
		case *format.NanoSeconds:
			return Leaf(&timestampNanoAdjustedToUTC)
		}
	} else {
		switch timeUnit.Value.(type) {
		case *format.MilliSeconds:
			return Leaf(&timestampMilliNotAdjustedToUTC)
		case *format.MicroSeconds:
			return Leaf(&timestampMicroNotAdjustedToUTC)
		case *format.NanoSeconds:
			return Leaf(&timestampNanoNotAdjustedToUTC)
		}
	}
	// Fallback for unknown unit types
	return Leaf(&timestampType{IsAdjustedToUTC: isAdjustedToUTC, Unit: timeUnit})
}

var timestampMilliAdjustedToUTC = timestampType{
	IsAdjustedToUTC: true,
	Unit:            format.TimeUnit{Value: new(format.MilliSeconds)},
}

var timestampMicroAdjustedToUTC = timestampType{
	IsAdjustedToUTC: true,
	Unit:            format.TimeUnit{Value: new(format.MicroSeconds)},
}

var timestampNanoAdjustedToUTC = timestampType{
	IsAdjustedToUTC: true,
	Unit:            format.TimeUnit{Value: new(format.NanoSeconds)},
}

var timestampMilliNotAdjustedToUTC = timestampType{
	IsAdjustedToUTC: false,
	Unit:            format.TimeUnit{Value: new(format.MilliSeconds)},
}

var timestampMicroNotAdjustedToUTC = timestampType{
	IsAdjustedToUTC: false,
	Unit:            format.TimeUnit{Value: new(format.MicroSeconds)},
}

var timestampNanoNotAdjustedToUTC = timestampType{
	IsAdjustedToUTC: false,
	Unit:            format.TimeUnit{Value: new(format.NanoSeconds)},
}

var timestampMilliAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimestampType)(&timestampMilliAdjustedToUTC),
}

var timestampMicroAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimestampType)(&timestampMicroAdjustedToUTC),
}

var timestampNanoAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimestampType)(&timestampNanoAdjustedToUTC),
}

var timestampMilliNotAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimestampType)(&timestampMilliNotAdjustedToUTC),
}

var timestampMicroNotAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimestampType)(&timestampMicroNotAdjustedToUTC),
}

var timestampNanoNotAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimestampType)(&timestampNanoNotAdjustedToUTC),
}

// canonicalTimestampType maps a decoded TimestampType onto the instance this
// package already declares for it. See canonicalIntType.
func canonicalTimestampType(t *format.TimestampType) *timestampType {
	if t.IsAdjustedToUTC {
		switch t.Unit.Value.(type) {
		case *format.MilliSeconds:
			return &timestampMilliAdjustedToUTC
		case *format.MicroSeconds:
			return &timestampMicroAdjustedToUTC
		case *format.NanoSeconds:
			return &timestampNanoAdjustedToUTC
		}
	} else {
		switch t.Unit.Value.(type) {
		case *format.MilliSeconds:
			return &timestampMilliNotAdjustedToUTC
		case *format.MicroSeconds:
			return &timestampMicroNotAdjustedToUTC
		case *format.NanoSeconds:
			return &timestampNanoNotAdjustedToUTC
		}
	}
	// No unit, or one we do not know; keep the value the file carried.
	return (*timestampType)(t)
}

type timestampType format.TimestampType

func (t *timestampType) tz() *time.Location {
	if t.IsAdjustedToUTC {
		return time.UTC
	} else {
		return time.Local
	}
}

func (t *timestampType) String() string { return (*format.TimestampType)(t).String() }

func (t *timestampType) Kind() Kind { return int64Type{}.Kind() }

func (t *timestampType) Length() int { return int64Type{}.Length() }

func (t *timestampType) EstimateSize(n int) int { return int64Type{}.EstimateSize(n) }

func (t *timestampType) EstimateNumValues(n int) int { return int64Type{}.EstimateNumValues(n) }

func (t *timestampType) Compare(a, b Value) int { return int64Type{}.Compare(a, b) }

func (t *timestampType) ColumnOrder() *format.ColumnOrder { return int64Type{}.ColumnOrder() }

func (t *timestampType) PhysicalType() *format.Type { return int64Type{}.PhysicalType() }

func (t *timestampType) LogicalType() *format.LogicalType {
	switch t {
	case &timestampMilliAdjustedToUTC:
		return &timestampMilliAdjustedToUTCLogicalType
	case &timestampMicroAdjustedToUTC:
		return &timestampMicroAdjustedToUTCLogicalType
	case &timestampNanoAdjustedToUTC:
		return &timestampNanoAdjustedToUTCLogicalType
	case &timestampMilliNotAdjustedToUTC:
		return &timestampMilliNotAdjustedToUTCLogicalType
	case &timestampMicroNotAdjustedToUTC:
		return &timestampMicroNotAdjustedToUTCLogicalType
	case &timestampNanoNotAdjustedToUTC:
		return &timestampNanoNotAdjustedToUTCLogicalType
	default:
		return &format.LogicalType{Value: (*format.TimestampType)(t)}
	}
}

func (t *timestampType) ConvertedType() *deprecated.ConvertedType {
	switch t.Unit.Value.(type) {
	case *format.MilliSeconds:
		return &convertedTypes[deprecated.TimestampMillis]
	case *format.MicroSeconds:
		return &convertedTypes[deprecated.TimestampMicros]
	default:
		return nil
	}
}

func (t *timestampType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return int64Type{}.NewColumnIndexer(sizeLimit)
}

func (t *timestampType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return int64Type{}.NewDictionary(columnIndex, numValues, data)
}

func (t *timestampType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return int64Type{}.NewColumnBuffer(columnIndex, numValues)
}

func (t *timestampType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return int64Type{}.NewPage(columnIndex, numValues, data)
}

func (t *timestampType) NewValues(values []byte, offsets []uint32) encoding.Values {
	return int64Type{}.NewValues(values, offsets)
}

func (t *timestampType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return int64Type{}.Encode(dst, src, enc)
}

func (t *timestampType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return int64Type{}.Decode(dst, src, enc)
}

func (t *timestampType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return int64Type{}.EstimateDecodeSize(numValues, src, enc)
}

func (t *timestampType) AssignValue(dst reflect.Value, src Value) error {
	switch dst.Type() {
	case reflect.TypeOf(time.Time{}):
		// Check if the value is NULL - if so, assign zero time.Time
		if src.IsNull() {
			dst.Set(reflect.ValueOf(time.Time{}))
			return nil
		}

		unit := Nanosecond.TimeUnit()
		lt := t.LogicalType()
		if ts, ok := logicalTypeOf[*format.TimestampType](lt); ok {
			unit = ts.Unit
		}

		nanos := src.int64() * int64(timeUnitDuration(unit))

		val := time.Unix(0, nanos).UTC()
		dst.Set(reflect.ValueOf(val))
		return nil
	case reflect.TypeOf((*time.Time)(nil)):
		// Handle *time.Time (pointer to time.Time)
		if src.IsNull() {
			// For NULL values, set the pointer to nil
			dst.Set(reflect.Zero(dst.Type()))
			return nil
		}

		unit := Nanosecond.TimeUnit()
		lt := t.LogicalType()
		if ts, ok := logicalTypeOf[*format.TimestampType](lt); ok {
			unit = ts.Unit
		}

		nanos := src.int64() * int64(timeUnitDuration(unit))

		val := time.Unix(0, nanos).UTC()
		ptr := &val
		dst.Set(reflect.ValueOf(ptr))
		return nil
	default:
		return int64Type{}.AssignValue(dst, src)
	}
}

func (t *timestampType) ConvertValue(val Value, typ Type) (Value, error) {
	switch src := typ.(type) {
	case *timestampType:
		return convertTimestampToTimestamp(val, src.Unit, t.Unit)
	case *dateType:
		return convertDateToTimestamp(val, t.Unit, t.tz())
	}
	return int64Type{}.ConvertValue(val, typ)
}
