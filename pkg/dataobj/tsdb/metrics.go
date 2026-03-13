package tsdb

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var processingDelayDesc = prometheus.NewDesc(
	"loki_index_builder_latest_processing_delay_seconds",
	"Latest time difference between record timestamp and processing time in seconds",
	[]string{"partition"},
	nil,
)

// processingDelayCollector implements prometheus.Collector to dynamically report
// processing delay only for active partitions, preventing cardinality explosion.
type processingDelayCollector struct {
	mtx    sync.RWMutex
	delays map[int32]float64 // partition -> delay in seconds
}

func newProcessingDelayCollector() *processingDelayCollector {
	return &processingDelayCollector{
		delays: make(map[int32]float64),
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
	for partition, delay := range c.delays {
		metrics <- prometheus.MustNewConstMetric(
			processingDelayDesc,
			prometheus.GaugeValue,
			delay,
			strconv.Itoa(int(partition)),
		)
	}
}

func (c *processingDelayCollector) set(partition int32, delay float64) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.delays[partition] = delay
}

func (c *processingDelayCollector) delete(partition int32) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	delete(c.delays, partition)
}

type builderMetrics struct {
	// Error counters
	commitFailures prometheus.Counter

	// Request counters
	commitsTotal prometheus.Counter

	// Processing delay metrics
	processingDelay *processingDelayCollector

	// Build time metrics
	buildTimeSeconds prometheus.Gauge

	// Request counters
	totalBuilds prometheus.Counter
}

func newBuilderMetrics() *builderMetrics {
	p := &builderMetrics{
		commitFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_tsdb_builder_commit_failures_total",
			Help: "Total number of commit failures",
		}),
		commitsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_tsdb_builder_commits_total",
			Help: "Total number of commits",
		}),
		totalBuilds: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_tsdb_builder_builds_total",
			Help: "Total number of tsdb builds completed",
		}),
		buildTimeSeconds: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_tsdb_builder_build_time_seconds",
			Help: "Time spent on the last tsdb build in seconds",
		}),
		processingDelay: newProcessingDelayCollector(),
	}

	return p
}

func (p *builderMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.commitFailures,
		p.commitsTotal,
		p.totalBuilds,
		p.buildTimeSeconds,
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
		p.totalBuilds,
		p.buildTimeSeconds,
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

func (p *builderMetrics) setProcessingDelay(partition int32, recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() {
		p.processingDelay.set(partition, time.Since(recordTimestamp).Seconds())
	}
}

func (p *builderMetrics) deletePartitionMetrics(partition int32) {
	p.processingDelay.delete(partition)
}
