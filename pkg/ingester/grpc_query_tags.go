package ingester

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/pkg/util/httpreq"
)

const (
	lowerQueryTagsHeaderName = "x-query-tags"
)

func getQueryTags(ctx context.Context) string {
	v, _ := ctx.Value(httpreq.QueryTagsHTTPHeader).(string)
	return v
}

func injectIntoGRPCRequest(ctx context.Context) context.Context {
	queryTags := getQueryTags(ctx)
	if queryTags == "" {
		return ctx
	}

	// inject into GRPC metadata
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(map[string]string{})
	}
	md = md.Copy()
	md.Set(lowerQueryTagsHeaderName, queryTags)

	return metadata.NewOutgoingContext(ctx, md)
}

func extractFromGRPCRequest(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// No metadata, just return as is
		return ctx
	}

	headerValues, ok := md[lowerQueryTagsHeaderName]
	if !ok || len(headerValues) == 0 {
		return ctx
	}

	return context.WithValue(ctx, httpreq.QueryTagsHTTPHeader, headerValues[0])
}

// StreamClientQueryTagsInterceptor propagates the query tags from the context to gRPC metadata, which eventually ends up as a HTTP2 header.
// For streaming gRPC requests.
func StreamClientQueryTagsInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx = injectIntoGRPCRequest(ctx)
	return streamer(ctx, desc, cc, method, opts...)
}

// StreamServerUserHeaderInterceptor propagates the query tags from the gRPC metadata back to our context.
func StreamServerQueryTagsInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx := extractFromGRPCRequest(ss.Context())
	return handler(srv, serverStream{
		ctx:          ctx,
		ServerStream: ss,
	})
}

type serverStream struct {
	ctx context.Context
	grpc.ServerStream
}

func (ss serverStream) Context() context.Context {
	return ss.ctx
}
