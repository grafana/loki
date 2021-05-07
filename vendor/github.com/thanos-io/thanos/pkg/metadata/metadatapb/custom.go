// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package metadatapb

import (
	"unsafe"
)

func NewMetricMetadataResponse(metadata *MetricMetadata) *MetricMetadataResponse {
	return &MetricMetadataResponse{
		Result: &MetricMetadataResponse_Metadata{
			Metadata: metadata,
		},
	}
}

func NewWarningMetadataResponse(warning error) *MetricMetadataResponse {
	return &MetricMetadataResponse{
		Result: &MetricMetadataResponse_Warning{
			Warning: warning.Error(),
		},
	}
}

func FromMetadataMap(m map[string][]Meta) *MetricMetadata {
	return &MetricMetadata{Metadata: *(*map[string]MetricMetadataEntry)(unsafe.Pointer(&m))}
}
