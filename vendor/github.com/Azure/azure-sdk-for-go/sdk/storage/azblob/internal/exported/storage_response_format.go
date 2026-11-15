// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package exported

// StorageResponseFormat specifies the format the service should use to return list results.
type StorageResponseFormat string

const (
	// StorageResponseFormatAuto lets the SDK choose the response format.
	// For the current release this resolves to XML; a future release will resolve to Arrow.
	StorageResponseFormatAuto StorageResponseFormat = ""

	// StorageResponseFormatXML forces XML-only responses. Use when you need raw XML responses (e.g. debugging).
	StorageResponseFormatXML StorageResponseFormat = "xml"

	// StorageResponseFormatArrow sends both Arrow and XML in the Accept header; the service returns Arrow
	// when Photon is enabled, otherwise falls back to XML. The SDK handles both transparently.
	// Arrow format is only supported on non-HNS (hierarchical namespace) accounts via the Blob endpoint.
	StorageResponseFormatArrow StorageResponseFormat = "arrow"
)

// ResolveAutoFormat returns the effective format, mapping Auto (zero value) to the default for the current release.
func ResolveAutoFormat(f StorageResponseFormat) StorageResponseFormat {
	if f == StorageResponseFormatAuto {
		return StorageResponseFormatXML
	}
	return f
}
