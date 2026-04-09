package index

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	processingDelayDesc = prometheus.NewDesc(
		"loki_index_builder_latest_processing_delay_seconds",
		"Latest time difference between record timestamp and processing time in seconds",
		[]string{"partition"},
		nil,
	)
)

// processingDelayCollector implements prometheus.Collector to dynamically report
// processing delay only for active partitions, preventing cardinality explosion.
type processingDelayCollector struct {
	mtx        sync.RWMutex
	timestamps map[int32]time.Time // partition -> reference time; zero time means idle (emits 0)
}

func newProcessingDelayCollector() *processingDelayCollector {
	return &processingDelayCollector{
		timestamps: make(map[int32]time.Time),
	}
}

// Describe implements prometheus.Collector.
func (c *processingDelayCollector) Describe(descs chan<- *prometheus.Desc) {
	descs <- processingDelayDesc
}

// Collect implements prometheus.Collector.
func (c *processingDelayCollector) Collect(metrics chan<- prometheus.Metric) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	for partition, t := range c.timestamps {
		var delay float64
		if !t.IsZero() {
			delay = time.Since(t).Seconds()
		}
		metrics <- prometheus.MustNewConstMetric(
			processingDelayDesc,
			prometheus.GaugeValue,
			delay,
			strconv.Itoa(int(partition)),
		)
	}
}

func (c *processingDelayCollector) set(partition int32, t time.Time) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.timestamps[partition] = t
}

func (c *processingDelayCollector) delete(partition int32) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	delete(c.timestamps, partition)
}

type builderMetrics struct {
	// Error counters
	commitFailures prometheus.Counter

	// Request counters
	commitsTotal prometheus.Counter

	// Processing delay metrics
	processingDelay *processingDelayCollector

	// Partition rebalance counters
	partitionsAssigned prometheus.Counter
	partitionsRevoked  prometheus.Counter
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
		processingDelay: newProcessingDelayCollector(),
		partitionsAssigned: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_partition_assignments_total",
			Help: "Total number of partitions assigned",
		}),
		partitionsRevoked: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_partition_revocations_total",
			Help: "Total number of partitions revoked or lost",
		}),
	}

	return p
}

func (p *builderMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.commitFailures,
		p.commitsTotal,
		p.processingDelay,
		p.partitionsAssigned,
		p.partitionsRevoked,
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
		p.partitionsAssigned,
		p.partitionsRevoked,
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

func (p *builderMetrics) incPartitionsAssigned(n int) {
	p.partitionsAssigned.Add(float64(n))
}

func (p *builderMetrics) incPartitionsRevoked(n int) {
	p.partitionsRevoked.Add(float64(n))
}

func (p *builderMetrics) setProcessingDelay(partition int32, recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() {
		p.processingDelay.set(partition, recordTimestamp)
	}
}

func (p *builderMetrics) resetProcessingDelay(partition int32) {
	p.processingDelay.set(partition, time.Time{})
}

func (p *builderMetrics) deletePartitionMetrics(partition int32) {
	p.processingDelay.delete(partition)
}

type indexerMetrics struct {
	// Request counters
	totalRequests prometheus.Counter
	totalBuilds   prometheus.Counter

	// Build time metrics
	buildTimeSeconds prometheus.Gauge

	// Queue metrics
	queueDepth prometheus.Gauge

	// End-to-end processing time metric
	endToEndProcessingTime prometheus.Gauge
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
		endToEndProcessingTime: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingest_end_to_end_processing_time_seconds",
			Help: "Time between a log line being written to kafka by the distributors and the index-builder making it available for querying in seconds",
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
		m.endToEndProcessingTime,
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
		m.endToEndProcessingTime,
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

func (m *indexerMetrics) setEndToEndProcessingTime(duration time.Duration) {
	m.endToEndProcessingTime.Set(duration.Seconds())
}
