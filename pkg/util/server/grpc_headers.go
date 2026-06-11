package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

func injectHTTPHeadersIntoGRPCRequest(ctx context.Context) context.Context {
	disablePipelineWrappersHeader := httpreq.ExtractHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader)
	backfillHeader := httpreq.ExtractHeader(ctx, httpreq.LokiBackfillHeader)
	if disablePipelineWrappersHeader == "" && backfillHeader == "" {
		return ctx
	}

	// inject into GRPC metadata
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(map[string]string{})
	}
	md = md.Copy()
	if disablePipelineWrappersHeader != "" {
		md.Set(httpreq.LokiDisablePipelineWrappersHeader, disablePipelineWrappersHeader)
	}
	if backfillHeader != "" {
		md.Set(httpreq.LokiBackfillHeader, backfillHeader)
	}

	return metadata.NewOutgoingContext(ctx, md)
}

func extractHTTPHeadersFromGRPCRequest(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// No metadata, just return as is
		return ctx
	}

	disablePipelineWrappersHeaderValues := md.Get(httpreq.LokiDisablePipelineWrappersHeader)
	if len(disablePipelineWrappersHeaderValues) > 0 {
		ctx = httpreq.InjectHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader, disablePipelineWrappersHeaderValues[0])
	}

	backfillHeaderValues := md.Get(httpreq.LokiBackfillHeader)
	if len(backfillHeaderValues) > 0 {
		ctx = httpreq.InjectHeader(ctx, httpreq.LokiBackfillHeader, backfillHeaderValues[0])
	}

	return ctx
}

func UnaryClientHTTPHeadersInterceptor(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	return invoker(injectHTTPHeadersIntoGRPCRequest(ctx), method, req, reply, cc, opts...)
}

func StreamClientHTTPHeadersInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return streamer(injectHTTPHeadersIntoGRPCRequest(ctx), desc, cc, method, opts...)
}

func UnaryServerHTTPHeadersnIterceptor(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return handler(extractHTTPHeadersFromGRPCRequest(ctx), req)
}

func StreamServerHTTPHeadersInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	return handler(srv, serverStream{
		ctx:          extractHTTPHeadersFromGRPCRequest(ss.Context()),
		ServerStream: ss,
	})
}
