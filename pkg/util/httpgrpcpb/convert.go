package httpgrpcpb

import (
	"github.com/grafana/dskit/httpgrpc"
)

// FromHTTPRequest converts a dskit httpgrpc request into the local
// wire-identical representation. nil maps to nil.
func FromHTTPRequest(r *httpgrpc.HTTPRequest) *HTTPRequest {
	if r == nil {
		return nil
	}
	return &HTTPRequest{
		Method:  r.Method,
		Url:     r.Url,
		Headers: fromHeaders(r.Headers),
		Body:    r.Body,
	}
}

// ToHTTPRequest converts the local representation back to the dskit type.
func ToHTTPRequest(r *HTTPRequest) *httpgrpc.HTTPRequest {
	if r == nil {
		return nil
	}
	return &httpgrpc.HTTPRequest{
		Method:  r.Method,
		Url:     r.Url,
		Headers: toHeaders(r.Headers),
		Body:    r.Body,
	}
}

// FromHTTPResponse converts a dskit httpgrpc response into the local
// wire-identical representation. nil maps to nil.
func FromHTTPResponse(r *httpgrpc.HTTPResponse) *HTTPResponse {
	if r == nil {
		return nil
	}
	return &HTTPResponse{
		Code:    r.Code,
		Headers: fromHeaders(r.Headers),
		Body:    r.Body,
	}
}

// ToHTTPResponse converts the local representation back to the dskit type.
func ToHTTPResponse(r *HTTPResponse) *httpgrpc.HTTPResponse {
	if r == nil {
		return nil
	}
	return &httpgrpc.HTTPResponse{
		Code:    r.Code,
		Headers: toHeaders(r.Headers),
		Body:    r.Body,
	}
}

func fromHeaders(hs []*httpgrpc.Header) []*Header {
	if hs == nil {
		return nil
	}
	out := make([]*Header, 0, len(hs))
	for _, h := range hs {
		if h == nil {
			continue
		}
		out = append(out, &Header{Key: h.Key, Values: h.Values})
	}
	return out
}

func toHeaders(hs []*Header) []*httpgrpc.Header {
	if hs == nil {
		return nil
	}
	out := make([]*httpgrpc.Header, 0, len(hs))
	for _, h := range hs {
		if h == nil {
			continue
		}
		out = append(out, &httpgrpc.Header{Key: h.Key, Values: h.Values})
	}
	return out
}
