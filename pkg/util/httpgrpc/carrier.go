package httpgrpc

import (
	"context"

	weaveworks_httpgrpc "github.com/grafana/dskit/httpgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
)

type Request interface {
	GetQueryRequest() *queryrange.QueryRequest
	GetHttpRequest() *weaveworks_httpgrpc.HTTPRequest
}

// Used to transfer trace information from/to HTTP request.
type HeadersCarrier weaveworks_httpgrpc.HTTPRequest

func (c *HeadersCarrier) Get(key string) string {
	// Check if the key exists in the headers
	for _, h := range c.Headers {
		if h.Key == key {
			// Return the first value for the key
			if len(h.Values) > 0 {
				return h.Values[0]
			}
			break
		}
	}
	return ""
}

func (c *HeadersCarrier) Keys() []string {
	// Collect all unique keys from the headers
	keys := make([]string, 0, len(c.Headers))
	for _, h := range c.Headers {
		if h.Key != "" {
			keys = append(keys, h.Key)
		}
	}
	return keys
}

func (c *HeadersCarrier) Set(key, val string) {
	c.Headers = append(c.Headers, &weaveworks_httpgrpc.Header{
		Key:    key,
		Values: []string{val},
	})
}

func (c *HeadersCarrier) ForeachKey(handler func(key, val string) error) error {
	for _, h := range c.Headers {
		for _, v := range h.Values {
			if err := handler(h.Key, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func ExtractSpanFromHTTPRequest(ctx context.Context, req *weaveworks_httpgrpc.HTTPRequest) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, (*HeadersCarrier)(req))
}

func ExtractSpanFromQueryRequest(ctx context.Context, req *queryrange.QueryRequest) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(req.Metadata))
}

func ExtractSpanFromRequest(ctx context.Context, req Request) context.Context {
	if r := req.GetQueryRequest(); r != nil {
		return ExtractSpanFromQueryRequest(ctx, r)
	}

	if r := req.GetHttpRequest(); r != nil {
		return ExtractSpanFromHTTPRequest(ctx, r)
	}

	return ctx
}
