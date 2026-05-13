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
func (g *HighWatermarkGauge) Add(delta int64) {
	g.updateMaxVal(g.val.Add(delta))
}

// Sub subtracts the delta from the current value.
func (g *HighWatermarkGauge) Sub(delta int64) {
	g.val.Add(-delta)
}

// Inc increments the counter.
func (g *HighWatermarkGauge) Inc() {
	g.updateMaxVal(g.val.Add(1))
}

// Dec decrements the counter.
func (g *HighWatermarkGauge) Dec() {
	g.val.Add(-1)
}

// Describe implements [prometheus.Collector].
func (g *HighWatermarkGauge) Describe(descs chan<- *prometheus.Desc) {
	descs <- g.desc
}

// Collect implements [prometheus.Collector]. It reports the peak observed
// in the last 2 minutes.
func (g *HighWatermarkGauge) Collect(metrics chan<- prometheus.Metric) {
	g.resetStaleMaxVal()
	metrics <- prometheus.MustNewConstMetric(
		g.desc,
		prometheus.GaugeValue,
		float64(g.maxVal.Load()),
	)
}

// resetStaleMaxVal resets max val if its stale.
func (g *HighWatermarkGauge) resetStaleMaxVal() {
	for {
		lastResetSecs := g.lastResetSecs.Load()
		nowSecs := time.Now().Unix()
		if nowSecs-lastResetSecs < g.resetIntervalSecs {
			break
		}
		if g.lastResetSecs.CompareAndSwap(lastResetSecs, nowSecs) {
			g.maxVal.Swap(0)
			g.updateMaxVal(g.val.Load())
			break
		}
	}
}

// updateMaxVal bumps maxVal up to newVal if newVal is larger.
func (g *HighWatermarkGauge) updateMaxVal(newVal int64) {
	for {
		cur := g.maxVal.Load()
		if newVal <= cur {
			return
		}
		if g.maxVal.CompareAndSwap(cur, newVal) {
			return
		}
	}
}
