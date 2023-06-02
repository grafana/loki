package httpgrpc

import (
	"errors"

	"github.com/opentracing/opentracing-go"
	weaveworks_httpgrpc "github.com/weaveworks/common/httpgrpc"
)

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

func GetParentSpanForRequest(tracer opentracing.Tracer, req *weaveworks_httpgrpc.HTTPRequest) (opentracing.SpanContext, error) {
	if tracer == nil {
		return nil, nil
	}

	carrier := (*HeadersCarrier)(req)
	extracted, err := tracer.Extract(opentracing.HTTPHeaders, carrier)
	if errors.Is(err, opentracing.ErrSpanContextNotFound) {
		err = nil
	}
	return extracted, err
}
