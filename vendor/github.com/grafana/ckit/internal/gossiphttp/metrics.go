package gossiphttp

import (
	"github.com/grafana/ckit/internal/metricsutil"
	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	metricsutil.Container

	packetRxTotal       prometheus.Counter
	packetRxBytesTotal  prometheus.Counter
	packetTxTotal       prometheus.Counter
	packetTxBytesTotal  prometheus.Counter
	packetTxFailedTotal prometheus.Counter

	openStreams         prometheus.Gauge
	streamRxTotal       prometheus.Counter
	streamRxBytesTotal  prometheus.Counter
	streamRxFailedTotal prometheus.Counter
	streamTxTotal       prometheus.Counter
	streamTxBytesTotal  prometheus.Counter
	streamTxFailedTotal prometheus.Counter
}

func newMetrics() *metrics {
	var m metrics

	m.packetRxTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cluster_transport_rx_packets_total",
		Help: "Total number of HTTP/2 gossip transport packets read",
	})
	m.packetRxBytesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cluster_transport_rx_bytes_total",
		Help: "Total number of HTTP/2 gossip transport bytes read",
	})
	m.packetTxTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cluster_transport_tx_packets_total",
		Help: "Total number of HTTP/2 gossip transport packets written (failed or otherwise)",
	})
	m.packetTxBytesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cluster_transport_tx_bytes_total",
		Help: "Total number of HTTP/2 gossip transport bytes written (failed or otherwise)",
	})
	m.packetTxFailedTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cluster_transport_tx_packets_failed_total",
		Help: "Total number of failed HTTP/2 gossip transport packets",
	})

	m.openStreams = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cluster_transport_streams",
		Help: "Current number of HTTP/2 transport data streams",
	})
	m.streamRxTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cluster_transport_stream_rx_packets_total",
		Help: "Total number of HTTP/2 gossip transport stream packets read",
	})
	m.streamRxBytesTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cluster_transport_stream_rx_bytes_total",
		Help: "Total number of HTTP/2 gossip transport stream bytes read",
	})
	m.streamRxFailedTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cluster_transport_stream_rx_packets_failed_total",
		Help: "Total number of HTTP/2 gossip transport stream packets failed to read",
	})
	m.streamTxTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cluster_transport_stream_tx_packets_total",
		Help: "Total number of HTTP/2 gossip transport stream packets written",
	})
	m.streamTxBytesTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cluster_transport_stream_tx_bytes_total",
		Help: "Total number of HTTP/2 gossip transport stream bytes written",
	})
	m.streamTxFailedTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cluster_transport_stream_tx_packets_failed_total",
		Help: "Total number of failed HTTP/2 gossip transport stream packets",
	})

	m.Add(
		m.packetRxTotal,
		m.packetRxBytesTotal,
		m.packetTxTotal,
		m.packetTxBytesTotal,
		m.packetTxFailedTotal,
		m.openStreams,
		m.streamRxTotal,
		m.streamRxBytesTotal,
		m.streamTxTotal,
		m.streamTxBytesTotal,
		m.streamTxFailedTotal,
	)

	return &m
}
