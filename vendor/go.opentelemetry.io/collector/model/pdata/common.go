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

package pdata // import "go.opentelemetry.io/collector/model/pdata"

// This file contains data structures that are common for all telemetry types,
// such as timestamps, attributes, etc.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	otlpcommon "go.opentelemetry.io/collector/model/internal/data/protogen/common/v1"
)

// AttributeValueType specifies the type of AttributeValue.
type AttributeValueType int32

const (
	AttributeValueTypeEmpty AttributeValueType = iota
	AttributeValueTypeString
	AttributeValueTypeInt
	AttributeValueTypeDouble
	AttributeValueTypeBool
	AttributeValueTypeMap
	AttributeValueTypeArray
	AttributeValueTypeBytes
)

// String returns the string representation of the AttributeValueType.
func (avt AttributeValueType) String() string {
	switch avt {
	case AttributeValueTypeEmpty:
		return "EMPTY"
	case AttributeValueTypeString:
		return "STRING"
	case AttributeValueTypeBool:
		return "BOOL"
	case AttributeValueTypeInt:
		return "INT"
	case AttributeValueTypeDouble:
		return "DOUBLE"
	case AttributeValueTypeMap:
		return "MAP"
	case AttributeValueTypeArray:
		return "ARRAY"
	case AttributeValueTypeBytes:
		return "BYTES"
	}
	return ""
}

// AttributeValue is a mutable cell containing the value of an attribute. Typically used in AttributeMap.
// Must use one of NewAttributeValue+ functions below to create new instances.
//
// Intended to be passed by value since internally it is just a pointer to actual
// value representation. For the same reason passing by value and calling setters
// will modify the original, e.g.:
//
//   func f1(val AttributeValue) { val.SetIntVal(234) }
//   func f2() {
//       v := NewAttributeValueString("a string")
//       f1(v)
//       _ := v.Type() // this will return AttributeValueTypeInt
//   }
//
// Important: zero-initialized instance is not valid for use. All AttributeValue functions below must
// be called only on instances that are created via NewAttributeValue+ functions.
type AttributeValue struct {
	orig *otlpcommon.AnyValue
}

func newAttributeValue(orig *otlpcommon.AnyValue) AttributeValue {
	return AttributeValue{orig}
}

// NewAttributeValueEmpty creates a new AttributeValue with an empty value.
func NewAttributeValueEmpty() AttributeValue {
	return AttributeValue{orig: &otlpcommon.AnyValue{}}
}

// NewAttributeValueString creates a new AttributeValue with the given string value.
func NewAttributeValueString(v string) AttributeValue {
	return AttributeValue{orig: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: v}}}
}

// NewAttributeValueInt creates a new AttributeValue with the given int64 value.
func NewAttributeValueInt(v int64) AttributeValue {
	return AttributeValue{orig: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_IntValue{IntValue: v}}}
}

// NewAttributeValueDouble creates a new AttributeValue with the given float64 value.
func NewAttributeValueDouble(v float64) AttributeValue {
	return AttributeValue{orig: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_DoubleValue{DoubleValue: v}}}
}

// NewAttributeValueBool creates a new AttributeValue with the given bool value.
func NewAttributeValueBool(v bool) AttributeValue {
	return AttributeValue{orig: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_BoolValue{BoolValue: v}}}
}

// NewAttributeValueMap creates a new AttributeValue of map type.
func NewAttributeValueMap() AttributeValue {
	return AttributeValue{orig: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_KvlistValue{KvlistValue: &otlpcommon.KeyValueList{}}}}
}

// NewAttributeValueArray creates a new AttributeValue of array type.
func NewAttributeValueArray() AttributeValue {
	return AttributeValue{orig: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_ArrayValue{ArrayValue: &otlpcommon.ArrayValue{}}}}
}

// NewAttributeValueBytes creates a new AttributeValue with the given []byte value.
// The caller must ensure the []byte passed in is not modified after the call is made, sharing the data
// across multiple attributes is forbidden.
func NewAttributeValueBytes(v []byte) AttributeValue {
	return AttributeValue{orig: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_BytesValue{BytesValue: v}}}
}

// Type returns the type of the value for this AttributeValue.
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) Type() AttributeValueType {
	if a.orig.Value == nil {
		return AttributeValueTypeEmpty
	}
	switch a.orig.Value.(type) {
	case *otlpcommon.AnyValue_StringValue:
		return AttributeValueTypeString
	case *otlpcommon.AnyValue_BoolValue:
		return AttributeValueTypeBool
	case *otlpcommon.AnyValue_IntValue:
		return AttributeValueTypeInt
	case *otlpcommon.AnyValue_DoubleValue:
		return AttributeValueTypeDouble
	case *otlpcommon.AnyValue_KvlistValue:
		return AttributeValueTypeMap
	case *otlpcommon.AnyValue_ArrayValue:
		return AttributeValueTypeArray
	case *otlpcommon.AnyValue_BytesValue:
		return AttributeValueTypeBytes
	}
	return AttributeValueTypeEmpty
}

// StringVal returns the string value associated with this AttributeValue.
// If the Type() is not AttributeValueTypeString then returns empty string.
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) StringVal() string {
	return a.orig.GetStringValue()
}

// IntVal returns the int64 value associated with this AttributeValue.
// If the Type() is not AttributeValueTypeInt then returns int64(0).
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) IntVal() int64 {
	return a.orig.GetIntValue()
}

// DoubleVal returns the float64 value associated with this AttributeValue.
// If the Type() is not AttributeValueTypeDouble then returns float64(0).
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) DoubleVal() float64 {
	return a.orig.GetDoubleValue()
}

// BoolVal returns the bool value associated with this AttributeValue.
// If the Type() is not AttributeValueTypeBool then returns false.
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) BoolVal() bool {
	return a.orig.GetBoolValue()
}

// MapVal returns the map value associated with this AttributeValue.
// If the Type() is not AttributeValueTypeMap then returns an empty map. Note that modifying
// such empty map has no effect on this AttributeValue.
//
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) MapVal() AttributeMap {
	kvlist := a.orig.GetKvlistValue()
	if kvlist == nil {
		return NewAttributeMap()
	}
	return newAttributeMap(&kvlist.Values)
}

// SliceVal returns the slice value associated with this AttributeValue.
// If the Type() is not AttributeValueTypeArray then returns an empty slice. Note that modifying
// such empty slice has no effect on this AttributeValue.
//
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) SliceVal() AttributeValueSlice {
	arr := a.orig.GetArrayValue()
	if arr == nil {
		return NewAttributeValueSlice()
	}
	return newAttributeValueSlice(&arr.Values)
}

// BytesVal returns the []byte value associated with this AttributeValue.
// If the Type() is not AttributeValueTypeBytes then returns false.
// Calling this function on zero-initialized AttributeValue will cause a panic.
// Modifying the returned []byte in-place is forbidden.
func (a AttributeValue) BytesVal() []byte {
	return a.orig.GetBytesValue()
}

// SetStringVal replaces the string value associated with this AttributeValue,
// it also changes the type to be AttributeValueTypeString.
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) SetStringVal(v string) {
	a.orig.Value = &otlpcommon.AnyValue_StringValue{StringValue: v}
}

// SetIntVal replaces the int64 value associated with this AttributeValue,
// it also changes the type to be AttributeValueTypeInt.
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) SetIntVal(v int64) {
	a.orig.Value = &otlpcommon.AnyValue_IntValue{IntValue: v}
}

// SetDoubleVal replaces the float64 value associated with this AttributeValue,
// it also changes the type to be AttributeValueTypeDouble.
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) SetDoubleVal(v float64) {
	a.orig.Value = &otlpcommon.AnyValue_DoubleValue{DoubleValue: v}
}

// SetBoolVal replaces the bool value associated with this AttributeValue,
// it also changes the type to be AttributeValueTypeBool.
// Calling this function on zero-initialized AttributeValue will cause a panic.
func (a AttributeValue) SetBoolVal(v bool) {
	a.orig.Value = &otlpcommon.AnyValue_BoolValue{BoolValue: v}
}

// SetBytesVal replaces the []byte value associated with this AttributeValue,
// it also changes the type to be AttributeValueTypeBytes.
// Calling this function on zero-initialized AttributeValue will cause a panic.
// The caller must ensure the []byte passed in is not modified after the call is made, sharing the data
// across multiple attributes is forbidden.
func (a AttributeValue) SetBytesVal(v []byte) {
	a.orig.Value = &otlpcommon.AnyValue_BytesValue{BytesValue: v}
}

// copyTo copies the value to AnyValue. Will panic if dest is nil.
func (a AttributeValue) copyTo(dest *otlpcommon.AnyValue) {
	switch v := a.orig.Value.(type) {
	case *otlpcommon.AnyValue_KvlistValue:
		kv, ok := dest.Value.(*otlpcommon.AnyValue_KvlistValue)
		if !ok {
			kv = &otlpcommon.AnyValue_KvlistValue{KvlistValue: &otlpcommon.KeyValueList{}}
			dest.Value = kv
		}
		if v.KvlistValue == nil {
			kv.KvlistValue = nil
			return
		}
		// Deep copy to dest.
		newAttributeMap(&v.KvlistValue.Values).CopyTo(newAttributeMap(&kv.KvlistValue.Values))
	case *otlpcommon.AnyValue_ArrayValue:
		av, ok := dest.Value.(*otlpcommon.AnyValue_ArrayValue)
		if !ok {
			av = &otlpcommon.AnyValue_ArrayValue{ArrayValue: &otlpcommon.ArrayValue{}}
			dest.Value = av
		}
		if v.ArrayValue == nil {
			av.ArrayValue = nil
			return
		}
		// Deep copy to dest.
		newAttributeValueSlice(&v.ArrayValue.Values).CopyTo(newAttributeValueSlice(&av.ArrayValue.Values))
	default:
		// Primitive immutable type, no need for deep copy.
		dest.Value = a.orig.Value
	}
}

// CopyTo copies the attribute to a destination.
func (a AttributeValue) CopyTo(dest AttributeValue) {
	a.copyTo(dest.orig)
}

// Equal checks for equality, it returns true if the objects are equal otherwise false.
func (a AttributeValue) Equal(av AttributeValue) bool {
	if a.orig == av.orig {
		return true
	}

	if a.orig.Value == nil || av.orig.Value == nil {
		return a.orig.Value == av.orig.Value
	}

	if a.Type() != av.Type() {
		return false
	}

	switch v := a.orig.Value.(type) {
	case *otlpcommon.AnyValue_StringValue:
		return v.StringValue == av.orig.GetStringValue()
	case *otlpcommon.AnyValue_BoolValue:
		return v.BoolValue == av.orig.GetBoolValue()
	case *otlpcommon.AnyValue_IntValue:
		return v.IntValue == av.orig.GetIntValue()
	case *otlpcommon.AnyValue_DoubleValue:
		return v.DoubleValue == av.orig.GetDoubleValue()
	case *otlpcommon.AnyValue_ArrayValue:
		vv := v.ArrayValue.GetValues()
		avv := av.orig.GetArrayValue().GetValues()
		if len(vv) != len(avv) {
			return false
		}

		for i, val := range avv {
			val := val
			newAv := newAttributeValue(&vv[i])

			// According to the specification, array values must be scalar.
			if avType := newAv.Type(); avType == AttributeValueTypeArray || avType == AttributeValueTypeMap {
				return false
			}

			if !newAv.Equal(newAttributeValue(&val)) {
				return false
			}
		}
		return true
	case *otlpcommon.AnyValue_KvlistValue:
		cc := v.KvlistValue.GetValues()
		avv := av.orig.GetKvlistValue().GetValues()
		if len(cc) != len(avv) {
			return false
		}

		am := newAttributeMap(&avv)

		for _, val := range cc {
			newAv, ok := am.Get(val.Key)
			if !ok {
				return false
			}

			if !newAv.Equal(newAttributeValue(&val.Value)) {
				return false
			}
		}
		return true
	case *otlpcommon.AnyValue_BytesValue:
		return bytes.Equal(v.BytesValue, av.orig.GetBytesValue())
	}

	return false
}

// AsString converts an OTLP AttributeValue object of any type to its equivalent string
// representation. This differs from StringVal which only returns a non-empty value
// if the AttributeValueType is AttributeValueTypeString.
func (a AttributeValue) AsString() string {
	switch a.Type() {
	case AttributeValueTypeEmpty:
		return ""

	case AttributeValueTypeString:
		return a.StringVal()

	case AttributeValueTypeBool:
		return strconv.FormatBool(a.BoolVal())

	case AttributeValueTypeDouble:
		return strconv.FormatFloat(a.DoubleVal(), 'f', -1, 64)

	case AttributeValueTypeInt:
		return strconv.FormatInt(a.IntVal(), 10)

	case AttributeValueTypeMap:
		jsonStr, _ := json.Marshal(a.MapVal().AsRaw())
		return string(jsonStr)

	case AttributeValueTypeBytes:
		return base64.StdEncoding.EncodeToString(a.BytesVal())

	case AttributeValueTypeArray:
		jsonStr, _ := json.Marshal(a.SliceVal().asRaw())
		return string(jsonStr)

	default:
		return fmt.Sprintf("<Unknown OpenTelemetry attribute value type %q>", a.Type())
	}
}

func newAttributeKeyValueString(k string, v string) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	akv := AttributeValue{&orig.Value}
	akv.SetStringVal(v)
	return orig
}

func newAttributeKeyValueInt(k string, v int64) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	akv := AttributeValue{&orig.Value}
	akv.SetIntVal(v)
	return orig
}

func newAttributeKeyValueDouble(k string, v float64) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	akv := AttributeValue{&orig.Value}
	akv.SetDoubleVal(v)
	return orig
}

func newAttributeKeyValueBool(k string, v bool) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	akv := AttributeValue{&orig.Value}
	akv.SetBoolVal(v)
	return orig
}

func newAttributeKeyValueNull(k string) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	return orig
}

func newAttributeKeyValue(k string, av AttributeValue) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	av.copyTo(&orig.Value)
	return orig
}

func newAttributeKeyValueBytes(k string, v []byte) otlpcommon.KeyValue {
	orig := otlpcommon.KeyValue{Key: k}
	akv := AttributeValue{&orig.Value}
	akv.SetBytesVal(v)
	return orig
}

// AttributeMap stores a map of attribute keys to values.
type AttributeMap struct {
	orig *[]otlpcommon.KeyValue
}

// NewAttributeMap creates a AttributeMap with 0 elements.
func NewAttributeMap() AttributeMap {
	orig := []otlpcommon.KeyValue(nil)
	return AttributeMap{&orig}
}

// NewAttributeMapFromMap creates a AttributeMap with values
// from the given map[string]AttributeValue.
func NewAttributeMapFromMap(attrMap map[string]AttributeValue) AttributeMap {
	if len(attrMap) == 0 {
		kv := []otlpcommon.KeyValue(nil)
		return AttributeMap{&kv}
	}
	origs := make([]otlpcommon.KeyValue, len(attrMap))
	ix := 0
	for k, v := range attrMap {
		origs[ix].Key = k
		v.copyTo(&origs[ix].Value)
		ix++
	}
	return AttributeMap{&origs}
}

func newAttributeMap(orig *[]otlpcommon.KeyValue) AttributeMap {
	return AttributeMap{orig}
}

// Clear erases any existing entries in this AttributeMap instance.
func (am AttributeMap) Clear() {
	*am.orig = nil
}

// EnsureCapacity increases the capacity of this AttributeMap instance, if necessary,
// to ensure that it can hold at least the number of elements specified by the capacity argument.
func (am AttributeMap) EnsureCapacity(capacity int) {
	if capacity <= cap(*am.orig) {
		return
	}
	oldOrig := *am.orig
	*am.orig = make([]otlpcommon.KeyValue, 0, capacity)
	copy(*am.orig, oldOrig)
}

// Get returns the AttributeValue associated with the key and true. Returned
// AttributeValue is not a copy, it is a reference to the value stored in this map.
// It is allowed to modify the returned value using AttributeValue.Set* functions.
// Such modification will be applied to the value stored in this map.
//
// If the key does not exist returns an invalid instance of the KeyValue and false.
// Calling any functions on the returned invalid instance will cause a panic.
func (am AttributeMap) Get(key string) (AttributeValue, bool) {
	for i := range *am.orig {
		akv := &(*am.orig)[i]
		if akv.Key == key {
			return AttributeValue{&akv.Value}, true
		}
	}
	return AttributeValue{nil}, false
}

// Delete deletes the entry associated with the key and returns true if the key
// was present in the map, otherwise returns false.
func (am AttributeMap) Delete(key string) bool {
	for i := range *am.orig {
		akv := &(*am.orig)[i]
		if akv.Key == key {
			*akv = (*am.orig)[len(*am.orig)-1]
			*am.orig = (*am.orig)[:len(*am.orig)-1]
			return true
		}
	}
	return false
}

// Insert adds the AttributeValue to the map when the key does not exist.
// No action is applied to the map where the key already exists.
//
// Calling this function with a zero-initialized AttributeValue struct will cause a panic.
//
// Important: this function should not be used if the caller has access to
// the raw value to avoid an extra allocation.
func (am AttributeMap) Insert(k string, v AttributeValue) {
	if _, existing := am.Get(k); !existing {
		*am.orig = append(*am.orig, newAttributeKeyValue(k, v))
	}
}

// InsertNull adds a null Value to the map when the key does not exist.
// No action is applied to the map where the key already exists.
func (am AttributeMap) InsertNull(k string) {
	if _, existing := am.Get(k); !existing {
		*am.orig = append(*am.orig, newAttributeKeyValueNull(k))
	}
}

// InsertString adds the string Value to the map when the key does not exist.
// No action is applied to the map where the key already exists.
func (am AttributeMap) InsertString(k string, v string) {
	if _, existing := am.Get(k); !existing {
		*am.orig = append(*am.orig, newAttributeKeyValueString(k, v))
	}
}

// InsertInt adds the int Value to the map when the key does not exist.
// No action is applied to the map where the key already exists.
func (am AttributeMap) InsertInt(k string, v int64) {
	if _, existing := am.Get(k); !existing {
		*am.orig = append(*am.orig, newAttributeKeyValueInt(k, v))
	}
}

// InsertDouble adds the double Value to the map when the key does not exist.
// No action is applied to the map where the key already exists.
func (am AttributeMap) InsertDouble(k string, v float64) {
	if _, existing := am.Get(k); !existing {
		*am.orig = append(*am.orig, newAttributeKeyValueDouble(k, v))
	}
}

// InsertBool adds the bool Value to the map when the key does not exist.
// No action is applied to the map where the key already exists.
func (am AttributeMap) InsertBool(k string, v bool) {
	if _, existing := am.Get(k); !existing {
		*am.orig = append(*am.orig, newAttributeKeyValueBool(k, v))
	}
}

// InsertBytes adds the []byte Value to the map when the key does not exist.
// No action is applied to the map where the key already exists.
// The caller must ensure the []byte passed in is not modified after the call is made, sharing the data
// across multiple attributes is forbidden.
func (am AttributeMap) InsertBytes(k string, v []byte) {
	if _, existing := am.Get(k); !existing {
		*am.orig = append(*am.orig, newAttributeKeyValueBytes(k, v))
	}
}

// Update updates an existing AttributeValue with a value.
// No action is applied to the map where the key does not exist.
//
// Calling this function with a zero-initialized AttributeValue struct will cause a panic.
//
// Important: this function should not be used if the caller has access to
// the raw value to avoid an extra allocation.
func (am AttributeMap) Update(k string, v AttributeValue) {
	if av, existing := am.Get(k); existing {
		v.copyTo(av.orig)
	}
}

// UpdateString updates an existing string Value with a value.
// No action is applied to the map where the key does not exist.
func (am AttributeMap) UpdateString(k string, v string) {
	if av, existing := am.Get(k); existing {
		av.SetStringVal(v)
	}
}

// UpdateInt updates an existing int Value with a value.
// No action is applied to the map where the key does not exist.
func (am AttributeMap) UpdateInt(k string, v int64) {
	if av, existing := am.Get(k); existing {
		av.SetIntVal(v)
	}
}

// UpdateDouble updates an existing double Value with a value.
// No action is applied to the map where the key does not exist.
func (am AttributeMap) UpdateDouble(k string, v float64) {
	if av, existing := am.Get(k); existing {
		av.SetDoubleVal(v)
	}
}

// UpdateBool updates an existing bool Value with a value.
// No action is applied to the map where the key does not exist.
func (am AttributeMap) UpdateBool(k string, v bool) {
	if av, existing := am.Get(k); existing {
		av.SetBoolVal(v)
	}
}

// UpdateBytes updates an existing []byte Value with a value.
// No action is applied to the map where the key does not exist.
// The caller must ensure the []byte passed in is not modified after the call is made, sharing the data
// across multiple attributes is forbidden.
func (am AttributeMap) UpdateBytes(k string, v []byte) {
	if av, existing := am.Get(k); existing {
		av.SetBytesVal(v)
	}
}

// Upsert performs the Insert or Update action. The AttributeValue is
// inserted to the map that did not originally have the key. The key/value is
// updated to the map where the key already existed.
//
// Calling this function with a zero-initialized AttributeValue struct will cause a panic.
//
// Important: this function should not be used if the caller has access to
// the raw value to avoid an extra allocation.
func (am AttributeMap) Upsert(k string, v AttributeValue) {
	if av, existing := am.Get(k); existing {
		v.copyTo(av.orig)
	} else {
		*am.orig = append(*am.orig, newAttributeKeyValue(k, v))
	}
}

// UpsertString performs the Insert or Update action. The AttributeValue is
// inserted to the map that did not originally have the key. The key/value is
// updated to the map where the key already existed.
func (am AttributeMap) UpsertString(k string, v string) {
	if av, existing := am.Get(k); existing {
		av.SetStringVal(v)
	} else {
		*am.orig = append(*am.orig, newAttributeKeyValueString(k, v))
	}
}

// UpsertInt performs the Insert or Update action. The int Value is
// inserted to the map that did not originally have the key. The key/value is
// updated to the map where the key already existed.
func (am AttributeMap) UpsertInt(k string, v int64) {
	if av, existing := am.Get(k); existing {
		av.SetIntVal(v)
	} else {
		*am.orig = append(*am.orig, newAttributeKeyValueInt(k, v))
	}
}

// UpsertDouble performs the Insert or Update action. The double Value is
// inserted to the map that did not originally have the key. The key/value is
// updated to the map where the key already existed.
func (am AttributeMap) UpsertDouble(k string, v float64) {
	if av, existing := am.Get(k); existing {
		av.SetDoubleVal(v)
	} else {
		*am.orig = append(*am.orig, newAttributeKeyValueDouble(k, v))
	}
}

// UpsertBool performs the Insert or Update action. The bool Value is
// inserted to the map that did not originally have the key. The key/value is
// updated to the map where the key already existed.
func (am AttributeMap) UpsertBool(k string, v bool) {
	if av, existing := am.Get(k); existing {
		av.SetBoolVal(v)
	} else {
		*am.orig = append(*am.orig, newAttributeKeyValueBool(k, v))
	}
}

// UpsertBytes performs the Insert or Update action. The []byte Value is
// inserted to the map that did not originally have the key. The key/value is
// updated to the map where the key already existed.
// The caller must ensure the []byte passed in is not modified after the call is made, sharing the data
// across multiple attributes is forbidden.
func (am AttributeMap) UpsertBytes(k string, v []byte) {
	if av, existing := am.Get(k); existing {
		av.SetBytesVal(v)
	} else {
		*am.orig = append(*am.orig, newAttributeKeyValueBytes(k, v))
	}
}

// Sort sorts the entries in the AttributeMap so two instances can be compared.
// Returns the same instance to allow nicer code like:
//   assert.EqualValues(t, expected.Sort(), actual.Sort())
func (am AttributeMap) Sort() AttributeMap {
	// Intention is to move the nil values at the end.
	sort.SliceStable(*am.orig, func(i, j int) bool {
		return (*am.orig)[i].Key < (*am.orig)[j].Key
	})
	return am
}

// Len returns the length of this map.
//
// Because the AttributeMap is represented internally by a slice of pointers, and the data are comping from the wire,
// it is possible that when iterating using "Range" to get access to fewer elements because nil elements are skipped.
func (am AttributeMap) Len() int {
	return len(*am.orig)
}

// Range calls f sequentially for each key and value present in the map. If f returns false, range stops the iteration.
//
// Example:
//
//   sm.Range(func(k string, v AttributeValue) bool {
//       ...
//   })
func (am AttributeMap) Range(f func(k string, v AttributeValue) bool) {
	for i := range *am.orig {
		kv := &(*am.orig)[i]
		if !f(kv.Key, AttributeValue{&kv.Value}) {
			break
		}
	}
}

// CopyTo copies all elements from the current map to the dest.
func (am AttributeMap) CopyTo(dest AttributeMap) {
	newLen := len(*am.orig)
	oldCap := cap(*dest.orig)
	if newLen <= oldCap {
		// New slice fits in existing slice, no need to reallocate.
		*dest.orig = (*dest.orig)[:newLen:oldCap]
		for i := range *am.orig {
			akv := &(*am.orig)[i]
			destAkv := &(*dest.orig)[i]
			destAkv.Key = akv.Key
			AttributeValue{&akv.Value}.copyTo(&destAkv.Value)
		}
		return
	}

	// New slice is bigger than exist slice. Allocate new space.
	origs := make([]otlpcommon.KeyValue, len(*am.orig))
	for i := range *am.orig {
		akv := &(*am.orig)[i]
		origs[i].Key = akv.Key
		AttributeValue{&akv.Value}.copyTo(&origs[i].Value)
	}
	*dest.orig = origs
}

// AsRaw converts an OTLP AttributeMap to a standard go map
func (am AttributeMap) AsRaw() map[string]interface{} {
	rawMap := make(map[string]interface{})
	am.Range(func(k string, v AttributeValue) bool {
		switch v.Type() {
		case AttributeValueTypeString:
			rawMap[k] = v.StringVal()
		case AttributeValueTypeInt:
			rawMap[k] = v.IntVal()
		case AttributeValueTypeDouble:
			rawMap[k] = v.DoubleVal()
		case AttributeValueTypeBool:
			rawMap[k] = v.BoolVal()
		case AttributeValueTypeBytes:
			rawMap[k] = v.BytesVal()
		case AttributeValueTypeEmpty:
			rawMap[k] = nil
		case AttributeValueTypeMap:
			rawMap[k] = v.MapVal().AsRaw()
		case AttributeValueTypeArray:
			rawMap[k] = v.SliceVal().asRaw()
		}
		return true
	})
	return rawMap
}

// asRaw creates a slice out of a AttributeValueSlice.
func (es AttributeValueSlice) asRaw() []interface{} {
	rawSlice := make([]interface{}, 0, es.Len())
	for i := 0; i < es.Len(); i++ {
		v := es.At(i)
		switch v.Type() {
		case AttributeValueTypeString:
			rawSlice = append(rawSlice, v.StringVal())
		case AttributeValueTypeInt:
			rawSlice = append(rawSlice, v.IntVal())
		case AttributeValueTypeDouble:
			rawSlice = append(rawSlice, v.DoubleVal())
		case AttributeValueTypeBool:
			rawSlice = append(rawSlice, v.BoolVal())
		case AttributeValueTypeBytes:
			rawSlice = append(rawSlice, v.BytesVal())
		case AttributeValueTypeEmpty:
			rawSlice = append(rawSlice, nil)
		default:
			rawSlice = append(rawSlice, "<Invalid array value>")
		}
	}
	return rawSlice
}
