// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"go.opentelemetry.io/collector/pdata/internal"
	"go.opentelemetry.io/collector/pdata/internal/metadata"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// mapKeyValues returns the underlying KeyValue slice of a pcommon.Map.
func mapKeyValues(m pcommon.Map) []internal.KeyValue {
	return *internal.GetMapOrig(internal.MapWrapper(m))
}

// resolveProfilesReferences walks through all profiles data after unmarshaling
// and resolves any string_value_ref and key_ref to their actual string values.
// This ensures the pdata API works transparently with referenced strings.
func resolveProfilesReferences(profiles Profiles) {
	dict := profiles.Dictionary()

	// Resolve references in resource attributes
	for i := 0; i < profiles.ResourceProfiles().Len(); i++ {
		rp := profiles.ResourceProfiles().At(i)
		resolveKeyValueReferences(dict, mapKeyValues(rp.Resource().Attributes()))

		// Resolve references in scope attributes
		for j := 0; j < rp.ScopeProfiles().Len(); j++ {
			sp := rp.ScopeProfiles().At(j)
			resolveKeyValueReferences(dict, mapKeyValues(sp.Scope().Attributes()))
		}
	}
}

// resolveKeyValueReferences resolves key_ref and string_value_ref in a KeyValue slice
func resolveKeyValueReferences(dict ProfilesDictionary, kvs []internal.KeyValue) {
	for i := range kvs {
		kv := &kvs[i]
		// Resolve key_ref if set
		if kv.KeyStrindex >= 0 {
			idx := int(kv.KeyStrindex)
			if idx < dict.StringTable().Len() {
				kv.Key = dict.StringTable().At(idx)
				// N.b. keep KeyStrindex set to optimize re-marshaling. This is
				// technically a violation of the proto spec, but acceptable
				// for the in-memory pdata API since keys are immutable.
			}
		}
		// Resolve string_value_ref if set
		resolveAnyValueReference(dict, &kv.Value)
	}
}

// resolveAnyValueReference resolves string_value_ref in an AnyValue
func resolveAnyValueReference(dict ProfilesDictionary, anyValue *internal.AnyValue) {
	if ref, ok := anyValue.Value.(*internal.AnyValue_StringValueStrindex); ok && ref.StringValueStrindex != 0 {
		idx := int(ref.StringValueStrindex)
		if idx >= 0 && idx < dict.StringTable().Len() {
			str := dict.StringTable().At(idx)
			var ov *internal.AnyValue_StringValue
			if !metadata.PdataUseProtoPoolingFeatureGate.IsEnabled() {
				ov = &internal.AnyValue_StringValue{}
			} else {
				ov = internal.ProtoPoolAnyValue_StringValue.Get().(*internal.AnyValue_StringValue)
			}
			ov.StringValue = str
			anyValue.Value = ov
		}
	} else if kvList, ok := anyValue.Value.(*internal.AnyValue_KvlistValue); ok && kvList.KvlistValue != nil {
		resolveKeyValueReferences(dict, kvList.KvlistValue.Values)
	} else if arrVal, ok := anyValue.Value.(*internal.AnyValue_ArrayValue); ok && arrVal.ArrayValue != nil {
		for i := 0; i < len(arrVal.ArrayValue.Values); i++ {
			resolveAnyValueReference(dict, &arrVal.ArrayValue.Values[i])
		}
	}
}

// convertProfilesToReferences walks through all profiles data before marshaling
// and converts string values to references for efficient transmission.
// This builds up the string table in the dictionary and replaces strings with refs.
func convertProfilesToReferences(profiles Profiles) {
	dict := profiles.Dictionary()
	stringTable := dict.StringTable()

	// Map for quick string lookups - only allocate if needed
	var stringIndex map[string]int32
	getStringIndex := func(s string) int32 {
		if stringIndex == nil {
			stringIndex = make(map[string]int32, stringTable.Len())
			for i := 0; i < stringTable.Len(); i++ {
				stringIndex[stringTable.At(i)] = int32(i)
			}
		}

		if idx, ok := stringIndex[s]; ok {
			return idx
		}
		idx := int32(stringTable.Len())
		stringTable.Append(s)
		stringIndex[s] = idx
		return idx
	}

	// Convert strings in resource attributes
	for i := 0; i < profiles.ResourceProfiles().Len(); i++ {
		rp := profiles.ResourceProfiles().At(i)
		convertKeyValueToReferences(getStringIndex, mapKeyValues(rp.Resource().Attributes()))

		// Convert strings in scope attributes
		for j := 0; j < rp.ScopeProfiles().Len(); j++ {
			sp := rp.ScopeProfiles().At(j)
			convertKeyValueToReferences(getStringIndex, mapKeyValues(sp.Scope().Attributes()))
		}
	}
}

// convertKeyValueToReferences converts string keys and values to references in a KeyValue slice
func convertKeyValueToReferences(getStringIndex func(string) int32, kvs []internal.KeyValue) {
	for i := range kvs {
		kv := &kvs[i]

		// Convert key to reference
		if kv.Key != "" {
			kv.KeyStrindex = getStringIndex(kv.Key)
			kv.Key = ""
		}

		// Convert string values to references
		convertAnyValueToReference(getStringIndex, &kv.Value)
	}
}

// convertAnyValueToReference converts string values to string_value_ref
func convertAnyValueToReference(getStringIndex func(string) int32, anyValue *internal.AnyValue) {
	// Skip if already a reference
	if _, ok := anyValue.Value.(*internal.AnyValue_StringValueStrindex); ok {
		return
	}

	if strVal, ok := anyValue.Value.(*internal.AnyValue_StringValue); ok && strVal.StringValue != "" {
		// Convert to reference
		idx := getStringIndex(strVal.StringValue)
		var ov *internal.AnyValue_StringValueStrindex
		if !metadata.PdataUseProtoPoolingFeatureGate.IsEnabled() {
			ov = &internal.AnyValue_StringValueStrindex{}
		} else {
			ov = internal.ProtoPoolAnyValue_StringValueStrindex.Get().(*internal.AnyValue_StringValueStrindex)
		}
		ov.StringValueStrindex = idx
		anyValue.Value = ov
	} else if kvList, ok := anyValue.Value.(*internal.AnyValue_KvlistValue); ok && kvList.KvlistValue != nil {
		convertKeyValueToReferences(getStringIndex, kvList.KvlistValue.Values)
	} else if arrVal, ok := anyValue.Value.(*internal.AnyValue_ArrayValue); ok && arrVal.ArrayValue != nil {
		// Recursively convert arrays
		for i := 0; i < len(arrVal.ArrayValue.Values); i++ {
			convertAnyValueToReference(getStringIndex, &arrVal.ArrayValue.Values[i])
		}
	}
}
