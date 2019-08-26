package gcp

import (
	"context"
	"net/http"
	"strconv"
	"time"

	otgrpc "github.com/opentracing-contrib/go-grpc"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/api/option"
	google_http "google.golang.org/api/transport/http"
	"google.golang.org/grpc"

	"github.com/cortexproject/cortex/pkg/util/middleware"
)

var (
	bigtableRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "bigtable_request_duration_seconds",
		Help:      "Time spent doing Bigtable requests.",

		// Bigtable latency seems to range from a few ms to a few hundred ms and is
		// important.  So use 6 buckets from 1ms to 1s.
		Buckets: prometheus.ExponentialBuckets(0.001, 4, 6),
	}, []string{"operation", "status_code"})

	gcsRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
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

func gcsInstrumentation(ctx context.Context, scope string) (option.ClientOption, error) {
	transport, err := google_http.NewTransport(ctx, http.DefaultTransport, option.WithScopes(scope))
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Transport: instrumentedTransport{
			observer: gcsRequestDuration,
			next:     transport,
		},
	}
	return option.WithHTTPClient(client), nil
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
