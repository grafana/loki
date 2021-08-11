// Copyright 2016 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

// gRPC Prometheus monitoring interceptors for server-side gRPC.

package interceptors

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

// UnaryServerInterceptor is a gRPC server-side interceptor that provides reporting for Unary RPCs.
func UnaryServerInterceptor(reportable ServerReportable) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		r := newReport(Unary, info.FullMethod)
		reporter, newCtx := reportable.ServerReporter(ctx, req, r.rpcType, r.service, r.method)

		reporter.PostMsgReceive(req, nil, time.Since(r.startTime))
		resp, err := handler(newCtx, req)
		reporter.PostMsgSend(resp, err, time.Since(r.startTime))

		reporter.PostCall(err, time.Since(r.startTime))
		return resp, err
	}
}

// StreamServerInterceptor is a gRPC server-side interceptor that provides reporting for Streaming RPCs.
func StreamServerInterceptor(reportable ServerReportable) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		r := newReport(ServerStream, info.FullMethod)
		reporter, newCtx := reportable.ServerReporter(ss.Context(), nil, streamRPCType(info), r.service, r.method)
		err := handler(srv, &monitoredServerStream{ServerStream: ss, newCtx: newCtx, reporter: reporter})
		reporter.PostCall(err, time.Since(r.startTime))
		return err
	}
}

func streamRPCType(info *grpc.StreamServerInfo) GRPCType {
	if info.IsClientStream && !info.IsServerStream {
		return ClientStream
	} else if !info.IsClientStream && info.IsServerStream {
		return ServerStream
	}
	return BidiStream
}

// monitoredStream wraps grpc.ServerStream allowing each Sent/Recv of message to report.
type monitoredServerStream struct {
	grpc.ServerStream

	newCtx   context.Context
	reporter Reporter
}

func (s *monitoredServerStream) Context() context.Context {
	return s.newCtx
}

func (s *monitoredServerStream) SendMsg(m interface{}) error {
	start := time.Now()
	err := s.ServerStream.SendMsg(m)
	s.reporter.PostMsgSend(m, err, time.Since(start))
	return err
}

func (s *monitoredServerStream) RecvMsg(m interface{}) error {
	start := time.Now()
	err := s.ServerStream.RecvMsg(m)
	s.reporter.PostMsgReceive(m, err, time.Since(start))
	return err
}
