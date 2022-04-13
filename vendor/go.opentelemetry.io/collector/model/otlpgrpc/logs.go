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

	"github.com/gogo/protobuf/jsonpb"
	"google.golang.org/grpc"

	otlpcollectorlog "go.opentelemetry.io/collector/model/internal/data/protogen/collector/logs/v1"
	v1 "go.opentelemetry.io/collector/model/internal/data/protogen/common/v1"
	otlplogs "go.opentelemetry.io/collector/model/internal/data/protogen/logs/v1"
	ipdata "go.opentelemetry.io/collector/model/internal/pdata"
	"go.opentelemetry.io/collector/model/pdata"
)

var jsonMarshaler = &jsonpb.Marshaler{}
var jsonUnmarshaler = &jsonpb.Unmarshaler{}

// LogsResponse represents the response for gRPC client/server.
type LogsResponse struct {
	orig *otlpcollectorlog.ExportLogsServiceResponse
}

// NewLogsResponse returns an empty LogsResponse.
func NewLogsResponse() LogsResponse {
	return LogsResponse{orig: &otlpcollectorlog.ExportLogsServiceResponse{}}
}

// Deprecated: [v0.48.0] use LogsResponse.UnmarshalProto.
func UnmarshalLogsResponse(data []byte) (LogsResponse, error) {
	lr := NewLogsResponse()
	err := lr.UnmarshalProto(data)
	return lr, err
}

// Deprecated: [v0.48.0] use LogsResponse.UnmarshalJSON.
func UnmarshalJSONLogsResponse(data []byte) (LogsResponse, error) {
	lr := NewLogsResponse()
	err := lr.UnmarshalJSON(data)
	return lr, err
}

// Deprecated: [v0.48.0] use MarshalProto.
func (lr LogsResponse) Marshal() ([]byte, error) {
	return lr.MarshalProto()
}

// MarshalProto marshals LogsResponse into proto bytes.
func (lr LogsResponse) MarshalProto() ([]byte, error) {
	return lr.orig.Marshal()
}

// UnmarshalProto unmarshalls LogsResponse from proto bytes.
func (lr LogsResponse) UnmarshalProto(data []byte) error {
	return lr.orig.Unmarshal(data)
}

// MarshalJSON marshals LogsResponse into JSON bytes.
func (lr LogsResponse) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, lr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalJSON unmarshalls LogsResponse from JSON bytes.
func (lr LogsResponse) UnmarshalJSON(data []byte) error {
	return jsonUnmarshaler.Unmarshal(bytes.NewReader(data), lr.orig)
}

// LogsRequest represents the response for gRPC client/server.
type LogsRequest struct {
	orig *otlpcollectorlog.ExportLogsServiceRequest
}

// NewLogsRequest returns an empty LogsRequest.
func NewLogsRequest() LogsRequest {
	return LogsRequest{orig: &otlpcollectorlog.ExportLogsServiceRequest{}}
}

// Deprecated: [v0.48.0] use LogsRequest.UnmarshalProto.
func UnmarshalLogsRequest(data []byte) (LogsRequest, error) {
	lr := NewLogsRequest()
	err := lr.UnmarshalProto(data)
	return lr, err
}

// Deprecated: [v0.48.0] use LogsRequest.UnmarshalJSON.
func UnmarshalJSONLogsRequest(data []byte) (LogsRequest, error) {
	lr := NewLogsRequest()
	err := lr.UnmarshalJSON(data)
	return lr, err
}

// Deprecated: [v0.48.0] use MarshalProto.
func (lr LogsRequest) Marshal() ([]byte, error) {
	return lr.MarshalProto()
}

// MarshalProto marshals LogsRequest into proto bytes.
func (lr LogsRequest) MarshalProto() ([]byte, error) {
	return lr.orig.Marshal()
}

// UnmarshalProto unmarshalls LogsRequest from proto bytes.
func (lr LogsRequest) UnmarshalProto(data []byte) error {
	if err := lr.orig.Unmarshal(data); err != nil {
		return err
	}
	InstrumentationLibraryLogsToScope(lr.orig.ResourceLogs)
	return nil
}

// MarshalJSON marshals LogsRequest into JSON bytes.
func (lr LogsRequest) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, lr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalJSON unmarshalls LogsRequest from JSON bytes.
func (lr LogsRequest) UnmarshalJSON(data []byte) error {
	if err := jsonUnmarshaler.Unmarshal(bytes.NewReader(data), lr.orig); err != nil {
		return err
	}
	InstrumentationLibraryLogsToScope(lr.orig.ResourceLogs)
	return nil
}

func (lr LogsRequest) SetLogs(ld pdata.Logs) {
	lr.orig.ResourceLogs = ipdata.LogsToOtlp(ld).ResourceLogs
}

func (lr LogsRequest) Logs() pdata.Logs {
	return ipdata.LogsFromOtlp(&otlplogs.LogsData{ResourceLogs: lr.orig.ResourceLogs})
}

// LogsClient is the client API for OTLP-GRPC Logs service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type LogsClient interface {
	// Export pdata.Logs to the server.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(ctx context.Context, request LogsRequest, opts ...grpc.CallOption) (LogsResponse, error)
}

type logsClient struct {
	rawClient otlpcollectorlog.LogsServiceClient
}

// NewLogsClient returns a new LogsClient connected using the given connection.
func NewLogsClient(cc *grpc.ClientConn) LogsClient {
	return &logsClient{rawClient: otlpcollectorlog.NewLogsServiceClient(cc)}
}

func (c *logsClient) Export(ctx context.Context, request LogsRequest, opts ...grpc.CallOption) (LogsResponse, error) {
	rsp, err := c.rawClient.Export(ctx, request.orig, opts...)
	return LogsResponse{orig: rsp}, err
}

// LogsServer is the server API for OTLP gRPC LogsService service.
type LogsServer interface {
	// Export is called every time a new request is received.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(context.Context, LogsRequest) (LogsResponse, error)
}

// RegisterLogsServer registers the LogsServer to the grpc.Server.
func RegisterLogsServer(s *grpc.Server, srv LogsServer) {
	otlpcollectorlog.RegisterLogsServiceServer(s, &rawLogsServer{srv: srv})
}

type rawLogsServer struct {
	srv LogsServer
}

func (s rawLogsServer) Export(ctx context.Context, request *otlpcollectorlog.ExportLogsServiceRequest) (*otlpcollectorlog.ExportLogsServiceResponse, error) {
	rsp, err := s.srv.Export(ctx, LogsRequest{orig: request})
	return rsp.orig, err
}

// InstrumentationLibraryLogsToScope implements the translation of resource logs data
// following the v0.15.0 upgrade:
//      receivers SHOULD check if instrumentation_library_logs is set
//      and scope_logs is not set then the value in instrumentation_library_logs
//      SHOULD be used instead by converting InstrumentationLibraryLogs into ScopeLogs.
//      If scope_logs is set then instrumentation_library_logs SHOULD be ignored.
// https://github.com/open-telemetry/opentelemetry-proto/blob/3c2915c01a9fb37abfc0415ec71247c4978386b0/opentelemetry/proto/logs/v1/logs.proto#L58
func InstrumentationLibraryLogsToScope(rls []*otlplogs.ResourceLogs) {
	for _, rl := range rls {
		if len(rl.ScopeLogs) == 0 {
			for _, ill := range rl.InstrumentationLibraryLogs {
				scopeLogs := otlplogs.ScopeLogs{
					Scope: v1.InstrumentationScope{
						Name:    ill.InstrumentationLibrary.Name,
						Version: ill.InstrumentationLibrary.Version,
					},
					LogRecords: ill.LogRecords,
					SchemaUrl:  ill.SchemaUrl,
				}
				rl.ScopeLogs = append(rl.ScopeLogs, &scopeLogs)
			}
		}
		rl.InstrumentationLibraryLogs = nil
	}
}
