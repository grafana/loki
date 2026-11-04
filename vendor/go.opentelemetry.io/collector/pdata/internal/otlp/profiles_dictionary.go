// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlp // import "go.opentelemetry.io/collector/pdata/internal/otlp"

import (
	"errors"
	"fmt"

	"go.opentelemetry.io/collector/pdata/internal"
	"go.opentelemetry.io/collector/pdata/internal/metadata"
)

var errProfilesStringIndexNotInitialized = errors.New("profiles dictionary string index is not initialized")

// ConvertProfilesToReferences interns resource and scope attribute strings in
// the profiles dictionary. The request must be mutable.
func ConvertProfilesToReferences(request *internal.ExportProfilesServiceRequest) error {
	stringTable := &request.Dictionary.StringTable
	if len(*stringTable) == 0 {
		// string_table[0] is the required empty-string sentinel.
		*stringTable = append(*stringTable, "")
	}

	stringIndex := make(map[string]int32, len(*stringTable))
	for i, value := range *stringTable {
		if i == 0 && value != "" {
			// Preserve existing dictionary indices without using the reserved
			// zero index for a new attribute key.
			continue
		}
		// Keep the first occurrence so the required empty string resolves to
		// the canonical index 0 even if the table already contains duplicates.
		if _, ok := stringIndex[value]; !ok {
			stringIndex[value] = int32(i)
		}
	}

	getStringIndex := func(s string) (int32, error) {
		if stringIndex == nil {
			return 0, errProfilesStringIndexNotInitialized
		}
		if index, ok := stringIndex[s]; ok {
			return index, nil
		}
		index := int32(len(*stringTable))
		*stringTable = append(*stringTable, s)
		stringIndex[s] = index
		return index, nil
	}

	for resourceIndex, resourceProfiles := range request.ResourceProfiles {
		if err := ConvertProfilesKeyValuesToReferences(getStringIndex, resourceProfiles.Resource.Attributes); err != nil {
			return fmt.Errorf("resource profiles %d resource attributes: %w", resourceIndex, err)
		}
		for scopeIndex, scopeProfiles := range resourceProfiles.ScopeProfiles {
			if err := ConvertProfilesKeyValuesToReferences(getStringIndex, scopeProfiles.Scope.Attributes); err != nil {
				return fmt.Errorf("resource profiles %d scope profiles %d attributes: %w", resourceIndex, scopeIndex, err)
			}
		}
	}
	return nil
}

// ResolveProfilesReferences resolves resource and scope attribute string-table
// references so pdata consumers can access the attributes transparently.
func ResolveProfilesReferences(request *internal.ExportProfilesServiceRequest) error {
	stringTable := request.Dictionary.StringTable
	for resourceIndex, resourceProfiles := range request.ResourceProfiles {
		if err := ResolveProfilesKeyValueReferences(stringTable, resourceProfiles.Resource.Attributes); err != nil {
			return fmt.Errorf("resource profiles %d resource attributes: %w", resourceIndex, err)
		}
		for scopeIndex, scopeProfiles := range resourceProfiles.ScopeProfiles {
			if err := ResolveProfilesKeyValueReferences(stringTable, scopeProfiles.Scope.Attributes); err != nil {
				return fmt.Errorf("resource profiles %d scope profiles %d attributes: %w", resourceIndex, scopeIndex, err)
			}
		}
	}
	return nil
}

// ConvertProfilesKeyValuesToReferences converts attribute strings to dictionary references.
func ConvertProfilesKeyValuesToReferences(getStringIndex func(string) (int32, error), keyValues []internal.KeyValue) error {
	for i := range keyValues {
		keyValue := &keyValues[i]
		if keyValue.Key != "" && keyValue.KeyStrindex != 0 {
			return fmt.Errorf("attribute %d has both key and key_strindex set", i)
		}
		if keyValue.Key != "" {
			index, err := getStringIndex(keyValue.Key)
			if err != nil {
				return err
			}
			keyValue.KeyStrindex = index
			keyValue.Key = ""
		}
		if err := ConvertProfilesAnyValueToReference(getStringIndex, &keyValue.Value); err != nil {
			return err
		}
	}
	return nil
}

// ConvertProfilesAnyValueToReference converts strings recursively in an attribute value.
func ConvertProfilesAnyValueToReference(getStringIndex func(string) (int32, error), value *internal.AnyValue) error {
	switch original := value.Value.(type) {
	case *internal.AnyValue_StringValueStrindex:
		return nil
	case *internal.AnyValue_StringValue:
		index, err := getStringIndex(original.StringValue)
		if err != nil {
			return err
		}
		var reference *internal.AnyValue_StringValueStrindex
		if metadata.PdataUseProtoPoolingFeatureGate.IsEnabled() {
			reference = internal.ProtoPoolAnyValue_StringValueStrindex.Get().(*internal.AnyValue_StringValueStrindex)
		} else {
			reference = &internal.AnyValue_StringValueStrindex{}
		}
		reference.StringValueStrindex = index
		value.Value = reference
	case *internal.AnyValue_KvlistValue:
		if original.KvlistValue != nil {
			return ConvertProfilesKeyValuesToReferences(getStringIndex, original.KvlistValue.Values)
		}
	case *internal.AnyValue_ArrayValue:
		if original.ArrayValue != nil {
			for i := range original.ArrayValue.Values {
				if err := ConvertProfilesAnyValueToReference(getStringIndex, &original.ArrayValue.Values[i]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ResolveProfilesKeyValueReferences resolves attribute dictionary references.
func ResolveProfilesKeyValueReferences(stringTable []string, keyValues []internal.KeyValue) error {
	for i := range keyValues {
		keyValue := &keyValues[i]
		if keyValue.Key != "" && keyValue.KeyStrindex != 0 {
			return fmt.Errorf("attribute %d has both key and key_strindex set", i)
		}
		if keyValue.KeyStrindex < 0 || (keyValue.KeyStrindex > 0 && int(keyValue.KeyStrindex) >= len(stringTable)) {
			return fmt.Errorf("attribute %d has invalid key_strindex %d", i, keyValue.KeyStrindex)
		}
		if keyValue.KeyStrindex > 0 {
			keyValue.Key = stringTable[keyValue.KeyStrindex]
			keyValue.KeyStrindex = 0
		}
		if err := ResolveProfilesAnyValueReference(stringTable, &keyValue.Value); err != nil {
			return fmt.Errorf("attribute %d value: %w", i, err)
		}
	}
	return nil
}

// ResolveProfilesAnyValueReference resolves string references recursively in an attribute value.
func ResolveProfilesAnyValueReference(stringTable []string, value *internal.AnyValue) error {
	switch original := value.Value.(type) {
	case *internal.AnyValue_StringValueStrindex:
		if original.StringValueStrindex < 0 || int(original.StringValueStrindex) >= len(stringTable) {
			return fmt.Errorf("invalid string_value_strindex %d", original.StringValueStrindex)
		}
		var resolved *internal.AnyValue_StringValue
		if metadata.PdataUseProtoPoolingFeatureGate.IsEnabled() {
			resolved = internal.ProtoPoolAnyValue_StringValue.Get().(*internal.AnyValue_StringValue)
		} else {
			resolved = &internal.AnyValue_StringValue{}
		}
		resolved.StringValue = stringTable[original.StringValueStrindex]
		value.Value = resolved
	case *internal.AnyValue_KvlistValue:
		if original.KvlistValue != nil {
			return ResolveProfilesKeyValueReferences(stringTable, original.KvlistValue.Values)
		}
	case *internal.AnyValue_ArrayValue:
		if original.ArrayValue != nil {
			for i := range original.ArrayValue.Values {
				if err := ResolveProfilesAnyValueReference(stringTable, &original.ArrayValue.Values[i]); err != nil {
					return fmt.Errorf("array value %d: %w", i, err)
				}
			}
		}
	}
	return nil
}
