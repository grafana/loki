package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

// propagatedHTTPHeaders is allow-listed to avoid leaking sensitive headers into gRPC metadata.
var propagatedHTTPHeaders = []string{
	httpreq.LokiDisablePipelineWrappersHeader,
	httpreq.LokiBackfillHeader,
}

func injectHTTPHeadersIntoGRPCRequest(ctx context.Context) context.Context {
	var md metadata.MD
	var copied bool
	for _, header := range propagatedHTTPHeaders {
		value := httpreq.ExtractHeader(ctx, header)
		if value == "" {
			continue
		}
		if !copied {
			if existing, ok := metadata.FromOutgoingContext(ctx); ok {
				md = existing.Copy()
			} else {
				md = metadata.New(map[string]string{})
			}
			copied = true
		}
		md.Set(header, value)
	}

	if !copied {
		return ctx
	}

	return metadata.NewOutgoingContext(ctx, md)
}

func extractHTTPHeadersFromGRPCRequest(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}

	for _, header := range propagatedHTTPHeaders {
		headerValues := md.Get(header)
		if len(headerValues) == 0 {
			continue
		}
		ctx = httpreq.InjectHeader(ctx, header, headerValues[0])
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
