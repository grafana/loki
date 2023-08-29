package gcp

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
)

var (
	gcsRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "gcs_request_duration_seconds",
		Help:      "Time spent doing GCS requests.",

		// 6 buckets from 5ms to 20s.
		Buckets: prometheus.ExponentialBuckets(0.005, 4, 7),
	}, []string{"operation", "status_code"})
)

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
