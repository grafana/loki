// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pcommon // import "go.opentelemetry.io/collector/pdata/pcommon"

// This file contains data structures that are common for all telemetry types,
// such as timestamps, attributes, etc.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"go.uber.org/multierr"

	"go.opentelemetry.io/collector/pdata/internal"
	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
)

// ValueType specifies the type of Value.
type ValueType int32

const (
	ValueTypeEmpty ValueType = iota
	ValueTypeStr
	ValueTypeInt
	ValueTypeDouble
	ValueTypeBool
	ValueTypeMap
	ValueTypeSlice
	ValueTypeBytes
)

// String returns the string representation of the ValueType.
func (avt ValueType) String() string {
	switch avt {
	case ValueTypeEmpty:
		return "Empty"
	case ValueTypeStr:
		return "Str"
	case ValueTypeBool:
		return "Bool"
	case ValueTypeInt:
		return "Int"
	case ValueTypeDouble:
		return "Double"
	case ValueTypeMap:
		return "Map"
	case ValueTypeSlice:
		return "Slice"
	case ValueTypeBytes:
		return "Bytes"
	}
	return ""
}

// Value is a mutable cell containing any value. Typically used as an element of Map or Slice.
// Must use one of NewValue+ functions below to create new instances.
//
// Intended to be passed by value since internally it is just a pointer to actual
// value representation. For the same reason passing by value and calling setters
// will modify the original, e.g.:
//
//	func f1(val Value) { val.SetInt(234) }
//	func f2() {
//	    v := NewValueStr("a string")
//	    f1(v)
//	    _ := v.Type() // this will return ValueTypeInt
//	}
//
// Important: zero-initialized instance is not valid for use. All Value functions below must
// be called only on instances that are created via NewValue+ functions.
type Value internal.Value

// NewValueEmpty creates a new Value with an empty value.
func NewValueEmpty() Value {
	return newValue(&otlpcommon.AnyValue{})
}

// NewValueStr creates a new Value with the given string value.
func NewValueStr(v string) Value {
	return newValue(&otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: v}})
}

// NewValueInt creates a new Value with the given int64 value.
func NewValueInt(v int64) Value {
	return newValue(&otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_IntValue{IntValue: v}})
}

// NewValueDouble creates a new Value with the given float64 value.
func NewValueDouble(v float64) Value {
	return newValue(&otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_DoubleValue{DoubleValue: v}})
}

// NewValueBool creates a new Value with the given bool value.
func NewValueBool(v bool) Value {
	return newValue(&otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_BoolValue{BoolValue: v}})
}

// NewValueMap creates a new Value of map type.
func NewValueMap() Value {
	return newValue(&otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_KvlistValue{KvlistValue: &otlpcommon.KeyValueList{}}})
}

// NewValueSlice creates a new Value of array type.
func NewValueSlice() Value {
	return newValue(&otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_ArrayValue{ArrayValue: &otlpcommon.ArrayValue{}}})
}

// NewValueBytes creates a new empty Value of byte type.
func NewValueBytes() Value {
	return newValue(&otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_BytesValue{BytesValue: nil}})
}

func newValue(orig *otlpcommon.AnyValue) Value {
	return Value(internal.NewValue(orig))
}

func (v Value) getOrig() *otlpcommon.AnyValue {
	return internal.GetOrigValue(internal.Value(v))
}

func (v Value) FromRaw(iv any) error {
	switch tv := iv.(type) {
	case nil:
		v.getOrig().Value = nil
	case string:
		v.SetStr(tv)
	case int:
		v.SetInt(int64(tv))
	case int8:
		v.SetInt(int64(tv))
	case int16:
		v.SetInt(int64(tv))
	case int32:
		v.SetInt(int64(tv))
	case int64:
		v.SetInt(tv)
	case uint:
		v.SetInt(int64(tv))
	case uint8:
		v.SetInt(int64(tv))
	case uint16:
		v.SetInt(int64(tv))
	case uint32:
		v.SetInt(int64(tv))
	case uint64:
		v.SetInt(int64(tv))
	case float32:
		v.SetDouble(float64(tv))
	case float64:
		v.SetDouble(tv)
	case bool:
		v.SetBool(tv)
	case []byte:
		v.SetEmptyBytes().FromRaw(tv)
	case map[string]any:
		return v.SetEmptyMap().FromRaw(tv)
	case []any:
		return v.SetEmptySlice().FromRaw(tv)
	default:
		return fmt.Errorf("<Invalid value type %T>", tv)
	}
	return nil
}

// Type returns the type of the value for this Value.
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) Type() ValueType {
	switch v.getOrig().Value.(type) {
	case *otlpcommon.AnyValue_StringValue:
		return ValueTypeStr
	case *otlpcommon.AnyValue_BoolValue:
		return ValueTypeBool
	case *otlpcommon.AnyValue_IntValue:
		return ValueTypeInt
	case *otlpcommon.AnyValue_DoubleValue:
		return ValueTypeDouble
	case *otlpcommon.AnyValue_KvlistValue:
		return ValueTypeMap
	case *otlpcommon.AnyValue_ArrayValue:
		return ValueTypeSlice
	case *otlpcommon.AnyValue_BytesValue:
		return ValueTypeBytes
	}
	return ValueTypeEmpty
}

// Str returns the string value associated with this Value.
// The shorter name is used instead of String to avoid implementing fmt.Stringer interface.
// If the Type() is not ValueTypeStr then returns empty string.
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) Str() string {
	return v.getOrig().GetStringValue()
}

// Int returns the int64 value associated with this Value.
// If the Type() is not ValueTypeInt then returns int64(0).
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) Int() int64 {
	return v.getOrig().GetIntValue()
}

// Double returns the float64 value associated with this Value.
// If the Type() is not ValueTypeDouble then returns float64(0).
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) Double() float64 {
	return v.getOrig().GetDoubleValue()
}

// Bool returns the bool value associated with this Value.
// If the Type() is not ValueTypeBool then returns false.
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) Bool() bool {
	return v.getOrig().GetBoolValue()
}

// Map returns the map value associated with this Value.
// If the Type() is not ValueTypeMap then returns an invalid map. Note that using
// such map can cause panic.
//
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) Map() Map {
	kvlist := v.getOrig().GetKvlistValue()
	if kvlist == nil {
		return Map{}
	}
	return newMap(&kvlist.Values)
}

// Slice returns the slice value associated with this Value.
// If the Type() is not ValueTypeSlice then returns an invalid slice. Note that using
// such slice can cause panic.
//
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) Slice() Slice {
	arr := v.getOrig().GetArrayValue()
	if arr == nil {
		return Slice{}
	}
	return newSlice(&arr.Values)
}

// Bytes returns the ByteSlice value associated with this Value.
// If the Type() is not ValueTypeBytes then returns an invalid ByteSlice object. Note that using
// such slice can cause panic.
//
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) Bytes() ByteSlice {
	bv, ok := v.getOrig().GetValue().(*otlpcommon.AnyValue_BytesValue)
	if !ok {
		return ByteSlice{}
	}
	return ByteSlice(internal.NewByteSlice(&bv.BytesValue))
}

// SetStr replaces the string value associated with this Value,
// it also changes the type to be ValueTypeStr.
// The shorter name is used instead of SetString to avoid implementing
// fmt.Stringer interface by the corresponding getter method.
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) SetStr(sv string) {
	v.getOrig().Value = &otlpcommon.AnyValue_StringValue{StringValue: sv}
}

// SetInt replaces the int64 value associated with this Value,
// it also changes the type to be ValueTypeInt.
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) SetInt(iv int64) {
	v.getOrig().Value = &otlpcommon.AnyValue_IntValue{IntValue: iv}
}

// SetDouble replaces the float64 value associated with this Value,
// it also changes the type to be ValueTypeDouble.
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) SetDouble(dv float64) {
	v.getOrig().Value = &otlpcommon.AnyValue_DoubleValue{DoubleValue: dv}
}

// SetBool replaces the bool value associated with this Value,
// it also changes the type to be ValueTypeBool.
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) SetBool(bv bool) {
	v.getOrig().Value = &otlpcommon.AnyValue_BoolValue{BoolValue: bv}
}

// SetEmptyBytes sets value to an empty byte slice and returns it.
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) SetEmptyBytes() ByteSlice {
	bv := otlpcommon.AnyValue_BytesValue{BytesValue: nil}
	v.getOrig().Value = &bv
	return ByteSlice(internal.NewByteSlice(&bv.BytesValue))
}

// SetEmptyMap sets value to an empty map and returns it.
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) SetEmptyMap() Map {
	kv := &otlpcommon.AnyValue_KvlistValue{KvlistValue: &otlpcommon.KeyValueList{}}
	v.getOrig().Value = kv
	return newMap(&kv.KvlistValue.Values)
}

// SetEmptySlice sets value to an empty slice and returns it.
// Calling this function on zero-initialized Value will cause a panic.
func (v Value) SetEmptySlice() Slice {
	av := &otlpcommon.AnyValue_ArrayValue{ArrayValue: &otlpcommon.ArrayValue{}}
	v.getOrig().Value = av
	return newSlice(&av.ArrayValue.Values)
}

// CopyTo copies the Value instance overriding the destination.
func (v Value) CopyTo(dest Value) {
	destOrig := dest.getOrig()
	switch ov := v.getOrig().Value.(type) {
	case *otlpcommon.AnyValue_KvlistValue:
		kv, ok := destOrig.Value.(*otlpcommon.AnyValue_KvlistValue)
		if !ok {
			kv = &otlpcommon.AnyValue_KvlistValue{KvlistValue: &otlpcommon.KeyValueList{}}
			destOrig.Value = kv
		}
		if ov.KvlistValue == nil {
			kv.KvlistValue = nil
			return
		}
		// Deep copy to dest.
		newMap(&ov.KvlistValue.Values).CopyTo(newMap(&kv.KvlistValue.Values))
	case *otlpcommon.AnyValue_ArrayValue:
		av, ok := destOrig.Value.(*otlpcommon.AnyValue_ArrayValue)
		if !ok {
			av = &otlpcommon.AnyValue_ArrayValue{ArrayValue: &otlpcommon.ArrayValue{}}
			destOrig.Value = av
		}
		if ov.ArrayValue == nil {
			av.ArrayValue = nil
			return
		}
		// Deep copy to dest.
		newSlice(&ov.ArrayValue.Values).CopyTo(newSlice(&av.ArrayValue.Values))
	case *otlpcommon.AnyValue_BytesValue:
		bv, ok := destOrig.Value.(*otlpcommon.AnyValue_BytesValue)
		if !ok {
			bv = &otlpcommon.AnyValue_BytesValue{}
			destOrig.Value = bv
		}
		bv.BytesValue = make([]byte, len(ov.BytesValue))
		copy(bv.BytesValue, ov.BytesValue)
	default:
		// Primitive immutable type, no need for deep copy.
		destOrig.Value = ov
	}
}

// Equal checks for equality, it returns true if the objects are equal otherwise false.
func (v Value) Equal(av Value) bool {
	if v.getOrig() == av.getOrig() {
		return true
	}

	if v.getOrig().Value == nil || av.getOrig().Value == nil {
		return v.getOrig().Value == av.getOrig().Value
	}

	if v.Type() != av.Type() {
		return false
	}

	switch v := v.getOrig().Value.(type) {
	case *otlpcommon.AnyValue_StringValue:
		return v.StringValue == av.getOrig().GetStringValue()
	case *otlpcommon.AnyValue_BoolValue:
		return v.BoolValue == av.getOrig().GetBoolValue()
	case *otlpcommon.AnyValue_IntValue:
		return v.IntValue == av.getOrig().GetIntValue()
	case *otlpcommon.AnyValue_DoubleValue:
		return v.DoubleValue == av.getOrig().GetDoubleValue()
	case *otlpcommon.AnyValue_ArrayValue:
		vv := v.ArrayValue.GetValues()
		avv := av.getOrig().GetArrayValue().GetValues()
		if len(vv) != len(avv) {
			return false
		}

		for i := range avv {
			if !newValue(&vv[i]).Equal(newValue(&avv[i])) {
				return false
			}
		}
		return true
	case *otlpcommon.AnyValue_KvlistValue:
		cc := v.KvlistValue.GetValues()
		avv := av.getOrig().GetKvlistValue().GetValues()
		if len(cc) != len(avv) {
			return false
		}

		m := newMap(&avv)

		for i := range cc {
			newAv, ok := m.Get(cc[i].Key)
			if !ok {
				return false
			}

			if !newAv.Equal(newValue(&cc[i].Value)) {
				return false
			}
		}
		return true
	case *otlpcommon.AnyValue_BytesValue:
		return bytes.Equal(v.BytesValue, av.getOrig().GetBytesValue())
	}

	return false
}

// AsString converts an OTLP Value object of any type to its equivalent string
// representation. This differs from GetString which only returns a non-empty value
// if the ValueType is ValueTypeStr.
func (v Value) AsString() string {
	switch v.Type() {
	case ValueTypeEmpty:
		return ""

	case ValueTypeStr:
		return v.Str()

	case ValueTypeBool:
		return strconv.FormatBool(v.Bool())

	case ValueTypeDouble:
		return float64AsString(v.Double())

	case ValueTypeInt:
		return strconv.FormatInt(v.Int(), 10)

	case ValueTypeMap:
		jsonStr, _ := json.Marshal(v.Map().AsRaw())
		return string(jsonStr)

	case ValueTypeBytes:
		return base64.StdEncoding.EncodeToString(*v.Bytes().getOrig())

	case ValueTypeSlice:
		jsonStr, _ := json.Marshal(v.Slice().AsRaw())
		return string(jsonStr)

	default:
		return fmt.Sprintf("<Unknown OpenTelemetry attribute value type %q>", v.Type())
	}
}

// See https://cs.opensource.google/go/go/+/refs/tags/go1.17.7:src/encoding/json/encode.go;l=585.
// This allows us to avoid using reflection.
func float64AsString(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return fmt.Sprintf("json: unsupported value: %s", strconv.FormatFloat(f, 'g', -1, 64))
	}

	// Convert as if by ES6 number to string conversion.
	// This matches most other JSON generators.
	// See golang.org/issue/6384 and golang.org/issue/14135.
	// Like fmt %g, but the exponent cutoffs are different
	// and exponents themselves are not padded to two digits.
	scratch := [64]byte{}
	b := scratch[:0]
	abs := math.Abs(f)
	fmt := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		fmt = 'e'
	}
	b = strconv.AppendFloat(b, f, fmt, -1, 64)
	if fmt == 'e' {
		// clean up e-09 to e-9
		n := len(b)
		if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return string(b)
}

func (v Value) AsRaw() any {
	switch v.Type() {
	case ValueTypeEmpty:
		return nil
	case ValueTypeStr:
		return v.Str()
	case ValueTypeBool:
		return v.Bool()
	case ValueTypeDouble:
		return v.Double()
	case ValueTypeInt:
		return v.Int()
	case ValueTypeBytes:
		return v.Bytes().AsRaw()
	case ValueTypeMap:
		return v.Map().AsRaw()
	case ValueTypeSlice:
		return v.Slice().AsRaw()
	}
	return fmt.Sprintf("<Unknown OpenTelemetry value type %q>", v.Type())
}

func newKeyValueString(k string, v string) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	akv := newValue(&orig.Value)
	akv.SetStr(v)
	return orig
}

func newKeyValueInt(k string, v int64) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	akv := newValue(&orig.Value)
	akv.SetInt(v)
	return orig
}

func newKeyValueDouble(k string, v float64) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	akv := newValue(&orig.Value)
	akv.SetDouble(v)
	return orig
}

func newKeyValueBool(k string, v bool) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	akv := newValue(&orig.Value)
	akv.SetBool(v)
	return orig
}

// Map stores a map of string keys to elements of Value type.
type Map internal.Map

// NewMap creates a Map with 0 elements.
func NewMap() Map {
	orig := []otlpcommon.KeyValue(nil)
	return Map(internal.NewMap(&orig))
}

func (m Map) getOrig() *[]otlpcommon.KeyValue {
	return internal.GetOrigMap(internal.Map(m))
}

func newMap(orig *[]otlpcommon.KeyValue) Map {
	return Map(internal.NewMap(orig))
}

// Clear erases any existing entries in this Map instance.
func (m Map) Clear() {
	*m.getOrig() = nil
}

// EnsureCapacity increases the capacity of this Map instance, if necessary,
// to ensure that it can hold at least the number of elements specified by the capacity argument.
func (m Map) EnsureCapacity(capacity int) {
	if capacity <= cap(*m.getOrig()) {
		return
	}
	oldOrig := *m.getOrig()
	*m.getOrig() = make([]otlpcommon.KeyValue, 0, capacity)
	copy(*m.getOrig(), oldOrig)
}

// Get returns the Value associated with the key and true. Returned
// Value is not a copy, it is a reference to the value stored in this map.
// It is allowed to modify the returned value using Value.Set* functions.
// Such modification will be applied to the value stored in this map.
//
// If the key does not exist returns an invalid instance of the KeyValue and false.
// Calling any functions on the returned invalid instance will cause a panic.
func (m Map) Get(key string) (Value, bool) {
	for i := range *m.getOrig() {
		akv := &(*m.getOrig())[i]
		if akv.Key == key {
			return newValue(&akv.Value), true
		}
	}
	return newValue(nil), false
}

// Remove removes the entry associated with the key and returns true if the key
// was present in the map, otherwise returns false.
func (m Map) Remove(key string) bool {
	for i := range *m.getOrig() {
		akv := &(*m.getOrig())[i]
		if akv.Key == key {
			*akv = (*m.getOrig())[len(*m.getOrig())-1]
			*m.getOrig() = (*m.getOrig())[:len(*m.getOrig())-1]
			return true
		}
	}
	return false
}

// RemoveIf removes the entries for which the function in question returns true
func (m Map) RemoveIf(f func(string, Value) bool) {
	newLen := 0
	for i := 0; i < len(*m.getOrig()); i++ {
		akv := &(*m.getOrig())[i]
		if f(akv.Key, newValue(&akv.Value)) {
			continue
		}
		if newLen == i {
			// Nothing to move, element is at the right place.
			newLen++
			continue
		}
		(*m.getOrig())[newLen] = (*m.getOrig())[i]
		newLen++
	}
	*m.getOrig() = (*m.getOrig())[:newLen]
}

// PutEmpty inserts or updates an empty value to the map under given key
// and return the updated/inserted value.
func (m Map) PutEmpty(k string) Value {
	if av, existing := m.Get(k); existing {
		av.getOrig().Value = nil
		return newValue(av.getOrig())
	}
	*m.getOrig() = append(*m.getOrig(), otlpcommon.KeyValue{Key: k})
	return newValue(&(*m.getOrig())[len(*m.getOrig())-1].Value)
}

// PutStr performs the Insert or Update action. The Value is
// inserted to the map that did not originally have the key. The key/value is
// updated to the map where the key already existed.
func (m Map) PutStr(k string, v string) {
	if av, existing := m.Get(k); existing {
		av.SetStr(v)
	} else {
		*m.getOrig() = append(*m.getOrig(), newKeyValueString(k, v))
	}
}

// PutInt performs the Insert or Update action. The int Value is
// inserted to the map that did not originally have the key. The key/value is
// updated to the map where the key already existed.
func (m Map) PutInt(k string, v int64) {
	if av, existing := m.Get(k); existing {
		av.SetInt(v)
	} else {
		*m.getOrig() = append(*m.getOrig(), newKeyValueInt(k, v))
	}
}

// PutDouble performs the Insert or Update action. The double Value is
// inserted to the map that did not originally have the key. The key/value is
// updated to the map where the key already existed.
func (m Map) PutDouble(k string, v float64) {
	if av, existing := m.Get(k); existing {
		av.SetDouble(v)
	} else {
		*m.getOrig() = append(*m.getOrig(), newKeyValueDouble(k, v))
	}
}

// PutBool performs the Insert or Update action. The bool Value is
// inserted to the map that did not originally have the key. The key/value is
// updated to the map where the key already existed.
func (m Map) PutBool(k string, v bool) {
	if av, existing := m.Get(k); existing {
		av.SetBool(v)
	} else {
		*m.getOrig() = append(*m.getOrig(), newKeyValueBool(k, v))
	}
}

// PutEmptyBytes inserts or updates an empty byte slice under given key and returns it.
func (m Map) PutEmptyBytes(k string) ByteSlice {
	bv := otlpcommon.AnyValue_BytesValue{}
	if av, existing := m.Get(k); existing {
		av.getOrig().Value = &bv
	} else {
		*m.getOrig() = append(*m.getOrig(), otlpcommon.KeyValue{Key: k, Value: otlpcommon.AnyValue{Value: &bv}})
	}
	return ByteSlice(internal.NewByteSlice(&bv.BytesValue))
}

// PutEmptyMap inserts or updates an empty map under given key and returns it.
func (m Map) PutEmptyMap(k string) Map {
	kvl := otlpcommon.AnyValue_KvlistValue{KvlistValue: &otlpcommon.KeyValueList{Values: []otlpcommon.KeyValue(nil)}}
	if av, existing := m.Get(k); existing {
		av.getOrig().Value = &kvl
	} else {
		*m.getOrig() = append(*m.getOrig(), otlpcommon.KeyValue{Key: k, Value: otlpcommon.AnyValue{Value: &kvl}})
	}
	return Map(internal.NewMap(&kvl.KvlistValue.Values))
}

// PutEmptySlice inserts or updates an empty slice under given key and returns it.
func (m Map) PutEmptySlice(k string) Slice {
	vl := otlpcommon.AnyValue_ArrayValue{ArrayValue: &otlpcommon.ArrayValue{Values: []otlpcommon.AnyValue(nil)}}
	if av, existing := m.Get(k); existing {
		av.getOrig().Value = &vl
	} else {
		*m.getOrig() = append(*m.getOrig(), otlpcommon.KeyValue{Key: k, Value: otlpcommon.AnyValue{Value: &vl}})
	}
	return Slice(internal.NewSlice(&vl.ArrayValue.Values))
}

// Sort sorts the entries in the Map so two instances can be compared.
// Returns the same instance to allow nicer code like:
//
//	assert.EqualValues(t, expected.Sort(), actual.Sort())
//
// IMPORTANT: Sort mutates the data, if you call this function in a consumer,
// the consumer must be configured that it mutates data.
func (m Map) Sort() Map {
	// Intention is to move the nil values at the end.
	sort.SliceStable(*m.getOrig(), func(i, j int) bool {
		return (*m.getOrig())[i].Key < (*m.getOrig())[j].Key
	})
	return m
}

// Len returns the length of this map.
//
// Because the Map is represented internally by a slice of pointers, and the data are comping from the wire,
// it is possible that when iterating using "Range" to get access to fewer elements because nil elements are skipped.
func (m Map) Len() int {
	return len(*m.getOrig())
}

// Range calls f sequentially for each key and value present in the map. If f returns false, range stops the iteration.
//
// Example:
//
//	sm.Range(func(k string, v Value) bool {
//	    ...
//	})
func (m Map) Range(f func(k string, v Value) bool) {
	for i := range *m.getOrig() {
		kv := &(*m.getOrig())[i]
		if !f(kv.Key, Value(internal.NewValue(&kv.Value))) {
			break
		}
	}
}

// CopyTo copies all elements from the current map overriding the destination.
func (m Map) CopyTo(dest Map) {
	newLen := len(*m.getOrig())
	oldCap := cap(*dest.getOrig())
	if newLen <= oldCap {
		// New slice fits in existing slice, no need to reallocate.
		*dest.getOrig() = (*dest.getOrig())[:newLen:oldCap]
		for i := range *m.getOrig() {
			akv := &(*m.getOrig())[i]
			destAkv := &(*dest.getOrig())[i]
			destAkv.Key = akv.Key
			newValue(&akv.Value).CopyTo(newValue(&destAkv.Value))
		}
		return
	}

	// New slice is bigger than exist slice. Allocate new space.
	origs := make([]otlpcommon.KeyValue, len(*m.getOrig()))
	for i := range *m.getOrig() {
		akv := &(*m.getOrig())[i]
		origs[i].Key = akv.Key
		newValue(&akv.Value).CopyTo(newValue(&origs[i].Value))
	}
	*dest.getOrig() = origs
}

// AsRaw converts an OTLP Map to a standard go map
func (m Map) AsRaw() map[string]any {
	rawMap := make(map[string]any)
	m.Range(func(k string, v Value) bool {
		rawMap[k] = v.AsRaw()
		return true
	})
	return rawMap
}

func (m Map) FromRaw(rawMap map[string]any) error {
	if len(rawMap) == 0 {
		*m.getOrig() = nil
		return nil
	}

	var errs error
	origs := make([]otlpcommon.KeyValue, len(rawMap))
	ix := 0
	for k, iv := range rawMap {
		origs[ix].Key = k
		errs = multierr.Append(errs, newValue(&origs[ix].Value).FromRaw(iv))
		ix++
	}
	*m.getOrig() = origs
	return errs
}

// AsRaw return []any copy of the Slice.
func (es Slice) AsRaw() []any {
	rawSlice := make([]any, 0, es.Len())
	for i := 0; i < es.Len(); i++ {
		rawSlice = append(rawSlice, es.At(i).AsRaw())
	}
	return rawSlice
}

// FromRaw copies []any into the Slice.
func (es Slice) FromRaw(rawSlice []any) error {
	if len(rawSlice) == 0 {
		*es.getOrig() = nil
		return nil
	}
	var errs error
	origs := make([]otlpcommon.AnyValue, len(rawSlice))
	for ix, iv := range rawSlice {
		errs = multierr.Append(errs, newValue(&origs[ix]).FromRaw(iv))
	}
	*es.getOrig() = origs
	return errs
}
