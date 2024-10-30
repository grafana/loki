package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

func injectHTTPHeadersIntoGRPCRequest(ctx context.Context) context.Context {
	header := httpreq.ExtractHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader)
	if header == "" {
		return ctx
	}

	// inject into GRPC metadata
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(map[string]string{})
	}
	md = md.Copy()
	md.Set(httpreq.LokiDisablePipelineWrappersHeader, header)

	return metadata.NewOutgoingContext(ctx, md)
}

func extractHTTPHeadersFromGRPCRequest(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// No metadata, just return as is
		return ctx
	}

	headerValues := md.Get(httpreq.LokiDisablePipelineWrappersHeader)
	if len(headerValues) == 0 {
		return ctx
	}

	return httpreq.InjectHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader, headerValues[0])
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
