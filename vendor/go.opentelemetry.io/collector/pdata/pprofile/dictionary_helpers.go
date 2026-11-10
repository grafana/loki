// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pprofile // import "go.opentelemetry.io/collector/pdata/pprofile"

import (
	"go.opentelemetry.io/collector/pdata/internal"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

// mapKeyValues returns the underlying KeyValue slice of a pcommon.Map.
func mapKeyValues(m pcommon.Map) []internal.KeyValue {
	return *internal.GetMapOrig(internal.MapWrapper(m))
}

// resolveProfilesReferences walks through all profiles data after unmarshaling
// and resolves any string_value_ref and key_ref to their actual string values.
// This ensures the pdata API works transparently with referenced strings.
func resolveProfilesReferences(profiles Profiles) error {
	return otlp.ResolveProfilesReferences(profiles.getOrig())
}

// resolveAnyValueReference resolves string_value_ref in an AnyValue
func resolveAnyValueReference(dict ProfilesDictionary, anyValue *internal.AnyValue) error {
	return otlp.ResolveProfilesAnyValueReference(dict.StringTable().AsRaw(), anyValue)
}

// convertProfilesToReferences walks through all profiles data before marshaling
// and converts string values to references for efficient transmission.
// This builds up the string table in the dictionary and replaces strings with refs.
func convertProfilesToReferences(profiles Profiles) error {
	return otlp.ConvertProfilesToReferences(profiles.getOrig())
}

// convertKeyValueToReferences converts string keys and values to references in a KeyValue slice
func convertKeyValueToReferences(getStringIndex func(string) int32, kvs []internal.KeyValue) error {
	return otlp.ConvertProfilesKeyValuesToReferences(
		func(value string) (int32, error) {
			return getStringIndex(value), nil
		},
		kvs,
	)
}

// convertAnyValueToReference converts string values to string_value_ref
func convertAnyValueToReference(getStringIndex func(string) int32, anyValue *internal.AnyValue) error {
	return otlp.ConvertProfilesAnyValueToReference(
		func(value string) (int32, error) {
			return getStringIndex(value), nil
		},
		anyValue,
	)
}
