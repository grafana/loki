package transport

import (
	"context"

	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

// GrpcRoundTripper is similar to http.RoundTripper, but works with HTTP requests converted to protobuf messages.
type GrpcRoundTripper interface {
	RoundTripGRPC(context.Context, *httpgrpc.HTTPRequest) (*httpgrpc.HTTPResponse, error)
}

type Codec interface {
	queryrangebase.Codec
	DecodeHTTPGrpcResponse(r *httpgrpc.HTTPResponse, req queryrangebase.Request) (queryrangebase.Response, error)
}
