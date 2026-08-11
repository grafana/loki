package aggregation

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

// These benchmarks demonstrate and quantify finding #6: Push.entries has no
// size bound, and it is only drained (via buildPayload -> entries.reset())
// once per completed send. buildPayload takes its snapshot and resets the
// queue *before* attempting to send, so every WriteEntry call that arrives
// while a send is stuck retrying (Loki down/slow) piles into the fresh queue
// with nothing capping it, for as long as the retry episode lasts.
//
// With the default backoff config (MaxRetries: 10, 100ms..10s) a single
// episode is bounded to a couple of minutes, so this is "holds more than it
// should for the length of one outage" rather than a leak. It becomes a true,
// unbounded leak under the dskit-documented "MaxRetries: 0 means infinite
// retries" configuration -- a legitimate choice for an operator who wants to
// never drop data -- because then nothing ever forces the retry loop to give
// up and return control to buildPayload.
//
// Run with, e.g.:
//
//	go test ./pkg/pattern/aggregation/ -run XXX -bench PushQueue -benchtime 1x -v
func BenchmarkPushQueue_DuringOutage(b *testing.B) {
	const writeRate = 500 // entries/sec; representative of many streams' sweep results funnelling into one tenant's Push during a burst

	for _, outage := range []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second} {
		b.Run(fmt.Sprintf("outage=%s", outage), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				runQueueScenario(b, outage, writeRate, true)
			}
		})
	}
}

// BenchmarkPushQueue_Healthy is the contrast case: the same write rate and
// window, but the backend accepts every push immediately. entries.reset()
// runs on schedule, so the queue never grows past roughly one push-period's
// worth of writes, regardless of how long the benchmark window is.
func BenchmarkPushQueue_Healthy(b *testing.B) {
	const writeRate = 500

	for _, window := range []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second} {
		b.Run(fmt.Sprintf("window=%s", window), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				runQueueScenario(b, window, writeRate, false)
			}
		})
	}
}

func runQueueScenario(b *testing.B, window time.Duration, rate int, down bool) {
	b.Helper()

	var failing atomic.Bool
	failing.Store(down)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		if failing.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	backoffCfg := &backoff.Config{
		MinBackoff: 20 * time.Millisecond,
		MaxBackoff: 20 * time.Millisecond,
		MaxRetries: 0, // infinite retries -- the configuration this benchmark targets
	}

	push, err := NewPush(
		srv.Listener.Addr().String(),
		"bench-tenant",
		50*time.Millisecond, // WriteTimeout
		50*time.Millisecond, // PushPeriod: short, so entries.reset() runs often in the healthy case
		config.DefaultHTTPClientConfig,
		"", "",
		false,
		backoffCfg,
		log.NewNopLogger(),
		NewMetrics(nil),
	)
	require.NoError(b, err)

	lbls := labels.FromStrings("cluster", "prod-us-central-0", "namespace", "loki-ops", "service_name", "querier")
	pattern := PatternEntry(time.Now(), 42, `ts=<_> level=info caller=<_> msg="<_>" duration=<_>`, lbls)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second / time.Duration(rate))
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				push.WriteEntry(time.Now(), pattern, lbls, nil)
			case <-stop:
				return
			}
		}
	}()

	time.Sleep(window)
	close(stop)
	wg.Wait()

	queued, queuedBytes := push.queuedEntriesForTest()

	// Let the backend "recover" so the in-flight (infinite) retry episode can
	// succeed and Stop() doesn't hang forever -- Push.Stop only closes p.quit,
	// which the outer select loop observes, but a backoff episode in progress
	// is not tied to that channel at all (each one is a fresh
	// context.Background()), so nothing but a successful send (or the process
	// dying) ends it.
	failing.Store(false)
	stopWithTimeout(b, push, 5*time.Second)

	b.ReportMetric(float64(queued), "queued_entries")
	b.ReportMetric(float64(queuedBytes), "queued_B")
	b.ReportMetric(float64(queued)/window.Seconds(), "queued_entries/outage_s")
}

// queuedEntriesForTest reports how many entries are currently sitting in the
// queue and an estimate of the bytes they hold, without draining them.
func (p *Push) queuedEntriesForTest() (count, bytes int) {
	p.entries.lock.Lock()
	defer p.entries.lock.Unlock()

	for _, e := range p.entries.entries {
		bytes += len(e.entry) + len(e.labels.String())
		for _, la := range e.structuredMetadata {
			bytes += len(la.Name) + len(la.Value)
		}
	}
	return len(p.entries.entries), bytes
}

func stopWithTimeout(tb testing.TB, p *Push, timeout time.Duration) {
	tb.Helper()

	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		tb.Fatal("Push.Stop() did not return in time: the run() goroutine is stuck inside an in-flight retry episode, which is not cancellable")
	}
}
