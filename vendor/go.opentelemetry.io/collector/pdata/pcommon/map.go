// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pcommon // import "go.opentelemetry.io/collector/pdata/pcommon"

import (
	"go.uber.org/multierr"

	"go.opentelemetry.io/collector/pdata/internal"
	otlpcommon "go.opentelemetry.io/collector/pdata/internal/data/protogen/common/v1"
)

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
	oldOrig := *m.getOrig()
	if capacity <= cap(oldOrig) {
		return
	}
	*m.getOrig() = make([]otlpcommon.KeyValue, len(oldOrig), capacity)
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

// AsRaw returns a standard go map representation of this Map.
func (m Map) AsRaw() map[string]any {
	rawMap := make(map[string]any)
	m.Range(func(k string, v Value) bool {
		rawMap[k] = v.AsRaw()
		return true
	})
	return rawMap
}

// FromRaw overrides this Map instance from a standard go map.
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
