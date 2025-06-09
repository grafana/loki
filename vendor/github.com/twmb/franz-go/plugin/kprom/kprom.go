// Package kprom provides prometheus plug-in metrics for a kgo client.
//
// This package tracks the following metrics under the following names,
// all metrics being counter vecs:
//
//	#{ns}_connects_total{node_id="#{node}"}
//	#{ns}_connect_errors_total{node_id="#{node}"}
//	#{ns}_write_errors_total{node_id="#{node}"}
//	#{ns}_write_bytes_total{node_id="#{node}"}
//	#{ns}_read_errors_total{node_id="#{node}"}
//	#{ns}_read_bytes_total{node_id="#{node}"}
//	#{ns}_produce_bytes_total{node_id="#{node}",topic="#{topic}"}
//	#{ns}_fetch_bytes_total{node_id="#{node}",topic="#{topic}"}
//	#{ns}_buffered_produce_records_total
//	#{ns}_buffered_fetch_records_total
//
// The above metrics can be expanded considerably with options in this package,
// allowing timings, uncompressed and compressed bytes, and different labels.
//
// This can be used in a client like so:
//
//	m := kprom.NewMetrics("my_namespace")
//	cl, err := kgo.NewClient(
//	        kgo.WithHooks(m),
//	        // ...other opts
//	)
//
// More examples are linked in the main project readme: https://github.com/twmb/franz-go/#metrics--logging
//
// By default, metrics are installed under the a new prometheus registry, but
// this can be overridden with the Registry option.
//
// Note that seed brokers use broker IDs prefixed with "seed_", with the number
// corresponding to which seed it is.
package kprom

import (
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/twmb/franz-go/pkg/kgo"
)

var ( // interface checks to ensure we implement the hooks properly
	_ kgo.HookBrokerConnect       = new(Metrics)
	_ kgo.HookBrokerDisconnect    = new(Metrics)
	_ kgo.HookBrokerWrite         = new(Metrics)
	_ kgo.HookBrokerRead          = new(Metrics)
	_ kgo.HookProduceBatchWritten = new(Metrics)
	_ kgo.HookFetchBatchRead      = new(Metrics)
	_ kgo.HookBrokerE2E           = new(Metrics)
	_ kgo.HookBrokerThrottle      = new(Metrics)
	_ kgo.HookNewClient           = new(Metrics)
	_ kgo.HookClientClosed        = new(Metrics)
)

// Metrics provides prometheus metrics
type Metrics struct {
	cfg cfg

	// Connection
	connConnectsTotal      *prometheus.CounterVec
	connConnectErrorsTotal *prometheus.CounterVec
	connDisconnectsTotal   *prometheus.CounterVec

	// Write
	writeBytesTotal  *prometheus.CounterVec
	writeErrorsTotal *prometheus.CounterVec
	writeWaitSeconds *prometheus.HistogramVec
	writeTimeSeconds *prometheus.HistogramVec

	// Read
	readBytesTotal  *prometheus.CounterVec
	readErrorsTotal *prometheus.CounterVec
	readWaitSeconds *prometheus.HistogramVec
	readTimeSeconds *prometheus.HistogramVec

	// Request E2E & Throttle
	requestDurationE2ESeconds *prometheus.HistogramVec
	requestThrottledSeconds   *prometheus.HistogramVec

	// Produce
	produceCompressedBytes   *prometheus.CounterVec
	produceUncompressedBytes *prometheus.CounterVec
	produceBatchesTotal      *prometheus.CounterVec
	produceRecordsTotal      *prometheus.CounterVec

	// Fetch
	fetchCompressedBytes   *prometheus.CounterVec
	fetchUncompressedBytes *prometheus.CounterVec
	fetchBatchesTotal      *prometheus.CounterVec
	fetchRecordsTotal      *prometheus.CounterVec

	// Buffered
	bufferedFetchRecords   prometheus.GaugeFunc
	bufferedProduceRecords prometheus.GaugeFunc

	// Holds references to all metric collectors
	allMetricCollectors []prometheus.Collector
}

// NewMetrics returns a new Metrics that adds prometheus metrics to the
// registry under the given namespace.
func NewMetrics(namespace string, opts ...Opt) *Metrics {
	return &Metrics{cfg: newCfg(namespace, opts...)}
}

// Registry returns the prometheus registry that metrics were added to.
//
// This is useful if you want the Metrics type to create its own registry for
// you to add additional metrics to.
func (m *Metrics) Registry() prometheus.Registerer {
	return m.cfg.reg
}

// Handler returns an http.Handler providing prometheus metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.cfg.gatherer, m.cfg.handlerOpts)
}

// OnNewClient implements the HookNewClient interface for metrics
// gathering.
// This method is meant to be called by the hook system and not by the user
func (m *Metrics) OnNewClient(client *kgo.Client) {
	var (
		factory     = promauto.With(m.cfg.reg)
		namespace   = m.cfg.namespace
		subsystem   = m.cfg.subsystem
		constLabels = m.cfg.withConstLabels
	)
	if m.cfg.withClientLabel {
		if constLabels == nil {
			constLabels = make(prometheus.Labels)
		}
		constLabels["client_id"] = client.OptValue(kgo.ClientID).(string)
	}

	// returns Hist buckets if set, otherwise defBucket
	getHistogramBuckets := func(h Histogram) []float64 {
		if buckets, ok := m.cfg.histograms[h]; ok && len(buckets) != 0 {
			return buckets
		}
		return m.cfg.defBuckets
	}

	// Connection

	m.connConnectsTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "connects_total",
		Help:        "Total number of connections opened",
	}, []string{"node_id"})

	m.connConnectErrorsTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "connect_errors_total",
		Help:        "Total number of connection errors",
	}, []string{"node_id"})

	m.connDisconnectsTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "disconnects_total",
		Help:        "Total number of connections closed",
	}, []string{"node_id"})

	// Write

	m.writeBytesTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "write_bytes_total",
		Help:        "Total number of bytes written to the TCP connection. The bytes count is tracked after compression (when used).",
	}, []string{"node_id"})

	m.writeErrorsTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "write_errors_total",
		Help:        "Total number of write errors",
	}, []string{"node_id"})

	m.writeWaitSeconds = factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "write_wait_seconds",
		Help:        "Time spent waiting to write to Kafka",
		Buckets:     getHistogramBuckets(WriteWait),
	}, []string{"node_id"})

	m.writeTimeSeconds = factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "write_time_seconds",
		Help:        "Time spent writing to Kafka",
		Buckets:     getHistogramBuckets(WriteTime),
	}, []string{"node_id"})

	// Read

	m.readBytesTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "read_bytes_total",
		Help:        "Total number of bytes read from the TCP connection. The bytes count is tracked before uncompression (when used).",
	}, []string{"node_id"})

	m.readErrorsTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "read_errors_total",
		Help:        "Total number of read errors",
	}, []string{"node_id"})

	m.readWaitSeconds = factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "read_wait_seconds",
		Help:        "Time spent waiting to read from Kafka",
		Buckets:     getHistogramBuckets(ReadWait),
	}, []string{"node_id"})

	m.readTimeSeconds = factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "read_time_seconds",
		Help:        "Time spent reading from Kafka",
		Buckets:     getHistogramBuckets(ReadTime),
	}, []string{"node_id"})

	// Request E2E duration & Throttle

	m.requestDurationE2ESeconds = factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "request_duration_e2e_seconds",
		Help:        "Time from the start of when a request is written to the end of when the response for that request was fully read",
		Buckets:     getHistogramBuckets(RequestDurationE2E),
	}, []string{"node_id"})

	m.requestThrottledSeconds = factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "request_throttled_seconds",
		Help:        "Time the request was throttled",
		Buckets:     getHistogramBuckets(RequestThrottled),
	}, []string{"node_id"})

	// Produce

	m.produceCompressedBytes = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "produce_compressed_bytes_total",
		Help:        "Total number of compressed bytes produced",
	}, m.cfg.fetchProduceOpts.labels)

	produceUncompressedBytesName := "produce_bytes_total"
	if m.cfg.fetchProduceOpts.consistentNaming {
		produceUncompressedBytesName = "produce_uncompressed_bytes_total"
	}
	m.produceUncompressedBytes = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        produceUncompressedBytesName,
		Help:        "Total number of uncompressed bytes produced",
	}, m.cfg.fetchProduceOpts.labels)

	m.produceBatchesTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "produce_batches_total",
		Help:        "Total number of batches produced",
	}, m.cfg.fetchProduceOpts.labels)

	m.produceRecordsTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "produce_records_total",
		Help:        "Total number of records produced",
	}, m.cfg.fetchProduceOpts.labels)

	// Fetch

	m.fetchCompressedBytes = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "fetch_compressed_bytes_total",
		Help:        "Total number of compressed bytes fetched",
	}, m.cfg.fetchProduceOpts.labels)

	fetchUncompressedBytesName := "fetch_bytes_total"
	if m.cfg.fetchProduceOpts.consistentNaming {
		fetchUncompressedBytesName = "fetch_uncompressed_bytes_total"
	}
	m.fetchUncompressedBytes = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        fetchUncompressedBytesName,
		Help:        "Total number of uncompressed bytes fetched",
	}, m.cfg.fetchProduceOpts.labels)

	m.fetchBatchesTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "fetch_batches_total",
		Help:        "Total number of batches fetched",
	}, m.cfg.fetchProduceOpts.labels)

	m.fetchRecordsTotal = factory.NewCounterVec(prometheus.CounterOpts{
		Namespace:   namespace,
		Subsystem:   subsystem,
		ConstLabels: constLabels,
		Name:        "fetch_records_total",
		Help:        "Total number of records fetched",
	}, m.cfg.fetchProduceOpts.labels)

	// Buffers

	m.bufferedProduceRecords = factory.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   subsystem,
			ConstLabels: constLabels,
			Name:        "buffered_produce_records_total",
			Help:        "Total number of records buffered within the client ready to be produced",
		},
		func() float64 { return float64(client.BufferedProduceRecords()) },
	)

	m.bufferedFetchRecords = factory.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   subsystem,
			ConstLabels: constLabels,
			Name:        "buffered_fetch_records_total",
			Help:        "Total number of records buffered within the client ready to be consumed",
		},
		func() float64 { return float64(client.BufferedFetchRecords()) },
	)

	m.allMetricCollectors = append(m.allMetricCollectors,
		m.connConnectsTotal,
		m.connConnectErrorsTotal,
		m.connDisconnectsTotal,
		m.writeBytesTotal,
		m.writeErrorsTotal,
		m.writeWaitSeconds,
		m.writeTimeSeconds,
		m.readBytesTotal,
		m.readErrorsTotal,
		m.readWaitSeconds,
		m.readTimeSeconds,
		m.requestDurationE2ESeconds,
		m.requestThrottledSeconds,
		m.produceCompressedBytes,
		m.produceUncompressedBytes,
		m.produceBatchesTotal,
		m.produceRecordsTotal,
		m.fetchCompressedBytes,
		m.fetchUncompressedBytes,
		m.fetchBatchesTotal,
		m.fetchRecordsTotal,
		m.bufferedFetchRecords,
		m.bufferedProduceRecords,
	)
}

// OnClientClosed will unregister kprom metrics from kprom registerer
func (m *Metrics) OnClientClosed(*kgo.Client) {
	if m.cfg.reg != nil {
		for _, c := range m.allMetricCollectors {
			m.cfg.reg.Unregister(c)
		}
	}
}

// OnBrokerConnect implements the HookBrokerConnect interface for metrics
// gathering.
// This method is meant to be called by the hook system and not by the user
func (m *Metrics) OnBrokerConnect(meta kgo.BrokerMetadata, _ time.Duration, _ net.Conn, err error) {
	nodeId := kgo.NodeName(meta.NodeID)
	if err != nil {
		m.connConnectErrorsTotal.WithLabelValues(nodeId).Inc()
		return
	}
	m.connConnectsTotal.WithLabelValues(nodeId).Inc()
}

// OnBrokerDisconnect implements the HookBrokerDisconnect interface for metrics
// gathering.
// This method is meant to be called by the hook system and not by the user
func (m *Metrics) OnBrokerDisconnect(meta kgo.BrokerMetadata, _ net.Conn) {
	nodeId := kgo.NodeName(meta.NodeID)
	m.connDisconnectsTotal.WithLabelValues(nodeId).Inc()
}

// OnBrokerThrottle implements the HookBrokerThrottle interface for metrics
// gathering.
// This method is meant to be called by the hook system and not by the user
func (m *Metrics) OnBrokerThrottle(meta kgo.BrokerMetadata, throttleInterval time.Duration, _ bool) {
	if _, ok := m.cfg.histograms[RequestThrottled]; ok {
		nodeId := kgo.NodeName(meta.NodeID)
		m.requestThrottledSeconds.WithLabelValues(nodeId).Observe(throttleInterval.Seconds())
	}
}

// OnProduceBatchWritten implements the HookProduceBatchWritten interface for
// metrics gathering.
// This method is meant to be called by the hook system and not by the user
func (m *Metrics) OnProduceBatchWritten(meta kgo.BrokerMetadata, topic string, _ int32, metrics kgo.ProduceBatchMetrics) {
	labels := m.fetchProducerLabels(kgo.NodeName(meta.NodeID), topic)
	if m.cfg.fetchProduceOpts.uncompressedBytes {
		m.produceUncompressedBytes.With(labels).Add(float64(metrics.UncompressedBytes))
	}
	if m.cfg.fetchProduceOpts.compressedBytes {
		m.produceCompressedBytes.With(labels).Add(float64(metrics.CompressedBytes))
	}
	if m.cfg.fetchProduceOpts.batches {
		m.produceBatchesTotal.With(labels).Inc()
	}
	if m.cfg.fetchProduceOpts.records {
		m.produceRecordsTotal.With(labels).Add(float64(metrics.NumRecords))
	}
}

// OnFetchBatchRead implements the HookFetchBatchRead interface for metrics
// gathering.
// This method is meant to be called by the hook system and not by the user
func (m *Metrics) OnFetchBatchRead(meta kgo.BrokerMetadata, topic string, _ int32, metrics kgo.FetchBatchMetrics) {
	labels := m.fetchProducerLabels(kgo.NodeName(meta.NodeID), topic)
	if m.cfg.fetchProduceOpts.uncompressedBytes {
		m.fetchUncompressedBytes.With(labels).Add(float64(metrics.UncompressedBytes))
	}
	if m.cfg.fetchProduceOpts.compressedBytes {
		m.fetchCompressedBytes.With(labels).Add(float64(metrics.CompressedBytes))
	}
	if m.cfg.fetchProduceOpts.batches {
		m.fetchBatchesTotal.With(labels).Inc()
	}
	if m.cfg.fetchProduceOpts.records {
		m.fetchRecordsTotal.With(labels).Add(float64(metrics.NumRecords))
	}
}

// // Nop hook for compat, logic moved to OnBrokerE2E
func (m *Metrics) OnBrokerRead(meta kgo.BrokerMetadata, _ int16, bytesRead int, _, _ time.Duration, err error) {
}

// Nop hook for compat, logic moved to OnBrokerE2E
func (m *Metrics) OnBrokerWrite(meta kgo.BrokerMetadata, _ int16, bytesWritten int, _, _ time.Duration, err error) {
}

// OnBrokerE2E implements the HookBrokerE2E interface for metrics gathering
// This method is meant to be called by the hook system and not by the user
func (m *Metrics) OnBrokerE2E(meta kgo.BrokerMetadata, _ int16, e2e kgo.BrokerE2E) {
	nodeId := kgo.NodeName(meta.NodeID)
	if e2e.WriteErr != nil {
		m.writeErrorsTotal.WithLabelValues(nodeId).Inc()
		return
	}
	m.writeBytesTotal.WithLabelValues(nodeId).Add(float64(e2e.BytesWritten))
	if _, ok := m.cfg.histograms[WriteWait]; ok {
		m.writeWaitSeconds.WithLabelValues(nodeId).Observe(e2e.WriteWait.Seconds())
	}
	if _, ok := m.cfg.histograms[WriteTime]; ok {
		m.writeTimeSeconds.WithLabelValues(nodeId).Observe(e2e.TimeToWrite.Seconds())
	}
	if e2e.ReadErr != nil {
		m.readErrorsTotal.WithLabelValues(nodeId).Inc()
		return
	}
	m.readBytesTotal.WithLabelValues(nodeId).Add(float64(e2e.BytesRead))
	if _, ok := m.cfg.histograms[ReadWait]; ok {
		m.readWaitSeconds.WithLabelValues(nodeId).Observe(e2e.ReadWait.Seconds())
	}
	if _, ok := m.cfg.histograms[ReadTime]; ok {
		m.readTimeSeconds.WithLabelValues(nodeId).Observe(e2e.TimeToRead.Seconds())
	}
	if _, ok := m.cfg.histograms[RequestDurationE2E]; ok {
		m.requestDurationE2ESeconds.WithLabelValues(nodeId).Observe(e2e.DurationE2E().Seconds())
	}
}

// Collect returns the current state of all metrics of the collector.
// Collect & Describe will allow applications that use kprom to
// register multiple collectors to the same registry by inverting the dependency.
// This follows the recommended approach of collector implementation
// https://github.com/prometheus/client_golang/blob/main/prometheus/collector.go#L16-L18
func (m *Metrics) Collect(ch chan<- prometheus.Metric) {
	for _, c := range m.allMetricCollectors {
		c.Collect(ch)
	}
}

// Describe returns all descriptions of the collector.
func (m *Metrics) Describe(ch chan<- *prometheus.Desc) {
	for _, c := range m.allMetricCollectors {
		c.Describe(ch)
	}
}

func (m *Metrics) fetchProducerLabels(nodeId, topic string) prometheus.Labels {
	labels := make(prometheus.Labels, 2)
	for _, l := range m.cfg.fetchProduceOpts.labels {
		switch l {
		case "topic":
			labels[l] = topic
		case "node_id":
			labels[l] = nodeId
		}
	}
	return labels
}
