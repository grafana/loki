package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

const LokiReplayGRPCMetadataKey = "loki-replay"

var grpcHeaderPropagationMappings = []struct {
	contextHeader string
	grpcMetadata  string
}{
	{
		contextHeader: httpreq.LokiDisablePipelineWrappersHeader,
		grpcMetadata:  httpreq.LokiDisablePipelineWrappersHeader,
	},
	{
		contextHeader: httpreq.AdaptiveTelemetryReplayHeader,
		grpcMetadata:  LokiReplayGRPCMetadataKey,
	},
}

func injectHTTPHeadersIntoGRPCRequest(ctx context.Context) context.Context {
	var copied bool
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(map[string]string{})
	}

	for _, mapping := range grpcHeaderPropagationMappings {
		header := httpreq.ExtractHeader(ctx, mapping.contextHeader)
		if header == "" {
			continue
		}
		if !copied {
			md = md.Copy()
			copied = true
		}
		md.Set(mapping.grpcMetadata, header)
	}

	if !copied {
		return ctx
	}

	return metadata.NewOutgoingContext(ctx, md)
}

func extractHTTPHeadersFromGRPCRequest(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// No metadata, just return as is
		return ctx
	}

	for _, mapping := range grpcHeaderPropagationMappings {
		headerValues := md.Get(mapping.grpcMetadata)
		if len(headerValues) == 0 {
			continue
		}
		ctx = httpreq.InjectHeader(ctx, mapping.contextHeader, headerValues[0])
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
