// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/middleware/instrument.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package middleware

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/felixge/httpsnoop"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/dskit/instrument"
)

const mb = 1024 * 1024

// BodySizeBuckets defines buckets for request/response body sizes.
var BodySizeBuckets = []float64{1 * mb, 2.5 * mb, 5 * mb, 10 * mb, 25 * mb, 50 * mb, 100 * mb, 250 * mb}

// RouteMatcher matches routes
type RouteMatcher interface {
	Match(*http.Request, *mux.RouteMatch) bool
}

// PerTenantCallback is a function that returns a tenant ID for a given request. When the returned tenant ID is not empty, it is used to label the duration histogram.
type PerTenantCallback func(context.Context) string

func (f PerTenantCallback) shouldInstrument(ctx context.Context) (string, bool) {
	if f == nil {
		return "", false
	}
	tenantID := f(ctx)
	if tenantID == "" {
		return "", false
	}
	return tenantID, true
}

// Instrument is a Middleware which records timings for every HTTP request
type Instrument struct {
	Duration          *prometheus.HistogramVec
	PerTenantDuration *prometheus.HistogramVec
	PerTenantCallback PerTenantCallback
	RequestBodySize   *prometheus.HistogramVec
	ResponseBodySize  *prometheus.HistogramVec
	InflightRequests  *prometheus.GaugeVec
}

// IsWSHandshakeRequest returns true if the given request is a websocket handshake request.
func IsWSHandshakeRequest(req *http.Request) bool {
	if strings.ToLower(req.Header.Get("Upgrade")) == "websocket" {
		// Connection header values can be of form "foo, bar, ..."
		parts := strings.Split(strings.ToLower(req.Header.Get("Connection")), ",")
		for _, part := range parts {
			if strings.TrimSpace(part) == "upgrade" {
				return true
			}
		}
	}
	return false
}

// Wrap implements middleware.Interface
func (i Instrument) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := i.getRouteName(r)
		inflight := i.InflightRequests.WithLabelValues(r.Method, route)
		inflight.Inc()
		defer inflight.Dec()

		origBody := r.Body
		defer func() {
			// No need to leak our Body wrapper beyond the scope of this handler.
			r.Body = origBody
		}()

		rBody := &reqBody{b: origBody}
		r.Body = rBody

		isWS := strconv.FormatBool(IsWSHandshakeRequest(r))

		respMetrics := httpsnoop.CaptureMetricsFn(w, func(ww http.ResponseWriter) {
			next.ServeHTTP(ww, r)
		})

		i.RequestBodySize.WithLabelValues(r.Method, route).Observe(float64(rBody.read))
		i.ResponseBodySize.WithLabelValues(r.Method, route).Observe(float64(respMetrics.Written))

		labelValues := []string{
			r.Method,
			route,
			strconv.Itoa(respMetrics.Code),
			isWS,
			"", // this is a placeholder for the tenant ID
		}
		labelValues = labelValues[:len(labelValues)-1]
		instrument.ObserveWithExemplar(r.Context(), i.Duration.WithLabelValues(labelValues...), respMetrics.Duration.Seconds())
		if tenantID, ok := i.PerTenantCallback.shouldInstrument(r.Context()); ok {
			labelValues = append(labelValues, tenantID)
			instrument.ObserveWithExemplar(r.Context(), i.PerTenantDuration.WithLabelValues(labelValues...), respMetrics.Duration.Seconds())
		}
	})
}

// Return a name identifier for ths request.  There are three options:
//  1. The request matches a gorilla mux route, with a name.  Use that.
//  2. The request matches an unamed gorilla mux router.  Munge the path
//     template such that templates like '/api/{org}/foo' come out as
//     'api_org_foo'.
//  3. The request doesn't match a mux route. Return "other"
//
// We do all this as we do not wish to emit high cardinality labels to
// prometheus.
func (i Instrument) getRouteName(r *http.Request) string {
	route := ExtractRouteName(r.Context())
	if route == "" {
		route = "other"
	}

	return route
}

type reqBody struct {
	b    io.ReadCloser
	read int64
}

func (w *reqBody) Read(p []byte) (int, error) {
	n, err := w.b.Read(p)
	if n > 0 {
		w.read += int64(n)
	}
	return n, err
}

func (w *reqBody) Close() error {
	return w.b.Close()
}
