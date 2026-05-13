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

// TraceServiceClient is the client API for TraceService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type TraceServiceClient interface {
	Export(context.Context, *internal.ExportTraceServiceRequest, ...grpc.CallOption) (*internal.ExportTraceServiceResponse, error)
}

type traceServiceClient struct {
	cc *grpc.ClientConn
}

func NewTraceServiceClient(cc *grpc.ClientConn) TraceServiceClient {
	return &traceServiceClient{cc}
}

func (c *traceServiceClient) Export(ctx context.Context, in *internal.ExportTraceServiceRequest, opts ...grpc.CallOption) (*internal.ExportTraceServiceResponse, error) {
	out := new(internal.ExportTraceServiceResponse)
	err := c.cc.Invoke(ctx, "/opentelemetry.proto.collector.trace.v1.TraceService/Export", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TraceServiceServer is the server API for TraceService service.
type TraceServiceServer interface {
	Export(context.Context, *internal.ExportTraceServiceRequest) (*internal.ExportTraceServiceResponse, error)
}

// UnimplementedTraceServiceServer can be embedded to have forward compatible implementations.
type UnimplementedTraceServiceServer struct{}

func (*UnimplementedTraceServiceServer) Export(context.Context, *internal.ExportTraceServiceRequest) (*internal.ExportTraceServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Export not implemented")
}

func RegisterTraceServiceServer(s *grpc.Server, srv TraceServiceServer) {
	s.RegisterService(&traceServiceServiceDesc, srv)
}

// Context cannot be the first parameter of the function because gRPC definition.
//
//nolint:revive
func traceServiceExportHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(internal.ExportTraceServiceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TraceServiceServer).Export(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opentelemetry.proto.collector.trace.v1.TraceService/Export",
	}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(TraceServiceServer).Export(ctx, req.(*internal.ExportTraceServiceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var traceServiceServiceDesc = grpc.ServiceDesc{
	ServiceName: "opentelemetry.proto.collector.trace.v1.TraceService",
	HandlerType: (*TraceServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Export",
			Handler:    traceServiceExportHandler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "opentelemetry/proto/collector/trace/v1/trace_service.proto",
}
