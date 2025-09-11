package index

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type indexBuilderMetrics struct {
	// Error counters
	commitFailures prometheus.Counter

	// Request counters
	commitsTotal prometheus.Counter

	// Processing delay metrics
	processingDelay prometheus.Gauge // Latest delta between record timestamp and current time
}

func newIndexBuilderMetrics() *indexBuilderMetrics {
	p := &indexBuilderMetrics{
		commitFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_commit_failures_total",
			Help: "Total number of commit failures",
		}),
		commitsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_commits_total",
			Help: "Total number of commits",
		}),
		processingDelay: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_index_builder_latest_processing_delay_seconds",
			Help: "Latest time difference between record timestamp and processing time in seconds",
		}),
	}

	return p
}

func (p *indexBuilderMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.commitFailures,
		p.commitsTotal,
		p.processingDelay,
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

func (p *indexBuilderMetrics) unregister(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		p.commitFailures,
		p.commitsTotal,
		p.processingDelay,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}

func (p *indexBuilderMetrics) incCommitFailures() {
	p.commitFailures.Inc()
}

func (p *indexBuilderMetrics) incCommitsTotal() {
	p.commitsTotal.Inc()
}

func (p *indexBuilderMetrics) setProcessingDelay(recordTimestamp time.Time) {
	// Convert milliseconds to seconds and calculate delay
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.processingDelay.Set(time.Since(recordTimestamp).Seconds())
	}
}
