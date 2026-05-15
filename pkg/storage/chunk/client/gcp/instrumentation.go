package gcp

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

var (
	gcsRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
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
