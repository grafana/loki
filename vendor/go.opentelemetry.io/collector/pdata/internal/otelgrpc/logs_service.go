// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelgrpc // import "go.opentelemetry.io/collector/pdata/internal/otelgrpc"

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/collector/pdata/internal"
)

// LogsServiceClient is the client API for LogsService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type LogsServiceClient interface {
	Export(context.Context, *internal.ExportLogsServiceRequest, ...grpc.CallOption) (*internal.ExportLogsServiceResponse, error)
}

type logsServiceClient struct {
	cc *grpc.ClientConn
}

func NewLogsServiceClient(cc *grpc.ClientConn) LogsServiceClient {
	return &logsServiceClient{cc}
}

func (c *logsServiceClient) Export(ctx context.Context, in *internal.ExportLogsServiceRequest, opts ...grpc.CallOption) (*internal.ExportLogsServiceResponse, error) {
	out := new(internal.ExportLogsServiceResponse)
	err := c.cc.Invoke(ctx, "/opentelemetry.proto.collector.logs.v1.LogsService/Export", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// LogsServiceServer is the server API for LogsService service.
type LogsServiceServer interface {
	Export(context.Context, *internal.ExportLogsServiceRequest) (*internal.ExportLogsServiceResponse, error)
}

// UnimplementedLogsServiceServer can be embedded to have forward compatible implementations.
type UnimplementedLogsServiceServer struct{}

func (*UnimplementedLogsServiceServer) Export(context.Context, *internal.ExportLogsServiceRequest) (*internal.ExportLogsServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Export not implemented")
}

func RegisterLogsServiceServer(s *grpc.Server, srv LogsServiceServer) {
	s.RegisterService(&logsServiceServiceDesc, srv)
}

// Context cannot be the first parameter of the function because gRPC definition.
//
//nolint:revive
func logsServiceExportHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(internal.ExportLogsServiceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(LogsServiceServer).Export(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opentelemetry.proto.collector.logs.v1.LogsService/Export",
	}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(LogsServiceServer).Export(ctx, req.(*internal.ExportLogsServiceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var logsServiceServiceDesc = grpc.ServiceDesc{
	ServiceName: "opentelemetry.proto.collector.logs.v1.LogsService",
	HandlerType: (*LogsServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Export",
			Handler:    logsServiceExportHandler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "opentelemetry/proto/collector/logs/v1/logs_service.proto",
}
