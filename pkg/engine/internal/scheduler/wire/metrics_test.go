package wire

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

type scriptedRecvConn struct {
	frame Frame
	size  int
	sent  bool
}

func (c *scriptedRecvConn) Send(context.Context, Frame) error { return nil }

func (c *scriptedRecvConn) Recv(ctx context.Context) (Frame, error) {
	frame, _, err := c.recvWithSize(ctx)
	return frame, err
}

func (c *scriptedRecvConn) recvWithSize(ctx context.Context) (Frame, int, error) {
	if !c.sent {
		c.sent = true
		return c.frame, c.size, nil
	}

	<-ctx.Done()
	return nil, 0, ctx.Err()
}

func (c *scriptedRecvConn) Close() error { return nil }

func (c *scriptedRecvConn) LocalAddr() net.Addr { return nil }

func (c *scriptedRecvConn) RemoteAddr() net.Addr { return nil }

// TestObservationGaugeBalance pins the subtlest property of the send/receive
// observations: the pending-request and handler-inflight gauges must net to
// zero across begin/finish even when the plane is discovered mid-call, because
// the gauge Dec must use the labels captured at begin, not the final labels.
func TestObservationGaugeBalance(t *testing.T) {
	const messageType = "TestMessage"

	t.Run("send", func(t *testing.T) {
		m := NewMetrics()
		obs := m.beginSend(RoleScheduler, PlaneUnknown, messageType)
		require.Equal(t, 1.0, testutil.ToFloat64(m.pendingRequests.WithLabelValues(string(RoleScheduler), string(PlaneUnknown), messageType)))

		// finish with a different plane, as if the connection was classified
		// after the send began.
		obs.finish(RoleScheduler, PlaneControl, nil)
		require.Equal(t, 0.0, testutil.ToFloat64(m.pendingRequests.WithLabelValues(string(RoleScheduler), string(PlaneUnknown), messageType)), "gauge must be decremented on the begin labels")
		require.Equal(t, 0.0, testutil.ToFloat64(m.pendingRequests.WithLabelValues(string(RoleScheduler), string(PlaneControl), messageType)), "gauge must not leak onto the finish labels")
		require.Equal(t, uint64(1), histogramSampleCount(t, m, "loki_engine_scheduler_wire_message_client_seconds", map[string]string{
			"role":         string(RoleScheduler),
			"plane":        string(PlaneControl),
			"mode":         messageClientModeSync,
			"message_type": messageType,
			"outcome":      messageOutcomeAck,
		}))
	})

	t.Run("receive", func(t *testing.T) {
		m := NewMetrics()
		obs := m.beginReceive(RoleScheduler, PlaneUnknown, messageType)
		require.Equal(t, 1.0, testutil.ToFloat64(m.handlerInflight.WithLabelValues(string(RoleScheduler), string(PlaneUnknown), messageType)))

		obs.finish(RoleScheduler, PlaneControl, nil)
		require.Equal(t, 0.0, testutil.ToFloat64(m.handlerInflight.WithLabelValues(string(RoleScheduler), string(PlaneUnknown), messageType)), "gauge must be decremented on the begin labels")
		require.Equal(t, 0.0, testutil.ToFloat64(m.handlerInflight.WithLabelValues(string(RoleScheduler), string(PlaneControl), messageType)), "gauge must not leak onto the finish labels")
	})
}

func TestRecordAsyncSendObservesClientWait(t *testing.T) {
	m := NewMetrics()

	m.recordAsyncSend(RoleWorker, PlaneControl, "WorkerReady", time.Second, nil)

	metric := histogramMetric(t, m, "loki_engine_scheduler_wire_message_client_seconds", map[string]string{
		"role":         string(RoleWorker),
		"plane":        string(PlaneControl),
		"mode":         messageClientModeAsync,
		"message_type": "WorkerReady",
		"outcome":      messageOutcomeAccepted,
	})
	require.Equal(t, uint64(1), metric.GetHistogram().GetSampleCount())
	require.Equal(t, 1.0, metric.GetHistogram().GetSampleSum())
	require.Equal(t, 1.0, testutil.ToFloat64(m.messagesTotal.WithLabelValues(
		string(RoleWorker),
		string(PlaneControl),
		messageDirectionSent,
		"WorkerReady",
		messageOutcomeAccepted,
	)))
}

func histogramSampleCount(t *testing.T, m *Metrics, name string, labels map[string]string) uint64 {
	t.Helper()

	return histogramMetric(t, m, name, labels).GetHistogram().GetSampleCount()
}

func histogramMetric(t *testing.T, m *Metrics, name string, labels map[string]string) *dto.Metric {
	t.Helper()

	families, err := m.reg.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if metricHasLabels(metric, labels) {
				return metric
			}
		}
	}
	t.Fatalf("metric %s with labels %v not found", name, labels)
	return nil
}

func metricHasLabels(metric *dto.Metric, labels map[string]string) bool {
	if len(metric.GetLabel()) != len(labels) {
		return false
	}
	for _, label := range metric.GetLabel() {
		if labels[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
}

func TestFrameMessageType(t *testing.T) {
	tests := []struct {
		name  string
		frame Frame
		want  string
	}{
		{name: "message frame", frame: MessageFrame{Message: TaskAssignMessage{}}, want: "TaskAssign"},
		{name: "message frame without message", frame: MessageFrame{}, want: "none"},
		{name: "ack frame", frame: AckFrame{}, want: "none"},
		{name: "nack frame", frame: NackFrame{}, want: "none"},
		{name: "discard frame", frame: DiscardFrame{}, want: "none"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, frameMessageType(tt.frame))
		})
	}
}

func TestRecordFrameTraffic(t *testing.T) {
	m := NewMetrics()
	frame := MessageFrame{ID: 42, Message: WorkerReadyMessage{}}
	size, err := DefaultFrameCodec.encodedSize(frame)
	require.NoError(t, err)

	m.recordFrame(RoleScheduler, PlaneControl, messageDirectionSent, frame, "WorkerReady", size)

	require.Equal(t, 1.0, testutil.ToFloat64(m.framesTotal.WithLabelValues(
		string(RoleScheduler),
		string(PlaneControl),
		messageDirectionSent,
		FrameKindMessage.String(),
		"WorkerReady",
	)))
	require.Equal(t, float64(size), testutil.ToFloat64(m.frameBytesTotal.WithLabelValues(
		string(RoleScheduler),
		string(PlaneControl),
		messageDirectionSent,
		FrameKindMessage.String(),
		"WorkerReady",
	)))
}

func TestRecvMessagesRecordsAckFrameWithRequestMessageType(t *testing.T) {
	m := NewMetrics()
	ackFrame := AckFrame{ID: 42}
	size, err := DefaultFrameCodec.encodedSize(ackFrame)
	require.NoError(t, err)

	conn := &scriptedRecvConn{frame: ackFrame, size: size}
	p := &Peer{
		Metrics: m,
		Conn:    conn,
		Role:    RoleScheduler,
	}
	p.lazyInit()
	p.SetPlane(PlaneControl)
	p.sentRequests.Store(uint64(42), &request{
		messageType: "TaskAssign",
		result:      make(chan error, 1),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.recvMessages(ctx) }()

	val, found := p.sentRequests.Load(uint64(42))
	require.True(t, found)
	req := val.(*request)
	select {
	case err := <-req.result:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ack")
	}
	cancel()
	require.NoError(t, <-done)

	require.Equal(t, 1.0, testutil.ToFloat64(m.framesTotal.WithLabelValues(
		string(RoleScheduler),
		string(PlaneControl),
		messageDirectionReceived,
		FrameKindAck.String(),
		"TaskAssign",
	)))
	require.Equal(t, float64(size), testutil.ToFloat64(m.frameBytesTotal.WithLabelValues(
		string(RoleScheduler),
		string(PlaneControl),
		messageDirectionReceived,
		FrameKindAck.String(),
		"TaskAssign",
	)))
}

func TestProcessMessagePreservesMessageTypeOnAckFrames(t *testing.T) {
	tests := []struct {
		name       string
		handlerErr error
		wantKind   FrameKind
	}{
		{name: "ack", wantKind: FrameKindAck},
		{name: "nack", handlerErr: errors.New("failed"), wantKind: FrameKindNack},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Peer{
				Metrics: NewMetrics(),
				Role:    RoleScheduler,
				Buffer:  1,
				Handler: func(context.Context, *Peer, Message) error { return tt.handlerErr },
			}
			p.lazyInit()
			p.SetPlane(PlaneControl)

			p.processMessage(t.Context(), 1, TaskAssignMessage{})

			queued := <-p.outgoing
			require.Equal(t, tt.wantKind, queued.frame.FrameKind())
			require.Equal(t, "TaskAssign", queued.messageType)
		})
	}
}

func TestClassifyClientOutcome(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "ack", err: nil, want: messageOutcomeAck},
		{name: "timeout", err: context.DeadlineExceeded, want: messageOutcomeTimeout},
		{name: "canceled", err: context.Canceled, want: messageOutcomeCanceled},
		{name: "connection closed", err: ErrConnClosed, want: messageOutcomeConnClosed},
		{name: "nack", err: Errorf(http.StatusTooManyRequests, "busy"), want: messageOutcomeNack},
		{name: "send error", err: errors.New("write failed"), want: messageOutcomeSendError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyClientOutcome(tt.err))
		})
	}
}

func TestClassifyHandlerOutcome(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "ack", err: nil, want: messageOutcomeAck},
		{name: "unsupported", err: Errorf(http.StatusNotImplemented, "unsupported"), want: messageOutcomeUnsupported},
		{name: "handler error", err: errors.New("failed"), want: messageOutcomeHandlerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, classifyHandlerOutcome(tt.err))
		})
	}
}
