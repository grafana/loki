// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package metadatapb

import (
	"unsafe"
)

func NewMetadataResponse(metadata *MetricMetadata) *MetadataResponse {
	return &MetadataResponse{
		Result: &MetadataResponse_Metadata{
			Metadata: metadata,
		},
	}
}

func NewWarningMetadataResponse(warning error) *MetadataResponse {
	return &MetadataResponse{
		Result: &MetadataResponse_Warning{
			Warning: warning.Error(),
		},
	}
}

func FromMetadataMap(m map[string][]Meta) *MetricMetadata {
	return &MetricMetadata{Metadata: *(*map[string]MetricMetadataEntry)(unsafe.Pointer(&m))}
}
