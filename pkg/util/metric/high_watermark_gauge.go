package metric

import (
	"sync/atomic" //lint:ignore faillint we use new atomic types from sync/atomic.
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// A HighWatermarkGauge is a lock-free Prometheus collector that reports the
// maximum value observed in the last 2 minutes. It is updated on Add/Inc;
// Sub/Dec cannot produce a new maximum.
type HighWatermarkGauge struct {
	val, maxVal, lastResetSecs atomic.Int64
	resetIntervalSecs          int64
	desc                       *prometheus.Desc
}

// NewHighWatermarkGauge returns a new HighWatermarkGauge for the metric name
// and help.
func NewHighWatermarkGauge(fqName, help string) *HighWatermarkGauge {
	g := HighWatermarkGauge{
		resetIntervalSecs: 120,
		desc:              prometheus.NewDesc(fqName, help, nil, nil),
	}
	_ = g.lastResetSecs.Swap(time.Now().Unix())
	return &g
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
// in the last 2 minutes.
func (c *HighWatermarkGauge) Collect(metrics chan<- prometheus.Metric) {
	c.resetStaleMaxVal()
	metrics <- prometheus.MustNewConstMetric(
		c.desc,
		prometheus.GaugeValue,
		float64(c.maxVal.Load()),
	)
}

// resetStaleMaxVal resets max val if its stale.
func (c *HighWatermarkGauge) resetStaleMaxVal() {
	for {
		lastResetSecs := c.lastResetSecs.Load()
		nowSecs := time.Now().Unix()
		if nowSecs-lastResetSecs < c.resetIntervalSecs {
			break
		}
		if c.lastResetSecs.CompareAndSwap(lastResetSecs, nowSecs) {
			c.maxVal.Swap(0)
			c.updateMaxVal(c.val.Load())
			break
		}
	}
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
