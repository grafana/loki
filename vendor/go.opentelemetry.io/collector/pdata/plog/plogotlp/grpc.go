// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plogotlp // import "go.opentelemetry.io/collector/pdata/plog/plogotlp"

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/collector/pdata/internal"
	otlpcollectorlog "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/logs/v1"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

// GRPCClient is the client API for OTLP-GRPC Logs service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type GRPCClient interface {
	// Export plog.Logs to the server.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(ctx context.Context, request ExportRequest, opts ...grpc.CallOption) (ExportResponse, error)

	// unexported disallow implementation of the GRPCClient.
	unexported()
}

// NewGRPCClient returns a new GRPCClient connected using the given connection.
func NewGRPCClient(cc *grpc.ClientConn) GRPCClient {
	return &grpcClient{rawClient: otlpcollectorlog.NewLogsServiceClient(cc)}
}

type grpcClient struct {
	rawClient otlpcollectorlog.LogsServiceClient
}

func (c *grpcClient) Export(ctx context.Context, request ExportRequest, opts ...grpc.CallOption) (ExportResponse, error) {
	rsp, err := c.rawClient.Export(ctx, request.orig, opts...)
	if err != nil {
		return ExportResponse{}, err
	}
	state := internal.StateMutable
	return ExportResponse{orig: rsp, state: &state}, err
}

func (c *grpcClient) unexported() {}

// GRPCServer is the server API for OTLP gRPC LogsService service.
// Implementations MUST embed UnimplementedGRPCServer.
type GRPCServer interface {
	// Export is called every time a new request is received.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(context.Context, ExportRequest) (ExportResponse, error)

	// unexported disallow implementation of the GRPCServer.
	unexported()
}

var _ GRPCServer = (*UnimplementedGRPCServer)(nil)

// UnimplementedGRPCServer MUST be embedded to have forward compatible implementations.
type UnimplementedGRPCServer struct{}

func (*UnimplementedGRPCServer) Export(context.Context, ExportRequest) (ExportResponse, error) {
	return ExportResponse{}, status.Errorf(codes.Unimplemented, "method Export not implemented")
}

func (*UnimplementedGRPCServer) unexported() {}

// RegisterGRPCServer registers the Server to the grpc.Server.
func RegisterGRPCServer(s *grpc.Server, srv GRPCServer) {
	otlpcollectorlog.RegisterLogsServiceServer(s, &rawLogsServer{srv: srv})
}

type rawLogsServer struct {
	srv GRPCServer
}

func (s rawLogsServer) Export(ctx context.Context, request *otlpcollectorlog.ExportLogsServiceRequest) (*otlpcollectorlog.ExportLogsServiceResponse, error) {
	otlp.MigrateLogs(request.ResourceLogs)
	state := internal.StateMutable
	rsp, err := s.srv.Export(ctx, ExportRequest{orig: request, state: &state})
	return rsp.orig, err
}
