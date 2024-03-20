package ingester

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/util/httpreq"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	lowerQueryTagsHeaderName = "x-query-tags"
)

func getQueryTags(ctx context.Context) string {
	v, _ := ctx.Value(httpreq.QueryTagsHTTPHeader).(string) // it's ok to be empty
	return v
}

func injectIntoGRPCRequest(ctx context.Context) (context.Context, error) {
	queryTags := getQueryTags(ctx)
	if queryTags == "" {
		return ctx, nil
	}

	// inject into GRPC metadata
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(map[string]string{})
	}
	md = md.Copy()
	md.Set("x-query-tags", queryTags)

	level.Info(util_log.Logger).Log("msg", "inject query tag", "value", queryTags)

	newCtx := metadata.NewOutgoingContext(ctx, md)

	return newCtx, nil
}

func extractFromGRPCRequest(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// No metadata, just return as is
		return ctx, nil
	}

	headerValues, ok := md["x-query-tags"]
	if !ok || len(headerValues) == 0 {
		return ctx, nil
	}

	level.Info(util_log.Logger).Log("msg", "extract query tag", "value", headerValues[0])

	return context.WithValue(ctx, httpreq.QueryTagsHTTPHeader, headerValues[0]), nil
}

// StreamClientQueryTagsInterceptor propagates the query tags from the context to gRPC metadata, which eventually ends up as a HTTP2 header.
// For streaming gRPC requests.
func StreamClientQueryTagsInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	ctx, err := injectIntoGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}

	return streamer(ctx, desc, cc, method, opts...)
}

// StreamServerUserHeaderInterceptor propagates the query tags from the gRPC metadata back to our context.
func StreamServerQueryTagsInterceptor(srv interface{}, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, err := extractFromGRPCRequest(ss.Context())
	if err != nil {
		return err
	}

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
