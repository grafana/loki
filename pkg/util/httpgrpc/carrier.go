package httpgrpc

import (
	weaveworks_httpgrpc "github.com/grafana/dskit/httpgrpc"
	"github.com/opentracing/opentracing-go"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
)

type Request interface {
	GetQueryRequest() *queryrange.QueryRequest
	GetHttpRequest() *weaveworks_httpgrpc.HTTPRequest
}

// Used to transfer trace information from/to HTTP request.
type HeadersCarrier weaveworks_httpgrpc.HTTPRequest

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

func GetParentSpanForHTTPRequest(tracer opentracing.Tracer, req *weaveworks_httpgrpc.HTTPRequest) (opentracing.SpanContext, error) {
	if tracer == nil {
		return nil, nil
	}

	carrier := (*HeadersCarrier)(req)
	return tracer.Extract(opentracing.HTTPHeaders, carrier)
}

func GetParentSpanForQueryRequest(tracer opentracing.Tracer, req *queryrange.QueryRequest) (opentracing.SpanContext, error) {
	if tracer == nil {
		return nil, nil
	}

	carrier := opentracing.TextMapCarrier(req.Metadata)
	return tracer.Extract(opentracing.TextMap, carrier)
}

func GetParentSpanForRequest(tracer opentracing.Tracer, req Request) (opentracing.SpanContext, error) {
	if r := req.GetQueryRequest(); r != nil {
		return GetParentSpanForQueryRequest(tracer, r)
	}

	if r := req.GetHttpRequest(); r != nil {
		return GetParentSpanForHTTPRequest(tracer, r)
	}

	return nil, nil
}
