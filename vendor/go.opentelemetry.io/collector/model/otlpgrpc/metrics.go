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

	"go.opentelemetry.io/collector/model/internal"
	otlpcollectormetrics "go.opentelemetry.io/collector/model/internal/data/protogen/collector/metrics/v1"
	otlpmetrics "go.opentelemetry.io/collector/model/internal/data/protogen/metrics/v1"
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

// UnmarshalMetricsResponse unmarshalls MetricsResponse from proto bytes.
func UnmarshalMetricsResponse(data []byte) (MetricsResponse, error) {
	var orig otlpcollectormetrics.ExportMetricsServiceResponse
	if err := orig.Unmarshal(data); err != nil {
		return MetricsResponse{}, err
	}
	return MetricsResponse{orig: &orig}, nil
}

// UnmarshalJSONMetricsResponse unmarshalls MetricsResponse from JSON bytes.
func UnmarshalJSONMetricsResponse(data []byte) (MetricsResponse, error) {
	var orig otlpcollectormetrics.ExportMetricsServiceResponse
	if err := jsonUnmarshaler.Unmarshal(bytes.NewReader(data), &orig); err != nil {
		return MetricsResponse{}, err
	}
	return MetricsResponse{orig: &orig}, nil
}

// Marshal marshals MetricsResponse into proto bytes.
func (mr MetricsResponse) Marshal() ([]byte, error) {
	return mr.orig.Marshal()
}

// MarshalJSON marshals MetricsResponse into JSON bytes.
func (mr MetricsResponse) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, mr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MetricsRequest represents the response for gRPC client/server.
type MetricsRequest struct {
	orig *otlpcollectormetrics.ExportMetricsServiceRequest
}

// NewMetricsRequest returns an empty MetricsRequest.
func NewMetricsRequest() MetricsRequest {
	return MetricsRequest{orig: &otlpcollectormetrics.ExportMetricsServiceRequest{}}
}

// UnmarshalMetricsRequest unmarshalls MetricsRequest from proto bytes.
func UnmarshalMetricsRequest(data []byte) (MetricsRequest, error) {
	var orig otlpcollectormetrics.ExportMetricsServiceRequest
	if err := orig.Unmarshal(data); err != nil {
		return MetricsRequest{}, err
	}
	return MetricsRequest{orig: &orig}, nil
}

// UnmarshalJSONMetricsRequest unmarshalls MetricsRequest from JSON bytes.
func UnmarshalJSONMetricsRequest(data []byte) (MetricsRequest, error) {
	var orig otlpcollectormetrics.ExportMetricsServiceRequest
	if err := jsonUnmarshaler.Unmarshal(bytes.NewReader(data), &orig); err != nil {
		return MetricsRequest{}, err
	}
	return MetricsRequest{orig: &orig}, nil
}

// Marshal marshals MetricsRequest into proto bytes.
func (mr MetricsRequest) Marshal() ([]byte, error) {
	return mr.orig.Marshal()
}

// MarshalJSON marshals LogsRequest into JSON bytes.
func (mr MetricsRequest) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, mr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (mr MetricsRequest) SetMetrics(ld pdata.Metrics) {
	mr.orig.ResourceMetrics = internal.MetricsToOtlp(ld.InternalRep()).ResourceMetrics
}

func (mr MetricsRequest) Metrics() pdata.Metrics {
	return pdata.MetricsFromInternalRep(internal.MetricsFromOtlp(&otlpmetrics.MetricsData{ResourceMetrics: mr.orig.ResourceMetrics}))
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
