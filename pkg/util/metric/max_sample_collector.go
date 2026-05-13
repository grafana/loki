package metric

import (
	"context"
	"sync/atomic" //lint:ignore faillint we use new atomic types from sync/atomic.
	"time"

	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
)

// MaxSampleCollector is a special kind of prometheus collector that collects
// the max sample in the last 1 minute. The sample period is 1 second. It is
// lock-free.
type MaxSampleCollector struct {
	*services.BasicService
	// samples and secs are exclusive to [iterFunc], which is called from the
	// timer service in a loop, so no mutex is required.
	samples     [60]int64
	secs        int
	val, maxVal atomic.Int64
	desc        *prometheus.Desc
}

// NewMaxSampleCollector returns a new MaxSampleCollector for the metric name
// and help.
func NewMaxSampleCollector(fqName, help string) *MaxSampleCollector {
	c := &MaxSampleCollector{desc: prometheus.NewDesc(fqName, help, nil, nil)}
	c.BasicService = services.NewTimerService(time.Second, nil, c.iterFunc, nil)
	return c
}

// Add adds the delta to the current value.
func (c *MaxSampleCollector) Add(delta int64) {
	c.val.Add(delta)
}

// Sub subtracts the delta to the current value.
func (c *MaxSampleCollector) Sub(delta int64) {
	c.val.Add(-delta)
}

// Inc increments the counter.
func (c *MaxSampleCollector) Inc() {
	c.val.Add(1)
}

// Dec decrements the counter.
func (c *MaxSampleCollector) Dec() {
	c.val.Add(-1)
}

// Describe implements [prometheus.Collector].
func (c *MaxSampleCollector) Describe(descs chan<- *prometheus.Desc) {
	descs <- c.desc
}

// Collect implements [prometheus.Collector].
func (c *MaxSampleCollector) Collect(metrics chan<- prometheus.Metric) {
	metrics <- prometheus.MustNewConstMetric(
		c.desc,
		prometheus.GaugeValue,
		float64(c.maxVal.Load()),
	)
}

// iterFunc implements [services.OneIteration].
func (c *MaxSampleCollector) iterFunc(_ context.Context) error {
	c.samples[c.secs] = c.val.Load()
	c.secs = (c.secs + 1) % 60
	var maxVal int64
	for _, val := range c.samples {
		if val > maxVal {
			maxVal = val
		}
	}
	c.maxVal.Store(maxVal)
	return nil
}
