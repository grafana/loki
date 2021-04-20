// Copyright 2016 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

// gRPC Prometheus monitoring interceptors for client-side gRPC.

package interceptors

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

// UnaryClientInterceptor is a gRPC client-side interceptor that provides reporting for Unary RPCs.
func UnaryClientInterceptor(reportable ClientReportable) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		r := newReport(Unary, method)
		reporter, newCtx := reportable.ClientReporter(ctx, req, r.rpcType, r.service, r.method)

		reporter.PostMsgSend(req, nil, time.Since(r.startTime))
		err := invoker(newCtx, method, req, reply, cc, opts...)
		reporter.PostMsgReceive(reply, err, time.Since(r.startTime))

		reporter.PostCall(err, time.Since(r.startTime))
		return err
	}
}

// StreamClientInterceptor is a gRPC client-side interceptor that provides reporting for Stream RPCs.
func StreamClientInterceptor(reportable ClientReportable) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		r := newReport(clientStreamType(desc), method)
		reporter, newCtx := reportable.ClientReporter(ctx, nil, r.rpcType, r.service, r.method)

		clientStream, err := streamer(newCtx, desc, cc, method, opts...)
		if err != nil {
			reporter.PostCall(err, time.Since(r.startTime))
			return nil, err
		}
		return &monitoredClientStream{ClientStream: clientStream, startTime: r.startTime, reporter: reporter}, nil
	}
}

func clientStreamType(desc *grpc.StreamDesc) GRPCType {
	if desc.ClientStreams && !desc.ServerStreams {
		return ClientStream
	} else if !desc.ClientStreams && desc.ServerStreams {
		return ServerStream
	}
	return BidiStream
}

// monitoredClientStream wraps grpc.ClientStream allowing each Sent/Recv of message to report.
type monitoredClientStream struct {
	grpc.ClientStream

	startTime time.Time
	reporter  Reporter
}

func (s *monitoredClientStream) SendMsg(m interface{}) error {
	start := time.Now()
	err := s.ClientStream.SendMsg(m)
	s.reporter.PostMsgSend(m, err, time.Since(start))
	return err
}

func (s *monitoredClientStream) RecvMsg(m interface{}) error {
	start := time.Now()
	err := s.ClientStream.RecvMsg(m)
	s.reporter.PostMsgReceive(m, err, time.Since(start))

	if err == nil {
		return nil
	}

	s.reporter.PostCall(err, time.Since(s.startTime))
	return err
}
