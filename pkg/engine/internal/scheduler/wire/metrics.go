package wire

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/engine/internal/metrictimer"
	"github.com/grafana/loki/v3/pkg/engine/internal/obslock"
)

type sendMode string

// sendMode label values. A send is "sync" when issued through [Peer.SendMessage]
// (the caller waits for an ack) and "async" through [Peer.SendMessageAsync].
// Frames the peer handles on behalf of the remote side are "internal": the
// sync/async distinction is a send-side concept that is not carried on the
// wire for incoming messages. The Prometheus label is named "mode".
const (
	sendModeSync     sendMode = "sync"
	sendModeAsync    sendMode = "async"
	sendModeInternal sendMode = "internal"
)

func (m sendMode) String() string { return string(m) }

type queueName string

// queue label values. The queue implies the direction (the outgoing queue is
// outbound; the incoming and dispatch queues are inbound), so queue metrics do
// not carry a separate direction label.
const (
	queueOutgoing     queueName = "peer_outgoing"
	queueIncoming     queueName = "peer_incoming"
	queueConnDispatch queueName = "conn_dispatch"
)

func (q queueName) String() string { return string(q) }

type direction string

// direction label values.
const (
	directionIncoming direction = "incoming"
	directionOutgoing direction = "outgoing"
)

func (d direction) String() string { return string(d) }

type transport string

// transport label values.
const (
	transportHTTP2 transport = "http2"
	transportLocal transport = "local"
)

func (t transport) String() string { return string(t) }

// outcome label values for a synchronous round trip.
const (
	outcomeNone       metrictimer.Outcome = ""
	outcomeAck        metrictimer.Outcome = "ack"
	outcomeNack       metrictimer.Outcome = "nack"
	outcomeTimeout    metrictimer.Outcome = "timeout"
	outcomeCanceled   metrictimer.Outcome = "canceled"
	outcomeConnClosed metrictimer.Outcome = "conn_closed"
	outcomeSendError  metrictimer.Outcome = "send_error"
	outcomeDiscarded  metrictimer.Outcome = "discarded"
)

// phase label values for frame send and receive histograms.
const (
	phaseConnSendTotal       metrictimer.Phase = "conn_send_total"
	phaseDeserialize         metrictimer.Phase = "deserialize"
	phaseFlush               metrictimer.Phase = "flush"
	phaseLocalChannelReceive metrictimer.Phase = "local_channel_receive"
	phaseReadBytes           metrictimer.Phase = "read_bytes"
	phaseRouteToQueue        metrictimer.Phase = "route_to_queue_or_result"
	phaseSerialize           metrictimer.Phase = "serialize"
	phaseWrite               metrictimer.Phase = "write"

	phaseHandlerTotal   metrictimer.Phase = "handler_total"
	phaseRoundtripTotal metrictimer.Phase = "roundtrip_total"
)

type codecOperation string

// frame_codec_stage_seconds operation label values.
const (
	codecOperationDecode codecOperation = "decode"
	codecOperationEncode codecOperation = "encode"
)

func (o codecOperation) String() string { return string(o) }

type codecStage string

// frame_codec_stage_seconds stage label values.
const (
	codecStageArrowDecode       codecStage = "arrow_decode"
	codecStageArrowEncode       codecStage = "arrow_encode"
	codecStageProtobufMarshal   codecStage = "protobuf_marshal"
	codecStageProtobufUnmarshal codecStage = "protobuf_unmarshal"
	codecStageTaskAssignDecode  codecStage = "taskassign_plan_decode"
	codecStageTaskAssignEncode  codecStage = "taskassign_plan_encode"
)

func (s codecStage) String() string { return string(s) }

// Metrics is a set of metrics for a Peer.
type Metrics struct {
	reg *prometheus.Registry

	framesReceivedTotal *prometheus.CounterVec
	messagesSentTotal   *prometheus.CounterVec

	frameQueueLength       *prometheus.GaugeVec
	frameQueueWaitSeconds  *prometheus.HistogramVec
	queueBlockedSenders    *prometheus.GaugeVec
	frameSendSeconds       *prometheus.HistogramVec
	frameReceiveSeconds    *prometheus.HistogramVec
	frameCodecStageSeconds *prometheus.HistogramVec
	frameBytesTotal        *prometheus.CounterVec
	frameSizeBytes         *prometheus.HistogramVec
	writeBusySecondsTotal  *prometheus.CounterVec
	connectionsActive      *prometheus.GaugeVec
	handlerSeconds         *prometheus.HistogramVec

	messageRoundtripSeconds *prometheus.HistogramVec

	// lock holds the instruments for the connection write mutex.
	lock *obslock.Metrics
}

func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	return &Metrics{
		reg: reg,
		lock: obslock.NewMetrics(reg,
			"loki_engine_scheduler_wire_lock_wait_seconds",
			"loki_engine_scheduler_wire_lock_hold_seconds",
		),

		framesReceivedTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_frames_received_total",
			Help: "Total number of frames received by the wire",
		}, []string{"type", "message_type"}),
		messagesSentTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_messages_sent_total",
			Help: "Number of messages a peer accepted for sending, including async sends that carry no round trip",
		}, []string{"message_type", "mode"}),

		frameQueueLength: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_wire_frame_queue_length",
			Help: "Number of frames currently waiting in a peer or connection dispatch queue",
		}, []string{"queue", "frame_type", "message_type", "mode"}),
		frameQueueWaitSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_frame_queue_wait_seconds",
			Help: "Time a frame waited in a peer or connection dispatch queue before being dequeued",
		}, []string{"queue", "frame_type", "message_type", "mode"}),
		queueBlockedSenders: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_wire_queue_blocked_senders",
			Help: "Number of goroutines currently blocked trying to enqueue into a peer or connection queue",
		}, []string{"queue", "frame_type", "message_type", "mode"}),
		frameSendSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_frame_send_seconds",
			Help: "Time spent in send-side phases for a wire frame",
		}, []string{"phase", "transport", "frame_type", "message_type", "mode"}),
		frameReceiveSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_frame_receive_seconds",
			Help: "Time spent in receive-side phases for a wire frame",
		}, []string{"phase", "transport", "frame_type", "message_type", "mode"}),
		frameCodecStageSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_frame_codec_stage_seconds",
			Help: "Time spent in codec sub-stages for wire-frame serialization and deserialization",
		}, []string{"operation", "stage", "frame_type", "message_type"}),
		frameBytesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_frame_bytes_total",
			Help: "Total encoded wire-frame bytes sent or received, including the length prefix",
		}, []string{"direction", "frame_type", "message_type", "mode"}),
		frameSizeBytes: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_frame_size_bytes",
			Help: "Encoded wire-frame size in bytes, including the length prefix",
		}, []string{"direction", "frame_type", "message_type", "mode"}),
		writeBusySecondsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_scheduler_wire_write_busy_seconds_total",
			Help: "Wall-clock seconds each connection write path was busy sending frames",
		}, []string{"transport"}),
		connectionsActive: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_engine_scheduler_wire_connections_active",
			Help: "Number of active wire peer connections by transport",
		}, []string{"transport"}),
		handlerSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_handler_seconds",
			Help: "Time spent in the receiver's application handler before an ack or nack is enqueued",
		}, []string{"message_type", "outcome"}),

		messageRoundtripSeconds: newNativeHistogramVec(reg, prometheus.HistogramOpts{
			Name: "loki_engine_scheduler_wire_message_roundtrip_seconds",
			Help: "Time to synchronously send a message to another peer and receive its acknowledgement, by outcome",
		}, []string{"message_type", "outcome", "mode"}),
	}
}

func newNativeHistogramVec(r prometheus.Registerer, opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	applyNativeHistogramOpts(&opts)
	return promauto.With(r).NewHistogramVec(opts, labels)
}

func applyNativeHistogramOpts(opts *prometheus.HistogramOpts) {
	opts.NativeHistogramBucketFactor = 1.1
	opts.NativeHistogramMaxBucketNumber = 100
	opts.NativeHistogramMinResetDuration = time.Hour
}

func (m *Metrics) Register(reg prometheus.Registerer) error { return reg.Register(m.reg) }
func (m *Metrics) Unregister(reg prometheus.Registerer)     { reg.Unregister(m.reg) }

func (m *Metrics) incFrameReceived(frame Frame) {
	m.framesReceivedTotal.WithLabelValues(frame.FrameKind().String(), frameMessageType(frame)).Inc()
}

// frameMessageType returns the application [MessageKind] carried by frame, or
// "none" for frames that don't carry a message (acks, nacks, discards). It is
// used only by the legacy frames_received_total metric.
func frameMessageType(frame Frame) string {
	if mf, ok := frame.(MessageFrame); ok && mf.Message != nil {
		return mf.Message.Kind().String()
	}
	return "none"
}

// frameLabels are the bounded labels shared by mixed frame metrics.
type frameLabels struct {
	frameType   string
	messageType string
	sendMode    sendMode
}

func labelsForFrame(frame Frame, mode sendMode) frameLabels {
	labels := frameLabels{
		frameType:   frame.FrameKind().String(),
		messageType: "none",
		sendMode:    mode,
	}
	if mf, ok := frame.(MessageFrame); ok {
		labels.messageType = "unknown"
		if mf.Message != nil {
			labels.messageType = mf.Message.Kind().String()
		}
	}
	return labels
}

func (m *Metrics) incMessageSent(kind MessageKind, mode sendMode) {
	m.messagesSentTotal.WithLabelValues(kind.String(), mode.String()).Inc()
}

func (m *Metrics) incFrameQueued(queue queueName, frame Frame, mode sendMode) {
	labels := labelsForFrame(frame, mode)
	m.frameQueueLength.WithLabelValues(queue.String(), labels.frameType, labels.messageType, labels.sendMode.String()).Inc()
}

func (m *Metrics) decFrameQueued(queue queueName, frame Frame, mode sendMode) {
	labels := labelsForFrame(frame, mode)
	m.frameQueueLength.WithLabelValues(queue.String(), labels.frameType, labels.messageType, labels.sendMode.String()).Dec()
}

func (m *Metrics) observeFrameQueueWait(queue queueName, frame Frame, mode sendMode, d time.Duration) {
	labels := labelsForFrame(frame, mode)
	m.frameQueueWaitSeconds.WithLabelValues(queue.String(), labels.frameType, labels.messageType, labels.sendMode.String()).Observe(d.Seconds())
}

func (m *Metrics) incQueueBlockedSender(queue queueName, frame Frame, mode sendMode) {
	labels := labelsForFrame(frame, mode)
	m.queueBlockedSenders.WithLabelValues(queue.String(), labels.frameType, labels.messageType, labels.sendMode.String()).Inc()
}

func (m *Metrics) decQueueBlockedSender(queue queueName, frame Frame, mode sendMode) {
	labels := labelsForFrame(frame, mode)
	m.queueBlockedSenders.WithLabelValues(queue.String(), labels.frameType, labels.messageType, labels.sendMode.String()).Dec()
}

func (m *Metrics) noteQueueBlockedSender(queue queueName, frame Frame, mode sendMode) func() {
	if m == nil {
		return func() {}
	}
	m.incQueueBlockedSender(queue, frame, mode)
	return func() { m.decQueueBlockedSender(queue, frame, mode) }
}

// noteBlockedEnqueue records a frame that is about to block waiting for room in
// a queue: it increments both the queue length and the blocked-sender gauge and
// returns a function that decrements both once the enqueue settles (whether it
// succeeded, was canceled, or the connection closed). Coupling the two gauges
// keeps a single defer at the call site instead of one decrement per branch.
func (m *Metrics) noteBlockedEnqueue(queue queueName, frame Frame, mode sendMode) func() {
	if m == nil {
		return func() {}
	}
	m.incFrameQueued(queue, frame, mode)
	m.incQueueBlockedSender(queue, frame, mode)
	return func() {
		m.decFrameQueued(queue, frame, mode)
		m.decQueueBlockedSender(queue, frame, mode)
	}
}

func (m *Metrics) startFrameSend(tr transport, frame Frame, mode sendMode) *metrictimer.Timer {
	if m == nil {
		return nil
	}
	labels := labelsForFrame(frame, mode)
	return metrictimer.New(metrictimer.Config{
		Total:    phaseConnSendTotal,
		HasTotal: true,
		Rows:     6,
		Emit: func(p metrictimer.Phase, _ metrictimer.Outcome, d time.Duration) {
			m.frameSendSeconds.WithLabelValues(p.String(), tr.String(), labels.frameType, labels.messageType, labels.sendMode.String()).Observe(d.Seconds())
			// write_busy is the wall-clock time the write path was actually
			// busy, so one send timer feeds both metrics without a separate
			// timer object. A local send takes no write lock, so its single
			// conn_send_total row is the busy time. An HTTP/2 send waits on
			// writeMu first (recorded on its own as the conn_write lock), so
			// summing only the write-path phases keeps that lock wait out of
			// write_busy.
			switch tr {
			case transportLocal:
				if p == phaseConnSendTotal {
					m.addWriteBusy(tr, d)
				}
			case transportHTTP2:
				switch p {
				case phaseSerialize, phaseWrite, phaseFlush:
					m.addWriteBusy(tr, d)
				}
			}
		},
	})
}

// frameReceiveTimer records receive-side phase durations for one frame. The
// receive path has no outcome dimension, so unlike the send timer it exposes
// none. The zero value records nothing.
type frameReceiveTimer struct {
	t *metrictimer.Timer
}

func (m *Metrics) startFrameReceive(tr transport, frame Frame, mode sendMode) frameReceiveTimer {
	if m == nil {
		return frameReceiveTimer{}
	}
	labels := labelsForFrame(frame, mode)
	return frameReceiveTimer{t: metrictimer.New(metrictimer.Config{
		Rows: 4,
		Emit: func(p metrictimer.Phase, _ metrictimer.Outcome, d time.Duration) {
			m.frameReceiveSeconds.WithLabelValues(p.String(), tr.String(), labels.frameType, labels.messageType, labels.sendMode.String()).Observe(d.Seconds())
		},
	})}
}

// Observe records d for phase p on the receive timer.
func (r frameReceiveTimer) Observe(p metrictimer.Phase, d time.Duration) { r.t.Observe(p, d) }

// Done flushes the receive timer's buffered phases.
func (r frameReceiveTimer) Done() { r.t.Done("") }

// timeFrameReceive runs fn and records its elapsed time to frame_receive_seconds
// for phase p as a single observation. The receive path has no outcome
// dimension. A nil *Metrics still runs fn.
func (m *Metrics) timeFrameReceive(phase metrictimer.Phase, tr transport, frame Frame, mode sendMode, fn func() error) error {
	if m == nil {
		return fn()
	}

	d, err := metrictimer.Time(fn)
	m.observeFrameReceive(phase, tr, frame, mode, d)
	return err
}

func (m *Metrics) observeFrameReceive(phase metrictimer.Phase, tr transport, frame Frame, mode sendMode, d time.Duration) {
	timer := m.startFrameReceive(tr, frame, mode)
	timer.Observe(phase, d)
	timer.Done()
}

func (m *Metrics) observeFrameBytes(dir direction, frame Frame, mode sendMode, size int) {
	if m == nil {
		return
	}
	labels := labelsForFrame(frame, mode)
	m.frameBytesTotal.WithLabelValues(dir.String(), labels.frameType, labels.messageType, labels.sendMode.String()).Add(float64(size))
	m.frameSizeBytes.WithLabelValues(dir.String(), labels.frameType, labels.messageType, labels.sendMode.String()).Observe(float64(size))
}

func (m *Metrics) addWriteBusy(tr transport, d time.Duration) {
	m.writeBusySecondsTotal.WithLabelValues(tr.String()).Add(d.Seconds())
}

func (m *Metrics) markActive(tr transport) {
	m.connectionsActive.WithLabelValues(tr.String()).Inc()
}

func (m *Metrics) markInactive(tr transport) {
	m.connectionsActive.WithLabelValues(tr.String()).Dec()
}

func (m *Metrics) startPeerHandler(messageType string) *metrictimer.Timer {
	if m == nil {
		return nil
	}
	return metrictimer.New(metrictimer.Config{
		Total:    phaseHandlerTotal,
		HasTotal: true,
		Rows:     1,
		Emit: func(_ metrictimer.Phase, out metrictimer.Outcome, d time.Duration) {
			m.handlerSeconds.WithLabelValues(messageType, out.String()).Observe(d.Seconds())
		},
	})
}

func (m *Metrics) startMessageRoundtrip(kind MessageKind, mode sendMode) *metrictimer.Timer {
	if m == nil {
		return nil
	}
	return metrictimer.New(metrictimer.Config{
		Total:    phaseRoundtripTotal,
		HasTotal: true,
		Rows:     1,
		Emit: func(_ metrictimer.Phase, out metrictimer.Outcome, d time.Duration) {
			m.messageRoundtripSeconds.WithLabelValues(kind.String(), out.String(), mode.String()).Observe(d.Seconds())
		},
	})
}
