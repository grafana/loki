package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// streamSink allows for sending records remotely across a stream.
type streamSink struct {
	Logger log.Logger
	// Metrics is required and records stream send/bind/status metrics.
	Metrics     *metrics
	WireMetrics *wire.Metrics
	Scheduler   *wire.Peer
	Stream      *workflow.Stream
	Dialer      func(ctx context.Context, addr net.Addr) (wire.Conn, error)

	initOnce  sync.Once
	ctx       context.Context    // Context used for peer connections.
	cancel    context.CancelFunc // Cancel function for peer connections.
	bound     chan struct{}
	closeOnce sync.Once

	bindOnce    sync.Once
	destination net.Addr

	destConnMut sync.Mutex
	destConn    *wire.Peer
}

// Bind informs the sink about the address to send stream data to. Calls to Bind
// after the first will return an error.
func (sink *streamSink) Bind(ctx context.Context, destination net.Addr) error {
	sink.lazyInit()

	var bound bool
	sink.bindOnce.Do(func() {
		bound = true

		// Best-effort inform the scheduler that we're ready to send data.
		err := sink.Scheduler.SendMessageAsync(ctx, wire.StreamStatusMessage{
			StreamID: sink.Stream.ULID,
			State:    workflow.StreamStateOpen,
		})
		sink.Metrics.streamStatusTotal.WithLabelValues(messageDirectionSent, classifyMessageOutcome(err)).Inc()

		sink.destination = destination
		close(sink.bound) // Wake up any Send goroutines
	})

	if !bound {
		return errors.New("stream destination already bound")
	}
	return nil
}

func (sink *streamSink) lazyInit() {
	sink.initOnce.Do(func() {
		sink.ctx, sink.cancel = context.WithCancel(context.Background())

		sink.bound = make(chan struct{})
	})
}

// Send sends a record to the remote side of the stream.
//
// Calls to Send block until:
//
// - There is a bound address for the destination.
// - The record has been sent successfully to the destination.
//
// Send will attempt to re-establish connection to the destination if the
// connection is lost.
//
// Send can be aborted by cancelling the provided context.
func (sink *streamSink) Send(ctx context.Context, rec arrow.RecordBatch) (err error) {
	sink.lazyInit()

	start := time.Now()
	defer func() {
		outcome := classifyStreamOutcome(err, streamOutcomeSent)
		sink.Metrics.streamDataSendSeconds.WithLabelValues(streamPhaseTotal, outcome).Observe(time.Since(start).Seconds())
		sink.Metrics.streamDataSentTotal.WithLabelValues(outcome).Inc()
		if err == nil {
			sink.Metrics.streamDataSentBatchesTotal.Inc()
			sink.Metrics.streamDataSentBytesTotal.Add(float64(recordBatchBytes(rec)))
		}
	}()

	bo := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 1 * time.Second,
	})

	for bo.Ongoing() {
		// We only want to retry on errors about the connection closing; errors
		// where the peer rejected our payload should be considered
		// nonretryable.
		err = sink.send(ctx, rec)
		if err == nil {
			break
		}

		if !sink.isRetryable(err) {
			level.Error(sink.Logger).Log("msg", "failed to send data to peer. Encountered non-retryable error", "err", err)
			return err
		}

		level.Warn(sink.Logger).Log("msg", "failed to send data to peer", "err", err)
		sink.waitRetry(bo)
	}

	return bo.Err()
}

// waitRetry counts a stream-data retry, waits for the backoff to elapse, and
// records how long the backoff took.
func (sink *streamSink) waitRetry(bo *backoff.Backoff) {
	sink.Metrics.streamDataRetriesTotal.WithLabelValues(streamOutcomeConnClosed).Inc()

	start := time.Now()
	bo.Wait()
	sink.Metrics.streamDataSendSeconds.WithLabelValues(streamPhaseRetryBackoff, classifyStreamOutcome(bo.Err(), streamOutcomeSent)).Observe(time.Since(start).Seconds())
}

func (sink *streamSink) send(ctx context.Context, rec arrow.RecordBatch) error {
	peer, err := sink.getPeer(ctx)
	if err != nil {
		return fmt.Errorf("connecting to peer: %w", err)
	}

	// TODO(rfratto): We should send a Blocked status update to the scheduler if
	// SendMessage doesn't finish quickly enough.
	//
	// We need to find a way to efficiently do that here that doesn't cancel the
	// send.
	err = peer.SendMessage(ctx, wire.StreamDataMessage{
		StreamID: sink.Stream.ULID,
		Data:     rec,
	})
	if err != nil {
		return fmt.Errorf("sending data to peer: %w", err)
	}

	return nil
}

func (sink *streamSink) getPeer(ctx context.Context) (*wire.Peer, error) {
	// Wait for destination.
	bindWaitStart := time.Now()
	select {
	case <-ctx.Done():
		sink.Metrics.streamDataSendSeconds.WithLabelValues(streamPhaseBindWait, classifyStreamOutcome(ctx.Err(), streamOutcomeSent)).Observe(time.Since(bindWaitStart).Seconds())
		return nil, ctx.Err()
	case <-sink.ctx.Done():
		sink.Metrics.streamDataSendSeconds.WithLabelValues(streamPhaseBindWait, streamOutcomeConnClosed).Observe(time.Since(bindWaitStart).Seconds())
		return nil, wire.ErrConnClosed
	case <-sink.bound:
		sink.Metrics.streamDataSendSeconds.WithLabelValues(streamPhaseBindWait, streamOutcomeSent).Observe(time.Since(bindWaitStart).Seconds())
	}

	sink.destConnMut.Lock()
	defer sink.destConnMut.Unlock()

	if sink.destConn != nil {
		return sink.destConn, nil
	}

	dialStart := time.Now()
	conn, err := sink.Dialer(ctx, sink.destination)
	sink.Metrics.streamDataSendSeconds.WithLabelValues(streamPhaseDial, classifyStreamOutcome(err, streamOutcomeSent)).Observe(time.Since(dialStart).Seconds())
	if err != nil {
		return nil, err
	}

	peer := &wire.Peer{
		Logger:  sink.Logger,
		Metrics: sink.WireMetrics,
		Conn:    conn,
		Role:    wire.RoleWorker,
		Handler: nil, // This is a send-only connection.
	}
	peer.SetPlane(wire.PlaneData)

	go func() {
		if err := peer.Serve(sink.ctx); err != nil && errors.Is(err, context.Canceled) {
			level.Warn(sink.Logger).Log("msg", "stream sink peer closed", "err", err)
		}

		// Clear out the cached connection so the next call to getPeer can
		// create a new one.
		sink.destConnMut.Lock()
		defer sink.destConnMut.Unlock()

		sink.destConn = nil
	}()

	sink.destConn = peer
	return peer, nil
}

// isRetryable checks if the error is retryable:
//
//   - Connections closed to the peer can be retried
func (sink *streamSink) isRetryable(err error) bool {
	return errors.Is(err, wire.ErrConnClosed)
}

// Close closes the sink.
func (sink *streamSink) Close(ctx context.Context) error {
	sink.lazyInit()

	var err error

	sink.closeOnce.Do(func() {
		sink.cancel()

		// Best-effort inform the scheduler that we're done sending data.
		err = sink.Scheduler.SendMessageAsync(ctx, wire.StreamStatusMessage{
			StreamID: sink.Stream.ULID,
			State:    workflow.StreamStateClosed,
		})
		sink.Metrics.streamStatusTotal.WithLabelValues(messageDirectionSent, classifyMessageOutcome(err)).Inc()
	})

	return err
}
