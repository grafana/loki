package wire

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/metrictimer"
)

func TestPeer_Metrics_Roundtrip(t *testing.T) {
	tt := []struct {
		name        string
		handlerErr  error
		wantOutcome metrictimer.Outcome
	}{
		{name: "ack", handlerErr: nil, wantOutcome: outcomeAck},
		{name: "nack", handlerErr: Errorf(429, "busy"), wantOutcome: outcomeNack},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			clientMetrics := NewMetrics()
			client := &Peer{Logger: log.NewNopLogger(), Metrics: clientMetrics, Buffer: 4}

			server := &Peer{
				Logger:  log.NewNopLogger(),
				Metrics: NewMetrics(),
				Buffer:  4,
				Handler: func(_ context.Context, _ *Peer, _ Message) error { return tc.handlerErr },
			}

			peerPair(ctx, t, client, server)

			err := client.SendMessage(ctx, WorkerReadyMessage{})
			if tc.handlerErr == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}

			kind := WorkerReadyMessage{}.Kind().String()

			// Round trip recorded once under the right outcome, mode="sync".
			require.Equal(t, uint64(1), histogramCount(t, clientMetrics.reg,
				"loki_engine_scheduler_wire_message_roundtrip_seconds",
				map[string]string{"message_type": kind, "outcome": tc.wantOutcome.String(), "mode": sendModeSync.String()}))

			// Deprecated alias is removed.
			require.Nil(t, gatherFamily(t, clientMetrics.reg, "loki_engine_scheduler_wire_message_rtt_seconds"))

			// Send counted once as a mode send.
			require.Equal(t, float64(1), counterValue(t, clientMetrics.reg,
				"loki_engine_scheduler_wire_messages_sent_total",
				map[string]string{"message_type": kind, "mode": sendModeSync.String()}))

			// Both queues drain back to empty once the round trip completes.
			require.Eventually(t, func() bool {
				return gaugeSum(t, clientMetrics.reg, "loki_engine_scheduler_wire_frame_queue_length") == 0
			}, 5*time.Second, 10*time.Millisecond, "client queues should drain")
		})
	}
}

func TestPeer_Metrics_AsyncSend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientMetrics := NewMetrics()
	client := &Peer{Logger: log.NewNopLogger(), Metrics: clientMetrics, Buffer: 4}

	handled := make(chan struct{}, 1)
	server := &Peer{
		Logger:  log.NewNopLogger(),
		Metrics: NewMetrics(),
		Buffer:  4,
		Handler: func(_ context.Context, _ *Peer, _ Message) error {
			handled <- struct{}{}
			return nil
		},
	}

	peerPair(ctx, t, client, server)

	require.NoError(t, client.SendMessageAsync(ctx, WorkerReadyMessage{}))

	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("server never handled async message")
	}

	kind := WorkerReadyMessage{}.Kind().String()

	// Async sends are counted, with mode="async", and never record a round trip.
	require.Equal(t, float64(1), counterValue(t, clientMetrics.reg,
		"loki_engine_scheduler_wire_messages_sent_total",
		map[string]string{"message_type": kind, "mode": sendModeAsync.String()}))
	require.Equal(t, 0, collectCount(t, clientMetrics.reg, "loki_engine_scheduler_wire_message_roundtrip_seconds"))
}

func TestPeer_Metrics_RoundtripOutcomeClassifiers(t *testing.T) {
	deadlineCtx, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	canceledCtx, cancelCanceled := context.WithCancel(context.Background())
	cancelCanceled()

	require.Equal(t, outcomeTimeout, contextOutcome(deadlineCtx))
	require.Equal(t, outcomeCanceled, contextOutcome(canceledCtx))

	require.Equal(t, outcomeConnClosed, sendErrorOutcome(ErrConnClosed))
	require.Equal(t, outcomeTimeout, sendErrorOutcome(context.DeadlineExceeded))
	require.Equal(t, outcomeCanceled, sendErrorOutcome(context.Canceled))
	require.Equal(t, outcomeSendError, sendErrorOutcome(errors.New("send failed")))

	require.Equal(t, outcomeAck, resultOutcome(nil))
	require.Equal(t, outcomeNack, resultOutcome(Errorf(429, "busy")))
	require.Equal(t, outcomeSendError, resultOutcome(errors.New("local delivery failed")))
}

func TestPeer_Metrics_SendMessageFailureOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		setup       func(*Peer) context.Context
		wantErr     error
		wantOutcome metrictimer.Outcome
	}{
		{
			name: "timeout",
			setup: func(*Peer) context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
				<-ctx.Done()
				cancel()
				return ctx
			},
			wantErr:     context.DeadlineExceeded,
			wantOutcome: outcomeTimeout,
		},
		{
			name: "canceled",
			setup: func(*Peer) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantErr:     context.Canceled,
			wantOutcome: outcomeCanceled,
		},
		{
			name: "connection closed",
			setup: func(p *Peer) context.Context {
				p.lazyInit()
				close(p.done)
				return context.Background()
			},
			wantErr:     ErrConnClosed,
			wantOutcome: outcomeConnClosed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metrics := NewMetrics()
			peer := &Peer{Logger: log.NewNopLogger(), Metrics: metrics, Buffer: 0}
			ctx := tc.setup(peer)

			err := peer.SendMessage(ctx, WorkerReadyMessage{})
			require.ErrorIs(t, err, tc.wantErr)

			require.Equal(t, uint64(1), histogramCount(t, metrics.reg,
				"loki_engine_scheduler_wire_message_roundtrip_seconds",
				map[string]string{
					"message_type": WorkerReadyMessage{}.Kind().String(),
					"outcome":      tc.wantOutcome.String(),
					"mode":         sendModeSync.String(),
				}))
		})
	}
}

func TestPeer_Metrics_ActiveConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clientMetrics := NewMetrics()
	client := &Peer{Logger: log.NewNopLogger(), Metrics: clientMetrics, Buffer: 4}
	server := &Peer{Logger: log.NewNopLogger(), Metrics: NewMetrics(), Buffer: 4}

	peerPair(ctx, t, client, server)

	require.Eventually(t, func() bool {
		return gaugeValue(t, clientMetrics.reg,
			"loki_engine_scheduler_wire_connections_active",
			map[string]string{"transport": transportLocal.String()}) == 1
	}, 5*time.Second, 10*time.Millisecond, "client connection should be marked active")

	cancel()

	require.Eventually(t, func() bool {
		return gaugeValue(t, clientMetrics.reg,
			"loki_engine_scheduler_wire_connections_active",
			map[string]string{"transport": transportLocal.String()}) == 0
	}, 5*time.Second, 10*time.Millisecond, "client connection should be marked inactive")
}

func TestPeer_Metrics_BlockedOutgoingQueue(t *testing.T) {
	metrics := NewMetrics()
	peer := &Peer{Logger: log.NewNopLogger(), Metrics: metrics, Buffer: 1}
	peer.lazyInit()

	frame := MessageFrame{ID: 1, Message: WorkerReadyMessage{}}
	peer.outgoing <- outgoingFrame{frame: frame, sendMode: sendModeAsync, enqueuedAt: time.Now()}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- peer.SendMessageAsync(ctx, WorkerReadyMessage{})
	}()

	labels := map[string]string{
		"queue":        queueOutgoing.String(),
		"frame_type":   FrameKindMessage.String(),
		"message_type": WorkerReadyMessage{}.Kind().String(),
		"mode":         sendModeAsync.String(),
	}
	require.Eventually(t, func() bool {
		return gaugeValue(t, metrics.reg, "loki_engine_scheduler_wire_queue_blocked_senders", labels) == 1
	}, 5*time.Second, 10*time.Millisecond, "sender should be blocked by the full outgoing queue")

	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
	require.Eventually(t, func() bool {
		return gaugeValue(t, metrics.reg, "loki_engine_scheduler_wire_queue_blocked_senders", labels) == 0
	}, 5*time.Second, 10*time.Millisecond, "blocked-sender gauge should drain")
}

func TestPeer_Metrics_LocalRoundTripFrameLabels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		clientMetrics := NewMetrics()
		client := &Peer{Logger: log.NewNopLogger(), Metrics: clientMetrics, Buffer: 4}
		server := &Peer{
			Logger:  log.NewNopLogger(),
			Metrics: NewMetrics(),
			Buffer:  4,
			Handler: func(context.Context, *Peer, Message) error { return nil },
		}
		peerPair(ctx, t, client, server)

		require.NoError(t, client.SendMessage(ctx, WorkerReadyMessage{}))

		// Wait for the outgoing goroutine to record the send-side metric.
		synctest.Wait()

		messageType := WorkerReadyMessage{}.Kind().String()
		// A local send times only the connection total (the dropped
		// local_channel_send phase equalled it), which also feeds write_busy.
		require.Equal(t, uint64(1), histogramCount(t, clientMetrics.reg,
			"loki_engine_scheduler_wire_frame_send_seconds",
			map[string]string{
				"phase":        phaseConnSendTotal.String(),
				"transport":    transportLocal.String(),
				"frame_type":   FrameKindMessage.String(),
				"message_type": messageType,
				"mode":         sendModeSync.String(),
			}))
		require.Equal(t, uint64(1), histogramCount(t, clientMetrics.reg,
			"loki_engine_scheduler_wire_frame_receive_seconds",
			map[string]string{
				"phase":        phaseLocalChannelReceive.String(),
				"transport":    transportLocal.String(),
				"frame_type":   FrameKindAck.String(),
				"message_type": "none",
				"mode":         sendModeInternal.String(),
			}))
	})
}

// TestHTTP2WriteBusyExcludesLockWait proves that the write-lock wait on an
// HTTP/2 send lands in the conn_write wire lock metric but not in
// write_busy_seconds_total, which measures only the actual write path.
func TestHTTP2WriteBusyExcludesLockWait(t *testing.T) {
	metrics := NewMetrics()
	ctx := context.Background()

	conn := newHTTP2Conn(ctx, LocalScheduler, LocalWorker, io.NopCloser(&bytes.Buffer{}), io.Discard, nil, DefaultFrameCodec)
	conn.setMetrics(metrics)

	// Hold the write lock so the send has to wait to acquire it.
	hold := conn.writeMu.Lock("test_hold")

	const lockWait = 50 * time.Millisecond
	sendErr := make(chan error, 1)
	go func() { sendErr <- conn.sendFrame(ctx, AckFrame{ID: 1}, sendModeInternal) }()

	time.Sleep(lockWait)
	hold.Unlock()
	require.NoError(t, <-sendErr)

	// The wait to acquire writeMu is attributed to the wire lock metric.
	waitLabels := map[string]string{"lock": "conn_write", "mode": "write", "reason": "send_frame"}
	require.Equal(t, uint64(1), histogramCount(t, metrics.reg, "loki_engine_scheduler_wire_lock_wait_seconds", waitLabels))
	lockWaitSum := histogramSum(t, metrics.reg, "loki_engine_scheduler_wire_lock_wait_seconds", waitLabels)
	require.Greater(t, lockWaitSum, lockWait.Seconds()/2, "the writeMu acquisition wait should be recorded on the wire lock metric")

	// write_busy is only the actual write path (serialize + write + flush), so
	// it must exclude the lock wait entirely.
	busy := counterValue(t, metrics.reg, "loki_engine_scheduler_wire_write_busy_seconds_total", map[string]string{"transport": transportHTTP2.String()})
	require.Less(t, busy, lockWaitSum, "write_busy must exclude the writeMu acquisition wait")
}

// peerPair wires two Peers together over an in-process Local connection and
// runs both until the test's context is canceled.
func peerPair(ctx context.Context, t *testing.T, client *Peer, server *Peer) {
	t.Helper()

	listener := &Local{Address: LocalScheduler}

	accepted := make(chan Conn, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err != nil {
			return
		}
		accepted <- conn
	}()

	clientConn, err := listener.DialFrom(ctx, LocalWorker)
	require.NoError(t, err)

	var serverConn Conn
	select {
	case serverConn = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server connection")
	}

	client.Conn = clientConn
	server.Conn = serverConn

	go func() { _ = client.Serve(ctx) }()
	go func() { _ = server.Serve(ctx) }()
}

func gatherFamily(t *testing.T, reg *prometheus.Registry, name string) *dto.MetricFamily {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, fam := range families {
		if fam.GetName() == name {
			return fam
		}
	}
	return nil
}

// labelsMatch reports whether metric carries exactly the wanted label set and
// no others. Exact matching means an accidentally added label (for example a
// high-cardinality one) fails the lookup instead of being silently ignored.
func labelsMatch(metric *dto.Metric, want map[string]string) bool {
	if len(metric.GetLabel()) != len(want) {
		return false
	}
	for _, lp := range metric.GetLabel() {
		if want[lp.GetName()] != lp.GetValue() {
			return false
		}
	}
	return true
}

func histogramCount(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) uint64 {
	t.Helper()
	fam := gatherFamily(t, reg, name)
	if fam == nil {
		return 0
	}
	for _, m := range fam.GetMetric() {
		if labelsMatch(m, labels) {
			return m.GetHistogram().GetSampleCount()
		}
	}
	return 0
}

func histogramSum(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	fam := gatherFamily(t, reg, name)
	if fam == nil {
		return 0
	}
	for _, m := range fam.GetMetric() {
		if labelsMatch(m, labels) {
			return m.GetHistogram().GetSampleSum()
		}
	}
	return 0
}

func counterValue(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	fam := gatherFamily(t, reg, name)
	if fam == nil {
		return 0
	}
	for _, m := range fam.GetMetric() {
		if labelsMatch(m, labels) {
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

func gaugeValue(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	fam := gatherFamily(t, reg, name)
	if fam == nil {
		return 0
	}
	for _, m := range fam.GetMetric() {
		if labelsMatch(m, labels) {
			return m.GetGauge().GetValue()
		}
	}
	return 0
}

func gaugeSum(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	fam := gatherFamily(t, reg, name)
	if fam == nil {
		return 0
	}
	var sum float64
	for _, m := range fam.GetMetric() {
		sum += m.GetGauge().GetValue()
	}
	return sum
}

func collectCount(t *testing.T, reg *prometheus.Registry, name string) int {
	t.Helper()
	fam := gatherFamily(t, reg, name)
	if fam == nil {
		return 0
	}
	return len(fam.GetMetric())
}
