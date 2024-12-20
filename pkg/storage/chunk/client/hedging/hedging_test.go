package hedging

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func resetMetrics() *prometheus.Registry {
	//TODO: clean up this massive hack...
	reg := prometheus.NewRegistry()
	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg
	initMetrics()
	return reg
}

func TestHedging(t *testing.T) {
	reg := resetMetrics()
	cfg := &Config{
		At:           time.Duration(1),
		UpTo:         3,
		MaxPerSecond: 1000,
	}
	count := atomic.NewInt32(0)
	client, err := cfg.ClientWithRegisterer(&http.Client{
		Transport: RoundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			count.Inc()
			time.Sleep(200 * time.Millisecond)
			return &http.Response{
				StatusCode: http.StatusOK,
			}, nil
		}),
	}, reg)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.Get("http://example.com")

	require.Equal(t, int32(3), count.Load())
	require.NoError(t, testutil.GatherAndCompare(reg,
		strings.NewReader(`
# HELP hedged_requests_rate_limited_total The total number of hedged requests rejected via rate limiting.
# TYPE hedged_requests_rate_limited_total counter
hedged_requests_rate_limited_total 0
# HELP hedged_requests_total The total number of hedged requests.
# TYPE hedged_requests_total counter
hedged_requests_total 2
`,
		), "hedged_requests_total", "hedged_requests_rate_limited_total"))
}

func TestHedgingRateLimit(t *testing.T) {
	reg := resetMetrics()
	cfg := &Config{
		At:           time.Duration(1),
		UpTo:         20,
		MaxPerSecond: 1,
	}
	count := atomic.NewInt32(0)
	client, err := cfg.ClientWithRegisterer(&http.Client{
		Transport: RoundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			count.Inc()
			time.Sleep(200 * time.Millisecond)
			return &http.Response{
				StatusCode: http.StatusOK,
			}, nil
		}),
	}, reg)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.Get("http://example.com")

	require.Equal(t, int32(2), count.Load())
	require.NoError(t, testutil.GatherAndCompare(reg,
		strings.NewReader(`
# HELP hedged_requests_rate_limited_total The total number of hedged requests rejected via rate limiting.
# TYPE hedged_requests_rate_limited_total counter
hedged_requests_rate_limited_total 18
# HELP hedged_requests_total The total number of hedged requests.
# TYPE hedged_requests_total counter
hedged_requests_total 1
`,
		), "hedged_requests_total", "hedged_requests_rate_limited_total"))
}
