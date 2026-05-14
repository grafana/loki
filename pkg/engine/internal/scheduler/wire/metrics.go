package wire

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics is a set of event-driven metrics for [Peer] instances.
type Metrics struct {
	reg *prometheus.Registry

	framesReceivedTotal    *prometheus.CounterVec
	framesSentTotal        *prometheus.CounterVec
	messageRTTSeconds      prometheus.Histogram
	enqueueOutgoingSeconds prometheus.Histogram
	enqueueIncomingSeconds prometheus.Histogram
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	return &Metrics{
		reg: reg,

		framesReceivedTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_wire_frames_received_total",
			Help: "Total number of frames received by type",
		}, []string{"type"}),
		framesSentTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_wire_frames_sent_total",
			Help: "Total number of frames sent by type",
		}, []string{"type"}),
		messageRTTSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_engine_wire_message_rtt_seconds",
			Help:                            "Round-trip time to synchronously send a message to another peer",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
		enqueueOutgoingSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_engine_wire_enqueue_outgoing_seconds",
			Help:                            "Time spent blocking while enqueuing a frame to the outgoing buffer",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
		enqueueIncomingSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_engine_wire_enqueue_incoming_seconds",
			Help:                            "Time spent blocking while enqueuing a message to the incoming buffer",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
	}
}

func (m *Metrics) Register(reg prometheus.Registerer) error { return reg.Register(m.reg) }
func (m *Metrics) Unregister(reg prometheus.Registerer)     { reg.Unregister(m.reg) }

func (m *Metrics) incFrameReceived(frameType string) {
	m.framesReceivedTotal.WithLabelValues(frameType).Inc()
}

func (m *Metrics) incFrameSent(frameType string) {
	m.framesSentTotal.WithLabelValues(frameType).Inc()
}

func (m *Metrics) newMessageRTTTimer() *prometheus.Timer {
	return prometheus.NewTimer(m.messageRTTSeconds)
}

func (m *Metrics) newEnqueueOutgoingTimer() *prometheus.Timer {
	return prometheus.NewTimer(m.enqueueOutgoingSeconds)
}

func (m *Metrics) newEnqueueIncomingTimer() *prometheus.Timer {
	return prometheus.NewTimer(m.enqueueIncomingSeconds)
}
