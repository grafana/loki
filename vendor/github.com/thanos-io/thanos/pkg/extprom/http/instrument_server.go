// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/uber/jaeger-client-go"
)

// InstrumentationMiddleware holds necessary metrics to instrument an http.Server
// and provides necessary behaviors.
type InstrumentationMiddleware interface {
	// NewHandler wraps the given HTTP handler for instrumentation.
	NewHandler(handlerName string, handler http.Handler) http.HandlerFunc
}

type nopInstrumentationMiddleware struct{}

func (ins nopInstrumentationMiddleware) NewHandler(handlerName string, handler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}
}

// NewNopInstrumentationMiddleware provides a InstrumentationMiddleware which does nothing.
func NewNopInstrumentationMiddleware() InstrumentationMiddleware {
	return nopInstrumentationMiddleware{}
}

type defaultInstrumentationMiddleware struct {
	requestDuration *prometheus.HistogramVec
	requestSize     *prometheus.SummaryVec
	requestsTotal   *prometheus.CounterVec
	responseSize    *prometheus.SummaryVec
}

// NewInstrumentationMiddleware provides default InstrumentationMiddleware.
// Passing nil as buckets uses the default buckets.
func NewInstrumentationMiddleware(reg prometheus.Registerer, buckets []float64) InstrumentationMiddleware {
	if buckets == nil {
		buckets = []float64{0.001, 0.01, 0.1, 0.3, 0.6, 1, 3, 6, 9, 20, 30, 60, 90, 120, 240, 360, 720}
	}

	ins := defaultInstrumentationMiddleware{
		requestDuration: promauto.With(reg).NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "Tracks the latencies for HTTP requests.",
				Buckets: buckets,
			},
			[]string{"code", "handler", "method"},
		),

		requestSize: promauto.With(reg).NewSummaryVec(
			prometheus.SummaryOpts{
				Name: "http_request_size_bytes",
				Help: "Tracks the size of HTTP requests.",
			},
			[]string{"code", "handler", "method"},
		),

		requestsTotal: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Tracks the number of HTTP requests.",
			}, []string{"code", "handler", "method"},
		),

		responseSize: promauto.With(reg).NewSummaryVec(
			prometheus.SummaryOpts{
				Name: "http_response_size_bytes",
				Help: "Tracks the size of HTTP responses.",
			},
			[]string{"code", "handler", "method"},
		),
	}
	return &ins
}

// NewHandler wraps the given HTTP handler for instrumentation. It
// registers four metric collectors (if not already done) and reports HTTP
// metrics to the (newly or already) registered collectors: http_requests_total
// (CounterVec), http_request_duration_seconds (Histogram),
// http_request_size_bytes (Summary), http_response_size_bytes (Summary). Each
// has a constant label named "handler" with the provided handlerName as
// value. http_requests_total is a metric vector partitioned by HTTP method
// (label name "method") and HTTP status code (label name "code").
func (ins *defaultInstrumentationMiddleware) NewHandler(handlerName string, handler http.Handler) http.HandlerFunc {
	return promhttp.InstrumentHandlerRequestSize(
		ins.requestSize.MustCurryWith(prometheus.Labels{"handler": handlerName}),
		promhttp.InstrumentHandlerCounter(
			ins.requestsTotal.MustCurryWith(prometheus.Labels{"handler": handlerName}),
			promhttp.InstrumentHandlerResponseSize(
				ins.responseSize.MustCurryWith(prometheus.Labels{"handler": handlerName}),
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					now := time.Now()

					wd := &responseWriterDelegator{w: w}
					handler.ServeHTTP(wd, r)

					observer := ins.requestDuration.WithLabelValues(
						wd.Status(),
						handlerName,
						strings.ToLower(r.Method),
					)
					observer.Observe(time.Since(now).Seconds())

					// If we find a tracingID we'll expose it as Exemplar.
					span := opentracing.SpanFromContext(r.Context())
					if span != nil {
						spanCtx, ok := span.Context().(jaeger.SpanContext)
						if ok && spanCtx.IsSampled() {
							observer.(prometheus.ExemplarObserver).ObserveWithExemplar(
								time.Since(now).Seconds(),
								prometheus.Labels{
									"traceID": spanCtx.TraceID().String(),
								},
							)
						}
					}
				}),
			),
		),
	)
}

// responseWriterDelegator implements http.ResponseWriter and extracts the statusCode.
type responseWriterDelegator struct {
	w          http.ResponseWriter
	written    bool
	statusCode int
}

func (wd *responseWriterDelegator) Header() http.Header {
	return wd.w.Header()
}

func (wd *responseWriterDelegator) Write(bytes []byte) (int, error) {
	return wd.w.Write(bytes)
}

func (wd *responseWriterDelegator) WriteHeader(statusCode int) {
	wd.written = true
	wd.statusCode = statusCode
	wd.w.WriteHeader(statusCode)
}

func (wd *responseWriterDelegator) StatusCode() int {
	if !wd.written {
		return http.StatusOK
	}
	return wd.statusCode
}

func (wd *responseWriterDelegator) Status() string {
	return fmt.Sprintf("%d", wd.StatusCode())
}
