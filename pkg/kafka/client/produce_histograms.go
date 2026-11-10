package client

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// produceHistograms exposes native histograms for how long each stage of a produce request takes.
type produceHistograms struct {
	reg prometheus.Registerer

	writeWaitSeconds *prometheus.HistogramVec
	writeTimeSeconds *prometheus.HistogramVec
	readWaitSeconds  *prometheus.HistogramVec
	readTimeSeconds  *prometheus.HistogramVec

	allMetricCollectors []prometheus.Collector
}

var (
	_ kgo.HookNewClient    = (*produceHistograms)(nil)
	_ kgo.HookClientClosed = (*produceHistograms)(nil)
	_ kgo.HookBrokerE2E    = (*produceHistograms)(nil)
)

// newProduceHistograms returns a produceHistograms hook that registers its
// metrics with reg when the client is created and unregisters them when the
// client is closed.
func newProduceHistograms(reg prometheus.Registerer) *produceHistograms {
	return &produceHistograms{reg: reg}
}

// OnNewClient implements kgo.HookNewClient.
func (m *produceHistograms) OnNewClient(*kgo.Client) {
	factory := promauto.With(m.reg)
	newHistogramVec := func(name, help string) *prometheus.HistogramVec {
		return factory.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:                       MetricsPrefix,
			Name:                            name,
			Help:                            help,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"node_id", "host"})
	}

	m.writeWaitSeconds = newHistogramVec("produce_write_wait_seconds", "Time spent waiting to write a Produce request to Kafka")
	m.writeTimeSeconds = newHistogramVec("produce_write_time_seconds", "Time spent writing a Produce request to Kafka")
	m.readWaitSeconds = newHistogramVec("produce_read_wait_seconds", "Time spent waiting to read a Produce response from Kafka")
	m.readTimeSeconds = newHistogramVec("produce_read_time_seconds", "Time spent reading a Produce response from Kafka")

	m.allMetricCollectors = append(m.allMetricCollectors,
		m.writeWaitSeconds,
		m.writeTimeSeconds,
		m.readWaitSeconds,
		m.readTimeSeconds,
	)
}

// OnClientClosed implements kgo.HookClientClosed.
func (m *produceHistograms) OnClientClosed(*kgo.Client) {
	for _, c := range m.allMetricCollectors {
		m.reg.Unregister(c)
	}
}

// OnBrokerE2E implements kgo.HookBrokerE2E.
func (m *produceHistograms) OnBrokerE2E(meta kgo.BrokerMetadata, key int16, e2e kgo.BrokerE2E) {
	if key != int16(kmsg.Produce) {
		return
	}
	nodeID := kgo.NodeName(meta.NodeID)
	if e2e.WriteErr != nil {
		return
	}
	m.writeWaitSeconds.WithLabelValues(nodeID, meta.Host).Observe(e2e.WriteWait.Seconds())
	m.writeTimeSeconds.WithLabelValues(nodeID, meta.Host).Observe(e2e.TimeToWrite.Seconds())
	if e2e.ReadErr != nil {
		return
	}
	m.readWaitSeconds.WithLabelValues(nodeID, meta.Host).Observe(e2e.ReadWait.Seconds())
	m.readTimeSeconds.WithLabelValues(nodeID, meta.Host).Observe(e2e.TimeToRead.Seconds())
}
