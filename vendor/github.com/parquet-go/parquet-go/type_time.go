package parquet

import (
	"reflect"
	"time"

	"github.com/parquet-go/parquet-go/deprecated"
	"github.com/parquet-go/parquet-go/encoding"
	"github.com/parquet-go/parquet-go/format"
)

// TimeUnit represents units of time in the parquet type system.
type TimeUnit interface {
	// Returns the precision of the time unit as a time.Duration value.
	Duration() time.Duration
	// Converts the TimeUnit value to its representation in the parquet thrift
	// format.
	TimeUnit() format.TimeUnit
}

var (
	Millisecond TimeUnit = &millisecond{}
	Microsecond TimeUnit = &microsecond{}
	Nanosecond  TimeUnit = &nanosecond{}
)

// The TimeUnit union holds exactly one of these; testing which one is common
// enough in this package to be worth naming.
func isMillis(u format.TimeUnit) bool { _, ok := u.Value.(*format.MilliSeconds); return ok }
func isMicros(u format.TimeUnit) bool { _, ok := u.Value.(*format.MicroSeconds); return ok }
func isNanos(u format.TimeUnit) bool  { _, ok := u.Value.(*format.NanoSeconds); return ok }

type millisecond format.MilliSeconds

func (u *millisecond) Duration() time.Duration { return time.Millisecond }
func (u *millisecond) TimeUnit() format.TimeUnit {
	return format.TimeUnit{Value: (*format.MilliSeconds)(u)}
}

type microsecond format.MicroSeconds

func (u *microsecond) Duration() time.Duration { return time.Microsecond }
func (u *microsecond) TimeUnit() format.TimeUnit {
	return format.TimeUnit{Value: (*format.MicroSeconds)(u)}
}

type nanosecond format.NanoSeconds

func (u *nanosecond) Duration() time.Duration { return time.Nanosecond }
func (u *nanosecond) TimeUnit() format.TimeUnit {
	return format.TimeUnit{Value: (*format.NanoSeconds)(u)}
}

// Time constructs a leaf node of TIME logical type.
// IsAdjustedToUTC is true by default.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#time
func Time(unit TimeUnit) Node {
	return TimeAdjusted(unit, true)
}

// TimeAdjusted constructs a leaf node of TIME logical type
// with the IsAdjustedToUTC property explicitly set.
//
// https://github.com/apache/parquet-format/blob/master/LogicalTypes.md#time
func TimeAdjusted(unit TimeUnit, isAdjustedToUTC bool) Node {
	// Use pre-allocated instances for common cases
	timeUnit := unit.TimeUnit()
	if isAdjustedToUTC {
		switch timeUnit.Value.(type) {
		case *format.MilliSeconds:
			return Leaf(&timeMilliAdjustedToUTC)
		case *format.MicroSeconds:
			return Leaf(&timeMicroAdjustedToUTC)
		case *format.NanoSeconds:
			return Leaf(&timeNanoAdjustedToUTC)
		}
	} else {
		switch timeUnit.Value.(type) {
		case *format.MilliSeconds:
			return Leaf(&timeMilliNotAdjustedToUTC)
		case *format.MicroSeconds:
			return Leaf(&timeMicroNotAdjustedToUTC)
		case *format.NanoSeconds:
			return Leaf(&timeNanoNotAdjustedToUTC)
		}
	}
	// Fallback for unknown unit types
	return Leaf(&timeType{IsAdjustedToUTC: isAdjustedToUTC, Unit: timeUnit})
}

var timeMilliAdjustedToUTC = timeType{
	IsAdjustedToUTC: true,
	Unit:            format.TimeUnit{Value: new(format.MilliSeconds)},
}

var timeMicroAdjustedToUTC = timeType{
	IsAdjustedToUTC: true,
	Unit:            format.TimeUnit{Value: new(format.MicroSeconds)},
}

var timeNanoAdjustedToUTC = timeType{
	IsAdjustedToUTC: true,
	Unit:            format.TimeUnit{Value: new(format.NanoSeconds)},
}

var timeMilliNotAdjustedToUTC = timeType{
	IsAdjustedToUTC: false,
	Unit:            format.TimeUnit{Value: new(format.MilliSeconds)},
}

var timeMicroNotAdjustedToUTC = timeType{
	IsAdjustedToUTC: false,
	Unit:            format.TimeUnit{Value: new(format.MicroSeconds)},
}

var timeNanoNotAdjustedToUTC = timeType{
	IsAdjustedToUTC: false,
	Unit:            format.TimeUnit{Value: new(format.NanoSeconds)},
}

var timeMilliAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimeType)(&timeMilliAdjustedToUTC),
}

var timeMicroAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimeType)(&timeMicroAdjustedToUTC),
}

var timeNanoAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimeType)(&timeNanoAdjustedToUTC),
}

var timeMilliNotAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimeType)(&timeMilliNotAdjustedToUTC),
}

var timeMicroNotAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimeType)(&timeMicroNotAdjustedToUTC),
}

var timeNanoNotAdjustedToUTCLogicalType = format.LogicalType{
	Value: (*format.TimeType)(&timeNanoNotAdjustedToUTC),
}

// canonicalTimeType maps a decoded TimeType onto the instance this package
// already declares for it. See canonicalIntType.
func canonicalTimeType(t *format.TimeType) *timeType {
	if t.IsAdjustedToUTC {
		switch t.Unit.Value.(type) {
		case *format.MilliSeconds:
			return &timeMilliAdjustedToUTC
		case *format.MicroSeconds:
			return &timeMicroAdjustedToUTC
		case *format.NanoSeconds:
			return &timeNanoAdjustedToUTC
		}
	} else {
		switch t.Unit.Value.(type) {
		case *format.MilliSeconds:
			return &timeMilliNotAdjustedToUTC
		case *format.MicroSeconds:
			return &timeMicroNotAdjustedToUTC
		case *format.NanoSeconds:
			return &timeNanoNotAdjustedToUTC
		}
	}
	// No unit, or one we do not know; keep the value the file carried.
	return (*timeType)(t)
}

type timeType format.TimeType

func (t *timeType) tz() *time.Location {
	if t.IsAdjustedToUTC {
		return time.UTC
	} else {
		return time.Local
	}
}

func (t *timeType) baseType() Type {
	if t.useInt32() {
		return int32Type{}
	} else {
		return int64Type{}
	}
}

func (t *timeType) useInt32() bool { return isMillis(t.Unit) }

func (t *timeType) useInt64() bool { return isMicros(t.Unit) }

func (t *timeType) String() string { return (*format.TimeType)(t).String() }

func (t *timeType) Kind() Kind { return t.baseType().Kind() }

func (t *timeType) Length() int { return t.baseType().Length() }

func (t *timeType) EstimateSize(n int) int { return t.baseType().EstimateSize(n) }

func (t *timeType) EstimateNumValues(n int) int { return t.baseType().EstimateNumValues(n) }

func (t *timeType) Compare(a, b Value) int { return t.baseType().Compare(a, b) }

func (t *timeType) ColumnOrder() *format.ColumnOrder { return t.baseType().ColumnOrder() }

func (t *timeType) PhysicalType() *format.Type { return t.baseType().PhysicalType() }

func (t *timeType) LogicalType() *format.LogicalType {
	switch t {
	case &timeMilliAdjustedToUTC:
		return &timeMilliAdjustedToUTCLogicalType
	case &timeMicroAdjustedToUTC:
		return &timeMicroAdjustedToUTCLogicalType
	case &timeNanoAdjustedToUTC:
		return &timeNanoAdjustedToUTCLogicalType
	case &timeMilliNotAdjustedToUTC:
		return &timeMilliNotAdjustedToUTCLogicalType
	case &timeMicroNotAdjustedToUTC:
		return &timeMicroNotAdjustedToUTCLogicalType
	case &timeNanoNotAdjustedToUTC:
		return &timeNanoNotAdjustedToUTCLogicalType
	default:
		return &format.LogicalType{Value: (*format.TimeType)(t)}
	}
}

func (t *timeType) ConvertedType() *deprecated.ConvertedType {
	switch {
	case t.useInt32():
		return &convertedTypes[deprecated.TimeMillis]
	case t.useInt64():
		return &convertedTypes[deprecated.TimeMicros]
	default:
		return nil
	}
}

func (t *timeType) NewColumnIndexer(sizeLimit int) ColumnIndexer {
	return t.baseType().NewColumnIndexer(sizeLimit)
}

func (t *timeType) NewColumnBuffer(columnIndex, numValues int) ColumnBuffer {
	return t.baseType().NewColumnBuffer(columnIndex, numValues)
}

func (t *timeType) NewDictionary(columnIndex, numValues int, data encoding.Values) Dictionary {
	return t.baseType().NewDictionary(columnIndex, numValues, data)
}

func (t *timeType) NewPage(columnIndex, numValues int, data encoding.Values) Page {
	return t.baseType().NewPage(columnIndex, numValues, data)
}

func (t *timeType) NewValues(values []byte, offset []uint32) encoding.Values {
	return t.baseType().NewValues(values, offset)
}

func (t *timeType) Encode(dst []byte, src encoding.Values, enc encoding.Encoding) ([]byte, error) {
	return t.baseType().Encode(dst, src, enc)
}

func (t *timeType) Decode(dst encoding.Values, src []byte, enc encoding.Encoding) (encoding.Values, error) {
	return t.baseType().Decode(dst, src, enc)
}

func (t *timeType) EstimateDecodeSize(numValues int, src []byte, enc encoding.Encoding) int {
	return t.baseType().EstimateDecodeSize(numValues, src, enc)
}

func (t *timeType) AssignValue(dst reflect.Value, src Value) error {
	// Handle time.Duration specially to convert from the stored time unit to nanoseconds
	if dst.Type() == reflect.TypeFor[time.Duration]() {
		v := src.int64()
		nanos := v * int64(timeUnitDuration(t.Unit))
		dst.SetInt(nanos)
		return nil
	}
	return t.baseType().AssignValue(dst, src)
}

func (t *timeType) ConvertValue(val Value, typ Type) (Value, error) {
	switch src := typ.(type) {
	case *stringType:
		tz := t.tz()
		if isMicros(t.Unit) {
			return convertStringToTimeMicros(val, tz)
		} else {
			return convertStringToTimeMillis(val, tz)
		}
	case *timestampType:
		tz := t.tz()
		if isMicros(t.Unit) {
			return convertTimestampToTimeMicros(val, src.Unit, src.tz(), tz)
		} else {
			return convertTimestampToTimeMillis(val, src.Unit, src.tz(), tz)
		}
	}
	return t.baseType().ConvertValue(val, typ)
}
