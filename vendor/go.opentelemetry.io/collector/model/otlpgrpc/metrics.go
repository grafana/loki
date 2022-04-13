// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package otlpgrpc // import "go.opentelemetry.io/collector/model/otlpgrpc"

import (
	"bytes"
	"context"

	"google.golang.org/grpc"

	otlpcollectormetrics "go.opentelemetry.io/collector/model/internal/data/protogen/collector/metrics/v1"
	v1 "go.opentelemetry.io/collector/model/internal/data/protogen/common/v1"
	otlpmetrics "go.opentelemetry.io/collector/model/internal/data/protogen/metrics/v1"
	ipdata "go.opentelemetry.io/collector/model/internal/pdata"
	"go.opentelemetry.io/collector/model/pdata"
)

// MetricsResponse represents the response for gRPC client/server.
type MetricsResponse struct {
	orig *otlpcollectormetrics.ExportMetricsServiceResponse
}

// NewMetricsResponse returns an empty MetricsResponse.
func NewMetricsResponse() MetricsResponse {
	return MetricsResponse{orig: &otlpcollectormetrics.ExportMetricsServiceResponse{}}
}

// Deprecated: [v0.48.0] use MetricsResponse.UnmarshalProto.
func UnmarshalMetricsResponse(data []byte) (MetricsResponse, error) {
	mr := NewMetricsResponse()
	err := mr.UnmarshalProto(data)
	return mr, err
}

// Deprecated: [v0.48.0] use MetricsResponse.UnmarshalJSON.
func UnmarshalJSONMetricsResponse(data []byte) (MetricsResponse, error) {
	mr := NewMetricsResponse()
	err := mr.UnmarshalJSON(data)
	return mr, err
}

// Deprecated: [v0.48.0] use MarshalProto.
func (mr MetricsResponse) Marshal() ([]byte, error) {
	return mr.MarshalProto()
}

// MarshalProto marshals MetricsResponse into proto bytes.
func (mr MetricsResponse) MarshalProto() ([]byte, error) {
	return mr.orig.Marshal()
}

// UnmarshalProto unmarshalls MetricsResponse from proto bytes.
func (mr MetricsResponse) UnmarshalProto(data []byte) error {
	return mr.orig.Unmarshal(data)
}

// MarshalJSON marshals MetricsResponse into JSON bytes.
func (mr MetricsResponse) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, mr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalJSON unmarshalls MetricsResponse from JSON bytes.
func (mr MetricsResponse) UnmarshalJSON(data []byte) error {
	return jsonUnmarshaler.Unmarshal(bytes.NewReader(data), mr.orig)
}

// MetricsRequest represents the response for gRPC client/server.
type MetricsRequest struct {
	orig *otlpcollectormetrics.ExportMetricsServiceRequest
}

// NewMetricsRequest returns an empty MetricsRequest.
func NewMetricsRequest() MetricsRequest {
	return MetricsRequest{orig: &otlpcollectormetrics.ExportMetricsServiceRequest{}}
}

// Deprecated: [v0.48.0] use MetricsRequest.UnmarshalProto.
func UnmarshalMetricsRequest(data []byte) (MetricsRequest, error) {
	mr := NewMetricsRequest()
	err := mr.UnmarshalProto(data)
	return mr, err
}

// Deprecated: [v0.48.0] use MetricsRequest.UnmarshalJSON.
func UnmarshalJSONMetricsRequest(data []byte) (MetricsRequest, error) {
	mr := NewMetricsRequest()
	err := mr.UnmarshalJSON(data)
	return mr, err
}

// Deprecated: [v0.48.0] use MarshalProto.
func (mr MetricsRequest) Marshal() ([]byte, error) {
	return mr.MarshalProto()
}

// MarshalProto marshals MetricsRequest into proto bytes.
func (mr MetricsRequest) MarshalProto() ([]byte, error) {
	return mr.orig.Marshal()
}

// UnmarshalProto unmarshalls MetricsRequest from proto bytes.
func (mr MetricsRequest) UnmarshalProto(data []byte) error {
	return mr.orig.Unmarshal(data)
}

// MarshalJSON marshals MetricsRequest into JSON bytes.
func (mr MetricsRequest) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, mr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalJSON unmarshalls MetricsRequest from JSON bytes.
func (mr MetricsRequest) UnmarshalJSON(data []byte) error {
	if err := jsonUnmarshaler.Unmarshal(bytes.NewReader(data), mr.orig); err != nil {
		return err
	}
	InstrumentationLibraryMetricsToScope(mr.orig.ResourceMetrics)
	return nil
}

func (mr MetricsRequest) SetMetrics(ld pdata.Metrics) {
	mr.orig.ResourceMetrics = ipdata.MetricsToOtlp(ld).ResourceMetrics
}

func (mr MetricsRequest) Metrics() pdata.Metrics {
	return ipdata.MetricsFromOtlp(&otlpmetrics.MetricsData{ResourceMetrics: mr.orig.ResourceMetrics})
}

// MetricsClient is the client API for OTLP-GRPC Metrics service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type MetricsClient interface {
	// Export pdata.Metrics to the server.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(ctx context.Context, request MetricsRequest, opts ...grpc.CallOption) (MetricsResponse, error)
}

type metricsClient struct {
	rawClient otlpcollectormetrics.MetricsServiceClient
}

// NewMetricsClient returns a new MetricsClient connected using the given connection.
func NewMetricsClient(cc *grpc.ClientConn) MetricsClient {
	return &metricsClient{rawClient: otlpcollectormetrics.NewMetricsServiceClient(cc)}
}

func (c *metricsClient) Export(ctx context.Context, request MetricsRequest, opts ...grpc.CallOption) (MetricsResponse, error) {
	rsp, err := c.rawClient.Export(ctx, request.orig, opts...)
	return MetricsResponse{orig: rsp}, err
}

// MetricsServer is the server API for OTLP gRPC MetricsService service.
type MetricsServer interface {
	// Export is called every time a new request is received.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(context.Context, MetricsRequest) (MetricsResponse, error)
}

// RegisterMetricsServer registers the MetricsServer to the grpc.Server.
func RegisterMetricsServer(s *grpc.Server, srv MetricsServer) {
	otlpcollectormetrics.RegisterMetricsServiceServer(s, &rawMetricsServer{srv: srv})
}

type rawMetricsServer struct {
	srv MetricsServer
}

func (s rawMetricsServer) Export(ctx context.Context, request *otlpcollectormetrics.ExportMetricsServiceRequest) (*otlpcollectormetrics.ExportMetricsServiceResponse, error) {
	rsp, err := s.srv.Export(ctx, MetricsRequest{orig: request})
	return rsp.orig, err
}

// InstrumentationLibraryMetricsToScope implements the translation of resource metrics data
// following the v0.15.0 upgrade:
//      receivers SHOULD check if instrumentation_library_metrics is set
//      and scope_metrics is not set then the value in instrumentation_library_metrics
//      SHOULD be used instead by converting InstrumentationLibraryMetrics into ScopeMetrics.
//      If scope_metrics is set then instrumentation_library_metrics SHOULD be ignored.
// https://github.com/open-telemetry/opentelemetry-proto/blob/3c2915c01a9fb37abfc0415ec71247c4978386b0/opentelemetry/proto/metrics/v1/metrics.proto#L58
func InstrumentationLibraryMetricsToScope(rms []*otlpmetrics.ResourceMetrics) {
	for _, rm := range rms {
		if len(rm.ScopeMetrics) == 0 {
			for _, ilm := range rm.InstrumentationLibraryMetrics {
				scopeMetrics := otlpmetrics.ScopeMetrics{
					Scope: v1.InstrumentationScope{
						Name:    ilm.InstrumentationLibrary.Name,
						Version: ilm.InstrumentationLibrary.Version,
					},
					Metrics:   ilm.Metrics,
					SchemaUrl: ilm.SchemaUrl,
				}
				rm.ScopeMetrics = append(rm.ScopeMetrics, &scopeMetrics)
			}
		}
		rm.InstrumentationLibraryMetrics = nil
	}
}
