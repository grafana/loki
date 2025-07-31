// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pmetricotlp // import "go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/internal"
	otlpcollectormetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/metrics/v1"
	"go.opentelemetry.io/collector/pdata/internal/json"
)

// ExportResponse represents the response for gRPC/HTTP client/server.
type ExportResponse struct {
	orig  *otlpcollectormetrics.ExportMetricsServiceResponse
	state *internal.State
}

// NewExportResponse returns an empty ExportResponse.
func NewExportResponse() ExportResponse {
	state := internal.StateMutable
	return ExportResponse{
		orig:  &otlpcollectormetrics.ExportMetricsServiceResponse{},
		state: &state,
	}
}

// MarshalProto marshals ExportResponse into proto bytes.
func (ms ExportResponse) MarshalProto() ([]byte, error) {
	return ms.orig.Marshal()
}

// UnmarshalProto unmarshalls ExportResponse from proto bytes.
func (ms ExportResponse) UnmarshalProto(data []byte) error {
	return ms.orig.Unmarshal(data)
}

// MarshalJSON marshals ExportResponse into JSON bytes.
func (ms ExportResponse) MarshalJSON() ([]byte, error) {
	dest := json.BorrowStream(nil)
	defer json.ReturnStream(dest)
	dest.WriteObjectStart()
	dest.WriteObjectField("partialSuccess")
	ms.PartialSuccess().marshalJSONStream(dest)
	dest.WriteObjectEnd()
	return slices.Clone(dest.Buffer()), dest.Error()
}

// UnmarshalJSON unmarshalls ExportResponse from JSON bytes.
func (ms ExportResponse) UnmarshalJSON(data []byte) error {
	iter := json.BorrowIterator(data)
	defer json.ReturnIterator(iter)
	ms.unmarshalJSONIter(iter)
	return iter.Error()
}

// PartialSuccess returns the ExportLogsPartialSuccess associated with this ExportResponse.
func (ms ExportResponse) PartialSuccess() ExportPartialSuccess {
	return newExportPartialSuccess(&ms.orig.PartialSuccess, ms.state)
}

func (ms ExportResponse) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(iter *json.Iterator, f string) bool {
		switch f {
		case "partial_success", "partialSuccess":
			ms.PartialSuccess().unmarshalJSONIter(iter)
		default:
			iter.Skip()
		}
		return true
	})
}

func (ms ExportPartialSuccess) unmarshalJSONIter(iter *json.Iterator) {
	iter.ReadObjectCB(func(_ *json.Iterator, f string) bool {
		switch f {
		case "rejected_data_points", "rejectedDataPoints":
			ms.orig.RejectedDataPoints = iter.ReadInt64()
		case "error_message", "errorMessage":
			ms.orig.ErrorMessage = iter.ReadString()
		default:
			iter.Skip()
		}
		return true
	})
}
