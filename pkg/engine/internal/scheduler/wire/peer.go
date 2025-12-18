package wire

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"sync"

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
	Conn    Conn    // Connection to use for communication.
	Handler Handler // Handler for incoming messages from the remote peer.
	Buffer  int     // Buffer size for incoming and outgoing messages.

	done     chan struct{}     // Closed when the peer connection is closed.
	incoming chan MessageFrame // Buffered frame of incoming messages.
	outgoing chan Frame        // Buffered frame of outgoing frames.
	initOnce sync.Once

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

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error { return p.recvMessages(ctx) })
	g.Go(func() error { return p.handleIncoming(ctx) })
	g.Go(func() error { return p.handleOutgoing(ctx) })

	return g.Wait()
}

func (p *Peer) lazyInit() {
	if p.outgoing != nil {
		return
	}

	p.initOnce.Do(func() {
		p.done = make(chan struct{})
		p.incoming = make(chan MessageFrame, p.Buffer)
		p.outgoing = make(chan Frame, p.Buffer)
	})
}

func (p *Peer) recvMessages(ctx context.Context) error {
	defer close(p.done)

	for {
		frame, err := p.Conn.Recv(ctx)
		if err != nil && ctx.Err() != nil {
			// Context got canceled; shut down
			return nil
		} else if err != nil {
			return fmt.Errorf("recv: %w", err)
		}

		switch frame := frame.(type) {
		case MessageFrame:
			// Queue the message for processing.
			select {
			case p.incoming <- frame:
			case <-ctx.Done():
				return nil
			}

		case AckFrame:
			// If there's still a listener for this request, inform them of
			// the success.
			val, found := p.sentRequests.Load(frame.ID)
			if !found {
				continue
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
				continue
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
	}
}

func (p *Peer) handleIncoming(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.done:
			return nil // Closed connection.
		case frame := <-p.incoming:
			p.processMessage(ctx, frame.ID, frame.Message)
		}
	}
}

func (p *Peer) handleOutgoing(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.done:
			return nil // Closed connection.
		case frame := <-p.outgoing:
			if err := p.Conn.Send(ctx, frame); err != nil && ctx.Err() == nil {
				level.Warn(p.Logger).Log("msg", "failed to send message", "error", err)
				p.notifyError(frame, err)
			}
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
	if p.Handler == nil {
		_ = p.enqueueFrame(ctx, NackFrame{ID: id, Error: Errorf(http.StatusNotImplemented, "not implemented")})
		return
	}

	switch err := p.Handler(ctx, p, message); err {
	case nil:
		// TODO(rfratto): What should we do if this fails? Logs? Metrics?
		_ = p.enqueueFrame(ctx, AckFrame{ID: id})
	default:
		// TODO(rfratto): What should we do if this fails? Logs? Metrics?
		_ = p.enqueueFrame(ctx, NackFrame{ID: id, Error: convertError(err)})
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

	if err := p.enqueueFrame(ctx, MessageFrame{ID: reqID, Message: message}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		// TODO(rfratto): queue a DiscardFrame
		return ctx.Err()
	case <-p.done:
		return ErrConnClosed
	case err := <-req.result:
		return err
	}
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
	return p.enqueueFrame(ctx, MessageFrame{ID: reqID, Message: message})
}

// enqueueFrame enqueues a frame to be sent to the remote peer.
func (p *Peer) enqueueFrame(ctx context.Context, frame Frame) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return ErrConnClosed
	case p.outgoing <- frame:
		return nil
	}
}

// LocalAddr returns the address of the local peer.
func (p *Peer) LocalAddr() net.Addr { return p.Conn.LocalAddr() }

// RemoteAddr returns the address of the remote peer.
func (p *Peer) RemoteAddr() net.Addr { return p.Conn.RemoteAddr() }
