package worker

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// collector implements [prometheus.Collector], collecting metrics for a
// worker.
type collector struct {
	threadsMut     sync.RWMutex
	collectThreads []*thread

	threads *prometheus.Desc
}

var _ prometheus.Collector = (*collector)(nil)

// newCollector returns a new collector. The collector will report that the
// worker has no threads until calling setThreads.
func newCollector() *collector {
	return &collector{
		threads: prometheus.NewDesc(
			"loki_engine_worker_threads",
			"Number of worker threads by state",
			[]string{"state"},
			nil,
		),
	}
}

// setThreads sets the threads to be collected.
func (c *collector) setThreads(threads []*thread) {
	c.threadsMut.Lock()
	defer c.threadsMut.Unlock()
	c.collectThreads = threads
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	threadsByState := map[threadState]int{
		threadStateIdle:  0,
		threadStateReady: 0,
		threadStateBusy:  0,
	}

	c.threadsMut.RLock()
	defer c.threadsMut.RUnlock()

	for _, thread := range c.collectThreads {
		threadsByState[thread.State()]++
	}

	for state, count := range threadsByState {
		ch <- prometheus.MustNewConstMetric(c.threads, prometheus.GaugeValue, float64(count), state.String())
	}
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.threads
}
