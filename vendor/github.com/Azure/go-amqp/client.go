package amqp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrSessionClosed is propagated to Sender/Receivers
	// when Session.Close() is called.
	ErrSessionClosed = errors.New("amqp: session closed")

	// ErrLinkClosed returned by send and receive operations when
	// Sender.Close() or Receiver.Close() are called.
	ErrLinkClosed = errors.New("amqp: link closed")
)

// Client is an AMQP client connection.
type Client struct {
	conn *conn
}

// Dial connects to an AMQP server.
//
// If the addr includes a scheme, it must be "amqp" or "amqps".
// If no port is provided, 5672 will be used for "amqp" and 5671 for "amqps".
//
// If username and password information is not empty it's used as SASL PLAIN
// credentials, equal to passing ConnSASLPlain option.
func Dial(addr string, opts ...ConnOption) (*Client, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = "5672"
		if u.Scheme == "amqps" {
			port = "5671"
		}
	}

	// prepend SASL credentials when the user/pass segment is not empty
	if u.User != nil {
		pass, _ := u.User.Password()
		opts = append([]ConnOption{
			ConnSASLPlain(u.User.Username(), pass),
		}, opts...)
	}

	// append default options so user specified can overwrite
	opts = append([]ConnOption{
		ConnServerHostname(host),
	}, opts...)

	c, err := newConn(nil, opts...)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: c.connectTimeout}
	switch u.Scheme {
	case "amqp", "":
		c.net, err = dialer.Dial("tcp", net.JoinHostPort(host, port))
	case "amqps":
		c.initTLSConfig()
		c.tlsNegotiation = false
		c.net, err = tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, port), c.tlsConfig)
	default:
		return nil, errorErrorf("unsupported scheme %q", u.Scheme)
	}
	if err != nil {
		return nil, err
	}
	err = c.start()
	return &Client{conn: c}, err
}

// New establishes an AMQP client connection over conn.
func New(conn net.Conn, opts ...ConnOption) (*Client, error) {
	c, err := newConn(conn, opts...)
	if err != nil {
		return nil, err
	}
	err = c.start()
	return &Client{conn: c}, err
}

// Close disconnects the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// NewSession opens a new AMQP session to the server.
func (c *Client) NewSession(opts ...SessionOption) (*Session, error) {
	// get a session allocated by Client.mux
	var sResp newSessionResp
	select {
	case <-c.conn.done:
		return nil, c.conn.getErr()
	case sResp = <-c.conn.newSession:
	}

	if sResp.err != nil {
		return nil, sResp.err
	}
	s := sResp.session

	for _, opt := range opts {
		err := opt(s)
		if err != nil {
			_ = s.Close(context.Background()) // deallocate session on error
			return nil, err
		}
	}

	// send Begin to server
	begin := &performBegin{
		NextOutgoingID: 0,
		IncomingWindow: s.incomingWindow,
		OutgoingWindow: s.outgoingWindow,
		HandleMax:      s.handleMax,
	}
	debug(1, "TX: %s", begin)
	s.txFrame(begin, nil)

	// wait for response
	var fr frame
	select {
	case <-c.conn.done:
		return nil, c.conn.getErr()
	case fr = <-s.rx:
	}
	debug(1, "RX: %s", fr.body)

	begin, ok := fr.body.(*performBegin)
	if !ok {
		_ = s.Close(context.Background()) // deallocate session on error
		return nil, errorErrorf("unexpected begin response: %+v", fr.body)
	}

	// start Session multiplexor
	go s.mux(begin)

	return s, nil
}

// Default session options
const (
	DefaultMaxLinks = 4294967296
	DefaultWindow   = 100
)

// SessionOption is an function for configuring an AMQP session.
type SessionOption func(*Session) error

// SessionIncomingWindow sets the maximum number of unacknowledged
// transfer frames the server can send.
func SessionIncomingWindow(window uint32) SessionOption {
	return func(s *Session) error {
		s.incomingWindow = window
		return nil
	}
}

// SessionOutgoingWindow sets the maximum number of unacknowledged
// transfer frames the client can send.
func SessionOutgoingWindow(window uint32) SessionOption {
	return func(s *Session) error {
		s.outgoingWindow = window
		return nil
	}
}

// SessionMaxLinks sets the maximum number of links (Senders/Receivers)
// allowed on the session.
//
// n must be in the range 1 to 4294967296.
//
// Default: 4294967296.
func SessionMaxLinks(n int) SessionOption {
	return func(s *Session) error {
		if n < 1 {
			return errorNew("max sessions cannot be less than 1")
		}
		if int64(n) > 4294967296 {
			return errorNew("max sessions cannot be greater than 4294967296")
		}
		s.handleMax = uint32(n - 1)
		return nil
	}
}

// Session is an AMQP session.
//
// A session multiplexes Receivers.
type Session struct {
	channel       uint16                // session's local channel
	remoteChannel uint16                // session's remote channel, owned by conn.mux
	conn          *conn                 // underlying conn
	rx            chan frame            // frames destined for this session are sent on this chan by conn.mux
	tx            chan frameBody        // non-transfer frames to be sent; session must track disposition
	txTransfer    chan *performTransfer // transfer frames to be sent; session must track disposition

	// flow control
	incomingWindow uint32
	outgoingWindow uint32

	handleMax        uint32
	allocateHandle   chan *link // link handles are allocated by sending a link on this channel, nil is sent on link.rx once allocated
	deallocateHandle chan *link // link handles are deallocated by sending a link on this channel

	nextDeliveryID uint32 // atomically accessed sequence for deliveryIDs

	// used for gracefully closing link
	close     chan struct{}
	closeOnce sync.Once
	done      chan struct{}
	err       error
}

func newSession(c *conn, channel uint16) *Session {
	return &Session{
		conn:             c,
		channel:          channel,
		rx:               make(chan frame),
		tx:               make(chan frameBody),
		txTransfer:       make(chan *performTransfer),
		incomingWindow:   DefaultWindow,
		outgoingWindow:   DefaultWindow,
		handleMax:        DefaultMaxLinks - 1,
		allocateHandle:   make(chan *link),
		deallocateHandle: make(chan *link),
		close:            make(chan struct{}),
		done:             make(chan struct{}),
	}
}

// Close gracefully closes the session.
//
// If ctx expires while waiting for servers response, ctx.Err() will be returned.
// The session will continue to wait for the response until the Client is closed.
func (s *Session) Close(ctx context.Context) error {
	s.closeOnce.Do(func() { close(s.close) })
	select {
	case <-s.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if s.err == ErrSessionClosed {
		return nil
	}
	return s.err
}

// txFrame sends a frame to the connWriter
func (s *Session) txFrame(p frameBody, done chan deliveryState) error {
	return s.conn.wantWriteFrame(frame{
		type_:   frameTypeAMQP,
		channel: s.channel,
		body:    p,
		done:    done,
	})
}

// lockedRand provides a rand source that is safe for concurrent use.
type lockedRand struct {
	mu  sync.Mutex
	src *rand.Rand
}

func (r *lockedRand) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.src.Read(p)
}

// package scoped rand source to avoid any issues with seeding
// of the global source.
var pkgRand = &lockedRand{
	src: rand.New(rand.NewSource(time.Now().UnixNano())),
}

// randBytes returns a base64 encoded string of n bytes.
func randString(n int) string {
	b := make([]byte, n)
	pkgRand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewReceiver opens a new receiver link on the session.
func (s *Session) NewReceiver(opts ...LinkOption) (*Receiver, error) {
	r := &Receiver{
		batching:    DefaultLinkBatching,
		batchMaxAge: DefaultLinkBatchMaxAge,
		maxCredit:   DefaultLinkCredit,
	}

	l, err := attachLink(s, r, opts)
	if err != nil {
		return nil, err
	}

	r.link = l

	// batching is just extra overhead when maxCredits == 1
	if r.maxCredit == 1 {
		r.batching = false
	}

	// create dispositions channel and start dispositionBatcher if batching enabled
	if r.batching {
		// buffer dispositions chan to prevent disposition sends from blocking
		r.dispositions = make(chan messageDisposition, r.maxCredit)
		go r.dispositionBatcher()
	}

	return r, nil
}

// Sender sends messages on a single AMQP link.
type Sender struct {
	link *link

	mu              sync.Mutex // protects buf and nextDeliveryTag
	buf             buffer
	nextDeliveryTag uint64
}

// Send sends a Message.
//
// Blocks until the message is sent, ctx completes, or an error occurs.
//
// Send is safe for concurrent use. Since only a single message can be
// sent on a link at a time, this is most useful when settlement confirmation
// has been requested (receiver settle mode is "Second"). In this case,
// additional messages can be sent while the current goroutine is waiting
// for the confirmation.
func (s *Sender) Send(ctx context.Context, msg *Message) error {
	done, err := s.send(ctx, msg)
	if err != nil {
		return err
	}

	// wait for transfer to be confirmed
	select {
	case state := <-done:
		if state, ok := state.(*stateRejected); ok {
			return state.Error
		}
		return nil
	case <-s.link.done:
		return s.link.err
	case <-ctx.Done():
		return errorWrapf(ctx.Err(), "awaiting send")
	}
}

// send is separated from Send so that the mutex unlock can be deferred without
// locking the transfer confirmation that happens in Send.
func (s *Sender) send(ctx context.Context, msg *Message) (chan deliveryState, error) {
	if len(msg.DeliveryTag) > maxDeliveryTagLength {
		return nil, errorErrorf("delivery tag is over the allowed %v bytes, len: %v", maxDeliveryTagLength, len(msg.DeliveryTag))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.buf.reset()
	err := msg.marshal(&s.buf)
	if err != nil {
		return nil, err
	}

	if s.link.maxMessageSize != 0 && uint64(s.buf.len()) > s.link.maxMessageSize {
		return nil, errorErrorf("encoded message size exceeds max of %d", s.link.maxMessageSize)
	}

	var (
		maxPayloadSize = int64(s.link.session.conn.peerMaxFrameSize) - maxTransferFrameHeader
		sndSettleMode  = s.link.senderSettleMode
		senderSettled  = sndSettleMode != nil && (*sndSettleMode == ModeSettled || (*sndSettleMode == ModeMixed && msg.SendSettled))
		deliveryID     = atomic.AddUint32(&s.link.session.nextDeliveryID, 1)
	)

	deliveryTag := msg.DeliveryTag
	if len(deliveryTag) == 0 {
		// use uint64 encoded as []byte as deliveryTag
		deliveryTag = make([]byte, 8)
		binary.BigEndian.PutUint64(deliveryTag, s.nextDeliveryTag)
		s.nextDeliveryTag++
	}

	fr := performTransfer{
		Handle:        s.link.handle,
		DeliveryID:    &deliveryID,
		DeliveryTag:   deliveryTag,
		MessageFormat: &msg.Format,
		More:          s.buf.len() > 0,
	}

	for fr.More {
		buf, _ := s.buf.next(maxPayloadSize)
		fr.Payload = append([]byte(nil), buf...)
		fr.More = s.buf.len() > 0
		if !fr.More {
			// SSM=settled: overrides RSM; no acks.
			// SSM=unsettled: sender should wait for receiver to ack
			// RSM=first: receiver considers it settled immediately, but must still send ack (SSM=unsettled only)
			// RSM=second: receiver sends ack and waits for return ack from sender (SSM=unsettled only)

			// mark final transfer as settled when sender mode is settled
			fr.Settled = senderSettled

			// set done on last frame
			fr.done = make(chan deliveryState, 1)
		}

		select {
		case s.link.transfers <- fr:
		case <-s.link.done:
			return nil, s.link.err
		case <-ctx.Done():
			return nil, errorWrapf(ctx.Err(), "awaiting send")
		}

		// clear values that are only required on first message
		fr.DeliveryID = nil
		fr.DeliveryTag = nil
		fr.MessageFormat = nil
	}

	return fr.done, nil
}

// Address returns the link's address.
func (s *Sender) Address() string {
	if s.link.target == nil {
		return ""
	}
	return s.link.target.Address
}

// Close closes the Sender and AMQP link.
func (s *Sender) Close(ctx context.Context) error {
	return s.link.Close(ctx)
}

// NewSender opens a new sender link on the session.
func (s *Session) NewSender(opts ...LinkOption) (*Sender, error) {
	l, err := attachLink(s, nil, opts)
	if err != nil {
		return nil, err
	}

	return &Sender{link: l}, nil
}

func (s *Session) mux(remoteBegin *performBegin) {
	defer func() {
		// clean up session record in conn.mux()
		select {
		case s.conn.delSession <- s:
		case <-s.conn.done:
			s.err = s.conn.getErr()
		}
		if s.err == nil {
			s.err = ErrSessionClosed
		}
		// Signal goroutines waiting on the session.
		close(s.done)
	}()

	var (
		links      = make(map[uint32]*link)    // mapping of remote handles to links
		linksByKey = make(map[linkKey]*link)   // mapping of name+role link
		handles    = &bitmap{max: s.handleMax} // allocated handles

		handlesByDeliveryID       = make(map[uint32]uint32) // mapping of deliveryIDs to handles
		deliveryIDByHandle        = make(map[uint32]uint32) // mapping of handles to latest deliveryID
		handlesByRemoteDeliveryID = make(map[uint32]uint32) // mapping of remote deliveryID to handles

		settlementByDeliveryID = make(map[uint32]chan deliveryState)

		// flow control values
		nextOutgoingID       uint32
		nextIncomingID       = remoteBegin.NextOutgoingID
		remoteIncomingWindow = remoteBegin.IncomingWindow
		remoteOutgoingWindow = remoteBegin.OutgoingWindow
	)

	for {
		txTransfer := s.txTransfer
		// disable txTransfer if flow control windows have been exceeded
		if remoteIncomingWindow == 0 || s.outgoingWindow == 0 {
			txTransfer = nil
		}

		select {
		// conn has completed, exit
		case <-s.conn.done:
			s.err = s.conn.getErr()
			return

		// session is being closed by user
		case <-s.close:
			s.txFrame(&performEnd{}, nil)

			// discard frames until End is received or conn closed
		EndLoop:
			for {
				select {
				case fr := <-s.rx:
					_, ok := fr.body.(*performEnd)
					if ok {
						break EndLoop
					}
				case <-s.conn.done:
					s.err = s.conn.getErr()
					return
				}
			}
			return

		// handle allocation request
		case l := <-s.allocateHandle:
			// Check if link name already exists, if so then an error should be returned
			if linksByKey[l.key] != nil {
				l.err = errorErrorf("link with name '%v' already exists", l.key.name)
				l.rx <- nil
				continue
			}

			next, ok := handles.next()
			if !ok {
				l.err = errorErrorf("reached session handle max (%d)", s.handleMax)
				l.rx <- nil
				continue
			}

			l.handle = next       // allocate handle to the link
			linksByKey[l.key] = l // add to mapping
			l.rx <- nil           // send nil on channel to indicate allocation complete

		// handle deallocation request
		case l := <-s.deallocateHandle:
			delete(links, l.remoteHandle)
			delete(deliveryIDByHandle, l.handle)
			delete(linksByKey, l.key)
			handles.remove(l.handle)
			close(l.rx) // close channel to indicate deallocation

		// incoming frame for link
		case fr := <-s.rx:
			debug(1, "RX(Session): %s", fr.body)

			switch body := fr.body.(type) {
			// Disposition frames can reference transfers from more than one
			// link. Send this frame to all of them.
			case *performDisposition:
				start := body.First
				end := start
				if body.Last != nil {
					end = *body.Last
				}
				for deliveryID := start; deliveryID <= end; deliveryID++ {
					handles := handlesByDeliveryID
					if body.Role == roleSender {
						handles = handlesByRemoteDeliveryID
					}

					handle, ok := handles[deliveryID]
					if !ok {
						continue
					}
					delete(handles, deliveryID)

					if body.Settled && body.Role == roleReceiver {
						// check if settlement confirmation was requested, if so
						// confirm by closing channel
						if done, ok := settlementByDeliveryID[deliveryID]; ok {
							delete(settlementByDeliveryID, deliveryID)
							select {
							case done <- body.State:
							default:
							}
							close(done)
						}
					}

					link, ok := links[handle]
					if !ok {
						continue
					}

					s.muxFrameToLink(link, fr.body)
				}
				continue
			case *performFlow:
				if body.NextIncomingID == nil {
					// This is a protocol error:
					//       "[...] MUST be set if the peer has received
					//        the begin frame for the session"
					s.txFrame(&performEnd{
						Error: &Error{
							Condition:   ErrorNotAllowed,
							Description: "next-incoming-id not set after session established",
						},
					}, nil)
					s.err = errors.New("protocol error: received flow without next-incoming-id after session established")
					return
				}

				// "When the endpoint receives a flow frame from its peer,
				// it MUST update the next-incoming-id directly from the
				// next-outgoing-id of the frame, and it MUST update the
				// remote-outgoing-window directly from the outgoing-window
				// of the frame."
				nextIncomingID = body.NextOutgoingID
				remoteOutgoingWindow = body.OutgoingWindow

				// "The remote-incoming-window is computed as follows:
				//
				// next-incoming-id(flow) + incoming-window(flow) - next-outgoing-id(endpoint)
				//
				// If the next-incoming-id field of the flow frame is not set, then remote-incoming-window is computed as follows:
				//
				// initial-outgoing-id(endpoint) + incoming-window(flow) - next-outgoing-id(endpoint)"
				remoteIncomingWindow = body.IncomingWindow - nextOutgoingID
				remoteIncomingWindow += *body.NextIncomingID

				// Send to link if handle is set
				if body.Handle != nil {
					link, ok := links[*body.Handle]
					if !ok {
						continue
					}

					s.muxFrameToLink(link, fr.body)
					continue
				}

				if body.Echo {
					niID := nextIncomingID
					resp := &performFlow{
						NextIncomingID: &niID,
						IncomingWindow: s.incomingWindow,
						NextOutgoingID: nextOutgoingID,
						OutgoingWindow: s.outgoingWindow,
					}
					debug(1, "TX: %s", resp)
					s.txFrame(resp, nil)
				}

			case *performAttach:
				// On Attach response link should be looked up by name, then added
				// to the links map with the remote's handle contained in this
				// attach frame.
				//
				// Note body.Role is the remote peer's role, we reverse for the local key.
				link, linkOk := linksByKey[linkKey{name: body.Name, role: !body.Role}]
				if !linkOk {
					break
				}

				link.remoteHandle = body.Handle
				links[link.remoteHandle] = link

				s.muxFrameToLink(link, fr.body)

			case *performTransfer:
				// "Upon receiving a transfer, the receiving endpoint will
				// increment the next-incoming-id to match the implicit
				// transfer-id of the incoming transfer plus one, as well
				// as decrementing the remote-outgoing-window, and MAY
				// (depending on policy) decrement its incoming-window."
				nextIncomingID++
				remoteOutgoingWindow--
				link, ok := links[body.Handle]
				if !ok {
					continue
				}

				select {
				case <-s.conn.done:
				case link.rx <- fr.body:
				}

				// if this message is received unsettled and link rcv-settle-mode == second, add to handlesByRemoteDeliveryID
				if !body.Settled && body.DeliveryID != nil && link.receiverSettleMode != nil && *link.receiverSettleMode == ModeSecond {
					handlesByRemoteDeliveryID[*body.DeliveryID] = body.Handle
				}

				// Update peer's outgoing window if half has been consumed.
				if remoteOutgoingWindow < s.incomingWindow/2 {
					nID := nextIncomingID
					flow := &performFlow{
						NextIncomingID: &nID,
						IncomingWindow: s.incomingWindow,
						NextOutgoingID: nextOutgoingID,
						OutgoingWindow: s.outgoingWindow,
					}
					debug(1, "TX(Session): %s", flow)
					s.txFrame(flow, nil)
					remoteOutgoingWindow = s.incomingWindow
				}

			case *performDetach:
				link, ok := links[body.Handle]
				if !ok {
					continue
				}
				s.muxFrameToLink(link, fr.body)

			case *performEnd:
				s.txFrame(&performEnd{}, nil)
				s.err = errorErrorf("session ended by server: %s", body.Error)
				return

			default:
				fmt.Printf("Unexpected frame: %s\n", body)
			}

		case fr := <-txTransfer:

			// record current delivery ID
			var deliveryID uint32
			if fr.DeliveryID != nil {
				deliveryID = *fr.DeliveryID
				deliveryIDByHandle[fr.Handle] = deliveryID

				// add to handleByDeliveryID if not sender-settled
				if !fr.Settled {
					handlesByDeliveryID[deliveryID] = fr.Handle
				}
			} else {
				// if fr.DeliveryID is nil it must have been added
				// to deliveryIDByHandle already
				deliveryID = deliveryIDByHandle[fr.Handle]
			}

			// frame has been sender-settled, remove from map
			if fr.Settled {
				delete(handlesByDeliveryID, deliveryID)
			}

			// if not settled, add done chan to map
			// and clear from frame so conn doesn't close it.
			if !fr.Settled && fr.done != nil {
				settlementByDeliveryID[deliveryID] = fr.done
				fr.done = nil
			}

			debug(2, "TX(Session): %s", fr)
			s.txFrame(fr, fr.done)

			// "Upon sending a transfer, the sending endpoint will increment
			// its next-outgoing-id, decrement its remote-incoming-window,
			// and MAY (depending on policy) decrement its outgoing-window."
			nextOutgoingID++
			remoteIncomingWindow--

		case fr := <-s.tx:
			switch fr := fr.(type) {
			case *performFlow:
				niID := nextIncomingID
				fr.NextIncomingID = &niID
				fr.IncomingWindow = s.incomingWindow
				fr.NextOutgoingID = nextOutgoingID
				fr.OutgoingWindow = s.outgoingWindow
				debug(1, "TX(Session): %s", fr)
				s.txFrame(fr, nil)
				remoteOutgoingWindow = s.incomingWindow
			case *performTransfer:
				panic("transfer frames must use txTransfer")
			default:
				debug(1, "TX(Session): %s", fr)
				s.txFrame(fr, nil)
			}
		}
	}
}

func (s *Session) muxFrameToLink(l *link, fr frameBody) {
	select {
	case l.rx <- fr:
	case <-l.done:
	case <-s.conn.done:
	}
}

// DetachError is returned by a link (Receiver/Sender) when a detach frame is received.
//
// RemoteError will be nil if the link was detached gracefully.
type DetachError struct {
	RemoteError *Error
}

func (e *DetachError) Error() string {
	return fmt.Sprintf("link detached, reason: %+v", e.RemoteError)
}

// Default link options
const (
	DefaultLinkCredit      = 1
	DefaultLinkBatching    = false
	DefaultLinkBatchMaxAge = 5 * time.Second
)

// linkKey uniquely identifies a link on a connection by name and direction.
//
// A link can be identified uniquely by the ordered tuple
//     (source-container-id, target-container-id, name)
// On a single connection the container ID pairs can be abbreviated
// to a boolean flag indicating the direction of the link.
type linkKey struct {
	name string
	role role // Local role: sender/receiver
}

// link is a unidirectional route.
//
// May be used for sending or receiving.
type link struct {
	key           linkKey              // Name and direction
	handle        uint32               // our handle
	remoteHandle  uint32               // remote's handle
	dynamicAddr   bool                 // request a dynamic link address from the server
	rx            chan frameBody       // sessions sends frames for this link on this channel
	transfers     chan performTransfer // sender uses to send transfer frames
	closeOnce     sync.Once            // closeOnce protects close from being closed multiple times
	close         chan struct{}        // close signals the mux to shutdown
	done          chan struct{}        // done is closed by mux/muxDetach when the link is fully detached
	detachErrorMu sync.Mutex           // protects detachError
	detachError   *Error               // error to send to remote on detach, set by closeWithError
	session       *Session             // parent session
	receiver      *Receiver            // allows link options to modify Receiver
	source        *source
	target        *target
	properties    map[symbol]interface{} // additional properties sent upon link attach

	// "The delivery-count is initialized by the sender when a link endpoint is created,
	// and is incremented whenever a message is sent. Only the sender MAY independently
	// modify this field. The receiver's value is calculated based on the last known
	// value from the sender and any subsequent messages received on the link. Note that,
	// despite its name, the delivery-count is not a count but a sequence number
	// initialized at an arbitrary point by the sender."
	deliveryCount      uint32
	linkCredit         uint32 // maximum number of messages allowed between flow updates
	senderSettleMode   *SenderSettleMode
	receiverSettleMode *ReceiverSettleMode
	maxMessageSize     uint64
	detachReceived     bool
	err                error // err returned on Close()

	// message receiving
	paused        uint32        // atomically accessed; indicates that all link credits have been used by sender
	receiverReady chan struct{} // receiver sends on this when mux is paused to indicate it can handle more messages
	messages      chan Message  // used to send completed messages to receiver
	buf           buffer        // buffered bytes for current message
	more          bool          // if true, buf contains a partial message
	msg           Message       // current message being decoded
}

// attachLink is used by Receiver and Sender to create new links
func attachLink(s *Session, r *Receiver, opts []LinkOption) (*link, error) {
	l, err := newLink(s, r, opts)
	if err != nil {
		return nil, err
	}

	isReceiver := r != nil

	// buffer rx to linkCredit so that conn.mux won't block
	// attempting to send to a slow reader
	if isReceiver {
		l.rx = make(chan frameBody, l.linkCredit)
	} else {
		l.rx = make(chan frameBody, 1)
	}

	// request handle from Session.mux
	select {
	case <-s.done:
		return nil, s.err
	case s.allocateHandle <- l:
	}

	// wait for handle allocation
	select {
	case <-s.done:
		return nil, s.err
	case <-l.rx:
	}

	// check for link request error
	if l.err != nil {
		return nil, l.err
	}

	attach := &performAttach{
		Name:               l.key.name,
		Handle:             l.handle,
		ReceiverSettleMode: l.receiverSettleMode,
		SenderSettleMode:   l.senderSettleMode,
		MaxMessageSize:     l.maxMessageSize,
		Source:             l.source,
		Target:             l.target,
		Properties:         l.properties,
	}

	if isReceiver {
		attach.Role = roleReceiver
		if attach.Source == nil {
			attach.Source = new(source)
		}
		attach.Source.Dynamic = l.dynamicAddr
	} else {
		attach.Role = roleSender
		if attach.Target == nil {
			attach.Target = new(target)
		}
		attach.Target.Dynamic = l.dynamicAddr
	}

	// send Attach frame
	debug(1, "TX: %s", attach)
	s.txFrame(attach, nil)

	// wait for response
	var fr frameBody
	select {
	case <-s.done:
		return nil, s.err
	case fr = <-l.rx:
	}
	debug(3, "RX: %s", fr)
	resp, ok := fr.(*performAttach)
	if !ok {
		return nil, errorErrorf("unexpected attach response: %#v", fr)
	}

	// If the remote encounters an error during the attach it returns an Attach
	// with no Source or Target. The remote then sends a Detach with an error.
	//
	//   Note that if the application chooses not to create a terminus, the session
	//   endpoint will still create a link endpoint and issue an attach indicating
	//   that the link endpoint has no associated local terminus. In this case, the
	//   session endpoint MUST immediately detach the newly created link endpoint.
	//
	// http://docs.oasis-open.org/amqp/core/v1.0/csprd01/amqp-core-transport-v1.0-csprd01.html#doc-idp386144
	if resp.Source == nil && resp.Target == nil {
		// wait for detach
		select {
		case <-s.done:
			return nil, s.err
		case fr = <-l.rx:
		}

		detach, ok := fr.(*performDetach)
		if !ok {
			return nil, errorErrorf("unexpected frame while waiting for detach: %#v", fr)
		}

		// send return detach
		fr = &performDetach{
			Handle: l.handle,
			Closed: true,
		}
		debug(1, "TX: %s", fr)
		s.txFrame(fr, nil)

		if detach.Error == nil {
			return nil, errorErrorf("received detach with no error specified")
		}
		return nil, detach.Error
	}

	if l.maxMessageSize == 0 || resp.MaxMessageSize < l.maxMessageSize {
		l.maxMessageSize = resp.MaxMessageSize
	}

	if isReceiver {
		// if dynamic address requested, copy assigned name to address
		if l.dynamicAddr && resp.Source != nil {
			l.source.Address = resp.Source.Address
		}
		// deliveryCount is a sequence number, must initialize to sender's initial sequence number
		l.deliveryCount = resp.InitialDeliveryCount
		// buffer receiver so that link.mux doesn't block
		l.messages = make(chan Message, l.receiver.maxCredit)
	} else {
		// if dynamic address requested, copy assigned name to address
		if l.dynamicAddr && resp.Target != nil {
			l.target.Address = resp.Target.Address
		}
		l.transfers = make(chan performTransfer)
	}

	err = l.setSettleModes(resp)
	if err != nil {
		l.muxDetach()
		return nil, err
	}

	go l.mux()

	return l, nil
}

// setSettleModes sets the settlement modes based on the resp performAttach.
//
// If a settlement mode has been explicitly set locally and it was not honored by the
// server an error is returned.
func (l *link) setSettleModes(resp *performAttach) error {
	var (
		localRecvSettle = l.receiverSettleMode.value()
		respRecvSettle  = resp.ReceiverSettleMode.value()
	)
	if l.receiverSettleMode != nil && localRecvSettle != respRecvSettle {
		return fmt.Errorf("amqp: receiver settlement mode %q requested, received %q from server", l.receiverSettleMode, &respRecvSettle)
	}
	l.receiverSettleMode = &respRecvSettle

	var (
		localSendSettle = l.senderSettleMode.value()
		respSendSettle  = resp.SenderSettleMode.value()
	)
	if l.senderSettleMode != nil && localSendSettle != respSendSettle {
		return fmt.Errorf("amqp: sender settlement mode %q requested, received %q from server", l.senderSettleMode, &respSendSettle)
	}
	l.senderSettleMode = &respSendSettle

	return nil
}

func newLink(s *Session, r *Receiver, opts []LinkOption) (*link, error) {
	l := &link{
		key:           linkKey{randString(40), role(r != nil)},
		session:       s,
		receiver:      r,
		close:         make(chan struct{}),
		done:          make(chan struct{}),
		receiverReady: make(chan struct{}, 1),
	}

	// configure options
	for _, o := range opts {
		err := o(l)
		if err != nil {
			return nil, err
		}
	}

	return l, nil
}

func (l *link) mux() {
	defer l.muxDetach()

	var (
		isReceiver = l.receiver != nil
		isSender   = !isReceiver
	)

Loop:
	for {
		var outgoingTransfers chan performTransfer
		switch {
		// enable outgoing transfers case if sender and credits are available
		case isSender && l.linkCredit > 0:
			outgoingTransfers = l.transfers

		// if receiver && half maxCredits have been processed, send more credits
		case isReceiver && l.linkCredit+uint32(len(l.messages)) <= l.receiver.maxCredit/2:
			l.err = l.muxFlow()
			if l.err != nil {
				return
			}
			atomic.StoreUint32(&l.paused, 0)

		case isReceiver && l.linkCredit == 0:
			atomic.StoreUint32(&l.paused, 1)
		}

		select {
		// received frame
		case fr := <-l.rx:
			l.err = l.muxHandleFrame(fr)
			if l.err != nil {
				return
			}

		// send data
		case tr := <-outgoingTransfers:
			debug(3, "TX(link): %s", tr)

			// Ensure the session mux is not blocked
			for {
				select {
				case l.session.txTransfer <- &tr:
					// decrement link-credit after entire message transferred
					if !tr.More {
						l.deliveryCount++
						l.linkCredit--
					}
					continue Loop
				case fr := <-l.rx:
					l.err = l.muxHandleFrame(fr)
					if l.err != nil {
						return
					}
				case <-l.close:
					l.err = ErrLinkClosed
					return
				case <-l.session.done:
					l.err = l.session.err
					return
				}
			}

		case <-l.receiverReady:
			continue
		case <-l.close:
			l.err = ErrLinkClosed
			return
		case <-l.session.done:
			l.err = l.session.err
			return
		}
	}
}

// muxFlow sends tr to the session mux.
func (l *link) muxFlow() error {
	// copy because sent by pointer below; prevent race
	var (
		linkCredit    = l.receiver.maxCredit - uint32(len(l.messages))
		deliveryCount = l.deliveryCount
	)

	fr := &performFlow{
		Handle:        &l.handle,
		DeliveryCount: &deliveryCount,
		LinkCredit:    &linkCredit, // max number of messages
	}
	debug(3, "TX: %s", fr)

	// Update credit. This must happen before entering loop below
	// because incoming messages handled while waiting to transmit
	// flow increment deliveryCount. This causes the credit to become
	// out of sync with the server.
	l.linkCredit = linkCredit

	// Ensure the session mux is not blocked
	for {
		select {
		case l.session.tx <- fr:
			return nil
		case fr := <-l.rx:
			err := l.muxHandleFrame(fr)
			if err != nil {
				return err
			}
		case <-l.close:
			return ErrLinkClosed
		case <-l.session.done:
			return l.session.err
		}
	}
}

func (l *link) muxReceive(fr performTransfer) error {
	if !l.more {
		// this is the first transfer of a message,
		// record the delivery ID, message format,
		// and delivery Tag
		if fr.DeliveryID != nil {
			l.msg.deliveryID = *fr.DeliveryID
		}
		if fr.MessageFormat != nil {
			l.msg.Format = *fr.MessageFormat
		}
		l.msg.DeliveryTag = fr.DeliveryTag

		// these fields are required on first transfer of a message
		if fr.DeliveryID == nil {
			msg := "received message without a delivery-id"
			l.closeWithError(&Error{
				Condition:   ErrorNotAllowed,
				Description: msg,
			})
			return errorNew(msg)
		}
		if fr.MessageFormat == nil {
			msg := "received message without a message-format"
			l.closeWithError(&Error{
				Condition:   ErrorNotAllowed,
				Description: msg,
			})
			return errorNew(msg)
		}
		if fr.DeliveryTag == nil {
			msg := "received message without a delivery-tag"
			l.closeWithError(&Error{
				Condition:   ErrorNotAllowed,
				Description: msg,
			})
			return errorNew(msg)
		}
	} else {
		// this is a continuation of a multipart message
		// some fields may be omitted on continuation transfers,
		// but if they are included they must be consistent
		// with the first.

		if fr.DeliveryID != nil && *fr.DeliveryID != l.msg.deliveryID {
			msg := fmt.Sprintf(
				"received continuation transfer with inconsistent delivery-id: %d != %d",
				*fr.DeliveryID, l.msg.deliveryID,
			)
			l.closeWithError(&Error{
				Condition:   ErrorNotAllowed,
				Description: msg,
			})
			return errorNew(msg)
		}
		if fr.MessageFormat != nil && *fr.MessageFormat != l.msg.Format {
			msg := fmt.Sprintf(
				"received continuation transfer with inconsistent message-format: %d != %d",
				*fr.MessageFormat, l.msg.Format,
			)
			l.closeWithError(&Error{
				Condition:   ErrorNotAllowed,
				Description: msg,
			})
			return errorNew(msg)
		}
		if fr.DeliveryTag != nil && !bytes.Equal(fr.DeliveryTag, l.msg.DeliveryTag) {
			msg := fmt.Sprintf(
				"received continuation transfer with inconsistent delivery-tag: %q != %q",
				fr.DeliveryTag, l.msg.DeliveryTag,
			)
			l.closeWithError(&Error{
				Condition:   ErrorNotAllowed,
				Description: msg,
			})
			return errorNew(msg)
		}
	}

	// discard message if it's been aborted
	if fr.Aborted {
		l.buf.reset()
		l.msg = Message{}
		l.more = false
		return nil
	}

	// ensure maxMessageSize will not be exceeded
	if l.maxMessageSize != 0 && uint64(l.buf.len())+uint64(len(fr.Payload)) > l.maxMessageSize {
		msg := fmt.Sprintf("received message larger than max size of %d", l.maxMessageSize)
		l.closeWithError(&Error{
			Condition:   ErrorMessageSizeExceeded,
			Description: msg,
		})
		return errorNew(msg)
	}

	// add the payload the the buffer
	l.buf.write(fr.Payload)

	// mark as settled if at least one frame is settled
	l.msg.settled = l.msg.settled || fr.Settled

	// save in-progress status
	l.more = fr.More

	if fr.More {
		return nil
	}

	// last frame in message
	err := l.msg.unmarshal(&l.buf)
	if err != nil {
		return err
	}

	// send to receiver, this should never block due to buffering
	// and flow control.
	l.messages <- l.msg

	// reset progress
	l.buf.reset()
	l.msg = Message{}

	// decrement link-credit after entire message received
	l.deliveryCount++
	l.linkCredit--

	return nil
}

// muxHandleFrame processes fr based on type.
func (l *link) muxHandleFrame(fr frameBody) error {
	var (
		isSender               = l.receiver == nil
		errOnRejectDisposition = isSender && (l.receiverSettleMode == nil || *l.receiverSettleMode == ModeFirst)
	)

	switch fr := fr.(type) {
	// message frame
	case *performTransfer:
		debug(3, "RX: %s", fr)
		if isSender {
			// Senders should never receive transfer frames, but handle it just in case.
			l.closeWithError(&Error{
				Condition:   ErrorNotAllowed,
				Description: "sender cannot process transfer frame",
			})
			return errorErrorf("sender received transfer frame")
		}

		return l.muxReceive(*fr)

	// flow control frame
	case *performFlow:
		debug(3, "RX: %s", fr)
		if isSender {
			linkCredit := *fr.LinkCredit - l.deliveryCount
			if fr.DeliveryCount != nil {
				// DeliveryCount can be nil if the receiver hasn't processed
				// the attach. That shouldn't be the case here, but it's
				// what ActiveMQ does.
				linkCredit += *fr.DeliveryCount
			}
			l.linkCredit = linkCredit
		}

		if !fr.Echo {
			return nil
		}

		var (
			// copy because sent by pointer below; prevent race
			linkCredit    = l.linkCredit
			deliveryCount = l.deliveryCount
		)

		// send flow
		resp := &performFlow{
			Handle:        &l.handle,
			DeliveryCount: &deliveryCount,
			LinkCredit:    &linkCredit, // max number of messages
		}
		debug(1, "TX: %s", resp)
		l.session.txFrame(resp, nil)

	// remote side is closing links
	case *performDetach:
		debug(1, "RX: %s", fr)
		// don't currently support link detach and reattach
		if !fr.Closed {
			return errorErrorf("non-closing detach not supported: %+v", fr)
		}

		// set detach received and close link
		l.detachReceived = true

		return errorWrapf(&DetachError{fr.Error}, "received detach frame")

	case *performDisposition:
		debug(3, "RX: %s", fr)

		// Unblock receivers waiting for message disposition
		if l.receiver != nil {
			l.receiver.inFlight.remove(fr.First, fr.Last, nil)
		}

		// If sending async and a message is rejected, cause a link error.
		//
		// This isn't ideal, but there isn't a clear better way to handle it.
		if fr, ok := fr.State.(*stateRejected); ok && errOnRejectDisposition {
			return fr.Error
		}

		if fr.Settled {
			return nil
		}

		resp := &performDisposition{
			Role:    roleSender,
			First:   fr.First,
			Last:    fr.Last,
			Settled: true,
		}
		debug(1, "TX: %s", resp)
		l.session.txFrame(resp, nil)

	default:
		debug(1, "RX: %s", fr)
		fmt.Printf("Unexpected frame: %s\n", fr)
	}

	return nil
}

// close closes and requests deletion of the link.
//
// No operations on link are valid after close.
//
// If ctx expires while waiting for servers response, ctx.Err() will be returned.
// The session will continue to wait for the response until the Session or Client
// is closed.
func (l *link) Close(ctx context.Context) error {
	l.closeOnce.Do(func() { close(l.close) })
	select {
	case <-l.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if l.err == ErrLinkClosed {
		return nil
	}
	return l.err
}

func (l *link) closeWithError(de *Error) {
	l.closeOnce.Do(func() {
		l.detachErrorMu.Lock()
		l.detachError = de
		l.detachErrorMu.Unlock()
		close(l.close)
	})
}

func (l *link) muxDetach() {
	defer func() {
		// final cleanup and signaling

		// deallocate handle
		select {
		case l.session.deallocateHandle <- l:
		case <-l.session.done:
			if l.err == nil {
				l.err = l.session.err
			}
		}

		// signal other goroutines that link is done
		close(l.done)

		// unblock any in flight message dispositions
		if l.receiver != nil {
			l.receiver.inFlight.clear(l.err)
		}
	}()

	// "A peer closes a link by sending the detach frame with the
	// handle for the specified link, and the closed flag set to
	// true. The partner will destroy the corresponding link
	// endpoint, and reply with its own detach frame with the
	// closed flag set to true.
	//
	// Note that one peer MAY send a closing detach while its
	// partner is sending a non-closing detach. In this case,
	// the partner MUST signal that it has closed the link by
	// reattaching and then sending a closing detach."

	l.detachErrorMu.Lock()
	detachError := l.detachError
	l.detachErrorMu.Unlock()

	fr := &performDetach{
		Handle: l.handle,
		Closed: true,
		Error:  detachError,
	}

Loop:
	for {
		select {
		case l.session.tx <- fr:
			// after sending the detach frame, break the read loop
			break Loop
		case fr := <-l.rx:
			// discard incoming frames to avoid blocking session.mux
			if fr, ok := fr.(*performDetach); ok && fr.Closed {
				l.detachReceived = true
			}
		case <-l.session.done:
			if l.err == nil {
				l.err = l.session.err
			}
			return
		}
	}

	// don't wait for remote to detach when already
	// received or closing due to error
	if l.detachReceived || detachError != nil {
		return
	}

	for {
		select {
		// read from link until detach with Close == true is received,
		// other frames are discarded.
		case fr := <-l.rx:
			if fr, ok := fr.(*performDetach); ok && fr.Closed {
				return
			}

		// connection has ended
		case <-l.session.done:
			if l.err == nil {
				l.err = l.session.err
			}
			return
		}
	}
}

// LinkOption is a function for configuring an AMQP link.
//
// A link may be a Sender or a Receiver.
type LinkOption func(*link) error

// LinkAddress sets the link address.
//
// For a Receiver this configures the source address.
// For a Sender this configures the target address.
//
// Deprecated: use LinkSourceAddress or LinkTargetAddress instead.
func LinkAddress(source string) LinkOption {
	return func(l *link) error {
		if l.receiver != nil {
			return LinkSourceAddress(source)(l)
		}
		return LinkTargetAddress(source)(l)
	}
}

// LinkProperty sets an entry in the link properties map sent to the server.
//
// This option can be used multiple times.
func LinkProperty(key, value string) LinkOption {
	return linkProperty(key, value)
}

// LinkPropertyInt64 sets an entry in the link properties map sent to the server.
//
// This option can be used multiple times.
func LinkPropertyInt64(key string, value int64) LinkOption {
	return linkProperty(key, value)
}

func linkProperty(key string, value interface{}) LinkOption {
	return func(l *link) error {
		if key == "" {
			return errorNew("link property key must not be empty")
		}
		if l.properties == nil {
			l.properties = make(map[symbol]interface{})
		}
		l.properties[symbol(key)] = value
		return nil
	}
}

// LinkName sets the name of the link.
//
// The link names must be unique per-connection and direction.
//
// Default: randomly generated.
func LinkName(name string) LinkOption {
	return func(l *link) error {
		l.key.name = name
		return nil
	}
}

// LinkSourceCapabilities sets the source capabilities.
func LinkSourceCapabilities(capabilities ...string) LinkOption {
	return func(l *link) error {
		if l.source == nil {
			l.source = new(source)
		}

		// Convert string to symbol
		symbolCapabilities := make([]symbol, len(capabilities))
		for i, v := range capabilities {
			symbolCapabilities[i] = symbol(v)
		}

		l.source.Capabilities = append(l.source.Capabilities, symbolCapabilities...)
		return nil
	}
}

// LinkSourceAddress sets the source address.
func LinkSourceAddress(addr string) LinkOption {
	return func(l *link) error {
		if l.source == nil {
			l.source = new(source)
		}
		l.source.Address = addr
		return nil
	}
}

// LinkTargetAddress sets the target address.
func LinkTargetAddress(addr string) LinkOption {
	return func(l *link) error {
		if l.target == nil {
			l.target = new(target)
		}
		l.target.Address = addr
		return nil
	}
}

// LinkAddressDynamic requests a dynamically created address from the server.
func LinkAddressDynamic() LinkOption {
	return func(l *link) error {
		l.dynamicAddr = true
		return nil
	}
}

// LinkCredit specifies the maximum number of unacknowledged messages
// the sender can transmit.
func LinkCredit(credit uint32) LinkOption {
	return func(l *link) error {
		if l.receiver == nil {
			return errorNew("LinkCredit is not valid for Sender")
		}

		l.receiver.maxCredit = credit
		return nil
	}
}

// LinkBatching toggles batching of message disposition.
//
// When enabled, accepting a message does not send the disposition
// to the server until the batch is equal to link credit or the
// batch max age expires.
func LinkBatching(enable bool) LinkOption {
	return func(l *link) error {
		l.receiver.batching = enable
		return nil
	}
}

// LinkBatchMaxAge sets the maximum time between the start
// of a disposition batch and sending the batch to the server.
func LinkBatchMaxAge(d time.Duration) LinkOption {
	return func(l *link) error {
		l.receiver.batchMaxAge = d
		return nil
	}
}

// LinkSenderSettle sets the requested sender settlement mode.
//
// If a settlement mode is explicitly set and the server does not
// honor it an error will be returned during link attachment.
//
// Default: Accept the settlement mode set by the server, commonly ModeMixed.
func LinkSenderSettle(mode SenderSettleMode) LinkOption {
	return func(l *link) error {
		if mode > ModeMixed {
			return errorErrorf("invalid SenderSettlementMode %d", mode)
		}
		l.senderSettleMode = &mode
		return nil
	}
}

// LinkReceiverSettle sets the requested receiver settlement mode.
//
// If a settlement mode is explicitly set and the server does not
// honor it an error will be returned during link attachment.
//
// Default: Accept the settlement mode set by the server, commonly ModeFirst.
func LinkReceiverSettle(mode ReceiverSettleMode) LinkOption {
	return func(l *link) error {
		if mode > ModeSecond {
			return errorErrorf("invalid ReceiverSettlementMode %d", mode)
		}
		l.receiverSettleMode = &mode
		return nil
	}
}

// LinkSelectorFilter sets a selector filter (apache.org:selector-filter:string) on the link source.
func LinkSelectorFilter(filter string) LinkOption {
	// <descriptor name="apache.org:selector-filter:string" code="0x0000468C:0x00000004"/>
	return LinkSourceFilter("apache.org:selector-filter:string", 0x0000468C00000004, filter)
}

// LinkSourceFilter is an advanced API for setting non-standard source filters.
// Please file an issue or open a PR if a standard filter is missing from this
// library.
//
// The name is the key for the filter map. It will be encoded as an AMQP symbol type.
//
// The code is the descriptor of the described type value. The domain-id and descriptor-id
// should be concatenated together. If 0 is passed as the code, the name will be used as
// the descriptor.
//
// The value is the value of the descriped types. Acceptable types for value are specific
// to the filter.
//
// Example:
//
// The standard selector-filter is defined as:
//  <descriptor name="apache.org:selector-filter:string" code="0x0000468C:0x00000004"/>
// In this case the name is "apache.org:selector-filter:string" and the code is
// 0x0000468C00000004.
//  LinkSourceFilter("apache.org:selector-filter:string", 0x0000468C00000004, exampleValue)
//
// References:
//  http://docs.oasis-open.org/amqp/core/v1.0/os/amqp-core-messaging-v1.0-os.html#type-filter-set
//  http://docs.oasis-open.org/amqp/core/v1.0/os/amqp-core-types-v1.0-os.html#section-descriptor-values
func LinkSourceFilter(name string, code uint64, value interface{}) LinkOption {
	return func(l *link) error {
		if l.source == nil {
			l.source = new(source)
		}
		if l.source.Filter == nil {
			l.source.Filter = make(map[symbol]*describedType)
		}

		var descriptor interface{}
		if code != 0 {
			descriptor = code
		} else {
			descriptor = symbol(name)
		}

		l.source.Filter[symbol(name)] = &describedType{
			descriptor: descriptor,
			value:      value,
		}
		return nil
	}
}

// LinkMaxMessageSize sets the maximum message size that can
// be sent or received on the link.
//
// A size of zero indicates no limit.
//
// Default: 0.
func LinkMaxMessageSize(size uint64) LinkOption {
	return func(l *link) error {
		l.maxMessageSize = size
		return nil
	}
}

// LinkTargetDurability sets the target durability policy.
//
// Default: DurabilityNone.
func LinkTargetDurability(d Durability) LinkOption {
	return func(l *link) error {
		if d > DurabilityUnsettledState {
			return errorErrorf("invalid Durability %d", d)
		}

		if l.target == nil {
			l.target = new(target)
		}
		l.target.Durable = d

		return nil
	}
}

// LinkTargetExpiryPolicy sets the link expiration policy.
//
// Default: ExpirySessionEnd.
func LinkTargetExpiryPolicy(p ExpiryPolicy) LinkOption {
	return func(l *link) error {
		err := p.validate()
		if err != nil {
			return err
		}

		if l.target == nil {
			l.target = new(target)
		}
		l.target.ExpiryPolicy = p

		return nil
	}
}

// LinkSourceDurability sets the source durability policy.
//
// Default: DurabilityNone.
func LinkSourceDurability(d Durability) LinkOption {
	return func(l *link) error {
		if d > DurabilityUnsettledState {
			return errorErrorf("invalid Durability %d", d)
		}

		if l.source == nil {
			l.source = new(source)
		}
		l.source.Durable = d

		return nil
	}
}

// LinkSourceExpiryPolicy sets the link expiration policy.
//
// Default: ExpirySessionEnd.
func LinkSourceExpiryPolicy(p ExpiryPolicy) LinkOption {
	return func(l *link) error {
		err := p.validate()
		if err != nil {
			return err
		}

		if l.source == nil {
			l.source = new(source)
		}
		l.source.ExpiryPolicy = p

		return nil
	}
}

// Receiver receives messages on a single AMQP link.
type Receiver struct {
	link         *link                   // underlying link
	batching     bool                    // enable batching of message dispositions
	batchMaxAge  time.Duration           // maximum time between the start n batch and sending the batch to the server
	dispositions chan messageDisposition // message dispositions are sent on this channel when batching is enabled
	maxCredit    uint32                  // maximum allowed inflight messages
	inFlight     inFlight                // used to track message disposition when rcv-settle-mode == second
}

// Receive returns the next message from the sender.
//
// Blocks until a message is received, ctx completes, or an error occurs.
func (r *Receiver) Receive(ctx context.Context) (*Message, error) {
	if atomic.LoadUint32(&r.link.paused) == 1 {
		select {
		case r.link.receiverReady <- struct{}{}:
		default:
		}
	}

	// non-blocking receive to ensure buffered messages are
	// delivered regardless of whether the link has been closed.
	select {
	case msg := <-r.link.messages:
		msg.receiver = r
		return &msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// wait for the next message
	select {
	case msg := <-r.link.messages:
		msg.receiver = r
		return &msg, nil
	case <-r.link.done:
		return nil, r.link.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Address returns the link's address.
func (r *Receiver) Address() string {
	if r.link.source == nil {
		return ""
	}
	return r.link.source.Address
}

// Close closes the Receiver and AMQP link.
//
// If ctx expires while waiting for servers response, ctx.Err() will be returned.
// The session will continue to wait for the response until the Session or Client
// is closed.
func (r *Receiver) Close(ctx context.Context) error {
	return r.link.Close(ctx)
}

type messageDisposition struct {
	id    uint32
	state interface{}
}

func (r *Receiver) dispositionBatcher() {
	// batch operations:
	// Keep track of the first and last delivery ID, incrementing as
	// Accept() is called. After last-first == batchSize, send disposition.
	// If Reject()/Release() is called, send one disposition for previously
	// accepted, and one for the rejected/released message. If messages are
	// accepted out of order, send any existing batch and the current message.
	var (
		batchSize    = r.maxCredit
		batchStarted bool
		first        uint32
		last         uint32
	)

	// create an unstarted timer
	batchTimer := time.NewTimer(1 * time.Minute)
	batchTimer.Stop()
	defer batchTimer.Stop()

	for {
		select {
		case msgDis := <-r.dispositions:

			// not accepted or batch out of order
			_, isAccept := msgDis.state.(*stateAccepted)
			if !isAccept || (batchStarted && last+1 != msgDis.id) {
				// send the current batch, if any
				if batchStarted {
					lastCopy := last
					err := r.sendDisposition(first, &lastCopy, &stateAccepted{})
					if err != nil {
						r.inFlight.remove(first, &lastCopy, err)
					}
					batchStarted = false
				}

				// send the current message
				err := r.sendDisposition(msgDis.id, nil, msgDis.state)
				if err != nil {
					r.inFlight.remove(msgDis.id, nil, err)
				}
				continue
			}

			if batchStarted {
				// increment last
				last++
			} else {
				// start new batch
				batchStarted = true
				first = msgDis.id
				last = msgDis.id
				batchTimer.Reset(r.batchMaxAge)
			}

			// send batch if current size == batchSize
			if last-first+1 >= batchSize {
				lastCopy := last
				err := r.sendDisposition(first, &lastCopy, &stateAccepted{})
				if err != nil {
					r.inFlight.remove(first, &lastCopy, err)
				}
				batchStarted = false
				if !batchTimer.Stop() {
					<-batchTimer.C // batch timer must be drained if stop returns false
				}
			}

		// maxBatchAge elapsed, send batch
		case <-batchTimer.C:
			lastCopy := last
			err := r.sendDisposition(first, &lastCopy, &stateAccepted{})
			if err != nil {
				r.inFlight.remove(first, &lastCopy, err)
			}
			batchStarted = false
			batchTimer.Stop()

		case <-r.link.done:
			return
		}
	}
}

// sendDisposition sends a disposition frame to the peer
func (r *Receiver) sendDisposition(first uint32, last *uint32, state interface{}) error {
	fr := &performDisposition{
		Role:    roleReceiver,
		First:   first,
		Last:    last,
		Settled: r.link.receiverSettleMode == nil || *r.link.receiverSettleMode == ModeFirst,
		State:   state,
	}

	debug(1, "TX: %s", fr)
	return r.link.session.txFrame(fr, nil)
}

func (r *Receiver) messageDisposition(id uint32, state interface{}) error {
	var wait chan error
	if r.link.receiverSettleMode != nil && *r.link.receiverSettleMode == ModeSecond {
		wait = r.inFlight.add(id)
	}

	if r.batching {
		r.dispositions <- messageDisposition{id: id, state: state}
	} else {
		err := r.sendDisposition(id, nil, state)
		if err != nil {
			return err
		}
	}

	if wait == nil {
		return nil
	}

	return <-wait
}

// inFlight tracks in-flight message dispositions allowing receivers
// to block waiting for the server to respond when an appropriate
// settlement mode is configured.
type inFlight struct {
	mu sync.Mutex
	m  map[uint32]chan error
}

func (f *inFlight) add(id uint32) chan error {
	wait := make(chan error, 1)

	f.mu.Lock()
	if f.m == nil {
		f.m = map[uint32]chan error{id: wait}
	} else {
		f.m[id] = wait
	}
	f.mu.Unlock()

	return wait
}

func (f *inFlight) remove(first uint32, last *uint32, err error) {
	f.mu.Lock()

	if f.m == nil {
		f.mu.Unlock()
		return
	}

	ll := first
	if last != nil {
		ll = *last
	}

	for i := first; i <= ll; i++ {
		wait, ok := f.m[i]
		if ok {
			wait <- err
			delete(f.m, i)
		}
	}

	f.mu.Unlock()
}

func (f *inFlight) clear(err error) {
	f.mu.Lock()
	for id, wait := range f.m {
		wait <- err
		delete(f.m, id)
	}
	f.mu.Unlock()
}

const maxTransferFrameHeader = 66 // determined by calcMaxTransferFrameHeader

func calcMaxTransferFrameHeader() int {
	var buf buffer

	maxUint32 := uint32(math.MaxUint32)
	receiverSettleMode := ReceiverSettleMode(0)
	err := writeFrame(&buf, frame{
		type_:   frameTypeAMQP,
		channel: math.MaxUint16,
		body: &performTransfer{
			Handle:             maxUint32,
			DeliveryID:         &maxUint32,
			DeliveryTag:        bytes.Repeat([]byte{'a'}, 32),
			MessageFormat:      &maxUint32,
			Settled:            true,
			More:               true,
			ReceiverSettleMode: &receiverSettleMode,
			State:              nil, // TODO: determine whether state should be included in size
			Resume:             true,
			Aborted:            true,
			Batchable:          true,
			// Payload omitted as it is appended directly without any header
		},
	})
	if err != nil {
		panic(err)
	}

	return buf.len()
}
