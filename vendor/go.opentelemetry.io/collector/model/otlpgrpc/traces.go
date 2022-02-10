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
	otlpcollectortrace "go.opentelemetry.io/collector/model/internal/data/protogen/collector/trace/v1"
	otlptrace "go.opentelemetry.io/collector/model/internal/data/protogen/trace/v1"
	"go.opentelemetry.io/collector/model/pdata"
)

// TracesResponse represents the response for gRPC client/server.
type TracesResponse struct {
	orig *otlpcollectortrace.ExportTraceServiceResponse
}

// NewTracesResponse returns an empty TracesResponse.
func NewTracesResponse() TracesResponse {
	return TracesResponse{orig: &otlpcollectortrace.ExportTraceServiceResponse{}}
}

// UnmarshalTracesResponse unmarshalls TracesResponse from proto bytes.
func UnmarshalTracesResponse(data []byte) (TracesResponse, error) {
	var orig otlpcollectortrace.ExportTraceServiceResponse
	if err := orig.Unmarshal(data); err != nil {
		return TracesResponse{}, err
	}
	return TracesResponse{orig: &orig}, nil
}

// UnmarshalJSONTracesResponse unmarshalls TracesResponse from JSON bytes.
func UnmarshalJSONTracesResponse(data []byte) (TracesResponse, error) {
	var orig otlpcollectortrace.ExportTraceServiceResponse
	if err := jsonUnmarshaler.Unmarshal(bytes.NewReader(data), &orig); err != nil {
		return TracesResponse{}, err
	}
	return TracesResponse{orig: &orig}, nil
}

// Marshal marshals TracesResponse into proto bytes.
func (tr TracesResponse) Marshal() ([]byte, error) {
	return tr.orig.Marshal()
}

// MarshalJSON marshals TracesResponse into JSON bytes.
func (tr TracesResponse) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, tr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TracesRequest represents the response for gRPC client/server.
type TracesRequest struct {
	orig *otlpcollectortrace.ExportTraceServiceRequest
}

// NewTracesRequest returns an empty TracesRequest.
func NewTracesRequest() TracesRequest {
	return TracesRequest{orig: &otlpcollectortrace.ExportTraceServiceRequest{}}
}

// UnmarshalTracesRequest unmarshalls TracesRequest from proto bytes.
func UnmarshalTracesRequest(data []byte) (TracesRequest, error) {
	var orig otlpcollectortrace.ExportTraceServiceRequest
	if err := orig.Unmarshal(data); err != nil {
		return TracesRequest{}, err
	}
	return TracesRequest{orig: &orig}, nil
}

// UnmarshalJSONTracesRequest unmarshalls TracesRequest from JSON bytes.
func UnmarshalJSONTracesRequest(data []byte) (TracesRequest, error) {
	var orig otlpcollectortrace.ExportTraceServiceRequest
	if err := jsonUnmarshaler.Unmarshal(bytes.NewReader(data), &orig); err != nil {
		return TracesRequest{}, err
	}
	return TracesRequest{orig: &orig}, nil
}

// Marshal marshals TracesRequest into proto bytes.
func (tr TracesRequest) Marshal() ([]byte, error) {
	return tr.orig.Marshal()
}

// MarshalJSON marshals TracesRequest into JSON bytes.
func (tr TracesRequest) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, tr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (tr TracesRequest) SetTraces(td pdata.Traces) {
	tr.orig.ResourceSpans = internal.TracesToOtlp(td.InternalRep()).ResourceSpans
}

func (tr TracesRequest) Traces() pdata.Traces {
	return pdata.TracesFromInternalRep(internal.TracesFromOtlp(&otlptrace.TracesData{ResourceSpans: tr.orig.ResourceSpans}))
}

// TracesClient is the client API for OTLP-GRPC Traces service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type TracesClient interface {
	// Export pdata.Traces to the server.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(ctx context.Context, request TracesRequest, opts ...grpc.CallOption) (TracesResponse, error)
}

type tracesClient struct {
	rawClient otlpcollectortrace.TraceServiceClient
}

// NewTracesClient returns a new TracesClient connected using the given connection.
func NewTracesClient(cc *grpc.ClientConn) TracesClient {
	return &tracesClient{rawClient: otlpcollectortrace.NewTraceServiceClient(cc)}
}

// Export implements the TracesClient interface.
func (c *tracesClient) Export(ctx context.Context, request TracesRequest, opts ...grpc.CallOption) (TracesResponse, error) {
	rsp, err := c.rawClient.Export(ctx, request.orig, opts...)
	return TracesResponse{orig: rsp}, err
}

// TracesServer is the server API for OTLP gRPC TracesService service.
type TracesServer interface {
	// Export is called every time a new request is received.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(context.Context, TracesRequest) (TracesResponse, error)
}

// RegisterTracesServer registers the TracesServer to the grpc.Server.
func RegisterTracesServer(s *grpc.Server, srv TracesServer) {
	otlpcollectortrace.RegisterTraceServiceServer(s, &rawTracesServer{srv: srv})
}

type rawTracesServer struct {
	srv TracesServer
}

func (s rawTracesServer) Export(ctx context.Context, request *otlpcollectortrace.ExportTraceServiceRequest) (*otlpcollectortrace.ExportTraceServiceResponse, error) {
	rsp, err := s.srv.Export(ctx, TracesRequest{orig: request})
	return rsp.orig, err
}
