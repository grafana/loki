package wire

import (
	"context"
	"errors"
	"net"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/engine/internal/metrictimer"
)

// Peer wraps a [Conn] into a synchronous API that acts as both a
// server and a client.
//
// Callers must call [Peer.Serve] to run the peer.
type Peer struct {
	Logger  log.Logger
	Metrics *Metrics
	Conn    Conn    // Connection to use for communication.
	Handler Handler // Handler for incoming messages from the remote peer.
	Buffer  int     // Buffer size for incoming and outgoing messages.

	done     chan struct{}        // Closed when the peer connection is closed.
	incoming chan incomingMessage // Buffered queue of incoming messages.
	outgoing chan outgoingFrame   // Buffered queue of outgoing frames.
	initOnce sync.Once

	// Outbound frames are held in an unbounded queue drained by handleOutgoing,
	// rather than a bounded channel. enqueueFrame is called by message handlers
	// running on the incoming goroutine; if it could block (as a full bounded
	// channel would), incoming processing would stall and the duplex connection
	// could deadlock once both peers' outbound buffers fill. A non-blocking
	// enqueue guarantees receiving never waits on sending.
	outMu    sync.Mutex
	outQueue []Frame
	outWake  chan struct{} // Buffered(1) signal that outQueue is non-empty.

	requestID    atomic.Uint64
	sentRequests sync.Map // map[uint64]*request
}

// outgoingFrame wraps a Frame queued for sending with the metadata needed to
// attribute its time spent waiting in the outgoing queue.
type outgoingFrame struct {
	frame      Frame
	sendMode   sendMode
	enqueuedAt time.Time
}

// incomingMessage wraps a received MessageFrame with the time it was enqueued
// so the incoming-queue wait can be measured.
type incomingMessage struct {
	frame      MessageFrame
	enqueuedAt time.Time
}

// Handler is a function that handles a message received from the peer. The
// local peer is passed as an argument to allow using the same Handler for
// multiple Peers.
//
// Handlers are invoked in a dedicated goroutine. Slow handlers cause
// backpressure on the connection.
//
// Once Handler returns, the sending peer will be informed about the
// message delivery status. If Handler returns an error, the error
// message will be sent to the peer.
type Handler func(ctx context.Context, peer *Peer, message Message) error

// Serve runs the peer, blocking until the provided context is canceled.
func (p *Peer) Serve(ctx context.Context) error {
	p.lazyInit()
	p.Conn.setMetrics(p.Metrics)

	transport := p.Conn.transport()
	p.Metrics.markActive(transport)
	defer p.Metrics.markInactive(transport)

	// Defer connection close here in Serve since Peer does not have an explicit Close method.
	defer p.Conn.Close()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return p.recvMessages(ctx) })
	g.Go(func() error { return p.handleIncoming(ctx) })
	g.Go(func() error { return p.handleOutgoing(ctx) })

	return g.Wait()
}

func (p *Peer) lazyInit() {
	p.initOnce.Do(func() {
		p.done = make(chan struct{})
		p.incoming = make(chan incomingMessage, p.Buffer)
		p.outgoing = make(chan outgoingFrame, p.Buffer)
		p.outWake = make(chan struct{}, 1)
	})
}

func (p *Peer) recvMessages(ctx context.Context) error {
	defer close(p.done)

	for {
		frame, err := p.Conn.Recv(ctx)
		if err != nil {
			if ctx.Err() != nil {
				// Context got canceled; shut down
				return nil
			}
			return err
		}

		p.Metrics.incFrameReceived(frame)

		err = p.Metrics.timeFrameReceive(phaseRouteToQueue, p.Conn.transport(), frame, sendModeInternal, func() error {
			return p.routeFrame(ctx, frame)
		})
		if err != nil {
			return err
		}
	}
}

func (p *Peer) routeFrame(ctx context.Context, frame Frame) error {
	switch frame := frame.(type) {
	case MessageFrame:
		return p.enqueueIncoming(ctx, frame)

	case AckFrame:
		// If there's still a listener for this request, inform them of
		// the success.
		val, found := p.sentRequests.Load(frame.ID)
		if !found {
			return nil
		}
		req := val.(*request)

		select {
		case req.result <- nil:
		default:
			level.Warn(p.Logger).Log("msg", "ignoring duplicate acknowledgement")
		}

	case NackFrame:
		// If there's still a listener for this request, inform them of
		// the error.
		val, found := p.sentRequests.Load(frame.ID)
		if !found {
			return nil
		}
		req := val.(*request)

		select {
		case req.result <- frame.Error:
		default:
			level.Warn(p.Logger).Log("msg", "ignoring duplicate acknowledgement")
		}

	case DiscardFrame:
		// TODO(rfratto): cancel handleMessage goroutine

	default:
		level.Warn(p.Logger).Log("msg", "unknown frame type", "type", reflect.TypeOf(frame).String())
	}
	return nil
}

func (p *Peer) enqueueIncoming(ctx context.Context, frame MessageFrame) error {
	queued := incomingMessage{frame: frame, enqueuedAt: time.Now()}
	select {
	case p.incoming <- queued:
		p.noteFrameEnqueued(queueIncoming, frame, sendModeInternal)
		return nil
	case <-ctx.Done():
		return nil
	default:
	}

	done := p.noteQueueBlockedSender(queueIncoming, frame, sendModeInternal)
	defer done()

	select {
	case p.incoming <- queued:
		p.noteFrameEnqueued(queueIncoming, frame, sendModeInternal)
		return nil
	case <-ctx.Done():
		return nil
	}
}

func (p *Peer) handleIncoming(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.done:
			return nil // Closed connection.
		case queued := <-p.incoming:
			frame := p.dequeueIncoming(queued)
			p.processMessage(ctx, frame.ID, frame.Message)
		}
	}
}

func (p *Peer) handleOutgoing(ctx context.Context) error {
	for {
		p.outMu.Lock()
		batch := p.outQueue
		p.outQueue = nil
		p.outMu.Unlock()

		for _, frame := range batch {
			if err := p.Conn.Send(ctx, frame); err != nil && ctx.Err() == nil {
				level.Warn(p.Logger).Log("msg", "failed to send message", "error", err)
				p.notifyError(frame, err)
			}
		}

		// If we drained frames, loop again immediately to pick up anything
		// enqueued while we were sending (checking for shutdown first).
		if len(batch) > 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-p.done:
				return nil
			default:
			}
			continue
		}

		// Queue empty: wait for a wake-up or shutdown.
		select {
		case <-ctx.Done():
			return nil
		case <-p.done:
			return nil // Closed connection.
		case <-p.outWake:
		case queued := <-p.outgoing:
			frame, sendMode := p.dequeueOutgoing(queued)
			if err := p.Conn.sendFrame(ctx, frame, sendMode); err != nil && ctx.Err() == nil {
				level.Warn(p.Logger).Log("msg", "failed to send message", "error", err)
				p.notifyError(frame, err)
			}
		}
	}
}

func (p *Peer) dequeueIncoming(queued incomingMessage) MessageFrame {
	p.noteFrameDequeued(queueIncoming, queued.frame, sendModeInternal, queued.enqueuedAt)
	return queued.frame
}

func (p *Peer) dequeueOutgoing(queued outgoingFrame) (Frame, sendMode) {
	p.noteFrameDequeued(queueOutgoing, queued.frame, queued.sendMode, queued.enqueuedAt)
	return queued.frame, queued.sendMode
}

func (p *Peer) noteFrameEnqueued(queue queueName, frame Frame, sendMode sendMode) {
	p.Metrics.incFrameQueued(queue, frame, sendMode)
}

func (p *Peer) noteFrameDequeued(queue queueName, frame Frame, sendMode sendMode, enqueuedAt time.Time) {
	p.Metrics.decFrameQueued(queue, frame, sendMode)
	p.Metrics.observeFrameQueueWait(queue, frame, sendMode, time.Since(enqueuedAt))
}

func (p *Peer) noteQueueBlockedSender(queue queueName, frame Frame, sendMode sendMode) func() {
	return p.Metrics.noteQueueBlockedSender(queue, frame, sendMode)
}

// notifyError notifies any request listeners of a frame that an error occurred
// during delivery.
func (p *Peer) notifyError(frame Frame, err error) {
	switch frame := frame.(type) {
	case MessageFrame:
		val, found := p.sentRequests.Load(frame.ID)
		if !found {
			return
		}
		req := val.(*request)

		select {
		case <-p.done: // Connection closed
		case req.result <- err:
		default:
			level.Warn(p.Logger).Log("msg", "ignoring duplicate acknowledgement")
		}

	default:
		// Other frame types don't have listeners (at the moment) so there's nobody
		// to notify.
	}
}

// processMessage handles a message received from the peer.
func (p *Peer) processMessage(ctx context.Context, id uint64, message Message) {
	messageType := "unknown"
	if message != nil {
		messageType = message.Kind().String()
	}

	timer := p.Metrics.startPeerHandler(messageType)
	outcome := outcomeNack
	defer func() { timer.Done(outcome) }()

	if p.Handler == nil {
		_ = p.enqueueFrame(ctx, NackFrame{ID: id, Error: Errorf(http.StatusNotImplemented, "not implemented")}, sendModeInternal)
		return
	}

	switch err := p.Handler(ctx, p, message); err {
	case nil:
		outcome = outcomeAck
		// TODO(rfratto): What should we do if this fails? Logs? Metrics?
		_ = p.enqueueFrame(ctx, AckFrame{ID: id}, sendModeInternal)
	default:
		// TODO(rfratto): What should we do if this fails? Logs? Metrics?
		_ = p.enqueueFrame(ctx, NackFrame{ID: id, Error: convertError(err)}, sendModeInternal)
	}
}

func convertError(err error) *Error {
	var wireError *Error
	if errors.As(err, &wireError) {
		return wireError
	}

	return &Error{
		Code:    http.StatusInternalServerError,
		Message: err.Error(),
	}
}

type request struct {
	result chan error
}

// SendMessage sends a message to the remote peer. SendMessage blocks until the
// provided context is canceled or the remote peer positively or negatively
// acknowledges the message.
//
// [Peer.Serve] must be running when SendMessage is called, otherwise it blocks
// until the context is canceled.
func (p *Peer) SendMessage(ctx context.Context, message Message) error {
	p.lazyInit()

	reqID := p.requestID.Inc()
	req := &request{
		result: make(chan error, 1),
	}
	p.sentRequests.Store(reqID, req)
	defer p.sentRequests.Delete(reqID)

	roundtrip := p.Metrics.startMessageRoundtrip(message.Kind(), sendModeSync)
	outcome := outcomeSendError
	defer func() { roundtrip.Done(outcome) }()
	p.Metrics.incMessageSent(message.Kind(), sendModeSync)

	if err := p.enqueueFrame(ctx, MessageFrame{ID: reqID, Message: message}, sendModeSync); err != nil {
		outcome = sendErrorOutcome(err)
		return err
	}

	select {
	case <-ctx.Done():
		// TODO(rfratto): queue a DiscardFrame
		outcome = contextOutcome(ctx)
		return ctx.Err()
	case <-p.done:
		outcome = outcomeConnClosed
		return ErrConnClosed
	case err := <-req.result:
		outcome = resultOutcome(err)
		return err
	}
}

// contextOutcome classifies a canceled context into a round-trip outcome.
func contextOutcome(ctx context.Context) metrictimer.Outcome {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return outcomeTimeout
	}
	return outcomeCanceled
}

// sendErrorOutcome classifies an enqueue error into a round-trip outcome.
func sendErrorOutcome(err error) metrictimer.Outcome {
	switch {
	case errors.Is(err, ErrConnClosed):
		return outcomeConnClosed
	case errors.Is(err, context.DeadlineExceeded):
		return outcomeTimeout
	case errors.Is(err, context.Canceled):
		return outcomeCanceled
	default:
		return outcomeSendError
	}
}

// resultOutcome classifies the result delivered to a request into a round-trip
// outcome. A nil result is an ack, a wire [*Error] is a nack from the remote
// handler, and any other error is a local delivery failure.
func resultOutcome(err error) metrictimer.Outcome {
	if err == nil {
		return outcomeAck
	}
	var wireErr *Error
	if errors.As(err, &wireErr) {
		return outcomeNack
	}
	return outcomeSendError
}

// SendMessageAsync sends a message to the remote peer asynchronously.
// SendMessageAsync blocks until the message has been sent over the connection
// but does not wait for an acknowledgement or response.
//
// [Peer.Serve] must be running before SendMessageAsync is called, otherwise it
// blocks until the context is canceled.
func (p *Peer) SendMessageAsync(ctx context.Context, message Message) error {
	p.lazyInit()

	reqID := p.requestID.Inc()
	p.Metrics.incMessageSent(message.Kind(), sendModeAsync)
	return p.enqueueFrame(ctx, MessageFrame{ID: reqID, Message: message}, sendModeAsync)
}

// enqueueFrame enqueues a frame to be sent to the remote peer.
// enqueueFrame appends a frame to the unbounded outbound queue and signals
// handleOutgoing. It never blocks on a slow or backed-up peer. This is
// essential: enqueueFrame runs on the incoming/handler goroutine, so blocking
// here would stall incoming processing and can deadlock the duplex connection
// when both peers' outbound buffers fill (e.g. under a stream-status /
// cancellation storm). Backpressure is handled at the protocol layer
// (WorkerReady / 429), not by blocking raw frame delivery.
func (p *Peer) enqueueFrame(ctx context.Context, frame Frame, sendMode sendMode) error {
	queued := outgoingFrame{frame: frame, sendMode: sendMode, enqueuedAt: time.Now()}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return ErrConnClosed
	case p.outgoing <- queued:
		p.noteFrameEnqueued(queueOutgoing, frame, sendMode)
		return nil
	default:
	}

	done := p.noteQueueBlockedSender(queueOutgoing, frame, sendMode)
	defer done()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return ErrConnClosed
	case p.outgoing <- queued:
		p.noteFrameEnqueued(queueOutgoing, frame, sendMode)
		return nil
	}

	p.outMu.Lock()
	p.outQueue = append(p.outQueue, frame)
	p.outMu.Unlock()

	select {
	case p.outWake <- struct{}{}:
	default:
	}
	return nil
}

// LocalAddr returns the address of the local peer.
func (p *Peer) LocalAddr() net.Addr { return p.Conn.LocalAddr() }

// RemoteAddr returns the address of the remote peer.
func (p *Peer) RemoteAddr() net.Addr { return p.Conn.RemoteAddr() }
