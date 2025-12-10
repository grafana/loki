package consumer

import (
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type partitionOffsetMetrics struct {
	currentOffset         prometheus.Gauge
	consumptionLag        prometheus.Gauge
	consumptionLagSeconds prometheus.Gauge

	// Error counters
	commitFailures prometheus.Counter
	appendFailures prometheus.Counter

	// Request counters
	commitsTotal prometheus.Counter
	appendsTotal prometheus.Counter

	// Replaced with consumption lag metric. Remove once rolled out.
	latestDelay      prometheus.Gauge // Latest delta between record timestamp and current time
	processedRecords prometheus.Counter
}

func newPartitionOffsetMetrics() *partitionOffsetMetrics {
	p := &partitionOffsetMetrics{
		currentOffset: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_current_offset",
			Help: "The last consumed offset.",
		}),
		consumptionLag: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_consumption_lag",
			Help: "The consumption lag in offsets.",
		}),
		consumptionLagSeconds: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_consumption_lag_seconds",
			Help: "The consumption lag in seconds.",
		}),
		commitFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commit_failures_total",
			Help: "Total number of commit failures",
		}),
		appendFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_append_failures_total",
			Help: "Total number of append failures",
		}),
		commitsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commits_total",
			Help: "Total number of commits",
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
	return p
}

func (p *partitionOffsetMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.consumptionLag,
		p.consumptionLagSeconds,
		p.commitFailures,
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
		p.consumptionLag,
		p.consumptionLagSeconds,
		p.commitFailures,
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

func (p *partitionOffsetMetrics) incCommitFailures() {
	p.commitFailures.Inc()
}

func (p *partitionOffsetMetrics) incAppendFailures() {
	p.appendFailures.Inc()
}

func (p *partitionOffsetMetrics) incAppendsTotal() {
	p.appendsTotal.Inc()
}

func (p *partitionOffsetMetrics) incCommitsTotal() {
	p.commitsTotal.Inc()
}

func (p *partitionOffsetMetrics) observeConsumptionLag(lastProducedOffset, offset int64) {
	p.consumptionLag.Set(math.Abs(float64(lastProducedOffset - offset)))
}

func (p *partitionOffsetMetrics) observeConsumptionLagSeconds(t time.Time) {
	if !t.IsZero() {
		secs := time.Since(t).Seconds()
		p.consumptionLagSeconds.Set(secs)
		p.latestDelay.Set(secs)
	}
}
