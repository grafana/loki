package congestion

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

var errFakeFailure = errors.New("fake failure")

func TestRequestNoopRetry(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
		},
	}

	metrics := NewMetrics(t.Name(), cfg)
	ctrl := NewController(cfg, log.NewNopLogger(), metrics)

	// allow 1 request through, fail the rest
	cli := newMockObjectClient(maxFailer{max: 1})
	ctrl.Wrap(cli)

	ctx := context.Background()

	// first request succeeds
	_, _, err := ctrl.GetObject(ctx, "foo")
	require.NoError(t, err)

	// nothing is done for failed requests
	_, _, err = ctrl.GetObject(ctx, "foo")
	require.ErrorIs(t, err, errFakeFailure)

	require.EqualValues(t, 2, testutil.ToFloat64(metrics.requests))
	require.EqualValues(t, 0, testutil.ToFloat64(metrics.retries))
	metrics.Unregister()
}

func TestRequestZeroLimitedRetry(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
		},
		Retry: RetrierConfig{
			Strategy: "limited",
			Limit:    0,
		},
	}

	metrics := NewMetrics(t.Name(), cfg)
	ctrl := NewController(cfg, log.NewNopLogger(), metrics)

	// fail all requests
	cli := newMockObjectClient(maxFailer{max: 0})
	ctrl.Wrap(cli)

	ctx := context.Background()

	// first request fails, no retry is executed because limit = 0
	_, _, err := ctrl.GetObject(ctx, "foo")
	require.ErrorIs(t, err, RetriesExceeded)

	require.EqualValues(t, 1, testutil.ToFloat64(metrics.requests))
	require.EqualValues(t, 0, testutil.ToFloat64(metrics.retries))
	metrics.Unregister()
}

func TestRequestLimitedRetry(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
		},
		Retry: RetrierConfig{
			Strategy: "limited",
			Limit:    2,
		},
	}

	metrics := NewMetrics(t.Name(), cfg)
	ctrl := NewController(cfg, log.NewNopLogger(), metrics)

	// allow 1 request through, fail the rest
	cli := newMockObjectClient(maxFailer{max: 1})
	ctrl.Wrap(cli)

	ctx := context.Background()

	// first request succeeds, no retries
	_, _, err := ctrl.GetObject(ctx, "foo")
	require.NoError(t, err)
	require.EqualValues(t, 0, testutil.ToFloat64(metrics.retriesExceeded))
	require.EqualValues(t, 0, testutil.ToFloat64(metrics.retries))
	require.EqualValues(t, 1, testutil.ToFloat64(metrics.requests))

	// all requests will now fail, which should incur 1 request & 2 retries
	_, _, err = ctrl.GetObject(ctx, "foo")
	require.ErrorIs(t, err, RetriesExceeded)
	require.EqualValues(t, 1, testutil.ToFloat64(metrics.retriesExceeded))
	require.EqualValues(t, 2, testutil.ToFloat64(metrics.retries))
	require.EqualValues(t, 4, testutil.ToFloat64(metrics.requests))
	metrics.Unregister()
}

func TestRequestLimitedRetryNonRetryableErr(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
		},
		Retry: RetrierConfig{
			Strategy: "limited",
			Limit:    2,
		},
	}

	metrics := NewMetrics(t.Name(), cfg)
	ctrl := NewController(cfg, log.NewNopLogger(), metrics)

	// fail all requests
	cli := newMockObjectClient(maxFailer{max: 0})
	// mark errors as non-retryable
	cli.nonRetryableErrs = true
	ctrl.Wrap(cli)

	ctx := context.Background()

	// request fails, retries not done since error is non-retryable
	_, _, err := ctrl.GetObject(ctx, "foo")
	require.ErrorIs(t, err, errFakeFailure)
	require.EqualValues(t, 0, testutil.ToFloat64(metrics.retries))
	require.EqualValues(t, 1, testutil.ToFloat64(metrics.nonRetryableErrors))
	require.EqualValues(t, 1, testutil.ToFloat64(metrics.requests))
	metrics.Unregister()
}

func TestAIMDReducedThroughput(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
			AIMD: AIMD{
				Start:         1000,
				UpperBound:    5000,
				BackoffFactor: 0.5,
			},
		},
		Retry: RetrierConfig{
			Strategy: "limited",
			Limit:    1,
		},
	}

	var trigger atomic.Bool

	metrics := NewMetrics(t.Name(), cfg)
	ctrl := NewController(cfg, log.NewNopLogger(), metrics)

	// fail requests only when triggered
	cli := newMockObjectClient(triggeredFailer{trigger: &trigger})
	ctrl.Wrap(cli)

	statsCtx, ctx := stats.NewContext(context.Background())

	// run for 1 second, measure the per-second rate of requests & successful responses
	count, success := runAndMeasureRate(ctx, ctrl, time.Second)
	require.Greater(t, count, 1.0)
	require.Greater(t, success, 1.0)
	// no time spent backing off because the per-second limit will not be hit
	require.EqualValues(t, 0, testutil.ToFloat64(metrics.backoffSec))

	previousCount, previousSuccess := count, success

	var wg sync.WaitGroup
	done := make(chan bool, 1)

	// every 100ms trigger a failure
	wg.Add(1)
	go func(trigger *atomic.Bool) {
		defer wg.Done()

		tick := time.NewTicker(time.Millisecond * 100)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				trigger.Store(true)
			case <-done:
				return
			}
		}
	}(&trigger)

	// now, run the requests again but there will now be a failure rate & some throttling involved
	count, success = runAndMeasureRate(ctx, ctrl, time.Second)
	done <- true

	wg.Wait()

	// should have processed fewer requests than the last period
	require.Less(t, count, previousCount)
	require.Less(t, success, previousSuccess)

	// should have fewer successful requests than total since we may be failing some
	require.LessOrEqual(t, success, count)

	// should have registered some congestion latency in stats
	require.NotZero(t, statsCtx.Store().CongestionControlLatency)
	metrics.Unregister()
}

func runAndMeasureRate(ctx context.Context, ctrl Controller, duration time.Duration) (float64, float64) {
	var count, success float64

	tick := time.NewTimer(duration)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			goto result
		default:
			count++
			_, _, err := ctrl.GetObject(ctx, "foo")
			if err == nil {
				success++
			}
		}
	}

result:
	return count / duration.Seconds(), success / duration.Seconds()
}

type mockObjectClient struct {
	reqCounter       atomic.Uint64
	strategy         requestFailer
	nonRetryableErrs bool
}

func (m *mockObjectClient) PutObject(context.Context, string, io.Reader) error {
	panic("not implemented")
}

func (m *mockObjectClient) GetObject(context.Context, string) (io.ReadCloser, int64, error) {
	time.Sleep(time.Millisecond * 10)
	if m.strategy.fail(m.reqCounter.Inc()) {
		return nil, 0, errFakeFailure
	}

	return io.NopCloser(strings.NewReader("bar")), 3, nil
}
func (m *mockObjectClient) GetObjectRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	panic("not implemented")
}

func (m *mockObjectClient) ObjectExists(context.Context, string) (bool, error) {
	panic("not implemented")
}

func (m *mockObjectClient) GetAttributes(context.Context, string) (client.ObjectAttributes, error) {
	panic("not implemented")
}

func (m *mockObjectClient) List(context.Context, string, string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	panic("not implemented")
}

func (m *mockObjectClient) DeleteObject(context.Context, string) error {
	panic("not implemented")
}
func (m *mockObjectClient) IsObjectNotFoundErr(error) bool { return false }
func (m *mockObjectClient) IsRetryableErr(error) bool      { return !m.nonRetryableErrs }
func (m *mockObjectClient) Stop()                          {}

func newMockObjectClient(start requestFailer) *mockObjectClient {
	return &mockObjectClient{strategy: start}
}

type requestFailer interface {
	fail(i uint64) bool
}

type maxFailer struct {
	max uint64
}

func (m maxFailer) fail(i uint64) bool { return i > m.max }

type triggeredFailer struct {
	trigger *atomic.Bool
}

func (t triggeredFailer) fail(_ uint64) bool {
	if t.trigger.Load() {
		t.trigger.Store(false)
		return true
	}

	return false
}
