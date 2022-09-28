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

package plogotlp // import "go.opentelemetry.io/collector/pdata/plog/plogotlp"

import (
	"bytes"
	"context"

	"github.com/gogo/protobuf/jsonpb"
	"google.golang.org/grpc"

	"go.opentelemetry.io/collector/pdata/internal"
	otlpcollectorlog "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/logs/v1"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/internal/plogjson"
)

var jsonMarshaler = &jsonpb.Marshaler{}
var jsonUnmarshaler = &jsonpb.Unmarshaler{}

// Response represents the response for gRPC/HTTP client/server.
type Response struct {
	orig *otlpcollectorlog.ExportLogsServiceResponse
}

// NewResponse returns an empty Response.
func NewResponse() Response {
	return Response{orig: &otlpcollectorlog.ExportLogsServiceResponse{}}
}

// MarshalProto marshals Response into proto bytes.
func (lr Response) MarshalProto() ([]byte, error) {
	return lr.orig.Marshal()
}

// UnmarshalProto unmarshalls Response from proto bytes.
func (lr Response) UnmarshalProto(data []byte) error {
	return lr.orig.Unmarshal(data)
}

// MarshalJSON marshals Response into JSON bytes.
func (lr Response) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, lr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalJSON unmarshalls Response from JSON bytes.
func (lr Response) UnmarshalJSON(data []byte) error {
	return jsonUnmarshaler.Unmarshal(bytes.NewReader(data), lr.orig)
}

// Request represents the request for gRPC/HTTP client/server.
// It's a wrapper for plog.Logs data.
type Request struct {
	orig *otlpcollectorlog.ExportLogsServiceRequest
}

// NewRequest returns an empty Request.
func NewRequest() Request {
	return Request{orig: &otlpcollectorlog.ExportLogsServiceRequest{}}
}

// NewRequestFromLogs returns a Request from plog.Logs.
// Because Request is a wrapper for plog.Logs,
// any changes to the provided Logs struct will be reflected in the Request and vice versa.
func NewRequestFromLogs(ld plog.Logs) Request {
	return Request{orig: internal.GetOrigLogs(internal.Logs(ld))}
}

// MarshalProto marshals Request into proto bytes.
func (lr Request) MarshalProto() ([]byte, error) {
	return lr.orig.Marshal()
}

// UnmarshalProto unmarshalls Request from proto bytes.
func (lr Request) UnmarshalProto(data []byte) error {
	if err := lr.orig.Unmarshal(data); err != nil {
		return err
	}
	otlp.MigrateLogs(lr.orig.ResourceLogs)
	return nil
}

// MarshalJSON marshals Request into JSON bytes.
func (lr Request) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := jsonMarshaler.Marshal(&buf, lr.orig); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalJSON unmarshalls Request from JSON bytes.
func (lr Request) UnmarshalJSON(data []byte) error {
	return plogjson.UnmarshalExportLogsServiceRequest(data, lr.orig)
}

func (lr Request) Logs() plog.Logs {
	return plog.Logs(internal.NewLogs(lr.orig))
}

// Client is the client API for OTLP-GRPC Logs service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type Client interface {
	// Export plog.Logs to the server.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(ctx context.Context, request Request, opts ...grpc.CallOption) (Response, error)
}

type logsClient struct {
	rawClient otlpcollectorlog.LogsServiceClient
}

// NewClient returns a new Client connected using the given connection.
func NewClient(cc *grpc.ClientConn) Client {
	return &logsClient{rawClient: otlpcollectorlog.NewLogsServiceClient(cc)}
}

func (c *logsClient) Export(ctx context.Context, request Request, opts ...grpc.CallOption) (Response, error) {
	rsp, err := c.rawClient.Export(ctx, request.orig, opts...)
	return Response{orig: rsp}, err
}

// Server is the server API for OTLP gRPC LogsService service.
type Server interface {
	// Export is called every time a new request is received.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(context.Context, Request) (Response, error)
}

// RegisterServer registers the Server to the grpc.Server.
func RegisterServer(s *grpc.Server, srv Server) {
	otlpcollectorlog.RegisterLogsServiceServer(s, &rawLogsServer{srv: srv})
}

type rawLogsServer struct {
	srv Server
}

func (s rawLogsServer) Export(ctx context.Context, request *otlpcollectorlog.ExportLogsServiceRequest) (*otlpcollectorlog.ExportLogsServiceResponse, error) {
	otlp.MigrateLogs(request.ResourceLogs)
	rsp, err := s.srv.Export(ctx, Request{orig: request})
	return rsp.orig, err
}
