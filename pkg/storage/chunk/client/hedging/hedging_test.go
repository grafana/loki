package hedging

import (
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestHedging(t *testing.T) {
	beforeTotal := testutil.ToFloat64(totalHedgeRequests)
	beforeLimited := testutil.ToFloat64(totalRateLimitedHedgeRequests)

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
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.Get("http://example.com")

	require.Equal(t, int32(3), count.Load())
	require.Equal(t, 2.0, testutil.ToFloat64(totalHedgeRequests)-beforeTotal)
	require.Equal(t, 0.0, testutil.ToFloat64(totalRateLimitedHedgeRequests)-beforeLimited)
}

func TestHedgingRateLimit(t *testing.T) {
	beforeTotal := testutil.ToFloat64(totalHedgeRequests)
	beforeLimited := testutil.ToFloat64(totalRateLimitedHedgeRequests)

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
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.Get("http://example.com")

	require.Equal(t, int32(2), count.Load())
	require.Equal(t, 1.0, testutil.ToFloat64(totalHedgeRequests)-beforeTotal)
	require.Equal(t, 18.0, testutil.ToFloat64(totalRateLimitedHedgeRequests)-beforeLimited)
}
