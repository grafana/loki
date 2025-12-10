package consumer

import (
	"time"

	"go.uber.org/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

type partitionOffsetMetrics struct {
	currentOffset prometheus.GaugeFunc
	lastOffset    atomic.Int64

	// Error counters

	appendFailures prometheus.Counter

	// Request counters

	appendsTotal prometheus.Counter

	latestDelay      prometheus.Gauge // Latest delta between record timestamp and current time
	processedRecords prometheus.Counter
}

func newPartitionOffsetMetrics() *partitionOffsetMetrics {
	p := &partitionOffsetMetrics{

		appendFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_append_failures_total",
			Help: "Total number of append failures",
		}),

		appendsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_appends_total",
			Help: "Total number of appends",
		}),
		latestDelay: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_latest_processing_delay_seconds",
			Help: "Latest time difference bweteen record timestamp and processing time in seconds",
		}),
		processedRecords: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_processed_records_total",
			Help: "Total number of records processed.",
		}),
	}

	p.currentOffset = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_current_offset",
			Help: "The last consumed offset for this partition",
		},
		p.getCurrentOffset,
	)

	return p
}

func (p *partitionOffsetMetrics) getCurrentOffset() float64 {
	return float64(p.lastOffset.Load())
}

func (p *partitionOffsetMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.appendFailures,
		p.appendsTotal,
		p.latestDelay,
		p.processedRecords,
		p.currentOffset,
	}

	for _, collector := range collectors {
		if err := reg.Register(collector); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				return err
			}
		}
	}
	return nil
}

func (p *partitionOffsetMetrics) unregister(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		p.appendFailures,
		p.appendsTotal,
		p.latestDelay,
		p.processedRecords,
		p.currentOffset,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}

func (p *partitionOffsetMetrics) updateOffset(offset int64) {
	p.lastOffset.Store(offset)
}

func (p *partitionOffsetMetrics) incAppendFailures() {
	p.appendFailures.Inc()
}

func (p *partitionOffsetMetrics) incAppendsTotal() {
	p.appendsTotal.Inc()
}

func (p *partitionOffsetMetrics) observeProcessingDelay(recordTimestamp time.Time) {
	// Convert milliseconds to seconds and calculate delay
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		delay := time.Since(recordTimestamp).Seconds()

		p.latestDelay.Set(delay)
	}
}
