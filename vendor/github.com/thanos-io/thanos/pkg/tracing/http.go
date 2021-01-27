// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package tracing

import (
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
)

// HTTPMiddleware returns an HTTP handler that injects the given tracer and starts a new server span.
// If any client span is fetched from the wire, we include that as our parent.
func HTTPMiddleware(tracer opentracing.Tracer, name string, logger log.Logger, next http.Handler) http.HandlerFunc {
	operationName := fmt.Sprintf("/%s HTTP[server]", name)

	return func(w http.ResponseWriter, r *http.Request) {
		var span opentracing.Span
		wireContext, err := tracer.Extract(
			opentracing.HTTPHeaders,
			opentracing.HTTPHeadersCarrier(r.Header),
		)
		if err != nil && err != opentracing.ErrSpanContextNotFound {
			level.Error(logger).Log("msg", "failed to extract tracer from request", "operationName", operationName, "err", err)
		}

		span = tracer.StartSpan(operationName, ext.RPCServerOption(wireContext))
		ext.HTTPMethod.Set(span, r.Method)
		ext.HTTPUrl.Set(span, r.URL.String())

		// If client specified ForceTracingBaggageKey header, ensure span includes it to force tracing.
		span.SetBaggageItem(ForceTracingBaggageKey, r.Header.Get(ForceTracingBaggageKey))

		if t, ok := tracer.(Tracer); ok {
			if traceID, ok := t.GetTraceIDFromSpanContext(span.Context()); ok {
				w.Header().Set(traceIDResponseHeader, traceID)
			}
		}

		next.ServeHTTP(w, r.WithContext(opentracing.ContextWithSpan(ContextWithTracer(r.Context(), tracer), span)))
		span.Finish()
	}
}

type tripperware struct {
	logger log.Logger
	next   http.RoundTripper
}

func (t *tripperware) RoundTrip(r *http.Request) (*http.Response, error) {
	tracer := tracerFromContext(r.Context())
	if tracer == nil {
		// No tracer, programmatic mistake.
		level.Warn(t.logger).Log("msg", "Tracer not found in context.")
		return t.next.RoundTrip(r)
	}

	span := opentracing.SpanFromContext(r.Context())
	if span == nil {
		// No span.
		return t.next.RoundTrip(r)
	}

	ext.HTTPMethod.Set(span, r.Method)
	ext.HTTPUrl.Set(span, r.URL.String())
	host, portString, err := net.SplitHostPort(r.URL.Host)
	if err == nil {
		ext.PeerHostname.Set(span, host)
		if port, err := strconv.Atoi(portString); err != nil {
			ext.PeerPort.Set(span, uint16(port))
		}
	} else {
		ext.PeerHostname.Set(span, r.URL.Host)
	}

	// There's nothing we can do with any errors here.
	if err = tracer.Inject(
		span.Context(),
		opentracing.HTTPHeaders,
		opentracing.HTTPHeadersCarrier(r.Header),
	); err != nil {
		level.Warn(t.logger).Log("msg", "failed to inject trace", "err", err)
	}

	resp, err := t.next.RoundTrip(r)
	return resp, err
}

// HTTPTripperware returns HTTP tripper that assumes given span in context as client child span and injects it into the wire.
// NOTE: It assumes tracer is given in request context. Also, it is caller responsibility to finish span.
func HTTPTripperware(logger log.Logger, next http.RoundTripper) http.RoundTripper {
	return &tripperware{
		logger: logger,
		next:   next,
	}
}
