// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pmetricotlp // import "go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"

import (
	"go.opentelemetry.io/collector/pdata/internal"
	otlpcollectormetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/metrics/v1"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

var (
	jsonMarshaler   = &pmetric.JSONMarshaler{}
	jsonUnmarshaler = &pmetric.JSONUnmarshaler{}
)

// ExportRequest represents the request for gRPC/HTTP client/server.
// It's a wrapper for pmetric.Metrics data.
type ExportRequest struct {
	orig  *otlpcollectormetrics.ExportMetricsServiceRequest
	state *internal.State
}

// NewExportRequest returns an empty ExportRequest.
func NewExportRequest() ExportRequest {
	return ExportRequest{
		orig:  &otlpcollectormetrics.ExportMetricsServiceRequest{},
		state: internal.NewState(),
	}
}

// NewExportRequestFromMetrics returns a ExportRequest from pmetric.Metrics.
// Because ExportRequest is a wrapper for pmetric.Metrics,
// any changes to the provided Metrics struct will be reflected in the ExportRequest and vice versa.
func NewExportRequestFromMetrics(md pmetric.Metrics) ExportRequest {
	return ExportRequest{
		orig:  internal.GetOrigMetrics(internal.Metrics(md)),
		state: internal.GetMetricsState(internal.Metrics(md)),
	}
}

// MarshalProto marshals ExportRequest into proto bytes.
func (ms ExportRequest) MarshalProto() ([]byte, error) {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return ms.orig.Marshal()
	}
	size := internal.SizeProtoOrigExportMetricsServiceRequest(ms.orig)
	buf := make([]byte, size)
	_ = internal.MarshalProtoOrigExportMetricsServiceRequest(ms.orig, buf)
	return buf, nil
}

// UnmarshalProto unmarshalls ExportRequest from proto bytes.
func (ms ExportRequest) UnmarshalProto(data []byte) error {
	if !internal.UseCustomProtoEncoding.IsEnabled() {
		return ms.orig.Unmarshal(data)
	}
	err := internal.UnmarshalProtoOrigExportMetricsServiceRequest(ms.orig, data)
	if err != nil {
		return err
	}
	otlp.MigrateMetrics(ms.orig.ResourceMetrics)
	return nil
}

// MarshalJSON marshals ExportRequest into JSON bytes.
func (ms ExportRequest) MarshalJSON() ([]byte, error) {
	return jsonMarshaler.MarshalMetrics(pmetric.Metrics(internal.NewMetrics(ms.orig, nil)))
}

// UnmarshalJSON unmarshalls ExportRequest from JSON bytes.
func (ms ExportRequest) UnmarshalJSON(data []byte) error {
	md, err := jsonUnmarshaler.UnmarshalMetrics(data)
	if err != nil {
		return err
	}
	*ms.orig = *internal.GetOrigMetrics(internal.Metrics(md))
	return nil
}

func (ms ExportRequest) Metrics() pmetric.Metrics {
	return pmetric.Metrics(internal.NewMetrics(ms.orig, ms.state))
}
