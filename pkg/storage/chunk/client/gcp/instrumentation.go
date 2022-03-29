package gcp

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/grafana/dskit/middleware"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/api/option"
	google_http "google.golang.org/api/transport/http"
	"google.golang.org/grpc"
)

var (
	bigtableRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "bigtable_request_duration_seconds",
		Help:      "Time spent doing Bigtable requests.",

		// Bigtable latency seems to range from a few ms to a several seconds and is
		// important.  So use 9 buckets from 1ms to just over 1 minute (65s).
		Buckets: prometheus.ExponentialBuckets(0.001, 4, 9),
	}, []string{"operation", "status_code"})

	gcsRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "gcs_request_duration_seconds",
		Help:      "Time spent doing GCS requests.",

		// GCS latency seems to range from a few ms to a few secs and is
		// important.  So use 6 buckets from 5ms to 5s.
		Buckets: prometheus.ExponentialBuckets(0.005, 4, 6),
	}, []string{"operation", "status_code"})
)

func bigtableInstrumentation() ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	return []grpc.UnaryClientInterceptor{
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.PrometheusGRPCUnaryInstrumentation(bigtableRequestDuration),
		},
		[]grpc.StreamClientInterceptor{
			otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
			middleware.PrometheusGRPCStreamInstrumentation(bigtableRequestDuration),
		}
}

func gcsInstrumentation(ctx context.Context, scope string, insecure bool, http2 bool) (*http.Client, error) {
	customTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   200,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if !http2 {
		// disable HTTP/2 by setting TLSNextProto to non-nil empty map, as per the net/http documentation.
		// see http2 section of https://pkg.go.dev/net/http
		customTransport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
		customTransport.ForceAttemptHTTP2 = false
	}
	if insecure {
		customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	transport, err := google_http.NewTransport(ctx, customTransport, option.WithScopes(scope))
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Transport: instrumentedTransport{
			observer: gcsRequestDuration,
			next:     transport,
		},
	}
	return client, nil
}

func toOptions(opts []grpc.DialOption) []option.ClientOption {
	result := make([]option.ClientOption, 0, len(opts))
	for _, opt := range opts {
		result = append(result, option.WithGRPCDialOption(opt))
	}
	return result
}

type instrumentedTransport struct {
	observer prometheus.ObserverVec
	next     http.RoundTripper
}

func (i instrumentedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := i.next.RoundTrip(req)
	if err == nil {
		i.observer.WithLabelValues(req.Method, strconv.Itoa(resp.StatusCode)).Observe(time.Since(start).Seconds())
	}
	return resp, err
}
