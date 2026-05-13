package metric

import (
	"sync/atomic" //lint:ignore faillint we use new atomic types from sync/atomic.

	"github.com/prometheus/client_golang/prometheus"
)

// MaxSampleCollector is a lock-free Prometheus collector that reports the
// maximum value observed since the last scrape. It is updated on Add/Inc;
// Sub/Dec cannot produce a new maximum.
type MaxSampleCollector struct {
	val, maxVal atomic.Int64
	desc        *prometheus.Desc
}

// NewMaxSampleCollector returns a new MaxSampleCollector for the metric name
// and help.
func NewMaxSampleCollector(fqName, help string) *MaxSampleCollector {
	return &MaxSampleCollector{desc: prometheus.NewDesc(fqName, help, nil, nil)}
}

// Add adds the delta to the current value.
func (c *MaxSampleCollector) Add(delta int64) {
	c.updateMaxVal(c.val.Add(delta))
}

// Sub subtracts the delta from the current value.
func (c *MaxSampleCollector) Sub(delta int64) {
	c.val.Add(-delta)
}

// Inc increments the counter.
func (c *MaxSampleCollector) Inc() {
	c.updateMaxVal(c.val.Add(1))
}

// Dec decrements the counter.
func (c *MaxSampleCollector) Dec() {
	c.val.Add(-1)
}

// Describe implements [prometheus.Collector].
func (c *MaxSampleCollector) Describe(descs chan<- *prometheus.Desc) {
	descs <- c.desc
}

// Collect implements [prometheus.Collector]. It reports the peak observed
// since the last scrape and resets the high-water mark to the current value.
func (c *MaxSampleCollector) Collect(metrics chan<- prometheus.Metric) {
	maxVal := c.maxVal.Swap(0)
	c.updateMaxVal(c.val.Load())
	metrics <- prometheus.MustNewConstMetric(
		c.desc,
		prometheus.GaugeValue,
		float64(maxVal),
	)
}

// updateMaxVal bumps maxVal up to newVal if newVal is larger.
func (c *MaxSampleCollector) updateMaxVal(newVal int64) {
	for {
		cur := c.maxVal.Load()
		if newVal <= cur {
			return
		}
		if c.maxVal.CompareAndSwap(cur, newVal) {
			return
		}
	}
}
