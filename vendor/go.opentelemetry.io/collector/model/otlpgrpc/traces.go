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

	otlpcollectortrace "go.opentelemetry.io/collector/model/internal/data/protogen/collector/trace/v1"
	v1 "go.opentelemetry.io/collector/model/internal/data/protogen/common/v1"
	otlptrace "go.opentelemetry.io/collector/model/internal/data/protogen/trace/v1"
	ipdata "go.opentelemetry.io/collector/model/internal/pdata"
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

// Deprecated: [v0.48.0] use TracesResponse.UnmarshalProto.
func UnmarshalTracesResponse(data []byte) (TracesResponse, error) {
	tr := NewTracesResponse()
	err := tr.UnmarshalProto(data)
	return tr, err
}

// Deprecated: [v0.48.0] use TracesResponse.UnmarshalJSON.
func UnmarshalJSONTracesResponse(data []byte) (TracesResponse, error) {
	tr := NewTracesResponse()
	err := tr.UnmarshalJSON(data)
	return tr, err
}

// Deprecated: [v0.48.0] use MarshalProto.
func (tr TracesResponse) Marshal() ([]byte, error) {
	return tr.MarshalProto()
}

// MarshalProto marshals TracesResponse into proto bytes.
func (tr TracesResponse) MarshalProto() ([]byte, error) {
	return tr.orig.Marshal()
}

// UnmarshalProto unmarshalls TracesResponse from proto bytes.
func (tr TracesResponse) UnmarshalProto(data []byte) error {
	return tr.orig.Unmarshal(data)
}

// MarshalJSON marshals TracesResponse into JSON bytes.
func (tr TracesResponse) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, tr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalJSON unmarshalls TracesResponse from JSON bytes.
func (tr TracesResponse) UnmarshalJSON(data []byte) error {
	return jsonUnmarshaler.Unmarshal(bytes.NewReader(data), tr.orig)
}

// TracesRequest represents the response for gRPC client/server.
type TracesRequest struct {
	orig *otlpcollectortrace.ExportTraceServiceRequest
}

// NewTracesRequest returns an empty TracesRequest.
func NewTracesRequest() TracesRequest {
	return TracesRequest{orig: &otlpcollectortrace.ExportTraceServiceRequest{}}
}

// Deprecated: [v0.48.0] use TracesRequest.UnmarshalProto.
func UnmarshalTracesRequest(data []byte) (TracesRequest, error) {
	tr := NewTracesRequest()
	err := tr.UnmarshalProto(data)
	return tr, err
}

// Deprecated: [v0.48.0] use TracesRequest.UnmarshalJSON.
func UnmarshalJSONTracesRequest(data []byte) (TracesRequest, error) {
	tr := NewTracesRequest()
	err := tr.UnmarshalJSON(data)
	return tr, err
}

// Deprecated: [v0.48.0] use MarshalProto.
func (tr TracesRequest) Marshal() ([]byte, error) {
	return tr.MarshalProto()
}

// MarshalProto marshals TracesRequest into proto bytes.
func (tr TracesRequest) MarshalProto() ([]byte, error) {
	return tr.orig.Marshal()
}

// UnmarshalProto unmarshalls TracesRequest from proto bytes.
func (tr TracesRequest) UnmarshalProto(data []byte) error {
	if err := tr.orig.Unmarshal(data); err != nil {
		return err
	}
	InstrumentationLibrarySpansToScope(tr.orig.ResourceSpans)
	return nil
}

// MarshalJSON marshals TracesRequest into JSON bytes.
func (tr TracesRequest) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, tr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalJSON unmarshalls TracesRequest from JSON bytes.
func (tr TracesRequest) UnmarshalJSON(data []byte) error {
	if err := jsonUnmarshaler.Unmarshal(bytes.NewReader(data), tr.orig); err != nil {
		return err
	}
	InstrumentationLibrarySpansToScope(tr.orig.ResourceSpans)
	return nil
}

func (tr TracesRequest) SetTraces(td pdata.Traces) {
	tr.orig.ResourceSpans = ipdata.TracesToOtlp(td).ResourceSpans
}

func (tr TracesRequest) Traces() pdata.Traces {
	return ipdata.TracesFromOtlp(&otlptrace.TracesData{ResourceSpans: tr.orig.ResourceSpans})
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

// InstrumentationLibraryToScope implements the translation of resource span data
// following the v0.15.0 upgrade:
//      receivers SHOULD check if instrumentation_library_spans is set
//      and scope_spans is not set then the value in instrumentation_library_spans
//      SHOULD be used instead by converting InstrumentationLibrarySpans into ScopeSpans.
//      If scope_spans is set then instrumentation_library_spans SHOULD be ignored.
// https://github.com/open-telemetry/opentelemetry-proto/blob/3c2915c01a9fb37abfc0415ec71247c4978386b0/opentelemetry/proto/trace/v1/trace.proto#L58
func InstrumentationLibrarySpansToScope(rss []*otlptrace.ResourceSpans) {
	for _, rs := range rss {
		if len(rs.ScopeSpans) == 0 {
			for _, ils := range rs.InstrumentationLibrarySpans {
				scopeSpans := otlptrace.ScopeSpans{
					Scope: v1.InstrumentationScope{
						Name:    ils.InstrumentationLibrary.Name,
						Version: ils.InstrumentationLibrary.Version,
					},
					Spans:     ils.Spans,
					SchemaUrl: ils.SchemaUrl,
				}
				rs.ScopeSpans = append(rs.ScopeSpans, &scopeSpans)
			}
		}
		rs.InstrumentationLibrarySpans = nil
	}
}
