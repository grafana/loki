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

// ProfilesServiceClient is the client API for ProfilesService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type ProfilesServiceClient interface {
	Export(context.Context, *internal.ExportProfilesServiceRequest, ...grpc.CallOption) (*internal.ExportProfilesServiceResponse, error)
}

type profilesServiceClient struct {
	cc *grpc.ClientConn
}

func NewProfilesServiceClient(cc *grpc.ClientConn) ProfilesServiceClient {
	return &profilesServiceClient{cc}
}

func (c *profilesServiceClient) Export(ctx context.Context, in *internal.ExportProfilesServiceRequest, opts ...grpc.CallOption) (*internal.ExportProfilesServiceResponse, error) {
	out := new(internal.ExportProfilesServiceResponse)
	err := c.cc.Invoke(ctx, "/opentelemetry.proto.collector.profiles.v1development.ProfilesService/Export", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ProfilesServiceServer is the server API for ProfilesService service.
type ProfilesServiceServer interface {
	Export(context.Context, *internal.ExportProfilesServiceRequest) (*internal.ExportProfilesServiceResponse, error)
}

// UnimplementedProfilesServiceServer can be embedded to have forward compatible implementations.
type UnimplementedProfilesServiceServer struct{}

func (*UnimplementedProfilesServiceServer) Export(context.Context, *internal.ExportProfilesServiceRequest) (*internal.ExportProfilesServiceResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Export not implemented")
}

func RegisterProfilesServiceServer(s *grpc.Server, srv ProfilesServiceServer) {
	s.RegisterService(&profilesServiceServiceDesc, srv)
}

// Context cannot be the first parameter of the function because gRPC definition.
//
//nolint:revive
func profilesServiceExportHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(internal.ExportProfilesServiceRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ProfilesServiceServer).Export(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/opentelemetry.proto.collector.profiles.v1development.ProfilesService/Export",
	}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(ProfilesServiceServer).Export(ctx, req.(*internal.ExportProfilesServiceRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var profilesServiceServiceDesc = grpc.ServiceDesc{
	ServiceName: "opentelemetry.proto.collector.profiles.v1development.ProfilesService",
	HandlerType: (*ProfilesServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Export",
			Handler:    profilesServiceExportHandler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "opentelemetry/proto/collector/profiles/v1development/profiles_service.proto",
}
