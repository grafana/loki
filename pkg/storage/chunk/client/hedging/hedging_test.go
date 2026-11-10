package hedging

import (
	"context"
	"io"
	"net/http"
	"strings"
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

// The winning request's context must be cancelled once its body is closed,
// otherwise it stays attached to a long-lived parent context and leaks.
func TestHedgingCancelsWinnerContextOnBodyClose(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &Config{
		At:           time.Hour,
		UpTo:         2,
		MaxPerSecond: 1000,
	}
	ctxs := make(chan context.Context, 1)
	rt, err := cfg.RoundTripperWithRegisterer(RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		ctxs <- req.Context()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	}), nil)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(parent, http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)

	winnerCtx := <-ctxs
	require.NoError(t, winnerCtx.Err(), "context cancelled before body was read")

	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.ErrorIs(t, winnerCtx.Err(), context.Canceled)
	require.NoError(t, parent.Err())
}
