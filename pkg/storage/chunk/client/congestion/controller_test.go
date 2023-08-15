package congestion

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/storage/chunk/client"
)

var fakeFailure = errors.New("fake failure")

func TestRequestNoopRetry(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
		},
	}
	ctrl := NewController(cfg, NewMetrics(t.Name(), cfg))

	// allow 1 request through, fail the rest
	cli := newMockObjectClient(maxFailer{max: 1})
	ctrl.Wrap(cli)

	ctx := context.Background()

	_, _, err := ctrl.GetObject(ctx, "foo")
	require.NoError(t, err)

	// nothing is done for failed requests
	_, _, err = ctrl.GetObject(ctx, "foo")
	require.ErrorIs(t, err, fakeFailure)
}

func TestRequestLimitedRetry(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
		},
		Retry: RetrierConfig{
			Strategy: "limited",
			Limit:    1,
		},
	}
	ctrl := NewController(cfg, NewMetrics(t.Name(), cfg))

	// allow 1 request through, fail the rest
	cli := newMockObjectClient(maxFailer{max: 1})
	ctrl.Wrap(cli)

	ctx := context.Background()

	_, _, err := ctrl.GetObject(ctx, "foo")
	require.NoError(t, err)

	// nothing is done for failed requests
	_, _, err = ctrl.GetObject(ctx, "foo")
	require.ErrorIs(t, err, RetriesExceeded)
}

func TestAIMDReducedThroughput(t *testing.T) {
	cfg := Config{
		Controller: ControllerConfig{
			Strategy: "aimd",
			AIMD: AIMD{
				Start:         10,
				UpperBound:    1000,
				BackoffFactor: 0.5,
			},
		},
		Retry: RetrierConfig{
			Strategy: "limited",
			Limit:    1,
		},
	}

	var trigger atomic.Bool
	ctrl := NewController(cfg, NewMetrics(t.Name(), cfg))

	// fail requests only when triggered
	cli := newMockObjectClient(triggeredFailer{trigger: &trigger})
	ctrl.Wrap(cli)

	// run for 1 second, measure the per-second rate of requests & successful responses
	count, success := runAndMeasureRate(ctrl, time.Second)
	require.Greater(t, count, 1.0)
	require.Greater(t, success, 1.0)

	previousCount, previousSuccess := count, success
	count = 0
	success = 0

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
	count, success = runAndMeasureRate(ctrl, time.Second)
	done <- true

	wg.Wait()

	// should have processed fewer requests than the last period
	require.Less(t, count, previousCount)
	require.Less(t, success, previousSuccess)

	// should have fewer successful requests than total since we are failing some
	require.Less(t, success, count)
}

func runAndMeasureRate(ctrl Controller, duration time.Duration) (float64, float64) {
	var count, success float64

	tick := time.NewTimer(duration)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			goto result
		default:
			ctx := context.Background()

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
	reqCounter atomic.Uint64
	strategy   requestFailer
}

func (m *mockObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	panic("not implemented")
}

func (m *mockObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	if m.strategy.fail(m.reqCounter.Inc()) {
		return nil, 0, fakeFailure
	}

	return io.NopCloser(strings.NewReader("bar")), 3, nil
}

func (m *mockObjectClient) List(ctx context.Context, prefix string, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	panic("not implemented")
}

func (m *mockObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	panic("not implemented")
}
func (m *mockObjectClient) IsObjectNotFoundErr(err error) bool { return false }
func (m *mockObjectClient) IsRetryableErr(err error) bool      { return true }
func (m *mockObjectClient) Stop()                              {}

func newMockObjectClient(strat requestFailer) *mockObjectClient {
	return &mockObjectClient{strategy: strat}
}

type requestFailer interface {
	fail(i uint64) bool
}

type noopFailer struct{}

func (n noopFailer) fail(i uint64) bool { return false }

type moduloFailer struct {
	modulo uint64
}

func (m moduloFailer) fail(i uint64) bool {
	return i > 0 && i&m.modulo == 0
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
