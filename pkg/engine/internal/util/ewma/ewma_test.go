package ewma_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/ewma"
)

func Test(t *testing.T) {
	var idx int
	observations := []float64{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
		11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
	}

	source := ewma.SourceFunc(func() float64 {
		if idx >= len(observations) {
			<-t.Context().Done()
			return 0
		}

		res := observations[idx]
		idx++
		return res
	})

	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()

		m, err := ewma.New(ewma.Options{
			Name:            "my_ewma_metric",
			UpdateFrequency: time.Minute,
			Windows:         []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		}, source)
		require.NoError(t, err)

		reg.MustRegister(m)

		var wg sync.WaitGroup
		defer wg.Wait()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		wg.Go(func() { _ = m.Monitor(ctx) })

		// Wait for all observations to have been made.
		time.Sleep(time.Duration(len(observations)) * time.Minute)
		synctest.Wait()

		expect := `
# HELP
# TYPE my_ewma_metric gauge
my_ewma_metric{window="15m"} 9.581664816797675
my_ewma_metric{window="1m"} 19.41802329639137
my_ewma_metric{window="5m"} 15.584385505095714
`
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expect)))
	})
}

func TestEWMA_Collect(t *testing.T) {
	const timeout = 5 * time.Second

	opts := ewma.Options{
		Name:            "my_ewma_metric",
		UpdateFrequency: time.Millisecond,
		Windows:         []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
	}

	t.Run("does not block Monitor while it waits to send a metric", func(t *testing.T) {
		samples := make(chan struct{}, 1)
		source := ewma.SourceFunc(func() float64 {
			select {
			case samples <- struct{}{}:
			default:
			}
			return 1
		})

		m, err := ewma.New(opts, source)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		var wg sync.WaitGroup
		defer wg.Wait()
		defer cancel()
		wg.Go(func() { _ = m.Monitor(ctx) })

		// Collect reports nothing until Monitor runs, and Monitor samples only
		// after it starts.
		requireReceive(t, samples, timeout, "Monitor did not take a first sample")

		// An unbuffered channel that nobody reads stands in for a registry
		// that is busy evaluating earlier metrics.
		metrics := make(chan prometheus.Metric)
		go func() {
			m.Collect(metrics)
			close(metrics)
		}()
		defer func() {
			for range metrics {
			}
		}()
		requireReceive(t, metrics, timeout, "Collect did not send a first metric")

		// Monitor samples before it updates the windows, so the second sample
		// proves that one update finished while Collect waited.
		select {
		case <-samples:
		default:
		}
		requireReceive(t, samples, timeout, "Monitor is blocked while Collect waits to send a metric")
		requireReceive(t, samples, timeout, "Monitor is blocked while Collect waits to send a metric")
	})

	t.Run("does not wait for a source that blocks", func(t *testing.T) {
		sampling := make(chan struct{})
		release := make(chan struct{})
		var samplingOnce sync.Once
		source := ewma.SourceFunc(func() float64 {
			samplingOnce.Do(func() { close(sampling) })
			<-release
			return 1
		})

		m, err := ewma.New(opts, source)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		var wg sync.WaitGroup
		defer wg.Wait()
		defer cancel()
		defer close(release)
		wg.Go(func() { _ = m.Monitor(ctx) })

		requireReceive(t, sampling, timeout, "Monitor did not call the source")

		metrics := make(chan prometheus.Metric, len(opts.Windows))
		collected := make(chan struct{})
		go func() {
			m.Collect(metrics)
			close(collected)
		}()
		requireReceive(t, collected, timeout, "Collect waits for a blocked source")
	})
}

// requireReceive fails the test with msg if ch does not deliver a value or
// close within timeout.
func requireReceive[T any](t *testing.T, ch <-chan T, timeout time.Duration, msg string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatal(msg)
	}
}
