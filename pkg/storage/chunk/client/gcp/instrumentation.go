package gcp

import (
	"net/http"
	"strconv"
	"time"

	"github.com/grafana/dskit/middleware"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/api/option"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

var (
	bigtableRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "bigtable_request_duration_seconds",
		Help:      "Time spent doing Bigtable requests.",

		// Bigtable latency seems to range from a few ms to a several seconds and is
		// important.  So use 9 buckets from 1ms to just over 1 minute (65s).
		Buckets: prometheus.ExponentialBuckets(0.001, 4, 9),
	}, []string{"operation", "status_code"})

	gcsRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "gcs_request_duration_seconds",
		Help:      "Time spent doing GCS requests.",

		// 6 buckets from 5ms to 20s.
		Buckets: prometheus.ExponentialBuckets(0.005, 4, 7),
	}, []string{"operation", "status_code"})
)

func bigtableInstrumentation() ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	return []grpc.UnaryClientInterceptor{
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.UnaryClientInstrumentInterceptor(bigtableRequestDuration),
		},
		[]grpc.StreamClientInterceptor{
			otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
			middleware.StreamClientInstrumentInterceptor(bigtableRequestDuration),
		}
}

func gcsInstrumentation(transport http.RoundTripper) *http.Client {
	client := &http.Client{
		Transport: instrumentedTransport{
			observer: gcsRequestDuration,
			next:     transport,
		},
	}
	return client
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
