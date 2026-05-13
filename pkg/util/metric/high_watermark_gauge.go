package metric

import (
	"sync/atomic" //lint:ignore faillint we use new atomic types from sync/atomic.

	"github.com/prometheus/client_golang/prometheus"
)

// A HighWatermarkGauge is a lock-free Prometheus collector that reports the
// maximum value observed since the last scrape. It is updated on Add/Inc;
// Sub/Dec cannot produce a new maximum.
type HighWatermarkGauge struct {
	val, maxVal atomic.Int64
	desc        *prometheus.Desc
}

// NewHighWatermarkGauge returns a new HighWatermarkGauge for the metric name
// and help.
func NewHighWatermarkGauge(fqName, help string) *HighWatermarkGauge {
	return &HighWatermarkGauge{desc: prometheus.NewDesc(fqName, help, nil, nil)}
}

// Add adds the delta to the current value.
func (c *HighWatermarkGauge) Add(delta int64) {
	c.updateMaxVal(c.val.Add(delta))
}

// Sub subtracts the delta from the current value.
func (c *HighWatermarkGauge) Sub(delta int64) {
	c.val.Add(-delta)
}

// Inc increments the counter.
func (c *HighWatermarkGauge) Inc() {
	c.updateMaxVal(c.val.Add(1))
}

// Dec decrements the counter.
func (c *HighWatermarkGauge) Dec() {
	c.val.Add(-1)
}

// Describe implements [prometheus.Collector].
func (c *HighWatermarkGauge) Describe(descs chan<- *prometheus.Desc) {
	descs <- c.desc
}

// Collect implements [prometheus.Collector]. It reports the peak observed
// since the last scrape and resets the high-water mark to the current value.
func (c *HighWatermarkGauge) Collect(metrics chan<- prometheus.Metric) {
	maxVal := c.maxVal.Swap(0)
	c.updateMaxVal(c.val.Load())
	metrics <- prometheus.MustNewConstMetric(
		c.desc,
		prometheus.GaugeValue,
		float64(maxVal),
	)
}

// updateMaxVal bumps maxVal up to newVal if newVal is larger.
func (c *HighWatermarkGauge) updateMaxVal(newVal int64) {
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
