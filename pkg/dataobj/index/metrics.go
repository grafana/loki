package index

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type builderMetrics struct {
	// Error counters
	commitFailures prometheus.Counter

	// Request counters
	commitsTotal prometheus.Counter

	// Processing delay metrics
	processingDelay prometheus.Gauge // Latest delta between record timestamp and current time
}

func newBuilderMetrics() *builderMetrics {
	p := &builderMetrics{
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

func (p *builderMetrics) register(reg prometheus.Registerer) error {
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

func (p *builderMetrics) unregister(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		p.commitFailures,
		p.commitsTotal,
		p.processingDelay,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}

func (p *builderMetrics) incCommitFailures() {
	p.commitFailures.Inc()
}

func (p *builderMetrics) incCommitsTotal() {
	p.commitsTotal.Inc()
}

func (p *builderMetrics) setProcessingDelay(recordTimestamp time.Time) {
	// Convert milliseconds to seconds and calculate delay
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.processingDelay.Set(time.Since(recordTimestamp).Seconds())
	}
}

type indexerMetrics struct {
	// Request counters
	totalRequests prometheus.Counter
	totalBuilds   prometheus.Counter

	// Build time metrics
	buildTimeSeconds prometheus.Gauge

	// Queue metrics
	queueDepth prometheus.Gauge
}

func newIndexerMetrics() *indexerMetrics {
	m := &indexerMetrics{
		totalRequests: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_requests_total",
			Help: "Total number of build requests submitted to the indexer",
		}),
		totalBuilds: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_builds_total",
			Help: "Total number of index builds completed",
		}),
		buildTimeSeconds: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_index_builder_build_time_seconds",
			Help: "Time spent on the last index build in seconds",
		}),
		queueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_index_builder_queue_depth",
			Help: "Current depth of the build request queue",
		}),
	}

	return m
}

func (m *indexerMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		m.totalRequests,
		m.totalBuilds,
		m.buildTimeSeconds,
		m.queueDepth,
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

func (m *indexerMetrics) unregister(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		m.totalRequests,
		m.totalBuilds,
		m.buildTimeSeconds,
		m.queueDepth,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}

func (m *indexerMetrics) incRequests() {
	m.totalRequests.Inc()
}

func (m *indexerMetrics) incBuilds() {
	m.totalBuilds.Inc()
}

func (m *indexerMetrics) setBuildTime(duration time.Duration) {
	m.buildTimeSeconds.Set(duration.Seconds())
}

func (m *indexerMetrics) setQueueDepth(depth int) {
	m.queueDepth.Set(float64(depth))
}
