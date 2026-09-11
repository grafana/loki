// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package extensions

import (
	"fmt"
	"iter"
	"math"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/internal/json"
)

// TimestampWithOffsetType represents a timestamp column that stores a timezone offset per row instead of
// applying the same timezone offset to the entire column.
type TimestampWithOffsetType struct {
	arrow.ExtensionBase
}

func isOffsetTypeOk(offsetType arrow.DataType) bool {
	switch offsetType := offsetType.(type) {
	case *arrow.Int16Type:
		return true
	case *arrow.DictionaryType:
		return arrow.TypeEqual(offsetType.ValueType, arrow.PrimitiveTypes.Int16)
	case *arrow.RunEndEncodedType:
		return offsetType.ValidRunEndsType(offsetType.RunEnds()) &&
			arrow.TypeEqual(offsetType.Encoded(), arrow.PrimitiveTypes.Int16)
		// FIXME: Technically this should be non-nullable, but a Arrow IPC does not deserialize
		// ValueNullable properly, so enforcing this here would always fail when reading from an IPC
		// stream
		// !offsetType.ValueNullable
	default:
		return false
	}
}

// Whether the storageType is compatible with TimestampWithOffset.
//
// Returns (time_unit, offset_type, ok). If ok is false, time_unit and offset_type are garbage.
func isDataTypeCompatible(storageType arrow.DataType) (unit arrow.TimeUnit, offsetType arrow.DataType, ok bool) {
	unit = arrow.Second
	offsetType = arrow.PrimitiveTypes.Int16
	ok = false

	st, compat := storageType.(*arrow.StructType)
	if !compat || st.NumFields() != 2 {
		return
	}

	if ts, compat := st.Field(0).Type.(*arrow.TimestampType); compat && ts.TimeZone == "UTC" {
		unit = ts.TimeUnit()
	} else {
		return
	}

	maybeOffset := st.Field(1)
	offsetType = maybeOffset.Type

	ok = st.Field(0).Name == "timestamp" &&
		!st.Field(0).Nullable &&
		maybeOffset.Name == "offset_minutes" &&
		isOffsetTypeOk(offsetType) &&
		!maybeOffset.Nullable
	return
}

// NewTimestampWithOffsetType creates a new TimestampWithOffsetType with the underlying storage type set correctly to
// Struct(timestamp=Timestamp(T, "UTC"), offset_minutes=Int16), where T is any TimeUnit.
func NewTimestampWithOffsetType(unit arrow.TimeUnit) *TimestampWithOffsetType {
	v, _ := NewTimestampWithOffsetTypeCustomOffset(unit, arrow.PrimitiveTypes.Int16)
	// SAFETY: This should never error as Int16 is always a valid offset type

	return v
}

// NewTimestampWithOffsetTypeCustomOffset creates a new TimestampWithOffsetType with the underlying storage type set correctly to
// Struct(timestamp=Timestamp(T, "UTC"), offset_minutes=O), where T is any TimeUnit and O is a valid offset type.
//
// The error will be populated if the data type is not a valid encoding of the offsets field.
func NewTimestampWithOffsetTypeCustomOffset(unit arrow.TimeUnit, offsetType arrow.DataType) (*TimestampWithOffsetType, error) {
	if !isOffsetTypeOk(offsetType) {
		return nil, fmt.Errorf("invalid offset type %s", offsetType)
	}

	return &TimestampWithOffsetType{
		ExtensionBase: arrow.ExtensionBase{
			Storage: arrow.StructOf(
				arrow.Field{
					Name: "timestamp",
					Type: &arrow.TimestampType{
						Unit:     unit,
						TimeZone: "UTC",
					},
					Nullable: false,
				},
				arrow.Field{
					Name:     "offset_minutes",
					Type:     offsetType,
					Nullable: false,
				},
			),
		},
	}, nil
}

type DictIndexType interface {
	*arrow.Int8Type | *arrow.Int16Type | *arrow.Int32Type | *arrow.Int64Type |
		*arrow.Uint8Type | *arrow.Uint16Type | *arrow.Uint32Type | *arrow.Uint64Type
}

type RunEndsType interface {
	*arrow.Int16Type | *arrow.Int32Type | *arrow.Int64Type
}

// NewTimestampWithOffsetTypeDictionaryEncoded creates a new TimestampWithOffsetType with the underlying storage type set correctly to
// Struct(timestamp=Timestamp(T, "UTC"), offset_minutes=Dictionary(I, Int16)), where T is any TimeUnit and I is a
// valid Dictionary index type.
func NewTimestampWithOffsetTypeDictionaryEncoded[I DictIndexType](unit arrow.TimeUnit, index I) *TimestampWithOffsetType {
	offsetType := arrow.DictionaryType{
		IndexType: arrow.DataType(index),
		ValueType: arrow.PrimitiveTypes.Int16,
		Ordered:   false,
	}
	v, _ := NewTimestampWithOffsetTypeCustomOffset(unit, &offsetType)
	// SAFETY: This should never error as DictIndexType is always a valid index type

	return v
}

// NewTimestampWithOffsetTypeRunEndEncoded creates a new TimestampWithOffsetType with the underlying storage type set correctly to
// Struct(timestamp=Timestamp(T, "UTC"), offset_minutes=RunEndEncoded(E, Int16)), where T is any TimeUnit and E is a
// valid run-ends type.
func NewTimestampWithOffsetTypeRunEndEncoded[E RunEndsType](unit arrow.TimeUnit, runEnds E) *TimestampWithOffsetType {
	offsetType := arrow.RunEndEncodedOf(arrow.DataType(runEnds), arrow.PrimitiveTypes.Int16)

	v, _ := NewTimestampWithOffsetTypeCustomOffset(unit, offsetType)
	// SAFETY: This should never error as RunEndsType always a valid run ends type

	return v

}

func (b *TimestampWithOffsetType) ArrayType() reflect.Type {
	return reflect.TypeOf(TimestampWithOffsetArray{})
}

func (b *TimestampWithOffsetType) ExtensionName() string { return "arrow.timestamp_with_offset" }

func (b *TimestampWithOffsetType) String() string {
	return fmt.Sprintf("extension<%s>", b.ExtensionName())
}

func (e *TimestampWithOffsetType) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"name":"%s","metadata":%s}`, e.ExtensionName(), e.Serialize())), nil
}

func (b *TimestampWithOffsetType) Serialize() string { return "" }

func (b *TimestampWithOffsetType) Deserialize(storageType arrow.DataType, data string) (arrow.ExtensionType, error) {
	if data != "" && data != "{}" {
		return nil, fmt.Errorf("serialized metadata for TimestampWithOffset extension type must be '' or '{}', found: %s", data)
	}

	timeUnit, offsetType, ok := isDataTypeCompatible(storageType)
	if !ok {
		return nil, fmt.Errorf("invalid storage type for TimestampWithOffsetType: %s", storageType.Name())
	}

	return NewTimestampWithOffsetTypeCustomOffset(timeUnit, offsetType)
}

func (b *TimestampWithOffsetType) ExtensionEquals(other arrow.ExtensionType) bool {
	return b.ExtensionName() == other.ExtensionName() &&
		arrow.TypeEqual(b.StorageType(), other.StorageType())
}

func (b *TimestampWithOffsetType) OffsetType() arrow.DataType {
	return b.ExtensionBase.Storage.(*arrow.StructType).Field(1).Type
}

func (b *TimestampWithOffsetType) TimeUnit() arrow.TimeUnit {
	return b.ExtensionBase.Storage.(*arrow.StructType).Field(0).Type.(*arrow.TimestampType).TimeUnit()
}

func (b *TimestampWithOffsetType) NewBuilder(mem memory.Allocator) array.Builder {
	v, _ := NewTimestampWithOffsetBuilder(mem, b.TimeUnit(), b.OffsetType())
	return v
}

// TimestampWithOffsetArray is a simple array of struct
type TimestampWithOffsetArray struct {
	array.ExtensionArrayBase
}

func (a *TimestampWithOffsetArray) String() string {
	var o strings.Builder
	o.WriteString("[")
	for i := 0; i < a.Len(); i++ {
		if i > 0 {
			o.WriteString(" ")
		}
		switch {
		case a.IsNull(i):
			o.WriteString(array.NullValueStr)
		default:
			fmt.Fprintf(&o, "\"%s\"", a.Value(i))
		}
	}
	o.WriteString("]")
	return o.String()
}

func timeFromFieldValues(utcTimestamp arrow.Timestamp, offsetMinutes int16, unit arrow.TimeUnit) time.Time {
	// Derive the sign from the whole offset: integer hours is 0 for any offset
	// with magnitude below one hour and so cannot carry a negative sign (e.g.
	// -30 minutes must format as "UTC-00:30", not "UTC+00:30").
	sign := "+"
	abs := int(offsetMinutes)
	if abs < 0 {
		sign = "-"
		abs = -abs
	}

	name := fmt.Sprintf("UTC%s%02d:%02d", sign, abs/60, abs%60)
	loc := time.FixedZone(name, int(offsetMinutes)*60)
	return utcTimestamp.ToTime(unit).In(loc)
}

func fieldValuesFromTime(t time.Time, unit arrow.TimeUnit) (arrow.Timestamp, int16) {
	_, offsetSeconds := t.Zone()
	offsetMinutes := int16(offsetSeconds / 60)
	ts, _ := arrow.TimestampFromTime(t.UTC(), unit)
	return ts, offsetMinutes
}

// Get the raw arrow values at the given index
//
// SAFETY: the value at i must not be nil
func (a *TimestampWithOffsetArray) rawValueUnsafe(i int) (arrow.Timestamp, int16, arrow.TimeUnit) {
	structs := a.Storage().(*array.Struct)

	timestampField := structs.Field(0)
	timestamps := timestampField.(*array.Timestamp)

	timeUnit := timestampField.DataType().(*arrow.TimestampType).Unit
	utcTimestamp := timestamps.Value(i)

	var offsetMinutes int16

	switch offsets := structs.Field(1).(type) {
	case *array.Int16:
		offsetMinutes = offsets.Value(i)
	case *array.Dictionary:
		offsetMinutes = offsets.Dictionary().(*array.Int16).Value(offsets.GetValueIndex(i))
	case *array.RunEndEncoded:
		offsetMinutes = offsets.Values().(*array.Int16).Value(offsets.GetPhysicalIndex(i))
	}

	return utcTimestamp, offsetMinutes, timeUnit
}

func (a *TimestampWithOffsetArray) Value(i int) time.Time {
	if a.IsNull(i) {
		return time.Time{}
	}
	utcTimestamp, offsetMinutes, timeUnit := a.rawValueUnsafe(i)
	return timeFromFieldValues(utcTimestamp, offsetMinutes, timeUnit)
}

// Iterates over the array returning the timestamp at each position.
//
// The second parameter indicates whether the timestamp is valid or not.
//
// This will iterate using the fastest method given the underlying storage array
func (a *TimestampWithOffsetArray) iterValues() iter.Seq2[time.Time, bool] {
	return func(yield func(time.Time, bool) bool) {
		structs := a.Storage().(*array.Struct)
		offsets := structs.Field(1)
		if reeOffsets, isRee := offsets.(*array.RunEndEncoded); isRee {
			timestampField := structs.Field(0)
			timeUnit := timestampField.DataType().(*arrow.TimestampType).Unit
			timestamps := timestampField.(*array.Timestamp)

			offsetValues := reeOffsets.Values().(*array.Int16)
			// Run-ends are absolute over the unsliced offsets child, so a sliced
			// array must begin at the physical run covering its logical offset and
			// advance using absolute positions (logicalOffset + i).
			logicalOffset := reeOffsets.Offset()
			offsetPhysicalIdx := reeOffsets.GetPhysicalOffset()

			var getRunEnd (func(int) int)
			switch arr := reeOffsets.RunEndsArr().(type) {
			case *array.Int16:
				getRunEnd = func(idx int) int { return int(arr.Value(idx)) }
			case *array.Int32:
				getRunEnd = func(idx int) int { return int(arr.Value(idx)) }
			case *array.Int64:
				getRunEnd = func(idx int) int { return int(arr.Value(idx)) }
			}

			for i := 0; i < a.Len(); i++ {
				if logicalOffset+i >= getRunEnd(offsetPhysicalIdx) {
					offsetPhysicalIdx += 1
				}

				var ts time.Time
				valid := a.IsValid(i)
				if valid {
					utcTimestamp := timestamps.Value(i)
					offsetMinutes := offsetValues.Value(offsetPhysicalIdx)
					v := timeFromFieldValues(utcTimestamp, offsetMinutes, timeUnit)
					ts = v
				}

				if !yield(ts, valid) {
					return
				}
			}
		} else {
			for i := 0; i < a.Len(); i++ {
				var ts time.Time
				valid := a.IsValid(i)
				if valid {
					utcTimestamp, offsetMinutes, timeUnit := a.rawValueUnsafe(i)
					v := timeFromFieldValues(utcTimestamp, offsetMinutes, timeUnit)
					ts = v
				}

				if !yield(ts, valid) {
					return
				}
			}
		}
	}
}

func (a *TimestampWithOffsetArray) Values() []time.Time {
	return slices.Collect(func(yield func(time.Time) bool) {
		for t := range a.iterValues() {
			if !yield(t) {
				return
			}
		}
	})
}

func (a *TimestampWithOffsetArray) ValueStr(i int) string {
	switch {
	case a.IsNull(i):
		return array.NullValueStr
	default:
		return a.Value(i).String()
	}
}

func (a *TimestampWithOffsetArray) MarshalJSON() ([]byte, error) {
	values := make([]interface{}, a.Len())
	i := 0
	for ts, valid := range a.iterValues() {
		if !valid {
			values[i] = nil
		} else {
			values[i] = ts
		}
		i += 1
	}
	return json.Marshal(values)
}

func (a *TimestampWithOffsetArray) GetOneForMarshal(i int) interface{} {
	if a.IsNull(i) {
		return nil
	}
	return a.Value(i)
}

// noLastOffset is the sentinel value for TimestampWithOffsetBuilder.lastOffset
// indicating that no run-end-encoded run has been started yet. It is deliberately
// outside the range of valid timezone offsets in minutes (roughly [-720, 840]) so
// it can never compare equal to a real offset.
const noLastOffset int16 = math.MaxInt16

// TimestampWithOffsetBuilder is a convenience builder for the TimestampWithOffset extension type,
// allowing arrays to be built with boolean values rather than the underlying storage type.
type TimestampWithOffsetBuilder struct {
	*array.ExtensionBuilder

	// The layout used to parse any timestamps from strings. Defaults to time.RFC3339
	Layout     string
	unit       arrow.TimeUnit
	offsetType arrow.DataType
	// lastOffset tracks the offset of the current run when the offsets are run-end
	// encoded, so we know when to start a new run. It is initialized to noLastOffset
	// to signal that no run has been started yet.
	lastOffset int16
}

// NewTimestampWithOffsetBuilder creates a new TimestampWithOffsetBuilder, exposing a convenient and efficient interface
// for writing time.Time values to the underlying storage array.
func NewTimestampWithOffsetBuilder(mem memory.Allocator, unit arrow.TimeUnit, offsetType arrow.DataType) (*TimestampWithOffsetBuilder, error) {
	dataType, err := NewTimestampWithOffsetTypeCustomOffset(unit, offsetType)
	if err != nil {
		return nil, err
	}

	return &TimestampWithOffsetBuilder{
		unit:             unit,
		offsetType:       offsetType,
		lastOffset:       noLastOffset,
		Layout:           time.RFC3339,
		ExtensionBuilder: array.NewExtensionBuilder(mem, dataType),
	}, nil
}

// NewArray must route through this type's NewExtensionArray (not the embedded
// ExtensionBuilder's) so that the run-end-encoding tracker is reset on reuse.
func (b *TimestampWithOffsetBuilder) NewArray() arrow.Array {
	return b.NewExtensionArray()
}

// NewExtensionArray finalizes the current array and resets lastOffset so a
// reused builder starts a fresh run instead of continuing a run that belonged
// to the array just finalized (the underlying REE builder is reset too).
func (b *TimestampWithOffsetBuilder) NewExtensionArray() array.ExtensionArray {
	arr := b.ExtensionBuilder.NewExtensionArray()
	b.lastOffset = noLastOffset
	return arr
}

// AppendNull resets the run-end-encoding tracker: the embedded struct builder
// appends a null offset run, so a following value must start a new run instead
// of continuing the null run.
//
// NOTE(#918): this writes a null into the non-nullable offset_minutes storage
// field rather than a default value. This is intentional; comparison and JSON
// round-trip parity for non-nullable inner struct fields is tracked separately
// in https://github.com/apache/arrow-go/issues/918. Logical values (instant,
// offset, validity) are preserved, but a full array.RecordEqual round-trip is
// not guaranteed here.
func (b *TimestampWithOffsetBuilder) AppendNull() {
	b.ExtensionBuilder.AppendNull()
	b.lastOffset = noLastOffset
}

func (b *TimestampWithOffsetBuilder) AppendNulls(n int) {
	b.ExtensionBuilder.AppendNulls(n)
	b.lastOffset = noLastOffset
}

func (b *TimestampWithOffsetBuilder) Append(v time.Time) {
	timestamp, offsetMinutes := fieldValuesFromTime(v, b.unit)
	offsetMinutes16 := int16(offsetMinutes)
	structBuilder := b.Builder.(*array.StructBuilder)

	structBuilder.Append(true)
	structBuilder.FieldBuilder(0).(*array.TimestampBuilder).Append(timestamp)

	switch offsets := structBuilder.FieldBuilder(1).(type) {
	case *array.Int16Builder:
		offsets.Append(offsetMinutes16)
	case *array.Int16DictionaryBuilder:
		offsets.Append(offsetMinutes16)
	case *array.RunEndEncodedBuilder:
		if offsetMinutes != b.lastOffset {
			offsets.Append(1)
			offsets.ValueBuilder().(*array.Int16Builder).Append(offsetMinutes16)
		} else {
			offsets.ContinueRun(1)
		}

		b.lastOffset = offsetMinutes16
	}

}

// By default, this will try to parse the string using the RFC3339 layout.
//
// You can change the default layout by using builder.SetLayout()
func (b *TimestampWithOffsetBuilder) AppendValueFromString(s string) error {
	if s == array.NullValueStr {
		b.AppendNull()
		return nil
	}

	parsed, err := time.Parse(b.Layout, s)
	if err != nil {
		return err
	}

	b.Append(parsed)
	return nil
}

func (b *TimestampWithOffsetBuilder) AppendValues(values []time.Time, valids []bool) {
	if len(valids) != len(values) && len(valids) != 0 {
		panic("len(values) != len(valids) && len(valids) != 0")
	}
	if len(valids) == 0 {
		valids = make([]bool, len(values))
		for i := range valids {
			valids[i] = true
		}
	}

	structBuilder := b.Builder.(*array.StructBuilder)
	timestamps := structBuilder.FieldBuilder(0).(*array.TimestampBuilder)

	structBuilder.AppendValues(valids)
	// SAFETY: by this point we know all buffers have available space given the earlier
	// call to structBuilder.AppendValues which calls Reserve internally, so it's OK to
	// call UnsafeAppend on inner builders

	switch offsets := structBuilder.FieldBuilder(1).(type) {
	case *array.Int16Builder:
		for _, v := range values {
			timestamp, offsetMinutes := fieldValuesFromTime(v, b.unit)
			timestamps.UnsafeAppend(timestamp)
			offsets.UnsafeAppend(offsetMinutes)
		}
	case *array.Int16DictionaryBuilder:
		for _, v := range values {
			timestamp, offsetMinutes := fieldValuesFromTime(v, b.unit)
			timestamps.UnsafeAppend(timestamp)
			offsets.UnsafeAppend(offsetMinutes)
		}
	case *array.RunEndEncodedBuilder:
		offsetValuesBuilder := offsets.ValueBuilder().(*array.Int16Builder)
		for i, v := range values {
			timestamp, offsetMinutes := fieldValuesFromTime(v, b.unit)
			timestamps.UnsafeAppend(timestamp)

			// A null row's offset is masked by the struct validity bitmap, so we
			// continue the current run to maximize compression. A run must still be
			// started when none exists yet (e.g. leading null rows), otherwise
			// ContinueRun would advance the run-ends without a matching entry in the
			// values child and produce an invalid run-end encoded array. lastOffset
			// is only updated when a new run starts, so a null row never splits a
			// contiguous run into two adjacent runs sharing the same value.
			valid := valids[i]
			if b.lastOffset == noLastOffset || (valid && offsetMinutes != b.lastOffset) {
				offsets.Append(1)
				offsetValuesBuilder.Append(offsetMinutes)
				b.lastOffset = offsetMinutes
			} else {
				offsets.ContinueRun(1)
			}
		}
	}
}

func (b *TimestampWithOffsetBuilder) UnmarshalOne(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("failed to decode json: %w", err)
	}

	switch raw := tok.(type) {
	case string:
		t, err := time.Parse(b.Layout, raw)
		if err != nil {
			return fmt.Errorf("failed to parse string \"%s\" as time.Time using layout \"%s\"", raw, b.Layout)
		}
		b.Append(t)
	case nil:
		b.AppendNull()
	default:
		return fmt.Errorf("expected date string")
	}

	return nil
}

func (b *TimestampWithOffsetBuilder) Unmarshal(dec *json.Decoder) error {
	for dec.More() {
		if err := b.UnmarshalOne(dec); err != nil {
			return err
		}
	}
	return nil
}

var (
	_ arrow.ExtensionType          = (*TimestampWithOffsetType)(nil)
	_ array.CustomExtensionBuilder = (*TimestampWithOffsetType)(nil)
	_ array.ExtensionArray         = (*TimestampWithOffsetArray)(nil)
	_ array.Builder                = (*TimestampWithOffsetBuilder)(nil)
)
