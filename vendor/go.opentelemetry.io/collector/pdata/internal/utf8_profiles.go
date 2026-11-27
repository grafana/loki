// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

// SanitizeUTF8ExportProfilesServiceRequest replaces invalid UTF-8 byte sequences in every string field of req
// with the Unicode replacement character U+FFFD. Strings referenced through the profiles
// dictionary are sanitized once in the dictionary string table.
func SanitizeUTF8ExportProfilesServiceRequest(req *ExportProfilesServiceRequest) {
	SanitizeUTF8StringSlice(req.Dictionary.StringTable)
	for _, kv := range req.Dictionary.AttributeTable {
		SanitizeUTF8AnyValue(&kv.Value)
	}
	for _, rp := range req.ResourceProfiles {
		SanitizeUTF8Resource(&rp.Resource)
		rp.SchemaUrl = SanitizeUTF8String(rp.SchemaUrl)
		for _, sp := range rp.ScopeProfiles {
			SanitizeUTF8InstrumentationScope(&sp.Scope)
			sp.SchemaUrl = SanitizeUTF8String(sp.SchemaUrl)
			for _, p := range sp.Profiles {
				p.OriginalPayloadFormat = SanitizeUTF8String(p.OriginalPayloadFormat)
			}
		}
	}
}
