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

	// Role is the role of the local peer for communication metrics. It is set at
	// construction, immutable once Serve runs, and defaults to RoleUnknown.
	Role Role

	done     chan struct{}      // Closed when the peer connection is closed.
	incoming chan queuedMessage // Buffered frame of incoming messages.
	outgoing chan queuedFrame   // Buffered frame of outgoing frames.
	initOnce sync.Once

	// plane is the communication plane for metrics. Unlike Role, a peer's plane
	// is often not known until its first message arrives, so it is discovered at
	// runtime through SetPlane rather than set at construction.
	plane atomic.String

	requestID    atomic.Uint64
	sentRequests sync.Map // map[uint64]*request
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
		p.incoming = make(chan queuedMessage, p.Buffer)
		p.outgoing = make(chan queuedFrame, p.Buffer)
	})
}

// SetPlane updates the communication plane label used for future peer metrics.
// A peer's plane is often not known until its first message arrives.
func (p *Peer) SetPlane(plane Plane) {
	p.plane.Store(string(plane))
}

// labels returns the role and plane labels for the peer's metrics. Role is
// immutable; plane may have been updated by SetPlane.
func (p *Peer) labels() (Role, Plane) {
	role := p.Role
	if role == "" {
		role = RoleUnknown
	}
	plane := Plane(p.plane.Load())
	if plane == "" {
		plane = PlaneUnknown
	}
	return role, plane
}

func (p *Peer) inferPlaneFromMessage(message Message) {
	// Frame traffic is recorded before the application handler runs. Classify
	// new peers from the first message frame so those bytes don't land on the
	// unknown plane; already-classified peers keep their existing plane.
	_, plane := p.labels()
	if plane != PlaneUnknown || message == nil {
		return
	}

	switch message.Kind() {
	case MessageKindStreamData:
		p.SetPlane(PlaneData)
	default:
		p.SetPlane(PlaneControl)
	}
}

type queuedMessage struct {
	frame      MessageFrame
	role       Role
	plane      Plane
	enqueuedAt time.Time
}

type queuedFrame struct {
	frame       Frame
	role        Role
	plane       Plane
	messageType string
	enqueuedAt  time.Time
}

type sizedSender interface {
	sendWithSize(context.Context, Frame) (int, error)
}

type sizedReceiver interface {
	recvWithSize(context.Context) (Frame, int, error)
}

func (p *Peer) sendFrame(ctx context.Context, frame Frame) (int, bool, error) {
	if conn, ok := p.Conn.(sizedSender); ok {
		size, err := conn.sendWithSize(ctx, frame)
		return size, true, err
	}

	return 0, false, p.Conn.Send(ctx, frame)
}

func (p *Peer) recvFrame(ctx context.Context) (Frame, int, bool, error) {
	if conn, ok := p.Conn.(sizedReceiver); ok {
		frame, size, err := conn.recvWithSize(ctx)
		return frame, size, true, err
	}

	frame, err := p.Conn.Recv(ctx)
	return frame, 0, false, err
}

func (p *Peer) recvMessages(ctx context.Context) error {
	defer close(p.done)

	for {
		frame, frameSize, hasFrameSize, err := p.recvFrame(ctx)
		if err != nil {
			if ctx.Err() != nil {
				// Context got canceled; shut down
				return nil
			}
			return err
		}

		p.Metrics.incFrameReceived(frame)

		switch frame := frame.(type) {
		case MessageFrame:
			p.inferPlaneFromMessage(frame.Message)
			role, plane := p.labels()
			messageType := frameMessageType(frame)
			if hasFrameSize {
				p.Metrics.recordFrame(role, plane, messageDirectionReceived, frame, messageType, frameSize)
			}

			// Queue the message for processing.
			p.Metrics.incMessageQueued()
			p.Metrics.incQueueDepth(role, plane, queueDirectionIncoming)
			select {
			case p.incoming <- queuedMessage{frame: frame, role: role, plane: plane, enqueuedAt: time.Now()}:
			case <-ctx.Done():
				p.Metrics.decMessageQueued()
				p.Metrics.decQueueDepth(role, plane, queueDirectionIncoming)
				return nil
			}

		case AckFrame:
			role, plane := p.labels()
			// If there's still a listener for this request, inform them of
			// the success.
			var req *request
			messageType := "none"
			if val, found := p.sentRequests.Load(frame.ID); found {
				req = val.(*request)
				messageType = req.messageType
			}
			if hasFrameSize {
				p.Metrics.recordFrame(role, plane, messageDirectionReceived, frame, messageType, frameSize)
			}
			if req == nil {
				continue
			}

			select {
			case req.result <- nil:
			default:
				level.Warn(p.Logger).Log("msg", "ignoring duplicate acknowledgement")
			}

		case NackFrame:
			role, plane := p.labels()
			// If there's still a listener for this request, inform them of
			// the error.
			var req *request
			messageType := "none"
			if val, found := p.sentRequests.Load(frame.ID); found {
				req = val.(*request)
				messageType = req.messageType
			}
			if hasFrameSize {
				p.Metrics.recordFrame(role, plane, messageDirectionReceived, frame, messageType, frameSize)
			}
			if req == nil {
				continue
			}

			select {
			case req.result <- frame.Error:
			default:
				level.Warn(p.Logger).Log("msg", "ignoring duplicate acknowledgement")
			}

		case DiscardFrame:
			role, plane := p.labels()
			if hasFrameSize {
				p.Metrics.recordFrame(role, plane, messageDirectionReceived, frame, frameMessageType(frame), frameSize)
			}
			// TODO(rfratto): cancel handleMessage goroutine

		default:
			level.Warn(p.Logger).Log("msg", "unknown frame type", "type", reflect.TypeOf(frame).String())
		}
	}
}

func (p *Peer) handleIncoming(ctx context.Context) error {
	defer p.drainIncomingQueue()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.done:
			return nil // Closed connection.
		case queued := <-p.incoming:
			role, plane := p.labels()
			p.Metrics.decMessageQueued()
			p.Metrics.decQueueDepth(queued.role, queued.plane, queueDirectionIncoming)
			p.Metrics.observeIncomingQueue(role, plane, queued.frame.Message.Kind().String(), time.Since(queued.enqueuedAt))
			p.processMessage(ctx, queued.frame.ID, queued.frame.Message)
		}
	}
}

func (p *Peer) handleOutgoing(ctx context.Context) error {
	defer p.drainOutgoingQueue()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.done:
			return nil // Closed connection.
		case queued := <-p.outgoing:
			p.Metrics.decQueueDepth(queued.role, queued.plane, queueDirectionOutgoing)
			p.Metrics.observeOutgoingQueue(queued.role, queued.plane, queued.frame.FrameKind().String(), queued.messageType, time.Since(queued.enqueuedAt))
			frameSize, hasFrameSize, err := p.sendFrame(ctx, queued.frame)
			if err != nil {
				if ctx.Err() == nil {
					level.Warn(p.Logger).Log("msg", "failed to send message", "error", err)
					p.notifyError(queued.frame, err)
				}
				continue
			}
			if hasFrameSize {
				p.Metrics.recordFrame(queued.role, queued.plane, messageDirectionSent, queued.frame, queued.messageType, frameSize)
			}
		}
	}
}

func (p *Peer) drainIncomingQueue() {
	for {
		select {
		case queued := <-p.incoming:
			p.Metrics.decMessageQueued()
			p.Metrics.decQueueDepth(queued.role, queued.plane, queueDirectionIncoming)
		default:
			return
		}
	}
}

func (p *Peer) drainOutgoingQueue() {
	for {
		select {
		case queued := <-p.outgoing:
			p.Metrics.decQueueDepth(queued.role, queued.plane, queueDirectionOutgoing)
		default:
			return
		}
	}
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
	messageType := message.Kind().String()
	role, plane := p.labels()
	obs := p.Metrics.beginReceive(role, plane, messageType)

	err := p.handle(ctx, message)

	// Re-read labels after the handler: the first message on a connection can
	// classify an initially unknown peer as control or data plane.
	role, plane = p.labels()
	obs.finish(role, plane, err)

	if err != nil {
		// TODO(rfratto): What should we do if this fails? Logs? Metrics?
		_ = p.enqueueFrameWithMessageType(ctx, NackFrame{ID: id, Error: convertError(err)}, messageType)
		return
	}

	// TODO(rfratto): What should we do if this fails? Logs? Metrics?
	_ = p.enqueueFrameWithMessageType(ctx, AckFrame{ID: id}, messageType)
}

// handle dispatches a message to the application handler, returning the
// handler's result. If no handler is configured, it returns a not-implemented
// error.
func (p *Peer) handle(ctx context.Context, message Message) error {
	if p.Handler == nil {
		return Errorf(http.StatusNotImplemented, "not implemented")
	}
	return p.Handler(ctx, p, message)
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
	messageType string
	result      chan error
}

// SendMessage sends a message to the remote peer. SendMessage blocks until the
// provided context is canceled or the remote peer positively or negatively
// acknowledges the message.
//
// [Peer.Serve] must be running when SendMessage is called, otherwise it blocks
// until the context is canceled.
func (p *Peer) SendMessage(ctx context.Context, message Message) (err error) {
	p.lazyInit()

	reqID := p.requestID.Inc()
	messageType := message.Kind().String()
	req := &request{
		messageType: messageType,
		result:      make(chan error, 1),
	}
	p.sentRequests.Store(reqID, req)
	defer p.sentRequests.Delete(reqID)

	role, plane := p.labels()
	obs := p.Metrics.beginSend(role, plane, messageType)
	defer func() {
		// Re-read labels: the first message on a connection can classify an
		// initially unknown peer as control or data plane.
		role, plane := p.labels()
		obs.finish(role, plane, err)
	}()

	if err = p.enqueueFrame(ctx, MessageFrame{ID: reqID, Message: message}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		// TODO(rfratto): queue a DiscardFrame
		return ctx.Err()
	case <-p.done:
		return ErrConnClosed
	case err = <-req.result:
		return err
	}
}

// SendMessageAsync sends a message to the remote peer asynchronously.
// SendMessageAsync blocks until the message has been accepted into the outgoing
// queue but does not wait for an acknowledgement or response.
//
// [Peer.Serve] must be running before SendMessageAsync is called, otherwise it
// blocks until the context is canceled.
func (p *Peer) SendMessageAsync(ctx context.Context, message Message) error {
	p.lazyInit()

	reqID := p.requestID.Inc()
	messageType := message.Kind().String()
	start := time.Now()
	err := p.enqueueFrame(ctx, MessageFrame{ID: reqID, Message: message})

	role, plane := p.labels()
	p.Metrics.recordAsyncSend(role, plane, messageType, time.Since(start), err)
	return err
}

// enqueueFrame enqueues a frame to be sent to the remote peer.
func (p *Peer) enqueueFrame(ctx context.Context, frame Frame) error {
	return p.enqueueFrameWithMessageType(ctx, frame, frameMessageType(frame))
}

func (p *Peer) enqueueFrameWithMessageType(ctx context.Context, frame Frame, messageType string) error {
	role, plane := p.labels()
	p.Metrics.incQueueDepth(role, plane, queueDirectionOutgoing)
	select {
	case <-ctx.Done():
		p.Metrics.decQueueDepth(role, plane, queueDirectionOutgoing)
		return ctx.Err()
	case <-p.done:
		p.Metrics.decQueueDepth(role, plane, queueDirectionOutgoing)
		return ErrConnClosed
	case p.outgoing <- queuedFrame{frame: frame, role: role, plane: plane, messageType: messageType, enqueuedAt: time.Now()}:
		return nil
	}
}

// LocalAddr returns the address of the local peer.
func (p *Peer) LocalAddr() net.Addr { return p.Conn.LocalAddr() }

// RemoteAddr returns the address of the remote peer.
func (p *Peer) RemoteAddr() net.Addr { return p.Conn.RemoteAddr() }
