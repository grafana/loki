// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/httpgrpc/httpgrpc.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package httpgrpc

import (
	"context"
	"fmt"

	"github.com/go-kit/log/level"
	"google.golang.org/grpc/metadata"

	spb "github.com/gogo/googleapis/google/rpc"
	"github.com/gogo/protobuf/types"
	"github.com/gogo/status"

	"github.com/grafana/dskit/log"
)

// Errorf returns a HTTP gRPC error than is correctly forwarded over
// gRPC, and can eventually be converted back to a HTTP response with
// HTTPResponseFromError.
func Errorf(code int, tmpl string, args ...interface{}) error {
	return ErrorFromHTTPResponse(&HTTPResponse{
		Code: int32(code),
		Body: []byte(fmt.Sprintf(tmpl, args...)),
	})
}

// ErrorFromHTTPResponse converts an HTTP response into a grpc error
func ErrorFromHTTPResponse(resp *HTTPResponse) error {
	a, err := types.MarshalAny(resp)
	if err != nil {
		return err
	}

	return status.ErrorProto(&spb.Status{
		Code:    resp.Code,
		Message: string(resp.Body),
		Details: []*types.Any{a},
	})
}

// HTTPResponseFromError converts a grpc error into an HTTP response
func HTTPResponseFromError(err error) (*HTTPResponse, bool) {
	s, ok := status.FromError(err)
	if !ok {
		return nil, false
	}

	status := s.Proto()
	if len(status.Details) != 1 {
		return nil, false
	}

	var resp HTTPResponse
	if err := types.UnmarshalAny(status.Details[0], &resp); err != nil {
		level.Error(log.Global()).Log("msg", "got error containing non-response", "err", err)
		return nil, false
	}

	return &resp, true
}

const (
	MetadataMethod = "httpgrpc-method"
	MetadataURL    = "httpgrpc-url"
)

// AppendRequestMetadataToContext appends metadata of HTTPRequest into gRPC metadata.
func AppendRequestMetadataToContext(ctx context.Context, req *HTTPRequest) context.Context {
	return metadata.AppendToOutgoingContext(ctx,
		MetadataMethod, req.Method,
		MetadataURL, req.Url)
}
