package transport

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/httpgrpc/server"
)

// GrpcRoundTripper is similar to http.RoundTripper, but works with HTTP requests converted to protobuf messages.
type GrpcRoundTripper interface {
	RoundTripGRPC(context.Context, *httpgrpc.HTTPRequest) (*httpgrpc.HTTPResponse, error)
}

// TODO: remove
func AdaptGrpcRoundTripperToHTTPRoundTripper(r GrpcRoundTripper) http.RoundTripper {
	return &grpcRoundTripperAdapter{roundTripper: r}
}

// This adapter wraps GrpcRoundTripper and converted it into http.RoundTripper
type grpcRoundTripperAdapter struct {
	roundTripper GrpcRoundTripper
}

type Buffer interface {
	Bytes() []byte
}

type buffer struct {
	buff []byte
	io.ReadCloser
}

func (b *buffer) Bytes() []byte {
	return b.buff
}

func (a *grpcRoundTripperAdapter) RoundTrip(r *http.Request) (*http.Response, error) {
	req, err := server.HTTPRequest(r)
	if err != nil {
		return nil, err
	}

	resp, err := a.roundTripper.RoundTripGRPC(r.Context(), req)
	if err != nil {
		return nil, err
	}

	return HttpgrpcToHTTPResponse(resp), nil
}

func HttpgrpcToHTTPResponse(resp *httpgrpc.HTTPResponse) *http.Response {
	httpResp := &http.Response{
		StatusCode:    int(resp.Code),
		Body:          &buffer{buff: resp.Body, ReadCloser: io.NopCloser(bytes.NewReader(resp.Body))},
		Header:        http.Header{},
		ContentLength: int64(len(resp.Body)),
	}
	for _, h := range resp.Headers {
		httpResp.Header[h.Key] = h.Values
	}

	return httpResp
}

func HTTPtoHttpgrpcResponse(resp *http.Response) (*httpgrpc.HTTPResponse, error) {
	var buf []byte
	var err error
	if buffer, ok := resp.Body.(Buffer); ok {
		buf = buffer.Bytes()
	} else {
		buf, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "error decoding response: %v", err)
		}
	}

	grpcResp := &httpgrpc.HTTPResponse{
		Code: int32(resp.StatusCode),
		Body: buf,
	}
	for k, v := range resp.Header {
		grpcResp.Headers = append(grpcResp.Headers, &httpgrpc.Header{Key: k, Values: v})
	}

	return grpcResp, nil
}
