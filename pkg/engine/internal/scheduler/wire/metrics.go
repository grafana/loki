package wire

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics is a set of metrics for a Peer.
type Metrics struct {
	reg *prometheus.Registry

	framesReceivedTotal *prometheus.CounterVec
	messagesQueued      prometheus.Gauge
	messagesSentTotal   *prometheus.CounterVec
	messageRTTSeconds   *prometheus.HistogramVec
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	return &Metrics{
		reg: reg,

		framesReceivedTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_frames_received_total",
			Help: "Total number of frames received by the wire",
		}, []string{"type", "message_type"}),
		messagesQueued: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_wire_messages_queued",
			Help: "Number of messages queued by the wire",
		}),
		messagesSentTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_messages_sent_total",
			Help: "Number of messages sent by a peer",
		}, []string{"message_type"}),
		messageRTTSeconds: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_engine_scheduler_wire_message_rtt_seconds",
			Help:                            "Round-trip time to synchronously send a message to another peer",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"message_type"}),
	}
}

func (m *Metrics) Register(reg prometheus.Registerer) error { return reg.Register(m.reg) }
func (m *Metrics) Unregister(reg prometheus.Registerer)     { reg.Unregister(m.reg) }

func (m *Metrics) incFrameReceived(frame Frame) {
	m.framesReceivedTotal.WithLabelValues(frame.FrameKind().String(), frameMessageType(frame)).Inc()
}

// frameMessageType returns the application [MessageKind] carried by frame, or
// "none" for frames that don't carry a message (acks, nacks, discards).
func frameMessageType(frame Frame) string {
	if mf, ok := frame.(MessageFrame); ok && mf.Message != nil {
		return mf.Message.Kind().String()
	}
	return "none"
}

func (m *Metrics) incMessageQueued() {
	m.messagesQueued.Inc()
}

func (m *Metrics) decMessageQueued() {
	m.messagesQueued.Dec()
}

func (m *Metrics) incMessageSent(messageType string) {
	m.messagesSentTotal.WithLabelValues(messageType).Inc()
}

func (m *Metrics) newMessageRTTTimer(messageType string) *prometheus.Timer {
	return prometheus.NewTimer(m.messageRTTSeconds.WithLabelValues(messageType))
}
