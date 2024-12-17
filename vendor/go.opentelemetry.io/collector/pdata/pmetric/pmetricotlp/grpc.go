// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pmetricotlp // import "go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/collector/pdata/internal"
	otlpcollectormetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/collector/metrics/v1"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

// GRPCClient is the client API for OTLP-GRPC Metrics service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type GRPCClient interface {
	// Export pmetric.Metrics to the server.
	//
	// For performance reasons, it is recommended to keep this RPC
	// alive for the entire life of the application.
	Export(ctx context.Context, request ExportRequest, opts ...grpc.CallOption) (ExportResponse, error)

	// unexported disallow implementation of the GRPCClient.
	unexported()
}

// NewGRPCClient returns a new GRPCClient connected using the given connection.
func NewGRPCClient(cc *grpc.ClientConn) GRPCClient {
	return &grpcClient{rawClient: otlpcollectormetrics.NewMetricsServiceClient(cc)}
}

type grpcClient struct {
	rawClient otlpcollectormetrics.MetricsServiceClient
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

// GRPCServer is the server API for OTLP gRPC MetricsService service.
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

// RegisterGRPCServer registers the GRPCServer to the grpc.Server.
func RegisterGRPCServer(s *grpc.Server, srv GRPCServer) {
	otlpcollectormetrics.RegisterMetricsServiceServer(s, &rawMetricsServer{srv: srv})
}

type rawMetricsServer struct {
	srv GRPCServer
}

func (s rawMetricsServer) Export(ctx context.Context, request *otlpcollectormetrics.ExportMetricsServiceRequest) (*otlpcollectormetrics.ExportMetricsServiceResponse, error) {
	otlp.MigrateMetrics(request.ResourceMetrics)
	state := internal.StateMutable
	rsp, err := s.srv.Export(ctx, ExportRequest{orig: request, state: &state})
	return rsp.orig, err
}
