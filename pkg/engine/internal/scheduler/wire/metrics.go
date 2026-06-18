package wire

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Role identifies the local peer's role for communication metrics.
type Role string

const (
	// RoleUnknown is used when the local peer role is not known.
	RoleUnknown Role = "unknown"
	// RoleScheduler is used for scheduler-side peers.
	RoleScheduler Role = "scheduler"
	// RoleWorker is used for worker-side peers.
	RoleWorker Role = "worker"
)

// Plane identifies the communication plane for a peer.
type Plane string

const (
	// PlaneUnknown is used when the communication plane is not known.
	PlaneUnknown Plane = "unknown"
	// PlaneControl is used for scheduler/worker control traffic.
	PlaneControl Plane = "control"
	// PlaneData is used for stream data traffic.
	PlaneData Plane = "data"
)

const (
	messageOutcomeAck          = "ack"
	messageOutcomeNack         = "nack"
	messageOutcomeTimeout      = "timeout"
	messageOutcomeCanceled     = "canceled"
	messageOutcomeConnClosed   = "conn_closed"
	messageOutcomeSendError    = "send_error"
	messageOutcomeHandlerError = "handler_error"
	messageOutcomeUnsupported  = "unsupported"
	messageOutcomeAccepted     = "accepted"
)

const (
	messageDirectionSent     = "sent"
	messageDirectionReceived = "received"
)

const (
	queueDirectionIncoming = "incoming"
	queueDirectionOutgoing = "outgoing"
)

// Metrics is a set of metrics for a Peer.
type Metrics struct {
	reg *prometheus.Registry

	framesReceivedTotal *prometheus.CounterVec
	framesTotal         *prometheus.CounterVec
	frameBytesTotal     *prometheus.CounterVec
	messagesQueued      prometheus.Gauge
	messagesSentTotal   *prometheus.CounterVec
	messageRTTSeconds   *prometheus.HistogramVec

	messagesTotal         *prometheus.CounterVec
	messageClientSeconds  *prometheus.HistogramVec
	messageHandlerSeconds *prometheus.HistogramVec
	incomingQueueSeconds  *prometheus.HistogramVec
	outgoingQueueSeconds  *prometheus.HistogramVec
	queueDepth            *prometheus.GaugeVec
	pendingRequests       *prometheus.GaugeVec
	handlerInflight       *prometheus.GaugeVec
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	return &Metrics{
		reg: reg,

		framesReceivedTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_frames_received_total",
			Help: "Total number of frames received by the wire",
		}, []string{"type", "message_type"}),
		framesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_frames_total",
			Help: "Total number of encoded wire frames sent or received by role, plane, direction, frame kind, and message type.",
		}, []string{"role", "plane", "direction", "frame_kind", "message_type"}),
		frameBytesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_frame_bytes_total",
			Help: "Total encoded bytes in wire frames sent or received by role, plane, direction, frame kind, and message type.",
		}, []string{"role", "plane", "direction", "frame_kind", "message_type"}),
		messagesQueued: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_wire_messages_queued",
			Help: "Number of messages queued by the wire",
		}),
		messagesSentTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_messages_sent_total",
			Help: "Number of messages sent by a peer",
		}, []string{"message_type"}),
		messageRTTSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_message_rtt_seconds",
			Help: "Round-trip time to synchronously send a message to another peer",
		}, []string{"message_type"}),

		messagesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_messages_total",
			Help: "Total number of wire messages sent or received by outcome.",
		}, []string{"role", "plane", "direction", "message_type", "outcome"}),
		messageClientSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_message_client_seconds",
			Help: "Time spent synchronously sending a wire message and waiting for acknowledgement.",
		}, []string{"role", "plane", "message_type", "outcome"}),
		messageHandlerSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_message_handler_seconds",
			Help: "Time spent handling an incoming wire message in the application handler.",
		}, []string{"role", "plane", "message_type", "outcome"}),
		incomingQueueSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_incoming_queue_seconds",
			Help: "Time an incoming wire message spent waiting in the peer queue before handling.",
		}, []string{"role", "plane", "message_type"}),
		outgoingQueueSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_outgoing_queue_seconds",
			Help: "Time an outgoing wire frame spent waiting in the peer queue before sending.",
		}, []string{"role", "plane", "frame_kind", "message_type"}),
		queueDepth: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_wire_queue_depth",
			Help: "Current number of frames queued by direction.",
		}, []string{"role", "plane", "direction"}),
		pendingRequests: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_wire_pending_requests",
			Help: "Current number of synchronous requests waiting for acknowledgement.",
		}, []string{"role", "plane", "message_type"}),
		handlerInflight: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_wire_handler_inflight",
			Help: "Current number of incoming message handlers running.",
		}, []string{"role", "plane", "message_type"}),
	}
}

func (m *Metrics) Register(reg prometheus.Registerer) error { return reg.Register(m.reg) }
func (m *Metrics) Unregister(reg prometheus.Registerer)     { reg.Unregister(m.reg) }

// newNativeHistogramVec creates a HistogramVec that uses native histogram
// buckets, registered to reg.
func newNativeHistogramVec(reg prometheus.Registerer, opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	opts.NativeHistogramBucketFactor = 1.1
	opts.NativeHistogramMaxBucketNumber = 100
	opts.NativeHistogramMinResetDuration = time.Hour
	return promauto.With(reg).NewHistogramVec(opts, labels)
}

func (m *Metrics) incFrameReceived(frame Frame) {
	m.framesReceivedTotal.WithLabelValues(frame.FrameKind().String(), frameMessageType(frame)).Inc()
}

func (m *Metrics) recordFrame(role Role, plane Plane, direction string, frame Frame, messageType string, size int) {
	m.framesTotal.WithLabelValues(string(role), string(plane), direction, frame.FrameKind().String(), messageType).Inc()
	m.frameBytesTotal.WithLabelValues(string(role), string(plane), direction, frame.FrameKind().String(), messageType).Add(float64(size))
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

func (m *Metrics) incQueueDepth(role Role, plane Plane, direction string) {
	m.queueDepth.WithLabelValues(string(role), string(plane), direction).Inc()
}

func (m *Metrics) decQueueDepth(role Role, plane Plane, direction string) {
	m.queueDepth.WithLabelValues(string(role), string(plane), direction).Dec()
}

func (m *Metrics) observeIncomingQueue(role Role, plane Plane, messageType string, d time.Duration) {
	m.incomingQueueSeconds.WithLabelValues(string(role), string(plane), messageType).Observe(d.Seconds())
}

func (m *Metrics) observeOutgoingQueue(role Role, plane Plane, frameKind string, messageType string, d time.Duration) {
	m.outgoingQueueSeconds.WithLabelValues(string(role), string(plane), frameKind, messageType).Observe(d.Seconds())
}

// sendObservation records metrics across the lifetime of one synchronous send.
// The role and plane captured at the start balance the pending-request gauge;
// the outcome metrics use the labels passed to finish, which may differ if the
// plane was discovered mid-call.
type sendObservation struct {
	m           *Metrics
	role        Role
	plane       Plane
	messageType string
	start       time.Time
	timer       *prometheus.Timer
}

// beginSend starts recording a synchronous send. finish must be called once.
func (m *Metrics) beginSend(role Role, plane Plane, messageType string) sendObservation {
	m.messagesSentTotal.WithLabelValues(messageType).Inc()
	m.pendingRequests.WithLabelValues(string(role), string(plane), messageType).Inc()
	return sendObservation{
		m:           m,
		role:        role,
		plane:       plane,
		messageType: messageType,
		start:       time.Now(),
		timer:       prometheus.NewTimer(m.messageRTTSeconds.WithLabelValues(messageType)),
	}
}

func (o sendObservation) finish(role Role, plane Plane, err error) {
	o.timer.ObserveDuration()
	o.m.pendingRequests.WithLabelValues(string(o.role), string(o.plane), o.messageType).Dec()

	outcome := classifyClientOutcome(err)
	o.m.messageClientSeconds.WithLabelValues(string(role), string(plane), o.messageType, outcome).Observe(time.Since(o.start).Seconds())
	o.m.messagesTotal.WithLabelValues(string(role), string(plane), messageDirectionSent, o.messageType, outcome).Inc()
}

// recordAsyncSend records metrics for one asynchronous send, which completes as
// soon as the frame is accepted into the outgoing queue.
func (m *Metrics) recordAsyncSend(role Role, plane Plane, messageType string, err error) {
	m.messagesSentTotal.WithLabelValues(messageType).Inc()
	m.messagesTotal.WithLabelValues(string(role), string(plane), messageDirectionSent, messageType, classifyAsyncOutcome(err)).Inc()
}

// receiveObservation records metrics across the lifetime of one incoming
// message handler. The labels captured at the start balance the
// handler-inflight gauge; finish uses the labels passed to it for the outcome
// metrics.
type receiveObservation struct {
	m           *Metrics
	role        Role
	plane       Plane
	messageType string
	start       time.Time
}

// beginReceive starts recording an incoming handler. finish must be called once.
func (m *Metrics) beginReceive(role Role, plane Plane, messageType string) receiveObservation {
	m.handlerInflight.WithLabelValues(string(role), string(plane), messageType).Inc()
	return receiveObservation{
		m:           m,
		role:        role,
		plane:       plane,
		messageType: messageType,
		start:       time.Now(),
	}
}

func (o receiveObservation) finish(role Role, plane Plane, err error) {
	o.m.handlerInflight.WithLabelValues(string(o.role), string(o.plane), o.messageType).Dec()

	outcome := classifyHandlerOutcome(err)
	o.m.messageHandlerSeconds.WithLabelValues(string(role), string(plane), o.messageType, outcome).Observe(time.Since(o.start).Seconds())
	o.m.messagesTotal.WithLabelValues(string(role), string(plane), messageDirectionReceived, o.messageType, outcome).Inc()
}

func classifyClientOutcome(err error) string {
	switch {
	case err == nil:
		return messageOutcomeAck
	case errors.Is(err, context.DeadlineExceeded):
		return messageOutcomeTimeout
	case errors.Is(err, context.Canceled):
		return messageOutcomeCanceled
	case errors.Is(err, ErrConnClosed):
		return messageOutcomeConnClosed
	}

	var wireError *Error
	if errors.As(err, &wireError) {
		return messageOutcomeNack
	}

	return messageOutcomeSendError
}

func classifyAsyncOutcome(err error) string {
	switch {
	case err == nil:
		return messageOutcomeAccepted
	case errors.Is(err, context.DeadlineExceeded):
		return messageOutcomeTimeout
	case errors.Is(err, context.Canceled):
		return messageOutcomeCanceled
	case errors.Is(err, ErrConnClosed):
		return messageOutcomeConnClosed
	default:
		return messageOutcomeSendError
	}
}

func classifyHandlerOutcome(err error) string {
	if err == nil {
		return messageOutcomeAck
	}

	var wireError *Error
	if errors.As(err, &wireError) && wireError.Code == 501 {
		return messageOutcomeUnsupported
	}
	return messageOutcomeHandlerError
}
