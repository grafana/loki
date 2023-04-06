// Copyright 2012-2022 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/jwt/v2"
)

// Type of client connection.
const (
	// CLIENT is an end user.
	CLIENT = iota
	// ROUTER represents another server in the cluster.
	ROUTER
	// GATEWAY is a link between 2 clusters.
	GATEWAY
	// SYSTEM is an internal system client.
	SYSTEM
	// LEAF is for leaf node connections.
	LEAF
	// JETSTREAM is an internal jetstream client.
	JETSTREAM
	// ACCOUNT is for the internal client for accounts.
	ACCOUNT
)

// Extended type of a CLIENT connection. This is returned by c.clientType()
// and indicate what type of client connection we are dealing with.
// If invoked on a non CLIENT connection, NON_CLIENT type is returned.
const (
	// If the connection is not a CLIENT connection.
	NON_CLIENT = iota
	// Regular NATS client.
	NATS
	// MQTT client.
	MQTT
	// Websocket client.
	WS
)

const (
	// ClientProtoZero is the original Client protocol from 2009.
	// http://nats.io/documentation/internals/nats-protocol/
	ClientProtoZero = iota
	// ClientProtoInfo signals a client can receive more then the original INFO block.
	// This can be used to update clients on other cluster members, etc.
	ClientProtoInfo
)

const (
	pingProto = "PING" + _CRLF_
	pongProto = "PONG" + _CRLF_
	errProto  = "-ERR '%s'" + _CRLF_
	okProto   = "+OK" + _CRLF_
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const (
	// Scratch buffer size for the processMsg() calls.
	msgScratchSize  = 1024
	msgHeadProto    = "RMSG "
	msgHeadProtoLen = len(msgHeadProto)

	// For controlling dynamic buffer sizes.
	startBufSize    = 512   // For INFO/CONNECT block
	minBufSize      = 64    // Smallest to shrink to for PING/PONG
	maxBufSize      = 65536 // 64k
	shortsToShrink  = 2     // Trigger to shrink dynamic buffers
	maxFlushPending = 10    // Max fsps to have in order to wait for writeLoop
	readLoopReport  = 2 * time.Second

	// Server should not send a PING (for RTT) before the first PONG has
	// been sent to the client. However, in case some client libs don't
	// send CONNECT+PING, cap the maximum time before server can send
	// the RTT PING.
	maxNoRTTPingBeforeFirstPong = 2 * time.Second

	// For stalling fast producers
	stallClientMinDuration = 100 * time.Millisecond
	stallClientMaxDuration = time.Second

	// Threshold for not knowingly doing a potential blocking operation when internal and on a route or gateway or leafnode.
	noBlockThresh = 500 * time.Millisecond
)

var readLoopReportThreshold = readLoopReport

// Represent client booleans with a bitmask
type clientFlag uint16

const (
	hdrLine      = "NATS/1.0\r\n"
	emptyHdrLine = "NATS/1.0\r\n\r\n"
)

// Some client state represented as flags
const (
	connectReceived        clientFlag = 1 << iota // The CONNECT proto has been received
	infoReceived                                  // The INFO protocol has been received
	firstPongSent                                 // The first PONG has been sent
	handshakeComplete                             // For TLS clients, indicate that the handshake is complete
	flushOutbound                                 // Marks client as having a flushOutbound call in progress.
	noReconnect                                   // Indicate that on close, this connection should not attempt a reconnect
	closeConnection                               // Marks that closeConnection has already been called.
	connMarkedClosed                              // Marks that markConnAsClosed has already been called.
	writeLoopStarted                              // Marks that the writeLoop has been started.
	skipFlushOnClose                              // Marks that flushOutbound() should not be called on connection close.
	expectConnect                                 // Marks if this connection is expected to send a CONNECT
	connectProcessFinished                        // Marks if this connection has finished the connect process.
)

// set the flag (would be equivalent to set the boolean to true)
func (cf *clientFlag) set(c clientFlag) {
	*cf |= c
}

// clear the flag (would be equivalent to set the boolean to false)
func (cf *clientFlag) clear(c clientFlag) {
	*cf &= ^c
}

// isSet returns true if the flag is set, false otherwise
func (cf clientFlag) isSet(c clientFlag) bool {
	return cf&c != 0
}

// setIfNotSet will set the flag `c` only if that flag was not already
// set and return true to indicate that the flag has been set. Returns
// false otherwise.
func (cf *clientFlag) setIfNotSet(c clientFlag) bool {
	if *cf&c == 0 {
		*cf |= c
		return true
	}
	return false
}

// ClosedState is the reason client was closed. This will
// be passed into calls to clearConnection, but will only
// be stored in ConnInfo for monitoring.
type ClosedState int

const (
	ClientClosed = ClosedState(iota + 1)
	AuthenticationTimeout
	AuthenticationViolation
	TLSHandshakeError
	SlowConsumerPendingBytes
	SlowConsumerWriteDeadline
	WriteError
	ReadError
	ParseError
	StaleConnection
	ProtocolViolation
	BadClientProtocolVersion
	WrongPort
	MaxAccountConnectionsExceeded
	MaxConnectionsExceeded
	MaxPayloadExceeded
	MaxControlLineExceeded
	MaxSubscriptionsExceeded
	DuplicateRoute
	RouteRemoved
	ServerShutdown
	AuthenticationExpired
	WrongGateway
	MissingAccount
	Revocation
	InternalClient
	MsgHeaderViolation
	NoRespondersRequiresHeaders
	ClusterNameConflict
	DuplicateRemoteLeafnodeConnection
	DuplicateClientID
	DuplicateServerName
	MinimumVersionRequired
	ClusterNamesIdentical
)

// Some flags passed to processMsgResults
const pmrNoFlag int = 0
const (
	pmrCollectQueueNames int = 1 << iota
	pmrIgnoreEmptyQueueFilter
	pmrAllowSendFromRouteToRoute
	pmrMsgImportedFromService
)

type client struct {
	// Here first because of use of atomics, and memory alignment.
	stats
	gwReplyMapping
	kind  int
	srv   *Server
	acc   *Account
	perms *permissions
	in    readCache
	parseState
	opts       ClientOpts
	rrTracking *rrTracking
	mpay       int32
	msubs      int32
	mcl        int32
	mu         sync.Mutex
	cid        uint64
	start      time.Time
	nonce      []byte
	pubKey     string
	nc         net.Conn
	ncs        atomic.Value
	out        outbound
	user       *NkeyUser
	host       string
	port       uint16
	subs       map[string]*subscription
	replies    map[string]*resp
	mperms     *msgDeny
	darray     []string
	pcd        map[*client]struct{}
	atmr       *time.Timer
	ping       pinfo
	msgb       [msgScratchSize]byte
	last       time.Time
	headers    bool

	rtt      time.Duration
	rttStart time.Time

	route *route
	gw    *gateway
	leaf  *leaf
	ws    *websocket
	mqtt  *mqtt

	flags clientFlag // Compact booleans into a single field. Size will be increased when needed.

	rref byte

	trace bool
	echo  bool
	noIcb bool

	tags    jwt.TagList
	nameTag string

	tlsTo *time.Timer
}

type rrTracking struct {
	rmap map[string]*remoteLatency
	ptmr *time.Timer
	lrt  time.Duration
}

// Struct for PING initiation from the server.
type pinfo struct {
	tmr  *time.Timer
	last time.Time
	out  int
}

// outbound holds pending data for a socket.
type outbound struct {
	nb  net.Buffers   // Pending buffers for send, each has fixed capacity as per nbPool below.
	wnb net.Buffers   // Working copy of "nb", reused on each flushOutbound call, partial writes may leave entries here for next iteration.
	pb  int64         // Total pending/queued bytes.
	fsp int32         // Flush signals that are pending per producer from readLoop's pcd.
	sg  *sync.Cond    // To signal writeLoop that there is data to flush.
	wdl time.Duration // Snapshot of write deadline.
	mp  int64         // Snapshot of max pending for client.
	lft time.Duration // Last flush time for Write.
	stc chan struct{} // Stall chan we create to slow down producers on overrun, e.g. fan-in.
}

const nbPoolSizeSmall = 4096  // Underlying array size of small buffer
const nbPoolSizeLarge = 65536 // Underlying array size of large buffer

var nbPoolSmall = &sync.Pool{
	New: func() any {
		b := [nbPoolSizeSmall]byte{}
		return &b
	},
}

var nbPoolLarge = &sync.Pool{
	New: func() any {
		b := [nbPoolSizeLarge]byte{}
		return &b
	},
}

func nbPoolPut(b []byte) {
	switch cap(b) {
	case nbPoolSizeSmall:
		b := (*[nbPoolSizeSmall]byte)(b[0:nbPoolSizeSmall])
		nbPoolSmall.Put(b)
	case nbPoolSizeLarge:
		b := (*[nbPoolSizeLarge]byte)(b[0:nbPoolSizeLarge])
		nbPoolLarge.Put(b)
	default:
		// Ignore frames that are the wrong size, this might happen
		// with WebSocket/MQTT messages as they are framed
	}
}

type perm struct {
	allow *Sublist
	deny  *Sublist
}

type permissions struct {
	// Have these 2 first for memory alignment due to the use of atomic.
	pcsz   int32
	prun   int32
	sub    perm
	pub    perm
	resp   *ResponsePermission
	pcache sync.Map
}

// This is used to dynamically track responses and reply subjects
// for dynamic permissioning.
type resp struct {
	t time.Time
	n int
}

// msgDeny is used when a user permission for subscriptions has a deny
// clause but a subscription could be made that is of broader scope.
// e.g. deny = "foo", but user subscribes to "*". That subscription should
// succeed but no message sent on foo should be delivered.
type msgDeny struct {
	deny   *Sublist
	dcache map[string]bool
}

// routeTarget collects information regarding routes and queue groups for
// sending information to a remote.
type routeTarget struct {
	sub *subscription
	qs  []byte
	_qs [32]byte
}

const (
	maxResultCacheSize   = 512
	maxDenyPermCacheSize = 256
	maxPermCacheSize     = 128
	pruneSize            = 32
	routeTargetInit      = 8
	replyPermLimit       = 4096
)

// Represent read cache booleans with a bitmask
type readCacheFlag uint16

const (
	hasMappings readCacheFlag = 1 << iota // For account subject mappings.
	sysGroup                  = "_sys_"
)

// Used in readloop to cache hot subject lookups and group statistics.
type readCache struct {
	// These are for clients who are bound to a single account.
	genid   uint64
	results map[string]*SublistResult

	// This is for routes and gateways to have their own L1 as well that is account aware.
	pacache map[string]*perAccountCache

	// This is for when we deliver messages across a route. We use this structure
	// to make sure to only send one message and properly scope to queues as needed.
	rts []routeTarget

	prand *rand.Rand

	// These are all temporary totals for an invocation of a read in readloop.
	msgs  int32
	bytes int32
	subs  int32

	rsz int32 // Read buffer size
	srs int32 // Short reads, used for dynamic buffer resizing.

	// These are for readcache flags to avoid locks.
	flags readCacheFlag

	// Capture the time we started processing our readLoop.
	start time.Time
}

// set the flag (would be equivalent to set the boolean to true)
func (rcf *readCacheFlag) set(c readCacheFlag) {
	*rcf |= c
}

// clear the flag (would be equivalent to set the boolean to false)
func (rcf *readCacheFlag) clear(c readCacheFlag) {
	*rcf &= ^c
}

// isSet returns true if the flag is set, false otherwise
func (rcf readCacheFlag) isSet(c readCacheFlag) bool {
	return rcf&c != 0
}

const (
	defaultMaxPerAccountCacheSize   = 4096
	defaultPrunePerAccountCacheSize = 256
	defaultClosedSubsCheckInterval  = 5 * time.Minute
)

var (
	maxPerAccountCacheSize   = defaultMaxPerAccountCacheSize
	prunePerAccountCacheSize = defaultPrunePerAccountCacheSize
	closedSubsCheckInterval  = defaultClosedSubsCheckInterval
)

// perAccountCache is for L1 semantics for inbound messages from a route or gateway to mimic the performance of clients.
type perAccountCache struct {
	acc     *Account
	results *SublistResult
	genid   uint64
}

func (c *client) String() (id string) {
	loaded := c.ncs.Load()
	if loaded != nil {
		return loaded.(string)
	}

	return _EMPTY_
}

// GetNonce returns the nonce that was presented to the user on connection
func (c *client) GetNonce() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.nonce
}

// GetName returns the application supplied name for the connection.
func (c *client) GetName() string {
	c.mu.Lock()
	name := c.opts.Name
	c.mu.Unlock()
	return name
}

// GetOpts returns the client options provided by the application.
func (c *client) GetOpts() *ClientOpts {
	return &c.opts
}

// GetTLSConnectionState returns the TLS ConnectionState if TLS is enabled, nil
// otherwise. Implements the ClientAuth interface.
func (c *client) GetTLSConnectionState() *tls.ConnectionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nc == nil {
		return nil
	}
	tc, ok := c.nc.(*tls.Conn)
	if !ok {
		return nil
	}
	state := tc.ConnectionState()
	return &state
}

// For CLIENT connections, this function returns the client type, that is,
// NATS (for regular clients), MQTT or WS for websocket.
// If this is invoked for a non CLIENT connection, NON_CLIENT is returned.
//
// This function does not lock the client and accesses fields that are supposed
// to be immutable and therefore it can be invoked outside of the client's lock.
func (c *client) clientType() int {
	switch c.kind {
	case CLIENT:
		if c.isMqtt() {
			return MQTT
		} else if c.isWebsocket() {
			return WS
		}
		return NATS
	default:
		return NON_CLIENT
	}
}

var clientTypeStringMap = map[int]string{
	NON_CLIENT: _EMPTY_,
	NATS:       "nats",
	WS:         "websocket",
	MQTT:       "mqtt",
}

func (c *client) clientTypeString() string {
	if typeStringVal, ok := clientTypeStringMap[c.clientType()]; ok {
		return typeStringVal
	}
	return _EMPTY_
}

// This is the main subscription struct that indicates
// interest in published messages.
// FIXME(dlc) - This is getting bloated for normal subs, need
// to optionally have an opts section for non-normal stuff.
type subscription struct {
	client  *client
	im      *streamImport // This is for import stream support.
	rsi     bool
	si      bool
	shadow  []*subscription // This is to track shadowed accounts.
	icb     msgHandler
	subject []byte
	queue   []byte
	sid     []byte
	origin  []byte
	nm      int64
	max     int64
	qw      int32
	closed  int32
	mqtt    *mqttSub
}

// Indicate that this subscription is closed.
// This is used in pruning of route and gateway cache items.
func (s *subscription) close() {
	atomic.StoreInt32(&s.closed, 1)
}

// Return true if this subscription was unsubscribed
// or its connection has been closed.
func (s *subscription) isClosed() bool {
	return atomic.LoadInt32(&s.closed) == 1
}

type ClientOpts struct {
	Echo         bool   `json:"echo"`
	Verbose      bool   `json:"verbose"`
	Pedantic     bool   `json:"pedantic"`
	TLSRequired  bool   `json:"tls_required"`
	Nkey         string `json:"nkey,omitempty"`
	JWT          string `json:"jwt,omitempty"`
	Sig          string `json:"sig,omitempty"`
	Token        string `json:"auth_token,omitempty"`
	Username     string `json:"user,omitempty"`
	Password     string `json:"pass,omitempty"`
	Name         string `json:"name"`
	Lang         string `json:"lang"`
	Version      string `json:"version"`
	Protocol     int    `json:"protocol"`
	Account      string `json:"account,omitempty"`
	AccountNew   bool   `json:"new_account,omitempty"`
	Headers      bool   `json:"headers,omitempty"`
	NoResponders bool   `json:"no_responders,omitempty"`

	// Routes and Leafnodes only
	Import *SubjectPermission `json:"import,omitempty"`
	Export *SubjectPermission `json:"export,omitempty"`
}

var defaultOpts = ClientOpts{Verbose: true, Pedantic: true, Echo: true}
var internalOpts = ClientOpts{Verbose: false, Pedantic: false, Echo: false}

func (c *client) setTraceLevel() {
	if c.kind == SYSTEM && !(atomic.LoadInt32(&c.srv.logging.traceSysAcc) != 0) {
		c.trace = false
	} else {
		c.trace = (atomic.LoadInt32(&c.srv.logging.trace) != 0)
	}
}

// Lock should be held
func (c *client) initClient() {
	s := c.srv
	c.cid = atomic.AddUint64(&s.gcid, 1)

	// Outbound data structure setup
	c.out.sg = sync.NewCond(&(c.mu))
	opts := s.getOpts()
	// Snapshots to avoid mutex access in fast paths.
	c.out.wdl = opts.WriteDeadline
	c.out.mp = opts.MaxPending
	// Snapshot max control line since currently can not be changed on reload and we
	// were checking it on each call to parse. If this changes and we allow MaxControlLine
	// to be reloaded without restart, this code will need to change.
	c.mcl = int32(opts.MaxControlLine)
	if c.mcl == 0 {
		c.mcl = MAX_CONTROL_LINE_SIZE
	}

	c.subs = make(map[string]*subscription)
	c.echo = true

	c.setTraceLevel()

	// This is a scratch buffer used for processMsg()
	// The msg header starts with "RMSG ", which can be used
	// for both local and routes.
	// in bytes that is [82 77 83 71 32].
	c.msgb = [msgScratchSize]byte{82, 77, 83, 71, 32}

	// This is to track pending clients that have data to be flushed
	// after we process inbound msgs from our own connection.
	c.pcd = make(map[*client]struct{})

	// snapshot the string version of the connection
	var conn string
	if c.nc != nil {
		if addr := c.nc.RemoteAddr(); addr != nil {
			if conn = addr.String(); conn != _EMPTY_ {
				host, port, _ := net.SplitHostPort(conn)
				iPort, _ := strconv.Atoi(port)
				c.host, c.port = host, uint16(iPort)
				if c.isWebsocket() && c.ws.clientIP != _EMPTY_ {
					cip := c.ws.clientIP
					// Surround IPv6 addresses with square brackets, as
					// net.JoinHostPort would do...
					if strings.Contains(cip, ":") {
						cip = "[" + cip + "]"
					}
					conn = fmt.Sprintf("%s/%s", cip, conn)
				}
				// Now that we have extracted host and port, escape
				// the string because it is going to be used in Sprintf
				conn = strings.ReplaceAll(conn, "%", "%%")
			}
		}
	}

	switch c.kind {
	case CLIENT:
		switch c.clientType() {
		case NATS:
			c.ncs.Store(fmt.Sprintf("%s - cid:%d", conn, c.cid))
		case WS:
			c.ncs.Store(fmt.Sprintf("%s - wid:%d", conn, c.cid))
		case MQTT:
			var ws string
			if c.isWebsocket() {
				ws = "_ws"
			}
			c.ncs.Store(fmt.Sprintf("%s - mid%s:%d", conn, ws, c.cid))
		}
	case ROUTER:
		c.ncs.Store(fmt.Sprintf("%s - rid:%d", conn, c.cid))
	case GATEWAY:
		c.ncs.Store(fmt.Sprintf("%s - gid:%d", conn, c.cid))
	case LEAF:
		var ws string
		if c.isWebsocket() {
			ws = "_ws"
		}
		c.ncs.Store(fmt.Sprintf("%s - lid%s:%d", conn, ws, c.cid))
	case SYSTEM:
		c.ncs.Store("SYSTEM")
	case JETSTREAM:
		c.ncs.Store("JETSTREAM")
	case ACCOUNT:
		c.ncs.Store("ACCOUNT")
	}
}

// RemoteAddress expose the Address of the client connection,
// nil when not connected or unknown
func (c *client) RemoteAddress() net.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.nc == nil {
		return nil
	}

	return c.nc.RemoteAddr()
}

// Helper function to report errors.
func (c *client) reportErrRegisterAccount(acc *Account, err error) {
	if err == ErrTooManyAccountConnections {
		c.maxAccountConnExceeded()
		return
	}
	c.Errorf("Problem registering with account %q: %s", acc.Name, err)
	c.sendErr("Failed Account Registration")
}

// Kind returns the client kind and will be one of the defined constants like CLIENT, ROUTER, GATEWAY, LEAF
func (c *client) Kind() int {
	c.mu.Lock()
	kind := c.kind
	c.mu.Unlock()

	return kind
}

// registerWithAccount will register the given user with a specific
// account. This will change the subject namespace.
func (c *client) registerWithAccount(acc *Account) error {
	if acc == nil || acc.sl == nil {
		return ErrBadAccount
	}
	// If we were previously registered, usually to $G, do accounting here to remove.
	if c.acc != nil {
		if prev := c.acc.removeClient(c); prev == 1 && c.srv != nil {
			c.srv.decActiveAccounts()
		}
	}

	c.mu.Lock()
	kind := c.kind
	srv := c.srv
	c.acc = acc
	c.applyAccountLimits()
	c.mu.Unlock()

	// Check if we have a max connections violation
	if kind == CLIENT && acc.MaxTotalConnectionsReached() {
		return ErrTooManyAccountConnections
	} else if kind == LEAF && acc.MaxTotalLeafNodesReached() {
		return ErrTooManyAccountConnections
	}

	// Add in new one.
	if prev := acc.addClient(c); prev == 0 && srv != nil {
		srv.incActiveAccounts()
	}

	return nil
}

// Helper to determine if we have met or exceeded max subs.
func (c *client) subsAtLimit() bool {
	return c.msubs != jwt.NoLimit && len(c.subs) >= int(c.msubs)
}

func minLimit(value *int32, limit int32) bool {
	if *value != jwt.NoLimit {
		if limit != jwt.NoLimit {
			if limit < *value {
				*value = limit
				return true
			}
		}
	} else if limit != jwt.NoLimit {
		*value = limit
		return true
	}
	return false
}

// Apply account limits
// Lock is held on entry.
// FIXME(dlc) - Should server be able to override here?
func (c *client) applyAccountLimits() {
	if c.acc == nil || (c.kind != CLIENT && c.kind != LEAF) {
		return
	}
	c.mpay = jwt.NoLimit
	c.msubs = jwt.NoLimit
	if c.opts.JWT != _EMPTY_ { // user jwt implies account
		if uc, _ := jwt.DecodeUserClaims(c.opts.JWT); uc != nil {
			c.mpay = int32(uc.Limits.Payload)
			c.msubs = int32(uc.Limits.Subs)
			if uc.IssuerAccount != _EMPTY_ && uc.IssuerAccount != uc.Issuer {
				if scope, ok := c.acc.signingKeys[uc.Issuer]; ok {
					if userScope, ok := scope.(*jwt.UserScope); ok {
						// if signing key disappeared or changed and we don't get here, the client will be disconnected
						c.mpay = int32(userScope.Template.Limits.Payload)
						c.msubs = int32(userScope.Template.Limits.Subs)
					}
				}
			}
		}
	}
	minLimit(&c.mpay, c.acc.mpay)
	minLimit(&c.msubs, c.acc.msubs)
	s := c.srv
	opts := s.getOpts()
	mPay := opts.MaxPayload
	// options encode unlimited differently
	if mPay == 0 {
		mPay = jwt.NoLimit
	}
	mSubs := int32(opts.MaxSubs)
	if mSubs == 0 {
		mSubs = jwt.NoLimit
	}
	wasUnlimited := c.mpay == jwt.NoLimit
	if minLimit(&c.mpay, mPay) && !wasUnlimited {
		c.Errorf("Max Payload set to %d from server overrides account or user config", opts.MaxPayload)
	}
	wasUnlimited = c.msubs == jwt.NoLimit
	if minLimit(&c.msubs, mSubs) && !wasUnlimited {
		c.Errorf("Max Subscriptions set to %d from server overrides account or user config", opts.MaxSubs)
	}
	if c.subsAtLimit() {
		go func() {
			c.maxSubsExceeded()
			time.Sleep(20 * time.Millisecond)
			c.closeConnection(MaxSubscriptionsExceeded)
		}()
	}
}

// RegisterUser allows auth to call back into a new client
// with the authenticated user. This is used to map
// any permissions into the client and setup accounts.
func (c *client) RegisterUser(user *User) {
	// Register with proper account and sublist.
	if user.Account != nil {
		if err := c.registerWithAccount(user.Account); err != nil {
			c.reportErrRegisterAccount(user.Account, err)
			return
		}
	}

	c.mu.Lock()

	// Assign permissions.
	if user.Permissions == nil {
		// Reset perms to nil in case client previously had them.
		c.perms = nil
		c.mperms = nil
	} else {
		c.setPermissions(user.Permissions)
	}

	// allows custom authenticators to set a username to be reported in
	// server events and more
	if user.Username != _EMPTY_ {
		c.opts.Username = user.Username
	}

	c.mu.Unlock()
}

// RegisterNkeyUser allows auth to call back into a new nkey
// client with the authenticated user. This is used to map
// any permissions into the client and setup accounts.
func (c *client) RegisterNkeyUser(user *NkeyUser) error {
	// Register with proper account and sublist.
	if user.Account != nil {
		if err := c.registerWithAccount(user.Account); err != nil {
			c.reportErrRegisterAccount(user.Account, err)
			return err
		}
	}

	c.mu.Lock()
	c.user = user
	// Assign permissions.
	if user.Permissions == nil {
		// Reset perms to nil in case client previously had them.
		c.perms = nil
		c.mperms = nil
	} else {
		c.setPermissions(user.Permissions)
	}
	c.mu.Unlock()
	return nil
}

func splitSubjectQueue(sq string) ([]byte, []byte, error) {
	vals := strings.Fields(strings.TrimSpace(sq))
	s := []byte(vals[0])
	var q []byte
	if len(vals) == 2 {
		q = []byte(vals[1])
	} else if len(vals) > 2 {
		return nil, nil, fmt.Errorf("invalid subject-queue %q", sq)
	}
	return s, q, nil
}

// Initializes client.perms structure.
// Lock is held on entry.
func (c *client) setPermissions(perms *Permissions) {
	if perms == nil {
		return
	}
	c.perms = &permissions{}

	// Loop over publish permissions
	if perms.Publish != nil {
		if perms.Publish.Allow != nil {
			c.perms.pub.allow = NewSublistWithCache()
		}
		for _, pubSubject := range perms.Publish.Allow {
			sub := &subscription{subject: []byte(pubSubject)}
			c.perms.pub.allow.Insert(sub)
		}
		if len(perms.Publish.Deny) > 0 {
			c.perms.pub.deny = NewSublistWithCache()
		}
		for _, pubSubject := range perms.Publish.Deny {
			sub := &subscription{subject: []byte(pubSubject)}
			c.perms.pub.deny.Insert(sub)
		}
	}

	// Check if we are allowed to send responses.
	if perms.Response != nil {
		rp := *perms.Response
		c.perms.resp = &rp
		c.replies = make(map[string]*resp)
	}

	// Loop over subscribe permissions
	if perms.Subscribe != nil {
		var err error
		if len(perms.Subscribe.Allow) > 0 {
			c.perms.sub.allow = NewSublistWithCache()
		}
		for _, subSubject := range perms.Subscribe.Allow {
			sub := &subscription{}
			sub.subject, sub.queue, err = splitSubjectQueue(subSubject)
			if err != nil {
				c.Errorf("%s", err.Error())
				continue
			}
			c.perms.sub.allow.Insert(sub)
		}
		if len(perms.Subscribe.Deny) > 0 {
			c.perms.sub.deny = NewSublistWithCache()
			// Also hold onto this array for later.
			c.darray = perms.Subscribe.Deny
		}
		for _, subSubject := range perms.Subscribe.Deny {
			sub := &subscription{}
			sub.subject, sub.queue, err = splitSubjectQueue(subSubject)
			if err != nil {
				c.Errorf("%s", err.Error())
				continue
			}
			c.perms.sub.deny.Insert(sub)
		}
	}

	// If we are a leafnode and we are the hub copy the extracted perms
	// to resend back to soliciting server. These are reversed from the
	// way routes interpret them since this is how the soliciting server
	// will receive these back in an update INFO.
	if c.isHubLeafNode() {
		c.opts.Import = perms.Subscribe
		c.opts.Export = perms.Publish
	}
}

type denyType int

const (
	pub = denyType(iota + 1)
	sub
	both
)

// Merge client.perms structure with additional pub deny permissions
// Lock is held on entry.
func (c *client) mergeDenyPermissions(what denyType, denyPubs []string) {
	if len(denyPubs) == 0 {
		return
	}
	if c.perms == nil {
		c.perms = &permissions{}
	}
	var perms []*perm
	switch what {
	case pub:
		perms = []*perm{&c.perms.pub}
	case sub:
		perms = []*perm{&c.perms.sub}
	case both:
		perms = []*perm{&c.perms.pub, &c.perms.sub}
	}
	for _, p := range perms {
		if p.deny == nil {
			p.deny = NewSublistWithCache()
		}
	FOR_DENY:
		for _, subj := range denyPubs {
			r := p.deny.Match(subj)
			for _, v := range r.qsubs {
				for _, s := range v {
					if string(s.subject) == subj {
						continue FOR_DENY
					}
				}
			}
			for _, s := range r.psubs {
				if string(s.subject) == subj {
					continue FOR_DENY
				}
			}
			sub := &subscription{subject: []byte(subj)}
			p.deny.Insert(sub)
		}
	}
}

// Merge client.perms structure with additional pub deny permissions
// Client lock must not be held on entry
func (c *client) mergeDenyPermissionsLocked(what denyType, denyPubs []string) {
	c.mu.Lock()
	c.mergeDenyPermissions(what, denyPubs)
	c.mu.Unlock()
}

// Check to see if we have an expiration for the user JWT via base claims.
// FIXME(dlc) - Clear on connect with new JWT.
func (c *client) setExpiration(claims *jwt.ClaimsData, validFor time.Duration) {
	if claims.Expires == 0 {
		if validFor != 0 {
			c.setExpirationTimer(validFor)
		}
		return
	}
	expiresAt := time.Duration(0)
	tn := time.Now().Unix()
	if claims.Expires > tn {
		expiresAt = time.Duration(claims.Expires-tn) * time.Second
	}
	if validFor != 0 && validFor < expiresAt {
		c.setExpirationTimer(validFor)
	} else {
		c.setExpirationTimer(expiresAt)
	}
}

// This will load up the deny structure used for filtering delivered
// messages based on a deny clause for subscriptions.
// Lock should be held.
func (c *client) loadMsgDenyFilter() {
	c.mperms = &msgDeny{NewSublistWithCache(), make(map[string]bool)}
	for _, sub := range c.darray {
		c.mperms.deny.Insert(&subscription{subject: []byte(sub)})
	}
}

// writeLoop is the main socket write functionality.
// Runs in its own Go routine.
func (c *client) writeLoop() {
	defer c.srv.grWG.Done()
	c.mu.Lock()
	if c.isClosed() {
		c.mu.Unlock()
		return
	}
	c.flags.set(writeLoopStarted)
	c.mu.Unlock()

	// Used to check that we did flush from last wake up.
	waitOk := true
	var closed bool

	// Main loop. Will wait to be signaled and then will use
	// buffered outbound structure for efficient writev to the underlying socket.
	for {
		c.mu.Lock()
		if closed = c.isClosed(); !closed {
			owtf := c.out.fsp > 0 && c.out.pb < maxBufSize && c.out.fsp < maxFlushPending
			if waitOk && (c.out.pb == 0 || owtf) {
				c.out.sg.Wait()
				// Check that connection has not been closed while lock was released
				// in the conditional wait.
				closed = c.isClosed()
			}
		}
		if closed {
			c.flushAndClose(false)
			c.mu.Unlock()

			// We should always call closeConnection() to ensure that state is
			// properly cleaned-up. It will be a no-op if already done.
			c.closeConnection(WriteError)

			// Now explicitly call reconnect(). Thanks to ref counting, we know
			// that the reconnect will execute only after connection has been
			// removed from the server state.
			c.reconnect()
			return
		}
		// Flush data
		waitOk = c.flushOutbound()
		c.mu.Unlock()
	}
}

// flushClients will make sure to flush any clients we may have
// sent to during processing. We pass in a budget as a time.Duration
// for how much time to spend in place flushing for this client.
func (c *client) flushClients(budget time.Duration) time.Time {
	last := time.Now().UTC()

	// Check pending clients for flush.
	for cp := range c.pcd {
		// TODO(dlc) - Wonder if it makes more sense to create a new map?
		delete(c.pcd, cp)

		// Queue up a flush for those in the set
		cp.mu.Lock()
		// Update last activity for message delivery
		cp.last = last
		// Remove ourselves from the pending list.
		cp.out.fsp--

		// Just ignore if this was closed.
		if cp.isClosed() {
			cp.mu.Unlock()
			continue
		}

		if budget > 0 && cp.out.lft < 2*budget && cp.flushOutbound() {
			budget -= cp.out.lft
		} else {
			cp.flushSignal()
		}

		cp.mu.Unlock()
	}
	return last
}

// readLoop is the main socket read functionality.
// Runs in its own Go routine.
func (c *client) readLoop(pre []byte) {
	// Grab the connection off the client, it will be cleared on a close.
	// We check for that after the loop, but want to avoid a nil dereference
	c.mu.Lock()
	s := c.srv
	defer s.grWG.Done()
	if c.isClosed() {
		c.mu.Unlock()
		return
	}
	nc := c.nc
	ws := c.isWebsocket()
	if c.isMqtt() {
		c.mqtt.r = &mqttReader{reader: nc}
	}
	c.in.rsz = startBufSize

	// Check the per-account-cache for closed subscriptions
	cpacc := c.kind == ROUTER || c.kind == GATEWAY
	// Last per-account-cache check for closed subscriptions
	lpacc := time.Now()
	acc := c.acc
	var masking bool
	if ws {
		masking = c.ws.maskread
	}
	c.mu.Unlock()

	defer func() {
		if c.isMqtt() {
			s.mqttHandleClosedClient(c)
		}
		// These are used only in the readloop, so we can set them to nil
		// on exit of the readLoop.
		c.in.results, c.in.pacache = nil, nil
	}()

	// Start read buffer.
	b := make([]byte, c.in.rsz)

	// Websocket clients will return several slices if there are multiple
	// websocket frames in the blind read. For non WS clients though, we
	// will always have 1 slice per loop iteration. So we define this here
	// so non WS clients will use bufs[0] = b[:n].
	var _bufs [1][]byte
	bufs := _bufs[:1]

	var wsr *wsReadInfo
	if ws {
		wsr = &wsReadInfo{mask: masking}
		wsr.init()
	}

	for {
		var n int
		var err error

		// If we have a pre buffer parse that first.
		if len(pre) > 0 {
			b = pre
			n = len(pre)
			pre = nil
		} else {
			n, err = nc.Read(b)
			// If we have any data we will try to parse and exit at the end.
			if n == 0 && err != nil {
				c.closeConnection(closedStateForErr(err))
				return
			}
		}
		if ws {
			bufs, err = c.wsRead(wsr, nc, b[:n])
			if bufs == nil && err != nil {
				if err != io.EOF {
					c.Errorf("read error: %v", err)
				}
				c.closeConnection(closedStateForErr(err))
			} else if bufs == nil {
				continue
			}
		} else {
			bufs[0] = b[:n]
		}

		// Check if the account has mappings and if so set the local readcache flag.
		// We check here to make sure any changes such as config reload are reflected here.
		if c.kind == CLIENT || c.kind == LEAF {
			if acc.hasMappings() {
				c.in.flags.set(hasMappings)
			} else {
				c.in.flags.clear(hasMappings)
			}
		}

		c.in.start = time.Now()

		// Clear inbound stats cache
		c.in.msgs = 0
		c.in.bytes = 0
		c.in.subs = 0

		// Main call into parser for inbound data. This will generate callouts
		// to process messages, etc.
		for i := 0; i < len(bufs); i++ {
			if err := c.parse(bufs[i]); err != nil {
				if err == ErrMinimumVersionRequired {
					// Special case here, currently only for leaf node connections.
					// When process the CONNECT protocol, if the minimum version
					// required was not met, an error was printed and sent back to
					// the remote, and connection was closed after a certain delay
					// (to avoid "rapid" reconnection from the remote).
					// We don't need to do any of the things below, simply return.
					return
				}
				if dur := time.Since(c.in.start); dur >= readLoopReportThreshold {
					c.Warnf("Readloop processing time: %v", dur)
				}
				// Need to call flushClients because some of the clients have been
				// assigned messages and their "fsp" incremented, and need now to be
				// decremented and their writeLoop signaled.
				c.flushClients(0)
				// handled inline
				if err != ErrMaxPayload && err != ErrAuthentication {
					c.Error(err)
					c.closeConnection(ProtocolViolation)
				}
				return
			}
		}

		// Updates stats for client and server that were collected
		// from parsing through the buffer.
		if c.in.msgs > 0 {
			atomic.AddInt64(&c.inMsgs, int64(c.in.msgs))
			atomic.AddInt64(&c.inBytes, int64(c.in.bytes))
			if acc != nil {
				atomic.AddInt64(&acc.inMsgs, int64(c.in.msgs))
				atomic.AddInt64(&acc.inBytes, int64(c.in.bytes))
			}
			atomic.AddInt64(&s.inMsgs, int64(c.in.msgs))
			atomic.AddInt64(&s.inBytes, int64(c.in.bytes))
		}

		// Signal to writeLoop to flush to socket.
		last := c.flushClients(0)

		// Update activity, check read buffer size.
		c.mu.Lock()

		// Activity based on interest changes or data/msgs.
		if c.in.msgs > 0 || c.in.subs > 0 {
			c.last = last
		}

		if n >= cap(b) {
			c.in.srs = 0
		} else if n < cap(b)/2 { // divide by 2 b/c we want less than what we would shrink to.
			c.in.srs++
		}

		// Update read buffer size as/if needed.
		if n >= cap(b) && cap(b) < maxBufSize {
			// Grow
			c.in.rsz = int32(cap(b) * 2)
			b = make([]byte, c.in.rsz)
		} else if n < cap(b) && cap(b) > minBufSize && c.in.srs > shortsToShrink {
			// Shrink, for now don't accelerate, ping/pong will eventually sort it out.
			c.in.rsz = int32(cap(b) / 2)
			b = make([]byte, c.in.rsz)
		}
		// re-snapshot the account since it can change during reload, etc.
		acc = c.acc
		// Refresh nc because in some cases, we have upgraded c.nc to TLS.
		nc = c.nc
		c.mu.Unlock()

		// Connection was closed
		if nc == nil {
			return
		}

		if dur := time.Since(c.in.start); dur >= readLoopReportThreshold {
			c.Warnf("Readloop processing time: %v", dur)
		}

		// We could have had a read error from above but still read some data.
		// If so do the close here unconditionally.
		if err != nil {
			c.closeConnection(closedStateForErr(err))
			return
		}

		if cpacc && (c.in.start.Sub(lpacc)) >= closedSubsCheckInterval {
			c.pruneClosedSubFromPerAccountCache()
			lpacc = time.Now()
		}
	}
}

// Returns the appropriate closed state for a given read error.
func closedStateForErr(err error) ClosedState {
	if err == io.EOF {
		return ClientClosed
	}
	return ReadError
}

// collapsePtoNB will place primary onto nb buffer as needed in prep for WriteTo.
// This will return a copy on purpose.
func (c *client) collapsePtoNB() (net.Buffers, int64) {
	if c.isWebsocket() {
		return c.wsCollapsePtoNB()
	}
	return c.out.nb, c.out.pb
}

// This will handle the fixup needed on a partial write.
// Assume pending has been already calculated correctly.
func (c *client) handlePartialWrite(pnb net.Buffers) {
	if c.isWebsocket() {
		c.ws.frames = append(pnb, c.ws.frames...)
		return
	}
}

// flushOutbound will flush outbound buffer to a client.
// Will return true if data was attempted to be written.
// Lock must be held
func (c *client) flushOutbound() bool {
	if c.flags.isSet(flushOutbound) {
		// For CLIENT connections, it is possible that the readLoop calls
		// flushOutbound(). If writeLoop and readLoop compete and we are
		// here we should release the lock to reduce the risk of spinning.
		c.mu.Unlock()
		runtime.Gosched()
		c.mu.Lock()
		return false
	}
	c.flags.set(flushOutbound)
	defer c.flags.clear(flushOutbound)

	// Check for nothing to do.
	if c.nc == nil || c.srv == nil || c.out.pb == 0 {
		return true // true because no need to queue a signal.
	}

	// In the case of a normal socket connection, "collapsed" is just a ref
	// to "nb". In the case of WebSockets, additional framing is added to
	// anything that is waiting in "nb". Also keep a note of how many bytes
	// were queued before we release the mutex.
	collapsed, attempted := c.collapsePtoNB()

	// Frustratingly, (net.Buffers).WriteTo() modifies the receiver so we
	// can't work on "nb" directly — while the mutex is unlocked during IO,
	// something else might call queueOutbound and modify it. So instead we
	// need a working copy — we'll operate on "wnb" instead. Note that in
	// the case of a partial write, "wnb" may have remaining data from the
	// previous write, and in the case of WebSockets, that data may already
	// be framed, so we are careful not to re-frame "wnb" here. Instead we
	// will just frame up "nb" and append it onto whatever is left on "wnb".
	// "nb" will be reset back to its starting position so it can be modified
	// safely by queueOutbound calls.
	c.out.wnb = append(c.out.wnb, collapsed...)
	orig := append(net.Buffers{}, c.out.wnb...)
	c.out.nb = c.out.nb[:0]

	// Since WriteTo is lopping things off the beginning, we need to remember
	// the start position of the underlying array so that we can get back to it.
	// Otherwise we'll always "slide forward" and that will result in reallocs.
	startOfWnb := c.out.wnb[0:]

	// In case it goes away after releasing the lock.
	nc := c.nc

	// Capture this (we change the value in some tests)
	wdl := c.out.wdl

	// Do NOT hold lock during actual IO.
	c.mu.Unlock()

	// flush here
	start := time.Now()

	// FIXME(dlc) - writev will do multiple IOs past 1024 on
	// most platforms, need to account for that with deadline?
	nc.SetWriteDeadline(start.Add(wdl))

	// Actual write to the socket.
	n, err := c.out.wnb.WriteTo(nc)
	nc.SetWriteDeadline(time.Time{})

	lft := time.Since(start)

	// Re-acquire client lock.
	c.mu.Lock()

	// At this point, "wnb" has been mutated by WriteTo and any consumed
	// buffers have been lopped off the beginning, so in order to return
	// them to the pool, we need to look at the difference between "orig"
	// and "wnb".
	for i := 0; i < len(orig)-len(c.out.wnb); i++ {
		nbPoolPut(orig[i])
	}

	// At this point it's possible that "nb" has been modified by another
	// call to queueOutbound while the lock was released, so we'll leave
	// those for the next iteration. Meanwhile it's possible that we only
	// managed a partial write of "wnb", so we'll shift anything that
	// remains up to the beginning of the array to prevent reallocating.
	// Anything left in "wnb" has already been framed for WebSocket conns
	// so leave them alone for the next call to flushOutbound.
	c.out.wnb = append(startOfWnb[:0], c.out.wnb...)

	// If we've written everything but the underlying array of our working
	// buffer has grown excessively then free it — the GC will tidy it up
	// and we can allocate a new one next time.
	if len(c.out.wnb) == 0 && cap(c.out.wnb) > nbPoolSizeLarge*8 {
		c.out.wnb = nil
	}

	// Ignore ErrShortWrite errors, they will be handled as partials.
	if err != nil && err != io.ErrShortWrite {
		// Handle timeout error (slow consumer) differently
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			if closed := c.handleWriteTimeout(n, attempted, len(c.out.nb)); closed {
				return true
			}
		} else {
			// Other errors will cause connection to be closed.
			// For clients, report as debug but for others report as error.
			report := c.Debugf
			if c.kind != CLIENT {
				report = c.Errorf
			}
			report("Error flushing: %v", err)
			c.markConnAsClosed(WriteError)
			return true
		}
	}

	// Update flush time statistics.
	c.out.lft = lft

	// Subtract from pending bytes and messages.
	c.out.pb -= n
	if c.isWebsocket() {
		c.ws.fs -= n
	}

	// Check for partial writes
	// TODO(dlc) - zero write with no error will cause lost message and the writeloop to spin.
	if n != attempted && n > 0 {
		c.handlePartialWrite(c.out.nb)
	}

	// Check that if there is still data to send and writeLoop is in wait,
	// then we need to signal.
	if c.out.pb > 0 {
		c.flushSignal()
	}

	// Check if we have a stalled gate and if so and we are recovering release
	// any stalled producers. Only kind==CLIENT will stall.
	if c.out.stc != nil && (n == attempted || c.out.pb < c.out.mp/2) {
		close(c.out.stc)
		c.out.stc = nil
	}

	return true
}

// This is invoked from flushOutbound() for io/timeout error (slow consumer).
// Returns a boolean to indicate if the connection has been closed or not.
// Lock is held on entry.
func (c *client) handleWriteTimeout(written, attempted int64, numChunks int) bool {
	if tlsConn, ok := c.nc.(*tls.Conn); ok {
		if !tlsConn.ConnectionState().HandshakeComplete {
			// Likely a TLSTimeout error instead...
			c.markConnAsClosed(TLSHandshakeError)
			// Would need to coordinate with tlstimeout()
			// to avoid double logging, so skip logging
			// here, and don't report a slow consumer error.
			return true
		}
	} else if c.flags.isSet(expectConnect) && !c.flags.isSet(connectReceived) {
		// Under some conditions, a connection may hit a slow consumer write deadline
		// before the authorization timeout. If that is the case, then we handle
		// as slow consumer though we do not increase the counter as that can be
		// misleading.
		c.markConnAsClosed(SlowConsumerWriteDeadline)
		return true
	}

	// Slow consumer here..
	atomic.AddInt64(&c.srv.slowConsumers, 1)
	if c.acc != nil {
		atomic.AddInt64(&c.acc.slowConsumers, 1)
	}
	c.Noticef("Slow Consumer Detected: WriteDeadline of %v exceeded with %d chunks of %d total bytes.",
		c.out.wdl, numChunks, attempted)

	// We always close CLIENT connections, or when nothing was written at all...
	if c.kind == CLIENT || written == 0 {
		c.markConnAsClosed(SlowConsumerWriteDeadline)
		return true
	}
	return false
}

// Marks this connection has closed with the given reason.
// Sets the connMarkedClosed flag and skipFlushOnClose depending on the reason.
// Depending on the kind of connection, the connection will be saved.
// If a writeLoop has been started, the final flush will be done there, otherwise
// flush and close of TCP connection is done here in place.
// Returns true if closed in place, flase otherwise.
// Lock is held on entry.
func (c *client) markConnAsClosed(reason ClosedState) {
	// Possibly set skipFlushOnClose flag even if connection has already been
	// mark as closed. The rationale is that a connection may be closed with
	// a reason that justifies a flush (say after sending an -ERR), but then
	// the flushOutbound() gets a write error. If that happens, connection
	// being lost, there is no reason to attempt to flush again during the
	// teardown when the writeLoop exits.
	var skipFlush bool
	switch reason {
	case ReadError, WriteError, SlowConsumerPendingBytes, SlowConsumerWriteDeadline, TLSHandshakeError:
		c.flags.set(skipFlushOnClose)
		skipFlush = true
	}
	if c.flags.isSet(connMarkedClosed) {
		return
	}
	c.flags.set(connMarkedClosed)
	// For a websocket client, unless we are told not to flush, enqueue
	// a websocket CloseMessage based on the reason.
	if !skipFlush && c.isWebsocket() && !c.ws.closeSent {
		c.wsEnqueueCloseMessage(reason)
	}
	// Be consistent with the creation: for routes, gateways and leaf,
	// we use Noticef on create, so use that too for delete.
	if c.srv != nil {
		if c.kind == LEAF {
			c.Noticef("%s connection closed: %s account: %s", c.kindString(), reason, c.acc.traceLabel())
		} else if c.kind == ROUTER || c.kind == GATEWAY {
			c.Noticef("%s connection closed: %s", c.kindString(), reason)
		} else { // Client, System, Jetstream, and Account connections.
			c.Debugf("%s connection closed: %s", c.kindString(), reason)
		}
	}

	// Save off the connection if its a client or leafnode.
	if c.kind == CLIENT || c.kind == LEAF {
		if nc := c.nc; nc != nil && c.srv != nil {
			// TODO: May want to send events to single go routine instead
			// of creating a new go routine for each save.
			go c.srv.saveClosedClient(c, nc, reason)
		}
	}
	// If writeLoop exists, let it do the final flush, close and teardown.
	if c.flags.isSet(writeLoopStarted) {
		// Since we want the writeLoop to do the final flush and tcp close,
		// we want the reconnect to be done there too. However, it should'nt
		// happen before the connection has been removed from the server
		// state (end of closeConnection()). This ref count allows us to
		// guarantee that.
		c.rref++
		c.flushSignal()
		return
	}
	// Flush (if skipFlushOnClose is not set) and close in place. If flushing,
	// use a small WriteDeadline.
	c.flushAndClose(true)
}

// flushSignal will use server to queue the flush IO operation to a pool of flushers.
// Lock must be held.
func (c *client) flushSignal() {
	c.out.sg.Signal()
}

// Traces a message.
// Will NOT check if tracing is enabled, does NOT need the client lock.
func (c *client) traceMsg(msg []byte) {
	maxTrace := c.srv.getOpts().MaxTracedMsgLen
	if maxTrace > 0 && (len(msg)-LEN_CR_LF) > maxTrace {
		tm := fmt.Sprintf("%q", msg[:maxTrace])
		c.Tracef("<<- MSG_PAYLOAD: [\"%s...\"]", tm[1:maxTrace+1])
	} else {
		c.Tracef("<<- MSG_PAYLOAD: [%q]", msg[:len(msg)-LEN_CR_LF])
	}
}

// Traces an incoming operation.
// Will NOT check if tracing is enabled, does NOT need the client lock.
func (c *client) traceInOp(op string, arg []byte) {
	c.traceOp("<<- %s", op, arg)
}

// Traces an outgoing operation.
// Will NOT check if tracing is enabled, does NOT need the client lock.
func (c *client) traceOutOp(op string, arg []byte) {
	c.traceOp("->> %s", op, arg)
}

func (c *client) traceOp(format, op string, arg []byte) {
	opa := []interface{}{}
	if op != "" {
		opa = append(opa, op)
	}
	if arg != nil {
		opa = append(opa, string(arg))
	}
	c.Tracef(format, opa)
}

// Process the information messages from Clients and other Routes.
func (c *client) processInfo(arg []byte) error {
	info := Info{}
	if err := json.Unmarshal(arg, &info); err != nil {
		return err
	}
	switch c.kind {
	case ROUTER:
		c.processRouteInfo(&info)
	case GATEWAY:
		c.processGatewayInfo(&info)
	case LEAF:
		c.processLeafnodeInfo(&info)
	}
	return nil
}

func (c *client) processErr(errStr string) {
	close := true
	switch c.kind {
	case CLIENT:
		c.Errorf("Client Error %s", errStr)
	case ROUTER:
		c.Errorf("Route Error %s", errStr)
	case GATEWAY:
		c.Errorf("Gateway Error %s", errStr)
	case LEAF:
		c.Errorf("Leafnode Error %s", errStr)
		c.leafProcessErr(errStr)
		close = false
	case JETSTREAM:
		c.Errorf("JetStream Error %s", errStr)
	}
	if close {
		c.closeConnection(ParseError)
	}
}

// Password pattern matcher.
var passPat = regexp.MustCompile(`"?\s*pass\S*?"?\s*[:=]\s*"?(([^",\r\n}])*)`)

// removePassFromTrace removes any notion of passwords from trace
// messages for logging.
func removePassFromTrace(arg []byte) []byte {
	if !bytes.Contains(arg, []byte(`pass`)) {
		return arg
	}
	// Take a copy of the connect proto just for the trace message.
	var _arg [4096]byte
	buf := append(_arg[:0], arg...)

	m := passPat.FindAllSubmatchIndex(buf, -1)
	if len(m) == 0 {
		return arg
	}

	redactedPass := []byte("[REDACTED]")
	for _, i := range m {
		if len(i) < 4 {
			continue
		}
		start := i[2]
		end := i[3]

		// Replace password substring.
		buf = append(buf[:start], append(redactedPass, buf[end:]...)...)
		break
	}
	return buf
}

// Returns the RTT by computing the elapsed time since now and `start`.
// On Windows VM where I (IK) run tests, time.Since() will return 0
// (I suspect some time granularity issues). So return at minimum 1ns.
func computeRTT(start time.Time) time.Duration {
	rtt := time.Since(start)
	if rtt <= 0 {
		rtt = time.Nanosecond
	}
	return rtt
}

// processConnect will process a client connect op.
func (c *client) processConnect(arg []byte) error {
	supportsHeaders := c.srv.supportsHeaders()
	c.mu.Lock()
	// If we can't stop the timer because the callback is in progress...
	if !c.clearAuthTimer() {
		// wait for it to finish and handle sending the failure back to
		// the client.
		for !c.isClosed() {
			c.mu.Unlock()
			time.Sleep(25 * time.Millisecond)
			c.mu.Lock()
		}
		c.mu.Unlock()
		return nil
	}
	c.last = time.Now().UTC()
	// Estimate RTT to start.
	if c.kind == CLIENT {
		c.rtt = computeRTT(c.start)
		if c.srv != nil {
			c.clearPingTimer()
			c.setFirstPingTimer()
		}
	}
	kind := c.kind
	srv := c.srv

	// Moved unmarshalling of clients' Options under the lock.
	// The client has already been added to the server map, so it is possible
	// that other routines lookup the client, and access its options under
	// the client's lock, so unmarshalling the options outside of the lock
	// would cause data RACEs.
	if err := json.Unmarshal(arg, &c.opts); err != nil {
		c.mu.Unlock()
		return err
	}
	// Indicate that the CONNECT protocol has been received, and that the
	// server now knows which protocol this client supports.
	c.flags.set(connectReceived)
	// Capture these under lock
	c.echo = c.opts.Echo
	proto := c.opts.Protocol
	verbose := c.opts.Verbose
	lang := c.opts.Lang
	account := c.opts.Account
	accountNew := c.opts.AccountNew

	if c.kind == CLIENT {
		var ncs string
		if c.opts.Version != _EMPTY_ {
			ncs = fmt.Sprintf("v%s", c.opts.Version)
		}
		if c.opts.Lang != _EMPTY_ {
			if c.opts.Version == _EMPTY_ {
				ncs = c.opts.Lang
			} else {
				ncs = fmt.Sprintf("%s:%s", ncs, c.opts.Lang)
			}
		}
		if c.opts.Name != _EMPTY_ {
			if c.opts.Version == _EMPTY_ && c.opts.Lang == _EMPTY_ {
				ncs = c.opts.Name
			} else {
				ncs = fmt.Sprintf("%s:%s", ncs, c.opts.Name)
			}
		}
		if ncs != _EMPTY_ {
			c.ncs.CompareAndSwap(nil, fmt.Sprintf("%s - %q", c, ncs))
		}
	}

	// If websocket client and JWT not in the CONNECT, use the cookie JWT (possibly empty).
	if ws := c.ws; ws != nil && c.opts.JWT == "" {
		c.opts.JWT = ws.cookieJwt
	}
	// when not in operator mode, discard the jwt
	if srv != nil && srv.trustedKeys == nil {
		c.opts.JWT = _EMPTY_
	}
	ujwt := c.opts.JWT

	// For headers both client and server need to support.
	c.headers = supportsHeaders && c.opts.Headers
	c.mu.Unlock()

	if srv != nil {
		// Applicable to clients only:
		// As soon as c.opts is unmarshalled and if the proto is at
		// least ClientProtoInfo, we need to increment the following counter.
		// This is decremented when client is removed from the server's
		// clients map.
		if kind == CLIENT && proto >= ClientProtoInfo {
			srv.mu.Lock()
			srv.cproto++
			srv.mu.Unlock()
		}

		// Check for Auth
		if ok := srv.checkAuthentication(c); !ok {
			// We may fail here because we reached max limits on an account.
			if ujwt != _EMPTY_ {
				c.mu.Lock()
				acc := c.acc
				c.mu.Unlock()
				srv.mu.Lock()
				tooManyAccCons := acc != nil && acc != srv.gacc
				srv.mu.Unlock()
				if tooManyAccCons {
					return ErrTooManyAccountConnections
				}
			}
			c.authViolation()
			return ErrAuthentication
		}

		// Check for Account designation, we used to have this as an optional feature for dynamic
		// sandbox environments. Now its considered an error.
		if accountNew || account != _EMPTY_ {
			c.authViolation()
			return ErrAuthentication
		}

		// If no account designation.
		if c.acc == nil {
			// By default register with the global account.
			c.registerWithAccount(srv.globalAccount())
		}
	}

	switch kind {
	case CLIENT:
		// Check client protocol request if it exists.
		if proto < ClientProtoZero || proto > ClientProtoInfo {
			c.sendErr(ErrBadClientProtocol.Error())
			c.closeConnection(BadClientProtocolVersion)
			return ErrBadClientProtocol
		}
		// Check to see that if no_responders is requested
		// they have header support on as well.
		c.mu.Lock()
		misMatch := c.opts.NoResponders && !c.headers
		c.mu.Unlock()
		if misMatch {
			c.sendErr(ErrNoRespondersRequiresHeaders.Error())
			c.closeConnection(NoRespondersRequiresHeaders)
			return ErrNoRespondersRequiresHeaders
		}
		if verbose {
			c.sendOK()
		}
	case ROUTER:
		// Delegate the rest of processing to the route
		return c.processRouteConnect(srv, arg, lang)
	case GATEWAY:
		// Delegate the rest of processing to the gateway
		return c.processGatewayConnect(arg)
	case LEAF:
		// Delegate the rest of processing to the leaf node
		return c.processLeafNodeConnect(srv, arg, lang)
	}
	return nil
}

func (c *client) sendErrAndErr(err string) {
	c.sendErr(err)
	c.Errorf(err)
}

func (c *client) sendErrAndDebug(err string) {
	c.sendErr(err)
	c.Debugf(err)
}

func (c *client) authTimeout() {
	c.sendErrAndDebug("Authentication Timeout")
	c.closeConnection(AuthenticationTimeout)
}

func (c *client) authExpired() {
	c.sendErrAndDebug("User Authentication Expired")
	c.closeConnection(AuthenticationExpired)
}

func (c *client) accountAuthExpired() {
	c.sendErrAndDebug("Account Authentication Expired")
	c.closeConnection(AuthenticationExpired)
}

func (c *client) authViolation() {
	var s *Server
	var hasTrustedNkeys, hasNkeys, hasUsers bool
	if s = c.srv; s != nil {
		s.mu.Lock()
		hasTrustedNkeys = s.trustedKeys != nil
		hasNkeys = s.nkeys != nil
		hasUsers = s.users != nil
		s.mu.Unlock()
		defer s.sendAuthErrorEvent(c)

	}
	if hasTrustedNkeys {
		c.Errorf("%v", ErrAuthentication)
	} else if hasNkeys {
		c.Errorf("%s - Nkey %q",
			ErrAuthentication.Error(),
			c.opts.Nkey)
	} else if hasUsers {
		c.Errorf("%s - User %q",
			ErrAuthentication.Error(),
			c.opts.Username)
	} else {
		c.Errorf(ErrAuthentication.Error())
	}
	if c.isMqtt() {
		c.mqttEnqueueConnAck(mqttConnAckRCNotAuthorized, false)
	} else {
		c.sendErr("Authorization Violation")
	}
	c.closeConnection(AuthenticationViolation)
}

func (c *client) maxAccountConnExceeded() {
	c.sendErrAndErr(ErrTooManyAccountConnections.Error())
	c.closeConnection(MaxAccountConnectionsExceeded)
}

func (c *client) maxConnExceeded() {
	c.sendErrAndErr(ErrTooManyConnections.Error())
	c.closeConnection(MaxConnectionsExceeded)
}

func (c *client) maxSubsExceeded() {
	if c.acc.shouldLogMaxSubErr() {
		c.Errorf(ErrTooManySubs.Error())
	}
	c.sendErr(ErrTooManySubs.Error())
}

func (c *client) maxPayloadViolation(sz int, max int32) {
	c.Errorf("%s: %d vs %d", ErrMaxPayload.Error(), sz, max)
	c.sendErr("Maximum Payload Violation")
	c.closeConnection(MaxPayloadExceeded)
}

// queueOutbound queues data for a clientconnection.
// Lock should be held.
func (c *client) queueOutbound(data []byte) {
	// Do not keep going if closed
	if c.isClosed() {
		return
	}

	// Add to pending bytes total.
	c.out.pb += int64(len(data))

	// Take a copy of the slice ref so that we can chop bits off the beginning
	// without affecting the original "data" slice.
	toBuffer := data

	// All of the queued []byte have a fixed capacity, so if there's a []byte
	// at the tail of the buffer list that isn't full yet, we should top that
	// up first. This helps to ensure we aren't pulling more []bytes from the
	// pool than we need to.
	if len(c.out.nb) > 0 {
		last := &c.out.nb[len(c.out.nb)-1]
		if free := cap(*last) - len(*last); free > 0 {
			if l := len(toBuffer); l < free {
				free = l
			}
			*last = append(*last, toBuffer[:free]...)
			toBuffer = toBuffer[free:]
		}
	}

	// Now we can push the rest of the data into new []bytes from the pool
	// in fixed size chunks. This ensures we don't go over the capacity of any
	// of the buffers and end up reallocating.
	for len(toBuffer) > 0 {
		var new []byte
		if len(c.out.nb) == 0 && len(toBuffer) <= nbPoolSizeSmall {
			// If the buffer is empty, try to allocate a small buffer if the
			// message will fit in it. This will help for cases like pings.
			new = nbPoolSmall.Get().(*[nbPoolSizeSmall]byte)[:0]
		} else {
			// If "nb" isn't empty, default to large buffers in all cases as
			// this means we are always coalescing future messages into
			// larger buffers. Reduces the number of buffers into writev.
			new = nbPoolLarge.Get().(*[nbPoolSizeLarge]byte)[:0]
		}
		l := len(toBuffer)
		if c := cap(new); l > c {
			l = c
		}
		new = append(new, toBuffer[:l]...)
		c.out.nb = append(c.out.nb, new)
		toBuffer = toBuffer[l:]
	}

	// Check for slow consumer via pending bytes limit.
	// ok to return here, client is going away.
	if c.kind == CLIENT && c.out.pb > c.out.mp {
		// Perf wise, it looks like it is faster to optimistically add than
		// checking current pb+len(data) and then add to pb.
		c.out.pb -= int64(len(data))
		atomic.AddInt64(&c.srv.slowConsumers, 1)
		if c.acc != nil {
			atomic.AddInt64(&c.acc.slowConsumers, 1)
		}
		c.Noticef("Slow Consumer Detected: MaxPending of %d Exceeded", c.out.mp)
		c.markConnAsClosed(SlowConsumerPendingBytes)
		return
	}

	// Check here if we should create a stall channel if we are falling behind.
	// We do this here since if we wait for consumer's writeLoop it could be
	// too late with large number of fan in producers.
	if c.out.pb > c.out.mp/2 && c.out.stc == nil {
		c.out.stc = make(chan struct{})
	}
}

// Assume the lock is held upon entry.
func (c *client) enqueueProtoAndFlush(proto []byte, doFlush bool) {
	if c.isClosed() {
		return
	}
	c.queueOutbound(proto)
	if !(doFlush && c.flushOutbound()) {
		c.flushSignal()
	}
}

// Queues and then flushes the connection. This should only be called when
// the writeLoop cannot be started yet. Use enqueueProto() otherwise.
// Lock is held on entry.
func (c *client) sendProtoNow(proto []byte) {
	c.enqueueProtoAndFlush(proto, true)
}

// Enqueues the given protocol and signal the writeLoop if necessary.
// Lock is held on entry.
func (c *client) enqueueProto(proto []byte) {
	c.enqueueProtoAndFlush(proto, false)
}

// Assume the lock is held upon entry.
func (c *client) sendPong() {
	if c.trace {
		c.traceOutOp("PONG", nil)
	}
	c.enqueueProto([]byte(pongProto))
}

// Used to kick off a RTT measurement for latency tracking.
func (c *client) sendRTTPing() bool {
	c.mu.Lock()
	sent := c.sendRTTPingLocked()
	c.mu.Unlock()
	return sent
}

// Used to kick off a RTT measurement for latency tracking.
// This is normally called only when the caller has checked that
// the c.rtt is 0 and wants to force an update by sending a PING.
// Client lock held on entry.
func (c *client) sendRTTPingLocked() bool {
	if c.isMqtt() {
		return false
	}
	// Most client libs send a CONNECT+PING and wait for a PONG from the
	// server. So if firstPongSent flag is set, it is ok for server to
	// send the PING. But in case we have client libs that don't do that,
	// allow the send of the PING if more than 2 secs have elapsed since
	// the client TCP connection was accepted.
	if !c.isClosed() &&
		(c.flags.isSet(firstPongSent) || time.Since(c.start) > maxNoRTTPingBeforeFirstPong) {
		c.sendPing()
		return true
	}
	return false
}

// Assume the lock is held upon entry.
func (c *client) sendPing() {
	c.rttStart = time.Now().UTC()
	c.ping.out++
	if c.trace {
		c.traceOutOp("PING", nil)
	}
	c.enqueueProto([]byte(pingProto))
}

// Generates the INFO to be sent to the client with the client ID included.
// info arg will be copied since passed by value.
// Assume lock is held.
func (c *client) generateClientInfoJSON(info Info) []byte {
	info.CID = c.cid
	info.ClientIP = c.host
	info.MaxPayload = c.mpay
	if c.isWebsocket() {
		info.ClientConnectURLs = info.WSConnectURLs
	}
	info.WSConnectURLs = nil
	// Generate the info json
	b, _ := json.Marshal(info)
	pcs := [][]byte{[]byte("INFO"), b, []byte(CR_LF)}
	return bytes.Join(pcs, []byte(" "))
}

func (c *client) sendErr(err string) {
	c.mu.Lock()
	if c.trace {
		c.traceOutOp("-ERR", []byte(err))
	}
	if !c.isMqtt() {
		c.enqueueProto([]byte(fmt.Sprintf(errProto, err)))
	}
	c.mu.Unlock()
}

func (c *client) sendOK() {
	c.mu.Lock()
	if c.trace {
		c.traceOutOp("OK", nil)
	}
	c.enqueueProto([]byte(okProto))
	c.mu.Unlock()
}

func (c *client) processPing() {
	c.mu.Lock()

	if c.isClosed() {
		c.mu.Unlock()
		return
	}

	c.sendPong()

	// Record this to suppress us sending one if this
	// is within a given time interval for activity.
	c.ping.last = time.Now()

	// If not a CLIENT, we are done. Also the CONNECT should
	// have been received, but make sure it is so before proceeding
	if c.kind != CLIENT || !c.flags.isSet(connectReceived) {
		c.mu.Unlock()
		return
	}

	// If we are here, the CONNECT has been received so we know
	// if this client supports async INFO or not.
	var (
		checkInfoChange bool
		srv             = c.srv
	)
	// For older clients, just flip the firstPongSent flag if not already
	// set and we are done.
	if c.opts.Protocol < ClientProtoInfo || srv == nil {
		c.flags.setIfNotSet(firstPongSent)
	} else {
		// This is a client that supports async INFO protocols.
		// If this is the first PING (so firstPongSent is not set yet),
		// we will need to check if there was a change in cluster topology
		// or we have a different max payload. We will send this first before
		// pong since most clients do flush after connect call.
		checkInfoChange = !c.flags.isSet(firstPongSent)
	}
	c.mu.Unlock()

	if checkInfoChange {
		opts := srv.getOpts()
		srv.mu.Lock()
		c.mu.Lock()
		// Now that we are under both locks, we can flip the flag.
		// This prevents sendAsyncInfoToClients() and code here to
		// send a double INFO protocol.
		c.flags.set(firstPongSent)
		// If there was a cluster update since this client was created,
		// send an updated INFO protocol now.
		if srv.lastCURLsUpdate >= c.start.UnixNano() || c.mpay != int32(opts.MaxPayload) {
			c.enqueueProto(c.generateClientInfoJSON(srv.copyInfo()))
		}
		c.mu.Unlock()
		srv.mu.Unlock()
	}
}

func (c *client) processPong() {
	c.mu.Lock()
	c.ping.out = 0
	c.rtt = computeRTT(c.rttStart)
	srv := c.srv
	reorderGWs := c.kind == GATEWAY && c.gw.outbound
	c.mu.Unlock()
	if reorderGWs {
		srv.gateway.orderOutboundConnections()
	}
}

// Will return the parts from the raw wire msg.
func (c *client) msgParts(data []byte) (hdr []byte, msg []byte) {
	if c != nil && c.pa.hdr > 0 {
		return data[:c.pa.hdr], data[c.pa.hdr:]
	}
	return nil, data
}

// Header pubs take form HPUB <subject> [reply] <hdr_len> <total_len>\r\n
func (c *client) processHeaderPub(arg []byte) error {
	if !c.headers {
		return ErrMsgHeadersNotSupported
	}

	// Unroll splitArgs to avoid runtime/heap issues
	a := [MAX_HPUB_ARGS][]byte{}
	args := a[:0]
	start := -1
	for i, b := range arg {
		switch b {
		case ' ', '\t':
			if start >= 0 {
				args = append(args, arg[start:i])
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		args = append(args, arg[start:])
	}

	c.pa.arg = arg
	switch len(args) {
	case 3:
		c.pa.subject = args[0]
		c.pa.reply = nil
		c.pa.hdr = parseSize(args[1])
		c.pa.size = parseSize(args[2])
		c.pa.hdb = args[1]
		c.pa.szb = args[2]
	case 4:
		c.pa.subject = args[0]
		c.pa.reply = args[1]
		c.pa.hdr = parseSize(args[2])
		c.pa.size = parseSize(args[3])
		c.pa.hdb = args[2]
		c.pa.szb = args[3]
	default:
		return fmt.Errorf("processHeaderPub Parse Error: %q", arg)
	}
	if c.pa.hdr < 0 {
		return fmt.Errorf("processHeaderPub Bad or Missing Header Size: %q", arg)
	}
	// If number overruns an int64, parseSize() will have returned a negative value
	if c.pa.size < 0 {
		return fmt.Errorf("processHeaderPub Bad or Missing Total Size: %q", arg)
	}
	if c.pa.hdr > c.pa.size {
		return fmt.Errorf("processHeaderPub Header Size larger then TotalSize: %q", arg)
	}
	maxPayload := atomic.LoadInt32(&c.mpay)
	// Use int64() to avoid int32 overrun...
	if maxPayload != jwt.NoLimit && int64(c.pa.size) > int64(maxPayload) {
		c.maxPayloadViolation(c.pa.size, maxPayload)
		return ErrMaxPayload
	}
	if c.opts.Pedantic && !IsValidLiteralSubject(string(c.pa.subject)) {
		c.sendErr("Invalid Publish Subject")
	}
	return nil
}

func (c *client) processPub(arg []byte) error {
	// Unroll splitArgs to avoid runtime/heap issues
	a := [MAX_PUB_ARGS][]byte{}
	args := a[:0]
	start := -1
	for i, b := range arg {
		switch b {
		case ' ', '\t':
			if start >= 0 {
				args = append(args, arg[start:i])
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		args = append(args, arg[start:])
	}

	c.pa.arg = arg
	switch len(args) {
	case 2:
		c.pa.subject = args[0]
		c.pa.reply = nil
		c.pa.size = parseSize(args[1])
		c.pa.szb = args[1]
	case 3:
		c.pa.subject = args[0]
		c.pa.reply = args[1]
		c.pa.size = parseSize(args[2])
		c.pa.szb = args[2]
	default:
		return fmt.Errorf("processPub Parse Error: %q", arg)
	}
	// If number overruns an int64, parseSize() will have returned a negative value
	if c.pa.size < 0 {
		return fmt.Errorf("processPub Bad or Missing Size: %q", arg)
	}
	maxPayload := atomic.LoadInt32(&c.mpay)
	// Use int64() to avoid int32 overrun...
	if maxPayload != jwt.NoLimit && int64(c.pa.size) > int64(maxPayload) {
		c.maxPayloadViolation(c.pa.size, maxPayload)
		return ErrMaxPayload
	}
	if c.opts.Pedantic && !IsValidLiteralSubject(string(c.pa.subject)) {
		c.sendErr("Invalid Publish Subject")
	}
	return nil
}

func splitArg(arg []byte) [][]byte {
	a := [MAX_MSG_ARGS][]byte{}
	args := a[:0]
	start := -1
	for i, b := range arg {
		switch b {
		case ' ', '\t', '\r', '\n':
			if start >= 0 {
				args = append(args, arg[start:i])
				start = -1
			}
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		args = append(args, arg[start:])
	}
	return args
}

func (c *client) parseSub(argo []byte, noForward bool) error {
	// Copy so we do not reference a potentially large buffer
	// FIXME(dlc) - make more efficient.
	arg := make([]byte, len(argo))
	copy(arg, argo)
	args := splitArg(arg)
	var (
		subject []byte
		queue   []byte
		sid     []byte
	)
	switch len(args) {
	case 2:
		subject = args[0]
		queue = nil
		sid = args[1]
	case 3:
		subject = args[0]
		queue = args[1]
		sid = args[2]
	default:
		return fmt.Errorf("processSub Parse Error: %q", arg)
	}
	// If there was an error, it has been sent to the client. We don't return an
	// error here to not close the connection as a parsing error.
	c.processSub(subject, queue, sid, nil, noForward)
	return nil
}

func (c *client) processSub(subject, queue, bsid []byte, cb msgHandler, noForward bool) (*subscription, error) {
	return c.processSubEx(subject, queue, bsid, cb, noForward, false, false)
}

func (c *client) processSubEx(subject, queue, bsid []byte, cb msgHandler, noForward, si, rsi bool) (*subscription, error) {
	// Create the subscription
	sub := &subscription{client: c, subject: subject, queue: queue, sid: bsid, icb: cb, si: si, rsi: rsi}

	c.mu.Lock()

	// Indicate activity.
	c.in.subs++

	// Grab connection type, account and server info.
	kind := c.kind
	acc := c.acc
	srv := c.srv

	sid := string(sub.sid)

	// This check does not apply to SYSTEM or JETSTREAM or ACCOUNT clients (because they don't have a `nc`...)
	if c.isClosed() && (kind != SYSTEM && kind != JETSTREAM && kind != ACCOUNT) {
		c.mu.Unlock()
		return nil, ErrConnectionClosed
	}

	// Check permissions if applicable.
	if kind == CLIENT {
		// First do a pass whether queue subscription is valid. This does not necessarily
		// mean that it will not be able to plain subscribe.
		//
		// allow = ["foo"]            -> can subscribe or queue subscribe to foo using any queue
		// allow = ["foo v1"]         -> can only queue subscribe to 'foo v1', no plain subs allowed.
		// allow = ["foo", "foo v1"]  -> can subscribe to 'foo' but can only queue subscribe to 'foo v1'
		//
		if sub.queue != nil {
			if !c.canSubscribe(string(sub.subject), string(sub.queue)) || string(sub.queue) == sysGroup {
				c.mu.Unlock()
				c.subPermissionViolation(sub)
				return nil, ErrSubscribePermissionViolation
			}
		} else if !c.canSubscribe(string(sub.subject)) {
			c.mu.Unlock()
			c.subPermissionViolation(sub)
			return nil, ErrSubscribePermissionViolation
		}

		if opts := srv.getOpts(); opts != nil && opts.MaxSubTokens > 0 {
			if len(bytes.Split(sub.subject, []byte(tsep))) > int(opts.MaxSubTokens) {
				c.mu.Unlock()
				c.maxTokensViolation(sub)
				return nil, ErrTooManySubTokens
			}
		}
	}

	// Check if we have a maximum on the number of subscriptions.
	if c.subsAtLimit() {
		c.mu.Unlock()
		c.maxSubsExceeded()
		return nil, ErrTooManySubs
	}

	var updateGWs bool
	var err error

	// Subscribe here.
	es := c.subs[sid]
	if es == nil {
		c.subs[sid] = sub
		if acc != nil && acc.sl != nil {
			err = acc.sl.Insert(sub)
			if err != nil {
				delete(c.subs, sid)
			} else {
				updateGWs = c.srv.gateway.enabled
			}
		}
	}
	// Unlocked from here onward
	c.mu.Unlock()

	if err != nil {
		c.sendErr("Invalid Subject")
		return nil, ErrMalformedSubject
	} else if c.opts.Verbose && kind != SYSTEM {
		c.sendOK()
	}

	// If it was already registered, return it.
	if es != nil {
		return es, nil
	}

	// No account just return.
	if acc == nil {
		return sub, nil
	}

	if err := c.addShadowSubscriptions(acc, sub); err != nil {
		c.Errorf(err.Error())
	}

	if noForward {
		return sub, nil
	}

	// If we are routing and this is a local sub, add to the route map for the associated account.
	if kind == CLIENT || kind == SYSTEM || kind == JETSTREAM || kind == ACCOUNT {
		srv.updateRouteSubscriptionMap(acc, sub, 1)
		if updateGWs {
			srv.gatewayUpdateSubInterest(acc.Name, sub, 1)
		}
	}
	// Now check on leafnode updates.
	srv.updateLeafNodes(acc, sub, 1)
	return sub, nil
}

// Used to pass stream import matches to addShadowSub
type ime struct {
	im          *streamImport
	overlapSubj string
	dyn         bool
}

// If the client's account has stream imports and there are matches for
// this subscription's subject, then add shadow subscriptions in the
// other accounts that export this subject.
func (c *client) addShadowSubscriptions(acc *Account, sub *subscription) error {
	if acc == nil {
		return ErrMissingAccount
	}

	var (
		_ims           [16]ime
		ims            = _ims[:0]
		imTsa          [32]string
		tokens         []string
		tsa            [32]string
		hasWC          bool
		tokensModified bool
	)

	acc.mu.RLock()
	subj := string(sub.subject)
	if len(acc.imports.streams) > 0 {
		tokens = tokenizeSubjectIntoSlice(tsa[:0], subj)
		for _, tk := range tokens {
			if tk == pwcs {
				hasWC = true
				break
			}
		}
		if !hasWC && tokens[len(tokens)-1] == fwcs {
			hasWC = true
		}
	}
	// Loop over the import subjects. We have 4 scenarios. If we have an
	// exact match or a superset match we should use the from field from
	// the import. If we are a subset or overlap, we have to dynamically calculate
	// the subject. On overlap, ime requires the overlap subject.
	for _, im := range acc.imports.streams {
		if im.invalid {
			continue
		}
		if subj == im.to {
			ims = append(ims, ime{im, _EMPTY_, false})
			continue
		}
		if tokensModified {
			// re-tokenize subj to overwrite modifications from a previous iteration
			tokens = tokenizeSubjectIntoSlice(tsa[:0], subj)
			tokensModified = false
		}
		imTokens := tokenizeSubjectIntoSlice(imTsa[:0], im.to)

		if isSubsetMatchTokenized(tokens, imTokens) {
			ims = append(ims, ime{im, _EMPTY_, true})
		} else if hasWC {
			if isSubsetMatchTokenized(imTokens, tokens) {
				ims = append(ims, ime{im, _EMPTY_, false})
			} else {
				imTokensLen := len(imTokens)
				for i, t := range tokens {
					if i >= imTokensLen {
						break
					}
					if t == pwcs && imTokens[i] != fwcs {
						tokens[i] = imTokens[i]
						tokensModified = true
					}
				}
				tokensLen := len(tokens)
				lastIdx := tokensLen - 1
				if tokens[lastIdx] == fwcs {
					if imTokensLen >= tokensLen {
						// rewrite ">" in tokens to be more specific
						tokens[lastIdx] = imTokens[lastIdx]
						tokensModified = true
						if imTokensLen > tokensLen {
							// copy even more specific parts from import
							tokens = append(tokens, imTokens[tokensLen:]...)
						}
					}
				}
				if isSubsetMatchTokenized(tokens, imTokens) {
					// As isSubsetMatchTokenized was already called with tokens and imTokens,
					// we wouldn't be here if it where not for tokens being modified.
					// Hence, Join to re compute the subject string
					ims = append(ims, ime{im, strings.Join(tokens, tsep), true})
				}
			}
		}
	}
	acc.mu.RUnlock()

	var shadow []*subscription

	if len(ims) > 0 {
		shadow = make([]*subscription, 0, len(ims))
	}

	// Now walk through collected stream imports that matched.
	for i := 0; i < len(ims); i++ {
		ime := &ims[i]
		// We will create a shadow subscription.
		nsub, err := c.addShadowSub(sub, ime)
		if err != nil {
			return err
		}
		shadow = append(shadow, nsub)
	}

	if shadow != nil {
		c.mu.Lock()
		sub.shadow = shadow
		c.mu.Unlock()
	}

	return nil
}

// Add in the shadow subscription.
func (c *client) addShadowSub(sub *subscription, ime *ime) (*subscription, error) {
	im := ime.im
	nsub := *sub // copy
	nsub.im = im

	if !im.usePub && ime.dyn && im.tr != nil {
		if im.rtr == nil {
			im.rtr = im.tr.reverse()
		}
		s := string(nsub.subject)
		if ime.overlapSubj != _EMPTY_ {
			s = ime.overlapSubj
		}
		subj, err := im.rtr.transformSubject(s)
		if err != nil {
			return nil, err
		}
		nsub.subject = []byte(subj)
	} else if !im.usePub || (im.usePub && ime.overlapSubj != _EMPTY_) || !ime.dyn {
		if ime.overlapSubj != _EMPTY_ {
			nsub.subject = []byte(ime.overlapSubj)
		} else {
			nsub.subject = []byte(im.from)
		}
	}
	// Else use original subject
	c.Debugf("Creating import subscription on %q from account %q", nsub.subject, im.acc.Name)

	if err := im.acc.sl.Insert(&nsub); err != nil {
		errs := fmt.Sprintf("Could not add shadow import subscription for account %q", im.acc.Name)
		c.Debugf(errs)
		return nil, fmt.Errorf(errs)
	}

	// Update our route map here.
	c.srv.updateRemoteSubscription(im.acc, &nsub, 1)

	return &nsub, nil
}

// canSubscribe determines if the client is authorized to subscribe to the
// given subject. Assumes caller is holding lock.
func (c *client) canSubscribe(subject string, optQueue ...string) bool {
	if c.perms == nil {
		return true
	}

	allowed := true

	// Optional queue group.
	var queue string
	if len(optQueue) > 0 {
		queue = optQueue[0]
	}

	// Check allow list. If no allow list that means all are allowed. Deny can overrule.
	if c.perms.sub.allow != nil {
		r := c.perms.sub.allow.Match(subject)
		allowed = len(r.psubs) > 0
		if queue != _EMPTY_ && len(r.qsubs) > 0 {
			// If the queue appears in the allow list, then DO allow.
			allowed = queueMatches(queue, r.qsubs)
		}
		// Leafnodes operate slightly differently in that they allow broader scoped subjects.
		// They will prune based on publish perms before sending to a leafnode client.
		if !allowed && c.kind == LEAF && subjectHasWildcard(subject) {
			r := c.perms.sub.allow.ReverseMatch(subject)
			allowed = len(r.psubs) != 0
		}
	}
	// If we have a deny list and we think we are allowed, check that as well.
	if allowed && c.perms.sub.deny != nil {
		r := c.perms.sub.deny.Match(subject)
		allowed = len(r.psubs) == 0

		if queue != _EMPTY_ && len(r.qsubs) > 0 {
			// If the queue appears in the deny list, then DO NOT allow.
			allowed = !queueMatches(queue, r.qsubs)
		}

		// We use the actual subscription to signal us to spin up the deny mperms
		// and cache. We check if the subject is a wildcard that contains any of
		// the deny clauses.
		// FIXME(dlc) - We could be smarter and track when these go away and remove.
		if allowed && c.mperms == nil && subjectHasWildcard(subject) {
			// Whip through the deny array and check if this wildcard subject is within scope.
			for _, sub := range c.darray {
				if subjectIsSubsetMatch(sub, subject) {
					c.loadMsgDenyFilter()
					break
				}
			}
		}
	}
	return allowed
}

func queueMatches(queue string, qsubs [][]*subscription) bool {
	if len(qsubs) == 0 {
		return true
	}
	for _, qsub := range qsubs {
		qs := qsub[0]
		qname := string(qs.queue)

		// NOTE: '*' and '>' tokens can also be valid
		// queue names so we first check against the
		// literal name.  e.g. v1.* == v1.*
		if queue == qname || (subjectHasWildcard(qname) && subjectIsSubsetMatch(queue, qname)) {
			return true
		}
	}
	return false
}

// Low level unsubscribe for a given client.
func (c *client) unsubscribe(acc *Account, sub *subscription, force, remove bool) {
	c.mu.Lock()
	if !force && sub.max > 0 && sub.nm < sub.max {
		c.Debugf(
			"Deferring actual UNSUB(%s): %d max, %d received",
			string(sub.subject), sub.max, sub.nm)
		c.mu.Unlock()
		return
	}

	if c.trace {
		c.traceOp("<-> %s", "DELSUB", sub.sid)
	}

	if c.kind != CLIENT && c.kind != SYSTEM {
		c.removeReplySubTimeout(sub)
	}

	// Remove accounting if requested. This will be false when we close a connection
	// with open subscriptions.
	if remove {
		delete(c.subs, string(sub.sid))
		if acc != nil {
			acc.sl.Remove(sub)
		}
	}

	// Check to see if we have shadow subscriptions.
	var updateRoute bool
	var updateGWs bool
	shadowSubs := sub.shadow
	sub.shadow = nil
	if len(shadowSubs) > 0 {
		updateRoute = (c.kind == CLIENT || c.kind == SYSTEM || c.kind == LEAF) && c.srv != nil
		if updateRoute {
			updateGWs = c.srv.gateway.enabled
		}
	}
	sub.close()
	c.mu.Unlock()

	// Process shadow subs if we have them.
	for _, nsub := range shadowSubs {
		if err := nsub.im.acc.sl.Remove(nsub); err != nil {
			c.Debugf("Could not remove shadow import subscription for account %q", nsub.im.acc.Name)
		} else {
			if updateRoute {
				c.srv.updateRouteSubscriptionMap(nsub.im.acc, nsub, -1)
			}
			if updateGWs {
				c.srv.gatewayUpdateSubInterest(nsub.im.acc.Name, nsub, -1)
			}
		}
		// Now check on leafnode updates.
		c.srv.updateLeafNodes(nsub.im.acc, nsub, -1)
	}

	// Now check to see if this was part of a respMap entry for service imports.
	if acc != nil {
		acc.checkForReverseEntry(string(sub.subject), nil, true)
	}
}

func (c *client) processUnsub(arg []byte) error {
	args := splitArg(arg)
	var sid []byte
	max := int64(-1)

	switch len(args) {
	case 1:
		sid = args[0]
	case 2:
		sid = args[0]
		max = int64(parseSize(args[1]))
	default:
		return fmt.Errorf("processUnsub Parse Error: %q", arg)
	}

	var sub *subscription
	var ok, unsub bool

	c.mu.Lock()

	// Indicate activity.
	c.in.subs++

	// Grab connection type.
	kind := c.kind
	srv := c.srv
	var acc *Account

	updateGWs := false
	if sub, ok = c.subs[string(sid)]; ok {
		acc = c.acc
		if max > 0 && max > sub.nm {
			sub.max = max
		} else {
			// Clear it here to override
			sub.max = 0
			unsub = true
		}
		updateGWs = srv.gateway.enabled
	}
	c.mu.Unlock()

	if c.opts.Verbose {
		c.sendOK()
	}

	if unsub {
		c.unsubscribe(acc, sub, false, true)
		if acc != nil && (kind == CLIENT || kind == SYSTEM || kind == ACCOUNT || kind == JETSTREAM) {
			srv.updateRouteSubscriptionMap(acc, sub, -1)
			if updateGWs {
				srv.gatewayUpdateSubInterest(acc.Name, sub, -1)
			}
		}
		// Now check on leafnode updates.
		srv.updateLeafNodes(acc, sub, -1)
	}

	return nil
}

// checkDenySub will check if we are allowed to deliver this message in the
// presence of deny clauses for subscriptions. Deny clauses will not prevent
// larger scoped wildcard subscriptions, so we need to check at delivery time.
// Lock should be held.
func (c *client) checkDenySub(subject string) bool {
	if denied, ok := c.mperms.dcache[subject]; ok {
		return denied
	} else if r := c.mperms.deny.Match(subject); len(r.psubs) != 0 {
		c.mperms.dcache[subject] = true
		return true
	} else {
		c.mperms.dcache[subject] = false
	}
	if len(c.mperms.dcache) > maxDenyPermCacheSize {
		c.pruneDenyCache()
	}
	return false
}

// Create a message header for routes or leafnodes. Header and origin cluster aware.
func (c *client) msgHeaderForRouteOrLeaf(subj, reply []byte, rt *routeTarget, acc *Account) []byte {
	hasHeader := c.pa.hdr > 0
	subclient := rt.sub.client
	canReceiveHeader := subclient.headers

	mh := c.msgb[:msgHeadProtoLen]
	kind := subclient.kind
	var lnoc bool

	if kind == ROUTER {
		// If we are coming from a leaf with an origin cluster we need to handle differently
		// if we can. We will send a route based LMSG which has origin cluster and headers
		// by default.
		if c.kind == LEAF && c.remoteCluster() != _EMPTY_ {
			subclient.mu.Lock()
			lnoc = subclient.route.lnoc
			subclient.mu.Unlock()
		}
		if lnoc {
			mh[0] = 'L'
			mh = append(mh, c.remoteCluster()...)
			mh = append(mh, ' ')
		} else {
			// Router (and Gateway) nodes are RMSG. Set here since leafnodes may rewrite.
			mh[0] = 'R'
		}
		mh = append(mh, acc.Name...)
		mh = append(mh, ' ')
	} else {
		// Leaf nodes are LMSG
		mh[0] = 'L'
		// Remap subject if its a shadow subscription, treat like a normal client.
		if rt.sub.im != nil {
			if rt.sub.im.tr != nil {
				to, _ := rt.sub.im.tr.transformSubject(string(subj))
				subj = []byte(to)
			} else if !rt.sub.im.usePub {
				subj = []byte(rt.sub.im.to)
			}
		}
	}
	mh = append(mh, subj...)
	mh = append(mh, ' ')

	if len(rt.qs) > 0 {
		if len(reply) > 0 {
			mh = append(mh, "+ "...) // Signal that there is a reply.
			mh = append(mh, reply...)
			mh = append(mh, ' ')
		} else {
			mh = append(mh, "| "...) // Only queues
		}
		mh = append(mh, rt.qs...)
	} else if len(reply) > 0 {
		mh = append(mh, reply...)
		mh = append(mh, ' ')
	}

	if lnoc {
		// leafnode origin LMSG always have a header entry even if zero.
		if c.pa.hdr <= 0 {
			mh = append(mh, '0')
		} else {
			mh = append(mh, c.pa.hdb...)
		}
		mh = append(mh, ' ')
		mh = append(mh, c.pa.szb...)
	} else if hasHeader {
		if canReceiveHeader {
			mh[0] = 'H'
			mh = append(mh, c.pa.hdb...)
			mh = append(mh, ' ')
			mh = append(mh, c.pa.szb...)
		} else {
			// If we are here we need to truncate the payload size
			nsz := strconv.Itoa(c.pa.size - c.pa.hdr)
			mh = append(mh, nsz...)
		}
	} else {
		mh = append(mh, c.pa.szb...)
	}
	return append(mh, _CRLF_...)
}

// Create a message header for clients. Header aware.
func (c *client) msgHeader(subj, reply []byte, sub *subscription) []byte {
	// See if we should do headers. We have to have a headers msg and
	// the client we are going to deliver to needs to support headers as well.
	hasHeader := c.pa.hdr > 0
	canReceiveHeader := sub.client != nil && sub.client.headers

	var mh []byte
	if hasHeader && canReceiveHeader {
		mh = c.msgb[:msgHeadProtoLen]
		mh[0] = 'H'
	} else {
		mh = c.msgb[1:msgHeadProtoLen]
	}
	mh = append(mh, subj...)
	mh = append(mh, ' ')

	if len(sub.sid) > 0 {
		mh = append(mh, sub.sid...)
		mh = append(mh, ' ')
	}
	if reply != nil {
		mh = append(mh, reply...)
		mh = append(mh, ' ')
	}
	if hasHeader {
		if canReceiveHeader {
			mh = append(mh, c.pa.hdb...)
			mh = append(mh, ' ')
			mh = append(mh, c.pa.szb...)
		} else {
			// If we are here we need to truncate the payload size
			nsz := strconv.Itoa(c.pa.size - c.pa.hdr)
			mh = append(mh, nsz...)
		}
	} else {
		mh = append(mh, c.pa.szb...)
	}
	mh = append(mh, _CRLF_...)
	return mh
}

func (c *client) stalledWait(producer *client) {
	stall := c.out.stc
	ttl := stallDuration(c.out.pb, c.out.mp)
	c.mu.Unlock()
	defer c.mu.Lock()

	select {
	case <-stall:
	case <-time.After(ttl):
		producer.Debugf("Timed out of fast producer stall (%v)", ttl)
	}
}

func stallDuration(pb, mp int64) time.Duration {
	ttl := stallClientMinDuration
	if pb >= mp {
		ttl = stallClientMaxDuration
	} else if hmp := mp / 2; pb > hmp {
		bsz := hmp / 10
		additional := int64(ttl) * ((pb - hmp) / bsz)
		ttl += time.Duration(additional)
	}
	return ttl
}

// Used to treat maps as efficient set
var needFlush = struct{}{}

// deliverMsg will deliver a message to a matching subscription and its underlying client.
// We process all connection/client types. mh is the part that will be protocol/client specific.
func (c *client) deliverMsg(prodIsMQTT bool, sub *subscription, acc *Account, subject, reply, mh, msg []byte, gwrply bool) bool {
	if sub.client == nil {
		return false
	}
	client := sub.client
	client.mu.Lock()

	// Check echo
	if c == client && !client.echo {
		client.mu.Unlock()
		return false
	}

	// Check if we have a subscribe deny clause. This will trigger us to check the subject
	// for a match against the denied subjects.
	if client.mperms != nil && client.checkDenySub(string(subject)) {
		client.mu.Unlock()
		return false
	}

	// New race detector forces this now.
	if sub.isClosed() {
		client.mu.Unlock()
		return false
	}

	// Check if we are a leafnode and have perms to check.
	if client.kind == LEAF && client.perms != nil {
		if !client.pubAllowedFullCheck(string(subject), true, true) {
			client.mu.Unlock()
			client.Debugf("Not permitted to deliver to %q", subject)
			return false
		}
	}

	srv := client.srv

	sub.nm++

	// Check if we should auto-unsubscribe.
	if sub.max > 0 {
		if client.kind == ROUTER && sub.nm >= sub.max {
			// The only router based messages that we will see here are remoteReplies.
			// We handle these slightly differently.
			defer client.removeReplySub(sub)
		} else {
			// For routing..
			shouldForward := client.kind == CLIENT || client.kind == SYSTEM && client.srv != nil
			// If we are at the exact number, unsubscribe but
			// still process the message in hand, otherwise
			// unsubscribe and drop message on the floor.
			if sub.nm == sub.max {
				client.Debugf("Auto-unsubscribe limit of %d reached for sid '%s'", sub.max, string(sub.sid))
				// Due to defer, reverse the code order so that execution
				// is consistent with other cases where we unsubscribe.
				if shouldForward {
					defer srv.updateRemoteSubscription(client.acc, sub, -1)
				}
				defer client.unsubscribe(client.acc, sub, true, true)
			} else if sub.nm > sub.max {
				client.Debugf("Auto-unsubscribe limit [%d] exceeded", sub.max)
				client.mu.Unlock()
				client.unsubscribe(client.acc, sub, true, true)
				if shouldForward {
					srv.updateRemoteSubscription(client.acc, sub, -1)
				}
				return false
			}
		}
	}

	// Check here if we have a header with our message. If this client can not
	// support we need to strip the headers from the payload.
	// The actual header would have been processed correctly for us, so just
	// need to update payload.
	if c.pa.hdr > 0 && !sub.client.headers {
		msg = msg[c.pa.hdr:]
	}

	// Update statistics

	// The msg includes the CR_LF, so pull back out for accounting.
	msgSize := int64(len(msg))
	// MQTT producers send messages without CR_LF, so don't remove it for them.
	if !prodIsMQTT {
		msgSize -= int64(LEN_CR_LF)
	}

	// No atomic needed since accessed under client lock.
	// Monitor is reading those also under client's lock.
	client.outMsgs++
	client.outBytes += msgSize

	// Check for internal subscriptions.
	if sub.icb != nil && !c.noIcb {
		if gwrply {
			// We will store in the account, not the client since it will likely
			// be a different client that will send the reply.
			srv.trackGWReply(nil, client.acc, reply, c.pa.reply)
		}
		client.mu.Unlock()

		// Internal account clients are for service imports and need the '\r\n'.
		start := time.Now()
		if client.kind == ACCOUNT {
			sub.icb(sub, c, acc, string(subject), string(reply), msg)
		} else {
			sub.icb(sub, c, acc, string(subject), string(reply), msg[:msgSize])
		}
		if dur := time.Since(start); dur >= readLoopReportThreshold {
			srv.Warnf("Internal subscription on %q took too long: %v", subject, dur)
		}
		return true
	}

	// If we are a client and we detect that the consumer we are
	// sending to is in a stalled state, go ahead and wait here
	// with a limit.
	if c.kind == CLIENT && client.out.stc != nil {
		client.stalledWait(c)
	}

	// Check for closed connection
	if client.isClosed() {
		client.mu.Unlock()
		return false
	}

	// Do a fast check here to see if we should be tracking this from a latency
	// perspective. This will be for a request being received for an exported service.
	// This needs to be from a non-client (otherwise tracking happens at requestor).
	//
	// Also this check captures if the original reply (c.pa.reply) is a GW routed
	// reply (since it is known to be > minReplyLen). If that is the case, we need to
	// track the binding between the routed reply and the reply set in the message
	// header (which is c.pa.reply without the GNR routing prefix).
	if client.kind == CLIENT && len(c.pa.reply) > minReplyLen {
		if gwrply {
			// Note that we keep track of the GW routed reply in the destination
			// connection (`client`). The routed reply subject is in `c.pa.reply`,
			// should that change, we would have to pass the GW routed reply as
			// a parameter of deliverMsg().
			srv.trackGWReply(client, nil, reply, c.pa.reply)
		}

		// If we do not have a registered RTT queue that up now.
		if client.rtt == 0 {
			client.sendRTTPingLocked()
		}
		// FIXME(dlc) - We may need to optimize this.
		// We will have tagged this with a suffix ('.T') if we are tracking. This is
		// needed from sampling. Not all will be tracked.
		if c.kind != CLIENT && isTrackedReply(c.pa.reply) {
			client.trackRemoteReply(string(subject), string(c.pa.reply))
		}
	}

	// Queue to outbound buffer
	client.queueOutbound(mh)
	client.queueOutbound(msg)
	if prodIsMQTT {
		// Need to add CR_LF since MQTT producers don't send CR_LF
		client.queueOutbound([]byte(CR_LF))
	}

	// If we are tracking dynamic publish permissions that track reply subjects,
	// do that accounting here. We only look at client.replies which will be non-nil.
	if client.replies != nil && len(reply) > 0 {
		client.replies[string(reply)] = &resp{time.Now(), 0}
		if len(client.replies) > replyPermLimit {
			client.pruneReplyPerms()
		}
	}

	// Check outbound threshold and queue IO flush if needed.
	// This is specifically looking at situations where we are getting behind and may want
	// to intervene before this producer goes back to top of readloop. We are in the producer's
	// readloop go routine at this point.
	// FIXME(dlc) - We may call this alot, maybe suppress after first call?
	if len(client.out.nb) != 0 {
		client.flushSignal()
	}

	// Add the data size we are responsible for here. This will be processed when we
	// return to the top of the readLoop.
	c.addToPCD(client)

	if client.trace {
		client.traceOutOp(string(mh[:len(mh)-LEN_CR_LF]), nil)
	}

	client.mu.Unlock()

	return true
}

// Add the given sub's client to the list of clients that need flushing.
// This must be invoked from `c`'s readLoop. No lock for c is required,
// however, `client` lock must be held on entry. This holds true even
// if `client` is same than `c`.
func (c *client) addToPCD(client *client) {
	if _, ok := c.pcd[client]; !ok {
		client.out.fsp++
		c.pcd[client] = needFlush
	}
}

// This will track a remote reply for an exported service that has requested
// latency tracking.
// Lock assumed to be held.
func (c *client) trackRemoteReply(subject, reply string) {
	a := c.acc
	if a == nil {
		return
	}

	var lrt time.Duration
	var respThresh time.Duration

	a.mu.RLock()
	se := a.getServiceExport(subject)
	if se != nil {
		lrt = a.lowestServiceExportResponseTime()
		respThresh = se.respThresh
	}
	a.mu.RUnlock()

	if se == nil {
		return
	}

	if c.rrTracking == nil {
		c.rrTracking = &rrTracking{
			rmap: make(map[string]*remoteLatency),
			ptmr: time.AfterFunc(lrt, c.pruneRemoteTracking),
			lrt:  lrt,
		}
	}
	rl := remoteLatency{
		Account:    a.Name,
		ReqId:      reply,
		respThresh: respThresh,
	}
	rl.M2.RequestStart = time.Now().UTC()
	c.rrTracking.rmap[reply] = &rl
}

// pruneRemoteTracking will prune any remote tracking objects
// that are too old. These are orphaned when a service is not
// sending reponses etc.
// Lock should be held upon entry.
func (c *client) pruneRemoteTracking() {
	c.mu.Lock()
	if c.rrTracking == nil {
		c.mu.Unlock()
		return
	}
	now := time.Now()
	for subject, rl := range c.rrTracking.rmap {
		if now.After(rl.M2.RequestStart.Add(rl.respThresh)) {
			delete(c.rrTracking.rmap, subject)
		}
	}
	if len(c.rrTracking.rmap) > 0 {
		t := c.rrTracking.ptmr
		t.Stop()
		t.Reset(c.rrTracking.lrt)
	} else {
		c.rrTracking.ptmr.Stop()
		c.rrTracking = nil
	}
	c.mu.Unlock()
}

// pruneReplyPerms will remove any stale or expired entries
// in our reply cache. We make sure to not check too often.
func (c *client) pruneReplyPerms() {
	// Make sure we do not check too often.
	if c.perms.resp == nil {
		return
	}

	mm := c.perms.resp.MaxMsgs
	ttl := c.perms.resp.Expires
	now := time.Now()

	for k, resp := range c.replies {
		if mm > 0 && resp.n >= mm {
			delete(c.replies, k)
		} else if ttl > 0 && now.Sub(resp.t) > ttl {
			delete(c.replies, k)
		}
	}
}

// pruneDenyCache will prune the deny cache via randomly
// deleting items. Doing so pruneSize items at a time.
// Lock must be held for this one since it is shared under
// deliverMsg.
func (c *client) pruneDenyCache() {
	r := 0
	for subject := range c.mperms.dcache {
		delete(c.mperms.dcache, subject)
		if r++; r > pruneSize {
			break
		}
	}
}

// prunePubPermsCache will prune the cache via randomly
// deleting items. Doing so pruneSize items at a time.
func (c *client) prunePubPermsCache() {
	// There is a case where we can invoke this from multiple go routines,
	// (in deliverMsg() if sub.client is a LEAF), so we make sure to prune
	// from only one go routine at a time.
	if !atomic.CompareAndSwapInt32(&c.perms.prun, 0, 1) {
		return
	}
	const maxPruneAtOnce = 1000
	r := 0
	c.perms.pcache.Range(func(k, _ interface{}) bool {
		c.perms.pcache.Delete(k)
		if r++; (r > pruneSize && atomic.LoadInt32(&c.perms.pcsz) < int32(maxPermCacheSize)) ||
			(r > maxPruneAtOnce) {
			return false
		}
		return true
	})
	atomic.AddInt32(&c.perms.pcsz, -int32(r))
	atomic.StoreInt32(&c.perms.prun, 0)
}

// pubAllowed checks on publish permissioning.
// Lock should not be held.
func (c *client) pubAllowed(subject string) bool {
	return c.pubAllowedFullCheck(subject, true, false)
}

// pubAllowedFullCheck checks on all publish permissioning depending
// on the flag for dynamic reply permissions.
func (c *client) pubAllowedFullCheck(subject string, fullCheck, hasLock bool) bool {
	if c.perms == nil || (c.perms.pub.allow == nil && c.perms.pub.deny == nil) {
		return true
	}
	// Check if published subject is allowed if we have permissions in place.
	v, ok := c.perms.pcache.Load(subject)
	if ok {
		return v.(bool)
	}
	allowed := true
	// Cache miss, check allow then deny as needed.
	if c.perms.pub.allow != nil {
		r := c.perms.pub.allow.Match(subject)
		allowed = len(r.psubs) != 0
	}
	// If we have a deny list and are currently allowed, check that as well.
	if allowed && c.perms.pub.deny != nil {
		r := c.perms.pub.deny.Match(subject)
		allowed = len(r.psubs) == 0
	}

	// If we are currently not allowed but we are tracking reply subjects
	// dynamically, check to see if we are allowed here but avoid pcache.
	// We need to acquire the lock though.
	if !allowed && fullCheck && c.perms.resp != nil {
		if !hasLock {
			c.mu.Lock()
		}
		if resp := c.replies[subject]; resp != nil {
			resp.n++
			// Check if we have sent too many responses.
			if c.perms.resp.MaxMsgs > 0 && resp.n > c.perms.resp.MaxMsgs {
				delete(c.replies, subject)
			} else if c.perms.resp.Expires > 0 && time.Since(resp.t) > c.perms.resp.Expires {
				delete(c.replies, subject)
			} else {
				allowed = true
			}
		}
		if !hasLock {
			c.mu.Unlock()
		}
	} else {
		// Update our cache here.
		c.perms.pcache.Store(string(subject), allowed)
		if n := atomic.AddInt32(&c.perms.pcsz, 1); n > maxPermCacheSize {
			c.prunePubPermsCache()
		}
	}
	return allowed
}

// Test whether a reply subject is a service import reply.
func isServiceReply(reply []byte) bool {
	// This function is inlined and checking this way is actually faster
	// than byte-by-byte comparison.
	return len(reply) > 3 && string(reply[:4]) == replyPrefix
}

// Test whether a reply subject is a service import or a gateway routed reply.
func isReservedReply(reply []byte) bool {
	if isServiceReply(reply) {
		return true
	}
	rLen := len(reply)
	// Faster to check with string([:]) than byte-by-byte
	if rLen > jsAckPreLen && string(reply[:jsAckPreLen]) == jsAckPre {
		return true
	} else if rLen > gwReplyPrefixLen && string(reply[:gwReplyPrefixLen]) == gwReplyPrefix {
		return true
	}
	return false
}

// This will decide to call the client code or router code.
func (c *client) processInboundMsg(msg []byte) {
	switch c.kind {
	case CLIENT:
		c.processInboundClientMsg(msg)
	case ROUTER:
		c.processInboundRoutedMsg(msg)
	case GATEWAY:
		c.processInboundGatewayMsg(msg)
	case LEAF:
		c.processInboundLeafMsg(msg)
	}
}

// selectMappedSubject will chose the mapped subject based on the client's inbound subject.
func (c *client) selectMappedSubject() bool {
	nsubj, changed := c.acc.selectMappedSubject(string(c.pa.subject))
	if changed {
		c.pa.mapped = c.pa.subject
		c.pa.subject = []byte(nsubj)
	}
	return changed
}

// processInboundClientMsg is called to process an inbound msg from a client.
// Return if the message was delivered, and if the message was not delivered
// due to a permission issue.
func (c *client) processInboundClientMsg(msg []byte) (bool, bool) {
	// Update statistics
	// The msg includes the CR_LF, so pull back out for accounting.
	c.in.msgs++
	c.in.bytes += int32(len(msg) - LEN_CR_LF)

	// Check that client (could be here with SYSTEM) is not publishing on reserved "$GNR" prefix.
	if c.kind == CLIENT && hasGWRoutedReplyPrefix(c.pa.subject) {
		c.pubPermissionViolation(c.pa.subject)
		return false, true
	}

	// Mostly under testing scenarios.
	if c.srv == nil || c.acc == nil {
		return false, false
	}

	// Check pub permissions
	if c.perms != nil && (c.perms.pub.allow != nil || c.perms.pub.deny != nil) && !c.pubAllowed(string(c.pa.subject)) {
		c.pubPermissionViolation(c.pa.subject)
		return false, true
	}

	// Now check for reserved replies. These are used for service imports.
	if c.kind == CLIENT && len(c.pa.reply) > 0 && isReservedReply(c.pa.reply) {
		c.replySubjectViolation(c.pa.reply)
		return false, true
	}

	if c.opts.Verbose {
		c.sendOK()
	}

	// If MQTT client, check for retain flag now that we have passed permissions check
	if c.isMqtt() {
		c.mqttHandlePubRetain()
	}

	// Doing this inline as opposed to create a function (which otherwise has a measured
	// performance impact reported in our bench)
	var isGWRouted bool
	if c.kind != CLIENT {
		if atomic.LoadInt32(&c.acc.gwReplyMapping.check) > 0 {
			c.acc.mu.RLock()
			c.pa.subject, isGWRouted = c.acc.gwReplyMapping.get(c.pa.subject)
			c.acc.mu.RUnlock()
		}
	} else if atomic.LoadInt32(&c.gwReplyMapping.check) > 0 {
		c.mu.Lock()
		c.pa.subject, isGWRouted = c.gwReplyMapping.get(c.pa.subject)
		c.mu.Unlock()
	}

	// If we have an exported service and we are doing remote tracking, check this subject
	// to see if we need to report the latency.
	if c.rrTracking != nil {
		c.mu.Lock()
		rl := c.rrTracking.rmap[string(c.pa.subject)]
		if rl != nil {
			delete(c.rrTracking.rmap, string(c.pa.subject))
		}
		c.mu.Unlock()

		if rl != nil {
			sl := &rl.M2
			// Fill this in and send it off to the other side.
			sl.Status = 200
			sl.Responder = c.getClientInfo(true)
			sl.ServiceLatency = time.Since(sl.RequestStart) - sl.Responder.RTT
			sl.TotalLatency = sl.ServiceLatency + sl.Responder.RTT
			sanitizeLatencyMetric(sl)
			lsub := remoteLatencySubjectForResponse(c.pa.subject)
			c.srv.sendInternalAccountMsg(nil, lsub, rl) // Send to SYS account
		}
	}

	// If the subject was converted to the gateway routed subject, then handle it now
	// and be done with the rest of this function.
	if isGWRouted {
		c.handleGWReplyMap(msg)
		return true, false
	}

	// Match the subscriptions. We will use our own L1 map if
	// it's still valid, avoiding contention on the shared sublist.
	var r *SublistResult
	var ok bool

	genid := atomic.LoadUint64(&c.acc.sl.genid)
	if genid == c.in.genid && c.in.results != nil {
		r, ok = c.in.results[string(c.pa.subject)]
	} else {
		// Reset our L1 completely.
		c.in.results = make(map[string]*SublistResult)
		c.in.genid = genid
	}

	// Go back to the sublist data structure.
	if !ok {
		r = c.acc.sl.Match(string(c.pa.subject))
		c.in.results[string(c.pa.subject)] = r
		// Prune the results cache. Keeps us from unbounded growth. Random delete.
		if len(c.in.results) > maxResultCacheSize {
			n := 0
			for subject := range c.in.results {
				delete(c.in.results, subject)
				if n++; n > pruneSize {
					break
				}
			}
		}
	}

	// Indication if we attempted to deliver the message to anyone.
	var didDeliver bool
	var qnames [][]byte

	// Check for no interest, short circuit if so.
	// This is the fanout scale.
	if len(r.psubs)+len(r.qsubs) > 0 {
		flag := pmrNoFlag
		// If there are matching queue subs and we are in gateway mode,
		// we need to keep track of the queue names the messages are
		// delivered to. When sending to the GWs, the RMSG will include
		// those names so that the remote clusters do not deliver messages
		// to their queue subs of the same names.
		if len(r.qsubs) > 0 && c.srv.gateway.enabled &&
			atomic.LoadInt64(&c.srv.gateway.totalQSubs) > 0 {
			flag |= pmrCollectQueueNames
		}
		didDeliver, qnames = c.processMsgResults(c.acc, r, msg, c.pa.deliver, c.pa.subject, c.pa.reply, flag)
	}

	// Now deal with gateways
	if c.srv.gateway.enabled {
		reply := c.pa.reply
		if len(c.pa.deliver) > 0 && c.kind == JETSTREAM && len(c.pa.reply) > 0 {
			reply = append(reply, '@')
			reply = append(reply, c.pa.deliver...)
		}
		didDeliver = c.sendMsgToGateways(c.acc, msg, c.pa.subject, reply, qnames) || didDeliver
	}

	// Check to see if we did not deliver to anyone and the client has a reply subject set
	// and wants notification of no_responders.
	if !didDeliver && len(c.pa.reply) > 0 {
		c.mu.Lock()
		if c.opts.NoResponders {
			if sub := c.subForReply(c.pa.reply); sub != nil {
				proto := fmt.Sprintf("HMSG %s %s 16 16\r\nNATS/1.0 503\r\n\r\n\r\n", c.pa.reply, sub.sid)
				c.queueOutbound([]byte(proto))
				c.addToPCD(c)
			}
		}
		c.mu.Unlock()
	}

	return didDeliver, false
}

// Return the subscription for this reply subject. Only look at normal subs for this client.
func (c *client) subForReply(reply []byte) *subscription {
	r := c.acc.sl.Match(string(reply))
	for _, sub := range r.psubs {
		if sub.client == c {
			return sub
		}
	}
	return nil
}

// This is invoked knowing that c.pa.subject has been set to the gateway routed subject.
// This function will send the message to possibly LEAFs and directly back to the origin
// gateway.
func (c *client) handleGWReplyMap(msg []byte) bool {
	// Check for leaf nodes
	if c.srv.gwLeafSubs.Count() > 0 {
		if r := c.srv.gwLeafSubs.Match(string(c.pa.subject)); len(r.psubs) > 0 {
			c.processMsgResults(c.acc, r, msg, c.pa.deliver, c.pa.subject, c.pa.reply, pmrNoFlag)
		}
	}
	if c.srv.gateway.enabled {
		reply := c.pa.reply
		if len(c.pa.deliver) > 0 && c.kind == JETSTREAM && len(c.pa.reply) > 0 {
			reply = append(reply, '@')
			reply = append(reply, c.pa.deliver...)
		}
		c.sendMsgToGateways(c.acc, msg, c.pa.subject, reply, nil)
	}
	return true
}

// Used to setup the response map for a service import request that has a reply subject.
func (c *client) setupResponseServiceImport(acc *Account, si *serviceImport, tracking bool, header http.Header) *serviceImport {
	rsi := si.acc.addRespServiceImport(acc, string(c.pa.reply), si, tracking, header)
	if si.latency != nil {
		if c.rtt == 0 {
			// We have a service import that we are tracking but have not established RTT.
			c.sendRTTPing()
		}
		si.acc.mu.Lock()
		rsi.rc = c
		si.acc.mu.Unlock()
	}
	return rsi
}

// Will remove a header if present.
func removeHeaderIfPresent(hdr []byte, key string) []byte {
	start := bytes.Index(hdr, []byte(key))
	// key can't be first and we want to check that it is preceded by a '\n'
	if start < 1 || hdr[start-1] != '\n' {
		return hdr
	}
	index := start + len(key)
	if index >= len(hdr) || hdr[index] != ':' {
		return hdr
	}
	end := bytes.Index(hdr[start:], []byte(_CRLF_))
	if end < 0 {
		return hdr
	}
	hdr = append(hdr[:start], hdr[start+end+len(_CRLF_):]...)
	if len(hdr) <= len(emptyHdrLine) {
		return nil
	}
	return hdr
}

// Generate a new header based on optional original header and key value.
// More used in JetStream layers.
func genHeader(hdr []byte, key, value string) []byte {
	var bb bytes.Buffer
	if len(hdr) > LEN_CR_LF {
		bb.Write(hdr[:len(hdr)-LEN_CR_LF])
	} else {
		bb.WriteString(hdrLine)
	}
	http.Header{key: []string{value}}.Write(&bb)
	bb.WriteString(CR_LF)
	return bb.Bytes()
}

// This will set a header for the message.
// Lock does not need to be held but this should only be called
// from the inbound go routine. We will update the pubArgs.
// This will replace any previously set header and not add to it per normal spec.
func (c *client) setHeader(key, value string, msg []byte) []byte {
	var bb bytes.Buffer
	var omi int
	// Write original header if present.
	if c.pa.hdr > LEN_CR_LF {
		omi = c.pa.hdr
		hdr := removeHeaderIfPresent(msg[:c.pa.hdr-LEN_CR_LF], key)
		if len(hdr) == 0 {
			bb.WriteString(hdrLine)
		} else {
			bb.Write(hdr)
		}
	} else {
		bb.WriteString(hdrLine)
	}
	http.Header{key: []string{value}}.Write(&bb)
	bb.WriteString(CR_LF)
	nhdr := bb.Len()
	// Put the original message back.
	// FIXME(dlc) - This is inefficient.
	bb.Write(msg[omi:])
	nsize := bb.Len() - LEN_CR_LF
	// MQTT producers don't have CRLF, so add it back.
	if c.isMqtt() {
		nsize += LEN_CR_LF
	}
	// Update pubArgs
	// If others will use this later we need to save and restore original.
	c.pa.hdr = nhdr
	c.pa.size = nsize
	c.pa.hdb = []byte(strconv.Itoa(nhdr))
	c.pa.szb = []byte(strconv.Itoa(nsize))
	return bb.Bytes()
}

// Will return the value for the header denoted by key or nil if it does not exists.
// This function ignores errors and tries to achieve speed and no additional allocations.
func getHeader(key string, hdr []byte) []byte {
	if len(hdr) == 0 {
		return nil
	}
	index := bytes.Index(hdr, []byte(key))
	if index < 0 {
		return nil
	}
	// Make sure this key does not have additional prefix.
	if index < 2 || hdr[index-1] != '\n' || hdr[index-2] != '\r' {
		return nil
	}
	index += len(key)
	if index >= len(hdr) {
		return nil
	}
	if hdr[index] != ':' {
		return nil
	}
	index++

	var value []byte
	hdrLen := len(hdr)
	for hdr[index] == ' ' && index < hdrLen {
		index++
	}
	for index < hdrLen {
		if hdr[index] == '\r' && index < hdrLen-1 && hdr[index+1] == '\n' {
			break
		}
		value = append(value, hdr[index])
		index++
	}
	return value
}

// processServiceImport is an internal callback when a subscription matches an imported service
// from another account. This includes response mappings as well.
func (c *client) processServiceImport(si *serviceImport, acc *Account, msg []byte) {
	// If we are a GW and this is not a direct serviceImport ignore.
	isResponse := si.isRespServiceImport()
	if (c.kind == GATEWAY || c.kind == ROUTER) && !isResponse {
		return
	}
	// Detect cycles and ignore (return) when we detect one.
	if len(c.pa.psi) > 0 {
		for i := len(c.pa.psi) - 1; i >= 0; i-- {
			if psi := c.pa.psi[i]; psi.se == si.se {
				return
			}
		}
	}

	acc.mu.RLock()
	var checkJS bool
	shouldReturn := si.invalid || acc.sl == nil
	if !shouldReturn && !isResponse && si.to == jsAllAPI {
		subj := string(c.pa.subject)
		if strings.HasPrefix(subj, jsRequestNextPre) || strings.HasPrefix(subj, jsDirectGetPre) {
			checkJS = true
		}
	}
	acc.mu.RUnlock()

	// We have a special case where JetStream pulls in all service imports through one export.
	// However the GetNext for consumers and DirectGet for streams are a no-op and causes buildups of service imports,
	// response service imports and rrMap entries which all will need to simply expire.
	// TODO(dlc) - Come up with something better.
	if shouldReturn || (checkJS && si.se != nil && si.se.acc == c.srv.SystemAccount()) {
		return
	}

	var nrr []byte
	var rsi *serviceImport

	// Check if there is a reply present and set up a response.
	tracking, headers := shouldSample(si.latency, c)
	if len(c.pa.reply) > 0 {
		// Special case for now, need to formalize.
		// TODO(dlc) - Formalize as a service import option for reply rewrite.
		// For now we can't do $JS.ACK since that breaks pull consumers across accounts.
		if !bytes.HasPrefix(c.pa.reply, []byte(jsAckPre)) {
			if rsi = c.setupResponseServiceImport(acc, si, tracking, headers); rsi != nil {
				nrr = []byte(rsi.from)
			}
		} else {
			// This only happens when we do a pull subscriber that trampolines through another account.
			// Normally this code is not called.
			nrr = c.pa.reply
		}
	} else if !isResponse && si.latency != nil && tracking {
		// Check to see if this was a bad request with no reply and we were supposed to be tracking.
		si.acc.sendBadRequestTrackingLatency(si, c, headers)
	}

	// Send tracking info here if we are tracking this response.
	// This is always a response.
	var didSendTL bool
	if si.tracking && !si.didDeliver {
		// Stamp that we attempted delivery.
		si.didDeliver = true
		didSendTL = acc.sendTrackingLatency(si, c)
	}

	// Pick correct "to" subject. If we matched on a wildcard use the literal publish subject.
	to, subject := si.to, string(c.pa.subject)

	if si.tr != nil {
		// FIXME(dlc) - This could be slow, may want to look at adding cache to bare transforms?
		to, _ = si.tr.transformSubject(subject)
	} else if si.usePub {
		to = subject
	}

	// Copy our pubArg since this gets modified as we process the service import itself.
	pacopy := c.pa

	// Now check to see if this account has mappings that could affect the service import.
	// Can't use non-locked trick like in processInboundClientMsg, so just call into selectMappedSubject
	// so we only lock once.
	nsubj, changed := si.acc.selectMappedSubject(to)
	if changed {
		c.pa.mapped = []byte(to)
		to = nsubj
	}

	// Set previous service import to detect chaining.
	lpsi := len(c.pa.psi)
	hadPrevSi, share := lpsi > 0, si.share
	if hadPrevSi {
		share = c.pa.psi[lpsi-1].share
	}
	c.pa.psi = append(c.pa.psi, si)

	// Place our client info for the request in the original message.
	// This will survive going across routes, etc.
	if !isResponse {
		isSysImport := si.acc == c.srv.SystemAccount()
		var ci *ClientInfo
		if hadPrevSi && c.pa.hdr >= 0 {
			var cis ClientInfo
			if err := json.Unmarshal(getHeader(ClientInfoHdr, msg[:c.pa.hdr]), &cis); err == nil {
				ci = &cis
				ci.Service = acc.Name
				// Check if we are moving into a share details account from a non-shared
				// and add in server and cluster details.
				if !share && (si.share || isSysImport) {
					c.addServerAndClusterInfo(ci)
				}
			}
		} else if c.kind != LEAF || c.pa.hdr < 0 || len(getHeader(ClientInfoHdr, msg[:c.pa.hdr])) == 0 {
			ci = c.getClientInfo(share)
			// If we did not share but the imports destination is the system account add in the server and cluster info.
			if !share && isSysImport {
				c.addServerAndClusterInfo(ci)
			}
		} else if c.kind == LEAF && (si.share || isSysImport) {
			// We have a leaf header here for ci, augment as above.
			ci = c.getClientInfo(si.share)
			if !si.share && isSysImport {
				c.addServerAndClusterInfo(ci)
			}
		}
		// Set clientInfo if present.
		if ci != nil {
			if b, _ := json.Marshal(ci); b != nil {
				msg = c.setHeader(ClientInfoHdr, string(b), msg)
			}
		}
	}

	// Set our optional subject(to) and reply.
	if !isResponse && to != subject {
		c.pa.subject = []byte(to)
	}
	c.pa.reply = nrr

	if changed && c.isMqtt() && c.pa.hdr > 0 {
		c.srv.mqttStoreQoS1MsgForAccountOnNewSubject(c.pa.hdr, msg, si.acc.GetName(), to)
	}

	// FIXME(dlc) - Do L1 cache trick like normal client?
	rr := si.acc.sl.Match(to)

	// If we are a route or gateway or leafnode and this message is flipped to a queue subscriber we
	// need to handle that since the processMsgResults will want a queue filter.
	flags := pmrMsgImportedFromService
	if c.kind == GATEWAY || c.kind == ROUTER || c.kind == LEAF {
		flags |= pmrIgnoreEmptyQueueFilter
	}

	// We will be calling back into processMsgResults since we are now being called as a normal sub.
	// We need to take care of the c.in.rts, so save off what is there and use a local version. We
	// will put back what was there after.

	orts := c.in.rts

	var lrts [routeTargetInit]routeTarget
	c.in.rts = lrts[:0]

	var didDeliver bool

	// If this is not a gateway connection but gateway is enabled,
	// try to send this converted message to all gateways.
	if c.srv.gateway.enabled {
		flags |= pmrCollectQueueNames
		var queues [][]byte
		didDeliver, queues = c.processMsgResults(si.acc, rr, msg, c.pa.deliver, []byte(to), nrr, flags)
		didDeliver = c.sendMsgToGateways(si.acc, msg, []byte(to), nrr, queues) || didDeliver
	} else {
		didDeliver, _ = c.processMsgResults(si.acc, rr, msg, c.pa.deliver, []byte(to), nrr, flags)
	}

	// Restore to original values.
	c.in.rts = orts
	c.pa = pacopy

	// Determine if we should remove this service import. This is for response service imports.
	// We will remove if we did not deliver, or if we are a response service import and we are
	// a singleton, or we have an EOF message.
	shouldRemove := !didDeliver || (isResponse && (si.rt == Singleton || len(msg) == LEN_CR_LF))
	// If we are tracking and we did not actually send the latency info we need to suppress the removal.
	if si.tracking && !didSendTL {
		shouldRemove = false
	}
	// If we are streamed or chunked we need to update our timestamp to avoid cleanup.
	if si.rt != Singleton && didDeliver {
		acc.mu.Lock()
		si.ts = time.Now().UnixNano()
		acc.mu.Unlock()
	}

	// Cleanup of a response service import
	if shouldRemove {
		reason := rsiOk
		if !didDeliver {
			reason = rsiNoDelivery
		}
		if isResponse {
			acc.removeRespServiceImport(si, reason)
		} else {
			// This is a main import and since we could not even deliver to the exporting account
			// go ahead and remove the respServiceImport we created above.
			si.acc.removeRespServiceImport(rsi, reason)
		}
	}
}

func (c *client) addSubToRouteTargets(sub *subscription) {
	if c.in.rts == nil {
		c.in.rts = make([]routeTarget, 0, routeTargetInit)
	}

	for i := range c.in.rts {
		rt := &c.in.rts[i]
		if rt.sub.client == sub.client {
			if sub.queue != nil {
				rt.qs = append(rt.qs, sub.queue...)
				rt.qs = append(rt.qs, ' ')
			}
			return
		}
	}

	var rt *routeTarget
	lrts := len(c.in.rts)

	// If we are here we do not have the sub yet in our list
	// If we have to grow do so here.
	if lrts == cap(c.in.rts) {
		c.in.rts = append(c.in.rts, routeTarget{})
	}

	c.in.rts = c.in.rts[:lrts+1]
	rt = &c.in.rts[lrts]
	rt.sub = sub
	rt.qs = rt._qs[:0]
	if sub.queue != nil {
		rt.qs = append(rt.qs, sub.queue...)
		rt.qs = append(rt.qs, ' ')
	}
}

// This processes the sublist results for a given message.
// Returns if the message was delivered to at least target and queue filters.
func (c *client) processMsgResults(acc *Account, r *SublistResult, msg, deliver, subject, reply []byte, flags int) (bool, [][]byte) {
	// For sending messages across routes and leafnodes.
	// Reset if we have one since we reuse this data structure.
	if c.in.rts != nil {
		c.in.rts = c.in.rts[:0]
	}

	var rplyHasGWPrefix bool
	var creply = reply

	// If the reply subject is a GW routed reply, we will perform some
	// tracking in deliverMsg(). We also want to send to the user the
	// reply without the prefix. `creply` will be set to that and be
	// used to create the message header for client connections.
	if rplyHasGWPrefix = isGWRoutedReply(reply); rplyHasGWPrefix {
		creply = reply[gwSubjectOffset:]
	}

	// With JetStream we now have times where we want to match a subscription
	// on one subject, but deliver it with another. e.g. JetStream deliverables.
	// This only works for last mile, meaning to a client. For other types we need
	// to use the original subject.
	subj := subject
	if len(deliver) > 0 {
		subj = deliver
	}

	// Check for JetStream encoded reply subjects.
	// For now these will only be on $JS.ACK prefixed reply subjects.
	var remapped bool
	if len(creply) > 0 &&
		c.kind != CLIENT && c.kind != SYSTEM && c.kind != JETSTREAM && c.kind != ACCOUNT &&
		bytes.HasPrefix(creply, []byte(jsAckPre)) {
		// We need to rewrite the subject and the reply.
		if li := bytes.LastIndex(creply, []byte("@")); li != -1 && li < len(creply)-1 {
			remapped = true
			subj, creply = creply[li+1:], creply[:li]
		}
	}

	var didDeliver bool

	// delivery subject for clients
	var dsubj []byte
	// Used as scratch if mapping
	var _dsubj [64]byte

	// For stats, we will keep track of the number of messages that have been
	// delivered and then multiply by the size of that message and update
	// server and account stats in a "single" operation (instead of per-sub).
	// However, we account for situations where the message is possibly changed
	// by having an extra size
	var dlvMsgs int64
	var dlvExtraSize int64

	// We need to know if this is a MQTT producer because they send messages
	// without CR_LF (we otherwise remove the size of CR_LF from message size).
	prodIsMQTT := c.isMqtt()

	updateStats := func() {
		if dlvMsgs == 0 {
			return
		}
		totalBytes := dlvMsgs*int64(len(msg)) + dlvExtraSize
		// For non MQTT producers, remove the CR_LF * number of messages
		if !prodIsMQTT {
			totalBytes -= dlvMsgs * int64(LEN_CR_LF)
		}
		if acc != nil {
			atomic.AddInt64(&acc.outMsgs, dlvMsgs)
			atomic.AddInt64(&acc.outBytes, totalBytes)
		}
		if srv := c.srv; srv != nil {
			atomic.AddInt64(&srv.outMsgs, dlvMsgs)
			atomic.AddInt64(&srv.outBytes, totalBytes)
		}
	}

	// Loop over all normal subscriptions that match.
	for _, sub := range r.psubs {
		// Check if this is a send to a ROUTER. We now process
		// these after everything else.
		switch sub.client.kind {
		case ROUTER:
			if (c.kind != ROUTER && !c.isSpokeLeafNode()) || (flags&pmrAllowSendFromRouteToRoute != 0) {
				c.addSubToRouteTargets(sub)
			}
			continue
		case GATEWAY:
			// Never send to gateway from here.
			continue
		case LEAF:
			// We handle similarly to routes and use the same data structures.
			// Leaf node delivery audience is different however.
			// Also leaf nodes are always no echo, so we make sure we are not
			// going to send back to ourselves here. For messages from routes we want
			// to suppress in general unless we know from the hub or its a service reply.
			if c != sub.client && (c.kind != ROUTER || sub.client.isHubLeafNode() || isServiceReply(c.pa.subject)) {
				c.addSubToRouteTargets(sub)
			}
			continue
		}

		// Assume delivery subject is the normal subject to this point.
		dsubj = subj

		// Check for stream import mapped subs (shadow subs). These apply to local subs only.
		if sub.im != nil {
			// If this message was a service import do not re-export to an exported stream.
			if flags&pmrMsgImportedFromService != 0 {
				continue
			}
			if sub.im.tr != nil {
				to, _ := sub.im.tr.transformSubject(string(subject))
				dsubj = append(_dsubj[:0], to...)
			} else if sub.im.usePub {
				dsubj = append(_dsubj[:0], subj...)
			} else {
				dsubj = append(_dsubj[:0], sub.im.to...)
			}

			// Make sure deliver is set if inbound from a route.
			if remapped && (c.kind == GATEWAY || c.kind == ROUTER || c.kind == LEAF) {
				deliver = subj
			}
			// If we are mapping for a deliver subject we will reverse roles.
			// The original subj we set from above is correct for the msg header,
			// but we need to transform the deliver subject to properly route.
			if len(deliver) > 0 {
				dsubj, subj = subj, dsubj
			}
		}

		// Remap to the original subject if internal.
		if sub.icb != nil && sub.rsi {
			dsubj = subject
		}

		// Normal delivery
		mh := c.msgHeader(dsubj, creply, sub)
		if c.deliverMsg(prodIsMQTT, sub, acc, dsubj, creply, mh, msg, rplyHasGWPrefix) {
			// We don't count internal deliveries, so do only when sub.icb is nil.
			if sub.icb == nil {
				dlvMsgs++
			}
			didDeliver = true
		}
	}

	// Set these up to optionally filter based on the queue lists.
	// This is for messages received from routes which will have directed
	// guidance on which queue groups we should deliver to.
	qf := c.pa.queues

	// Declared here because of goto.
	var queues [][]byte

	// For all routes/leaf/gateway connections, we may still want to send messages to
	// leaf nodes or routes even if there are no queue filters since we collect
	// them above and do not process inline like normal clients.
	// However, do select queue subs if asked to ignore empty queue filter.
	if (c.kind == LEAF || c.kind == ROUTER || c.kind == GATEWAY) && len(qf) == 0 && flags&pmrIgnoreEmptyQueueFilter == 0 {
		goto sendToRoutesOrLeafs
	}

	// Check to see if we have our own rand yet. Global rand
	// has contention with lots of clients, etc.
	if c.in.prand == nil {
		c.in.prand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	// Process queue subs
	for i := 0; i < len(r.qsubs); i++ {
		qsubs := r.qsubs[i]
		// If we have a filter check that here. We could make this a map or someting more
		// complex but linear search since we expect queues to be small. Should be faster
		// and more cache friendly.
		if qf != nil && len(qsubs) > 0 {
			tqn := qsubs[0].queue
			for _, qn := range qf {
				if bytes.Equal(qn, tqn) {
					goto selectQSub
				}
			}
			continue
		}

	selectQSub:
		// We will hold onto remote or lead qsubs when we are coming from
		// a route or a leaf node just in case we can no longer do local delivery.
		var rsub, sub *subscription
		var _ql [32]*subscription

		src := c.kind
		// If we just came from a route we want to prefer local subs.
		// So only select from local subs but remember the first rsub
		// in case all else fails.
		if src == ROUTER {
			ql := _ql[:0]
			for i := 0; i < len(qsubs); i++ {
				sub = qsubs[i]
				if sub.client.kind == LEAF || sub.client.kind == ROUTER {
					// If we have assigned an rsub already, replace if the destination is a LEAF
					// since we want to favor that compared to a ROUTER. We could make sure that
					// we override only if previous was a ROUTE and not a LEAF, but we don't have to.
					if rsub == nil || sub.client.kind == LEAF {
						rsub = sub
					}
				} else {
					ql = append(ql, sub)
				}
			}
			qsubs = ql
		}

		sindex := 0
		lqs := len(qsubs)
		if lqs > 1 {
			sindex = c.in.prand.Int() % lqs
		}

		// Find a subscription that is able to deliver this message starting at a random index.
		for i := 0; i < lqs; i++ {
			if sindex+i < lqs {
				sub = qsubs[sindex+i]
			} else {
				sub = qsubs[(sindex+i)%lqs]
			}
			if sub == nil {
				continue
			}

			// We have taken care of preferring local subs for a message from a route above.
			// Here we just care about a client or leaf and skipping a leaf and preferring locals.
			if dst := sub.client.kind; dst == ROUTER || dst == LEAF {
				if (src == LEAF || src == CLIENT) && dst == LEAF {
					if rsub == nil {
						rsub = sub
					}
					continue
				} else {
					c.addSubToRouteTargets(sub)
					// Clear rsub since we added a sub.
					rsub = nil
					if flags&pmrCollectQueueNames != 0 {
						queues = append(queues, sub.queue)
					}
				}
				break
			}

			// Assume delivery subject is normal subject to this point.
			dsubj = subj
			// Check for stream import mapped subs. These apply to local subs only.
			if sub.im != nil {
				// If this message was a service import do not re-export to an exported stream.
				if flags&pmrMsgImportedFromService != 0 {
					continue
				}
				if sub.im.tr != nil {
					to, _ := sub.im.tr.transformSubject(string(subject))
					dsubj = append(_dsubj[:0], to...)
				} else if sub.im.usePub {
					dsubj = append(_dsubj[:0], subj...)
				} else {
					dsubj = append(_dsubj[:0], sub.im.to...)
				}
				// Make sure deliver is set if inbound from a route.
				if remapped && (c.kind == GATEWAY || c.kind == ROUTER || c.kind == LEAF) {
					deliver = subj
				}
				// If we are mapping for a deliver subject we will reverse roles.
				// The original subj we set from above is correct for the msg header,
				// but we need to transform the deliver subject to properly route.
				if len(deliver) > 0 {
					dsubj, subj = subj, dsubj
				}
			}

			mh := c.msgHeader(dsubj, creply, sub)
			if c.deliverMsg(prodIsMQTT, sub, acc, subject, creply, mh, msg, rplyHasGWPrefix) {
				if sub.icb == nil {
					dlvMsgs++
				}
				didDeliver = true
				// Clear rsub
				rsub = nil
				if flags&pmrCollectQueueNames != 0 {
					queues = append(queues, sub.queue)
				}
				break
			}
		}

		if rsub != nil {
			// If we are here we tried to deliver to a local qsub
			// but failed. So we will send it to a remote or leaf node.
			c.addSubToRouteTargets(rsub)
			if flags&pmrCollectQueueNames != 0 {
				queues = append(queues, rsub.queue)
			}
		}
	}

sendToRoutesOrLeafs:

	// If no messages for routes or leafnodes return here.
	if len(c.in.rts) == 0 {
		updateStats()
		return didDeliver, queues
	}

	// If we do have a deliver subject we need to do something with it.
	// Again this is when JetStream (but possibly others) wants the system
	// to rewrite the delivered subject. The way we will do that is place it
	// at the end of the reply subject if it exists.
	if len(deliver) > 0 && len(reply) > 0 {
		reply = append(reply, '@')
		reply = append(reply, deliver...)
	}

	// Copy off original pa in case it changes.
	pa := c.pa

	// We address by index to avoid struct copy.
	// We have inline structs for memory layout and cache coherency.
	for i := range c.in.rts {
		rt := &c.in.rts[i]
		dc := rt.sub.client
		dmsg, hset := msg, false

		// Check if we have an origin cluster set from a leafnode message.
		// If so make sure we do not send it back to the same cluster for a different
		// leafnode. Cluster wide no echo.
		if dc.kind == LEAF {
			// Check two scenarios. One is inbound from a route (c.pa.origin)
			if c.kind == ROUTER && len(c.pa.origin) > 0 {
				if string(c.pa.origin) == dc.remoteCluster() {
					continue
				}
			}
			// The other is leaf to leaf.
			if c.kind == LEAF {
				src, dest := c.remoteCluster(), dc.remoteCluster()
				if src != _EMPTY_ && src == dest {
					continue
				}
			}

			// We need to check if this is a request that has a stamped client information header.
			// This will contain an account but will represent the account from the leafnode. If
			// they are not named the same this would cause an account lookup failure trying to
			// process the request for something like JetStream or other system services that rely
			// on the client info header. We can just check for reply and the presence of a header
			// to avoid slow downs for all traffic.
			if len(c.pa.reply) > 0 && c.pa.hdr >= 0 {
				dmsg, hset = c.checkLeafClientInfoHeader(msg)
			}
		}

		mh := c.msgHeaderForRouteOrLeaf(subject, reply, rt, acc)
		if c.deliverMsg(prodIsMQTT, rt.sub, acc, subject, reply, mh, dmsg, false) {
			if rt.sub.icb == nil {
				dlvMsgs++
				dlvExtraSize += int64(len(dmsg) - len(msg))
			}
			didDeliver = true
		}

		// If we set the header reset the origin pub args.
		if hset {
			c.pa = pa
		}
	}
	updateStats()
	return didDeliver, queues
}

// Check and swap accounts on a client info header destined across a leafnode.
func (c *client) checkLeafClientInfoHeader(msg []byte) (dmsg []byte, setHdr bool) {
	if c.pa.hdr < 0 || len(msg) < c.pa.hdr {
		return msg, false
	}
	cir := getHeader(ClientInfoHdr, msg[:c.pa.hdr])
	if len(cir) == 0 {
		return msg, false
	}

	dmsg = msg

	var ci ClientInfo
	if err := json.Unmarshal(cir, &ci); err == nil {
		if v, _ := c.srv.leafRemoteAccounts.Load(ci.Account); v != nil {
			remoteAcc := v.(string)
			if ci.Account != remoteAcc {
				ci.Account = remoteAcc
				if b, _ := json.Marshal(ci); b != nil {
					dmsg, setHdr = c.setHeader(ClientInfoHdr, string(b), msg), true
				}
			}
		}
	}
	return dmsg, setHdr
}

func (c *client) pubPermissionViolation(subject []byte) {
	c.sendErr(fmt.Sprintf("Permissions Violation for Publish to %q", subject))
	c.Errorf("Publish Violation - %s, Subject %q", c.getAuthUser(), subject)
}

func (c *client) subPermissionViolation(sub *subscription) {
	errTxt := fmt.Sprintf("Permissions Violation for Subscription to %q", sub.subject)
	logTxt := fmt.Sprintf("Subscription Violation - %s, Subject %q, SID %s",
		c.getAuthUser(), sub.subject, sub.sid)

	if sub.queue != nil {
		errTxt = fmt.Sprintf("Permissions Violation for Subscription to %q using queue %q", sub.subject, sub.queue)
		logTxt = fmt.Sprintf("Subscription Violation - %s, Subject %q, Queue: %q, SID %s",
			c.getAuthUser(), sub.subject, sub.queue, sub.sid)
	}

	c.sendErr(errTxt)
	c.Errorf(logTxt)
}

func (c *client) replySubjectViolation(reply []byte) {
	c.sendErr(fmt.Sprintf("Permissions Violation for Publish with Reply of %q", reply))
	c.Errorf("Publish Violation - %s, Reply %q", c.getAuthUser(), reply)
}

func (c *client) maxTokensViolation(sub *subscription) {
	errTxt := fmt.Sprintf("Permissions Violation for Subscription to %q, too many tokens", sub.subject)
	logTxt := fmt.Sprintf("Subscription Violation Too Many Tokens - %s, Subject %q, SID %s",
		c.getAuthUser(), sub.subject, sub.sid)
	c.sendErr(errTxt)
	c.Errorf(logTxt)
}

func (c *client) processPingTimer() {
	c.mu.Lock()
	c.ping.tmr = nil
	// Check if connection is still opened
	if c.isClosed() {
		c.mu.Unlock()
		return
	}

	c.Debugf("%s Ping Timer", c.kindString())

	var sendPing bool

	pingInterval := c.srv.getOpts().PingInterval
	if c.kind == GATEWAY {
		pingInterval = adjustPingIntervalForGateway(pingInterval)
	}
	now := time.Now()
	needRTT := c.rtt == 0 || now.Sub(c.rttStart) > DEFAULT_RTT_MEASUREMENT_INTERVAL

	// Do not delay PINGs for GATEWAY or spoke LEAF connections.
	if c.kind == GATEWAY || c.isSpokeLeafNode() {
		sendPing = true
	} else {
		// If we have had activity within the PingInterval then
		// there is no need to send a ping. This can be client data
		// or if we received a ping from the other side.
		if delta := now.Sub(c.last); delta < pingInterval && !needRTT {
			c.Debugf("Delaying PING due to client activity %v ago", delta.Round(time.Second))
		} else if delta := now.Sub(c.ping.last); delta < pingInterval && !needRTT {
			c.Debugf("Delaying PING due to remote ping %v ago", delta.Round(time.Second))
		} else {
			sendPing = true
		}
	}

	if sendPing {
		// Check for violation
		if c.ping.out+1 > c.srv.getOpts().MaxPingsOut {
			c.Debugf("Stale Client Connection - Closing")
			c.enqueueProto([]byte(fmt.Sprintf(errProto, "Stale Connection")))
			c.mu.Unlock()
			c.closeConnection(StaleConnection)
			return
		}
		// Send PING
		c.sendPing()
	}

	// Reset to fire again.
	c.setPingTimer()
	c.mu.Unlock()
}

// Returns the smallest value between the given `d` and `gatewayMaxPingInterval` durations.
// Invoked for connections known to be of GATEWAY type.
func adjustPingIntervalForGateway(d time.Duration) time.Duration {
	if d > gatewayMaxPingInterval {
		return gatewayMaxPingInterval
	}
	return d
}

// Lock should be held
func (c *client) setPingTimer() {
	if c.srv == nil {
		return
	}
	d := c.srv.getOpts().PingInterval
	if c.kind == GATEWAY {
		d = adjustPingIntervalForGateway(d)
	}
	c.ping.tmr = time.AfterFunc(d, c.processPingTimer)
}

// Lock should be held
func (c *client) clearPingTimer() {
	if c.ping.tmr == nil {
		return
	}
	c.ping.tmr.Stop()
	c.ping.tmr = nil
}

func (c *client) clearTlsToTimer() {
	if c.tlsTo == nil {
		return
	}
	c.tlsTo.Stop()
	c.tlsTo = nil
}

// Lock should be held
func (c *client) setAuthTimer(d time.Duration) {
	c.atmr = time.AfterFunc(d, c.authTimeout)
}

// Lock should be held
func (c *client) clearAuthTimer() bool {
	if c.atmr == nil {
		return true
	}
	stopped := c.atmr.Stop()
	c.atmr = nil
	return stopped
}

// We may reuse atmr for expiring user jwts,
// so check connectReceived.
// Lock assume held on entry.
func (c *client) awaitingAuth() bool {
	return !c.flags.isSet(connectReceived) && c.atmr != nil
}

// This will set the atmr for the JWT expiration time.
// We will lock on entry.
func (c *client) setExpirationTimer(d time.Duration) {
	c.mu.Lock()
	c.atmr = time.AfterFunc(d, c.authExpired)
	c.mu.Unlock()
}

// Possibly flush the connection and then close the low level connection.
// The boolean `minimalFlush` indicates if the flush operation should have a
// minimal write deadline.
// Lock is held on entry.
func (c *client) flushAndClose(minimalFlush bool) {
	if !c.flags.isSet(skipFlushOnClose) && c.out.pb > 0 {
		if minimalFlush {
			const lowWriteDeadline = 100 * time.Millisecond

			// Reduce the write deadline if needed.
			if c.out.wdl > lowWriteDeadline {
				c.out.wdl = lowWriteDeadline
			}
		}
		c.flushOutbound()
	}
	for i := range c.out.nb {
		nbPoolPut(c.out.nb[i])
	}
	c.out.nb = nil

	// Close the low level connection.
	if c.nc != nil {
		// Starting with Go 1.16, the low level close will set its own deadline
		// of 5 seconds, so setting our own deadline does not work. Instead,
		// we will close the TLS connection in separate go routine.
		nc := c.nc
		c.nc = nil
		if _, ok := nc.(*tls.Conn); ok {
			go func() { nc.Close() }()
		} else {
			nc.Close()
		}
	}
}

var kindStringMap = map[int]string{
	CLIENT:    "Client",
	ROUTER:    "Router",
	GATEWAY:   "Gateway",
	LEAF:      "Leafnode",
	JETSTREAM: "JetStream",
	ACCOUNT:   "Account",
	SYSTEM:    "System",
}

func (c *client) kindString() string {
	if kindStringVal, ok := kindStringMap[c.kind]; ok {
		return kindStringVal
	}
	return "Unknown Type"
}

// swapAccountAfterReload will check to make sure the bound account for this client
// is current. Under certain circumstances after a reload we could be pointing to
// an older one.
func (c *client) swapAccountAfterReload() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.srv == nil {
		return
	}
	acc, _ := c.srv.LookupAccount(c.acc.Name)
	c.acc = acc
}

// processSubsOnConfigReload removes any subscriptions the client has that are no
// longer authorized, and checks for imports (accounts) due to a config reload.
func (c *client) processSubsOnConfigReload(awcsti map[string]struct{}) {
	c.mu.Lock()
	var (
		checkPerms = c.perms != nil
		checkAcc   = c.acc != nil
		acc        = c.acc
	)
	if !checkPerms && !checkAcc {
		c.mu.Unlock()
		return
	}
	var (
		_subs    [32]*subscription
		subs     = _subs[:0]
		_removed [32]*subscription
		removed  = _removed[:0]
		srv      = c.srv
	)
	if checkAcc {
		// We actually only want to check if stream imports have changed.
		if _, ok := awcsti[acc.Name]; !ok {
			checkAcc = false
		}
	}
	// We will clear any mperms we have here. It will rebuild on the fly with canSubscribe,
	// so we do that here as we collect them. We will check result down below.
	c.mperms = nil
	// Collect client's subs under the lock
	for _, sub := range c.subs {
		// Just checking to rebuild mperms under the lock, will collect removed though here.
		// Only collect under subs array of canSubscribe and checkAcc true.
		canSub := c.canSubscribe(string(sub.subject))
		canQSub := sub.queue != nil && c.canSubscribe(string(sub.subject), string(sub.queue))

		if !canSub && !canQSub {
			removed = append(removed, sub)
		} else if checkAcc {
			subs = append(subs, sub)
		}
	}
	c.mu.Unlock()

	// This list is all subs who are allowed and we need to check accounts.
	for _, sub := range subs {
		c.mu.Lock()
		oldShadows := sub.shadow
		sub.shadow = nil
		c.mu.Unlock()
		c.addShadowSubscriptions(acc, sub)
		for _, nsub := range oldShadows {
			nsub.im.acc.sl.Remove(nsub)
		}
	}

	// Unsubscribe all that need to be removed and report back to client and logs.
	for _, sub := range removed {
		c.unsubscribe(acc, sub, true, true)
		c.sendErr(fmt.Sprintf("Permissions Violation for Subscription to %q (sid %q)",
			sub.subject, sub.sid))
		srv.Noticef("Removed sub %q (sid %q) for %s - not authorized",
			sub.subject, sub.sid, c.getAuthUser())
	}
}

// Allows us to count up all the queue subscribers during close.
type qsub struct {
	sub *subscription
	n   int32
}

func (c *client) closeConnection(reason ClosedState) {
	c.mu.Lock()
	if c.flags.isSet(closeConnection) {
		c.mu.Unlock()
		return
	}
	// Note that we may have markConnAsClosed() invoked before closeConnection(),
	// so don't set this to 1, instead bump the count.
	c.rref++
	c.flags.set(closeConnection)
	c.clearAuthTimer()
	c.clearPingTimer()
	c.clearTlsToTimer()
	c.markConnAsClosed(reason)

	// Unblock anyone who is potentially stalled waiting on us.
	if c.out.stc != nil {
		close(c.out.stc)
		c.out.stc = nil
	}

	var (
		connectURLs   []string
		wsConnectURLs []string
		kind          = c.kind
		srv           = c.srv
		noReconnect   = c.flags.isSet(noReconnect)
		acc           = c.acc
		spoke         bool
	)

	// Snapshot for use if we are a client connection.
	// FIXME(dlc) - we can just stub in a new one for client
	// and reference existing one.
	var subs []*subscription
	if kind == CLIENT || kind == LEAF || kind == JETSTREAM {
		var _subs [32]*subscription
		subs = _subs[:0]
		for _, sub := range c.subs {
			// Auto-unsubscribe subscriptions must be unsubscribed forcibly.
			sub.max = 0
			sub.close()
			subs = append(subs, sub)
		}
		spoke = c.isSpokeLeafNode()
	}

	if c.route != nil {
		connectURLs = c.route.connectURLs
		wsConnectURLs = c.route.wsConnURLs
	}

	// If we have remote latency tracking running shut that down.
	if c.rrTracking != nil {
		c.rrTracking.ptmr.Stop()
		c.rrTracking = nil
	}

	c.mu.Unlock()

	// Remove client's or leaf node or jetstream subscriptions.
	if acc != nil && (kind == CLIENT || kind == LEAF || kind == JETSTREAM) {
		acc.sl.RemoveBatch(subs)
	} else if kind == ROUTER {
		go c.removeRemoteSubs()
	}

	if srv != nil {
		// If this is a route that disconnected, possibly send an INFO with
		// the updated list of connect URLs to clients that know how to
		// handle async INFOs.
		if (len(connectURLs) > 0 || len(wsConnectURLs) > 0) && !srv.getOpts().Cluster.NoAdvertise {
			srv.removeConnectURLsAndSendINFOToClients(connectURLs, wsConnectURLs)
		}

		// Unregister
		srv.removeClient(c)

		// Update remote subscriptions.
		if acc != nil && (kind == CLIENT || kind == LEAF || kind == JETSTREAM) {
			qsubs := map[string]*qsub{}
			for _, sub := range subs {
				// Call unsubscribe here to cleanup shadow subscriptions and such.
				c.unsubscribe(acc, sub, true, false)
				// Update route as normal for a normal subscriber.
				if sub.queue == nil {
					if !spoke {
						srv.updateRouteSubscriptionMap(acc, sub, -1)
						if srv.gateway.enabled {
							srv.gatewayUpdateSubInterest(acc.Name, sub, -1)
						}
					}
					srv.updateLeafNodes(acc, sub, -1)
				} else {
					// We handle queue subscribers special in case we
					// have a bunch we can just send one update to the
					// connected routes.
					num := int32(1)
					if kind == LEAF {
						num = sub.qw
					}
					key := string(sub.subject) + " " + string(sub.queue)
					if esub, ok := qsubs[key]; ok {
						esub.n += num
					} else {
						qsubs[key] = &qsub{sub, num}
					}
				}
			}
			// Process any qsubs here.
			for _, esub := range qsubs {
				if !spoke {
					srv.updateRouteSubscriptionMap(acc, esub.sub, -(esub.n))
					if srv.gateway.enabled {
						srv.gatewayUpdateSubInterest(acc.Name, esub.sub, -(esub.n))
					}
				}
				srv.updateLeafNodes(acc, esub.sub, -(esub.n))
			}
			if prev := acc.removeClient(c); prev == 1 {
				srv.decActiveAccounts()
			}
		}
	}

	// Don't reconnect connections that have been marked with
	// the no reconnect flag.
	if noReconnect {
		return
	}

	c.reconnect()
}

// Depending on the kind of connections, this may attempt to recreate a connection.
// The actual reconnect attempt will be started in a go routine.
func (c *client) reconnect() {
	var (
		retryImplicit bool
		gwName        string
		gwIsOutbound  bool
		gwCfg         *gatewayCfg
	)

	c.mu.Lock()
	// Decrease the ref count and perform the reconnect only if == 0.
	c.rref--
	if c.flags.isSet(noReconnect) || c.rref > 0 {
		c.mu.Unlock()
		return
	}
	if c.route != nil {
		// A route is marked as solicited if it was given an URL to connect to,
		// which would be the case even with implicit (due to gossip), so mark this
		// as a retry for a route that is solicited and not explicit.
		retryImplicit = c.route.retry || (c.route.didSolicit && c.route.routeType == Implicit)
	}
	kind := c.kind
	if kind == GATEWAY {
		gwName = c.gw.name
		gwIsOutbound = c.gw.outbound
		gwCfg = c.gw.cfg
	}
	srv := c.srv
	c.mu.Unlock()

	// Check for a solicited route. If it was, start up a reconnect unless
	// we are already connected to the other end.
	if c.isSolicitedRoute() || retryImplicit {
		// Capture these under lock
		c.mu.Lock()
		rid := c.route.remoteID
		rtype := c.route.routeType
		rurl := c.route.url
		c.mu.Unlock()

		srv.mu.Lock()
		defer srv.mu.Unlock()

		// It is possible that the server is being shutdown.
		// If so, don't try to reconnect
		if !srv.running {
			return
		}

		if rid != "" && srv.remotes[rid] != nil {
			srv.Debugf("Not attempting reconnect for solicited route, already connected to \"%s\"", rid)
			return
		} else if rid == srv.info.ID {
			srv.Debugf("Detected route to self, ignoring %q", rurl.Redacted())
			return
		} else if rtype != Implicit || retryImplicit {
			srv.Debugf("Attempting reconnect for solicited route \"%s\"", rurl.Redacted())
			// Keep track of this go-routine so we can wait for it on
			// server shutdown.
			srv.startGoRoutine(func() { srv.reConnectToRoute(rurl, rtype) })
		}
	} else if srv != nil && kind == GATEWAY && gwIsOutbound {
		if gwCfg != nil {
			srv.Debugf("Attempting reconnect for gateway %q", gwName)
			// Run this as a go routine since we may be called within
			// the solicitGateway itself if there was an error during
			// the creation of the gateway connection.
			srv.startGoRoutine(func() { srv.reconnectGateway(gwCfg) })
		} else {
			srv.Debugf("Gateway %q not in configuration, not attempting reconnect", gwName)
		}
	} else if c.isSolicitedLeafNode() {
		// Check if this is a solicited leaf node. Start up a reconnect.
		srv.startGoRoutine(func() { srv.reConnectToRemoteLeafNode(c.leaf.remote) })
	}
}

// Set the noReconnect flag. This is used before a call to closeConnection()
// to prevent the connection to reconnect (routes, gateways).
func (c *client) setNoReconnect() {
	c.mu.Lock()
	c.flags.set(noReconnect)
	c.mu.Unlock()
}

// Returns the client's RTT value with the protection of the client's lock.
func (c *client) getRTTValue() time.Duration {
	c.mu.Lock()
	rtt := c.rtt
	c.mu.Unlock()
	return rtt
}

// This function is used by ROUTER and GATEWAY connections to
// look for a subject on a given account (since these type of
// connections are not bound to a specific account).
// If the c.pa.subject is found in the cache, the cached result
// is returned, otherwse, we match the account's sublist and update
// the cache. The cache is pruned if reaching a certain size.
func (c *client) getAccAndResultFromCache() (*Account, *SublistResult) {
	var (
		acc *Account
		pac *perAccountCache
		r   *SublistResult
		ok  bool
	)
	// Check our cache.
	if pac, ok = c.in.pacache[string(c.pa.pacache)]; ok {
		// Check the genid to see if it's still valid.
		// sl could be swapped out on reload so need to lock.
		pac.acc.mu.RLock()
		sl := pac.acc.sl
		pac.acc.mu.RUnlock()

		if genid := atomic.LoadUint64(&sl.genid); genid != pac.genid {
			ok = false
			delete(c.in.pacache, string(c.pa.pacache))
		} else {
			acc = pac.acc
			r = pac.results
		}
	}

	if !ok {
		// Match correct account and sublist.
		if acc, _ = c.srv.LookupAccount(string(c.pa.account)); acc == nil {
			return nil, nil
		}

		// sl could be swapped out on reload so need to lock.
		acc.mu.RLock()
		sl := acc.sl
		acc.mu.RUnlock()

		// Match against the account sublist.
		r = sl.Match(string(c.pa.subject))

		// Store in our cache
		c.in.pacache[string(c.pa.pacache)] = &perAccountCache{acc, r, atomic.LoadUint64(&sl.genid)}

		// Check if we need to prune.
		if len(c.in.pacache) > maxPerAccountCacheSize {
			c.prunePerAccountCache()
		}
	}
	return acc, r
}

// Account will return the associated account for this client.
func (c *client) Account() *Account {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	acc := c.acc
	c.mu.Unlock()
	return acc
}

// prunePerAccountCache will prune off a random number of cache entries.
func (c *client) prunePerAccountCache() {
	n := 0
	for cacheKey := range c.in.pacache {
		delete(c.in.pacache, cacheKey)
		if n++; n > prunePerAccountCacheSize {
			break
		}
	}
}

// pruneClosedSubFromPerAccountCache remove entries that contain subscriptions
// that have been closed.
func (c *client) pruneClosedSubFromPerAccountCache() {
	for cacheKey, pac := range c.in.pacache {
		for _, sub := range pac.results.psubs {
			if sub.isClosed() {
				goto REMOVE
			}
		}
		for _, qsub := range pac.results.qsubs {
			for _, sub := range qsub {
				if sub.isClosed() {
					goto REMOVE
				}
			}
		}
		continue
	REMOVE:
		delete(c.in.pacache, cacheKey)
	}
}

// Returns our service account for this request.
func (ci *ClientInfo) serviceAccount() string {
	if ci == nil {
		return _EMPTY_
	}
	if ci.Service != _EMPTY_ {
		return ci.Service
	}
	return ci.Account
}

// Add in our server and cluster information to this client info.
func (c *client) addServerAndClusterInfo(ci *ClientInfo) {
	if ci == nil {
		return
	}
	// Server
	if c.kind != LEAF {
		ci.Server = c.srv.Name()
	} else if c.kind == LEAF {
		ci.Server = c.leaf.remoteServer
	}
	// Cluster
	ci.Cluster = c.srv.cachedClusterName()
	// If we have gateways fill in cluster alternates.
	// These will be in RTT asc order.
	if c.srv.gateway.enabled {
		var gws []*client
		c.srv.getOutboundGatewayConnections(&gws)
		for _, c := range gws {
			c.mu.Lock()
			cn := c.gw.name
			c.mu.Unlock()
			ci.Alternates = append(ci.Alternates, cn)
		}
	}
}

// Grabs the information for this client.
func (c *client) getClientInfo(detailed bool) *ClientInfo {
	if c == nil || (c.kind != CLIENT && c.kind != LEAF && c.kind != JETSTREAM && c.kind != ACCOUNT) {
		return nil
	}

	// Result
	var ci ClientInfo

	if detailed {
		c.addServerAndClusterInfo(&ci)
	}

	c.mu.Lock()
	// RTT and Account are always added.
	ci.Account = accForClient(c)
	ci.RTT = c.rtt
	// Detailed signals additional opt in.
	if detailed {
		ci.Start = &c.start
		ci.Host = c.host
		ci.ID = c.cid
		ci.Name = c.opts.Name
		ci.User = c.getRawAuthUser()
		ci.Lang = c.opts.Lang
		ci.Version = c.opts.Version
		ci.Jwt = c.opts.JWT
		ci.IssuerKey = issuerForClient(c)
		ci.NameTag = c.nameTag
		ci.Tags = c.tags
		ci.Kind = c.kindString()
		ci.ClientType = c.clientTypeString()
	}
	c.mu.Unlock()
	return &ci
}

func (c *client) doTLSServerHandshake(typ string, tlsConfig *tls.Config, timeout float64, pCerts PinnedCertSet) error {
	_, err := c.doTLSHandshake(typ, false, nil, tlsConfig, _EMPTY_, timeout, pCerts)
	return err
}

func (c *client) doTLSClientHandshake(typ string, url *url.URL, tlsConfig *tls.Config, tlsName string, timeout float64, pCerts PinnedCertSet) (bool, error) {
	return c.doTLSHandshake(typ, true, url, tlsConfig, tlsName, timeout, pCerts)
}

// Performs either server or client side (if solicit is true) TLS Handshake.
// On error, the TLS handshake error has been logged and the connection
// has been closed.
//
// Lock is held on entry.
func (c *client) doTLSHandshake(typ string, solicit bool, url *url.URL, tlsConfig *tls.Config, tlsName string, timeout float64, pCerts PinnedCertSet) (bool, error) {
	var host string
	var resetTLSName bool
	var err error

	// Capture kind for some debug/error statements.
	kind := c.kind

	// If we solicited, we will act like the client, otherwise the server.
	if solicit {
		c.Debugf("Starting TLS %s client handshake", typ)
		if tlsConfig.ServerName == _EMPTY_ {
			// If the given url is a hostname, use this hostname for the
			// ServerName. If it is an IP, use the cfg's tlsName. If none
			// is available, resort to current IP.
			host = url.Hostname()
			if tlsName != _EMPTY_ && net.ParseIP(host) != nil {
				host = tlsName
			}
			tlsConfig.ServerName = host
		}
		c.nc = tls.Client(c.nc, tlsConfig)
	} else {
		if kind == CLIENT {
			c.Debugf("Starting TLS client connection handshake")
		} else {
			c.Debugf("Starting TLS %s server handshake", typ)
		}
		c.nc = tls.Server(c.nc, tlsConfig)
	}

	conn := c.nc.(*tls.Conn)

	// Setup the timeout
	ttl := secondsToDuration(timeout)
	c.tlsTo = time.AfterFunc(ttl, func() { tlsTimeout(c, conn) })
	conn.SetReadDeadline(time.Now().Add(ttl))

	c.mu.Unlock()
	if err = conn.Handshake(); err != nil {
		if solicit {
			// Based on type of error, possibly clear the saved tlsName
			// See: https://github.com/nats-io/nats-server/issues/1256
			if _, ok := err.(x509.HostnameError); ok {
				if host == tlsName {
					resetTLSName = true
				}
			}
		}
	} else if !c.matchesPinnedCert(pCerts) {
		err = ErrCertNotPinned
	}

	if err != nil {
		if kind == CLIENT {
			c.Errorf("TLS handshake error: %v", err)
		} else {
			c.Errorf("TLS %s handshake error: %v", typ, err)
		}
		c.closeConnection(TLSHandshakeError)

		// Grab the lock before returning since the caller was holding the lock on entry
		c.mu.Lock()
		// Returning any error is fine. Since the connection is closed ErrConnectionClosed
		// is appropriate.
		return resetTLSName, ErrConnectionClosed
	}

	// Reset the read deadline
	conn.SetReadDeadline(time.Time{})

	// Re-Grab lock
	c.mu.Lock()

	// To be consistent with client, set this flag to indicate that handshake is done
	c.flags.set(handshakeComplete)

	// The connection still may have been closed on success handshake due
	// to a race with tls timeout. If that the case, return error indicating
	// that the connection is closed.
	if c.isClosed() {
		err = ErrConnectionClosed
	}

	return false, err
}

// getRAwAuthUser returns the raw auth user for the client.
// Lock should be held.
func (c *client) getRawAuthUser() string {
	switch {
	case c.opts.Nkey != "":
		return c.opts.Nkey
	case c.opts.Username != "":
		return c.opts.Username
	case c.opts.JWT != "":
		return c.pubKey
	case c.opts.Token != "":
		return c.opts.Token
	default:
		return ""
	}
}

// getAuthUser returns the auth user for the client.
// Lock should be held.
func (c *client) getAuthUser() string {
	switch {
	case c.opts.Nkey != "":
		return fmt.Sprintf("Nkey %q", c.opts.Nkey)
	case c.opts.Username != "":
		return fmt.Sprintf("User %q", c.opts.Username)
	case c.opts.JWT != "":
		return fmt.Sprintf("JWT User %q", c.pubKey)
	default:
		return `User "N/A"`
	}
}

// Given an array of strings, this function converts it to a map as long
// as all the content (converted to upper-case) matches some constants.

// Converts the given array of strings to a map of string.
// The strings are converted to upper-case and added to the map only
// if the server recognize them as valid connection types.
// If there are unknown connection types, the map of valid ones is returned
// along with an error that contains the name of the unknown.
func convertAllowedConnectionTypes(cts []string) (map[string]struct{}, error) {
	var unknown []string
	m := make(map[string]struct{}, len(cts))
	for _, i := range cts {
		i = strings.ToUpper(i)
		switch i {
		case jwt.ConnectionTypeStandard, jwt.ConnectionTypeWebsocket,
			jwt.ConnectionTypeLeafnode, jwt.ConnectionTypeLeafnodeWS,
			jwt.ConnectionTypeMqtt, jwt.ConnectionTypeMqttWS:
			m[i] = struct{}{}
		default:
			unknown = append(unknown, i)
		}
	}
	var err error
	// We will still return the map of valid ones.
	if len(unknown) != 0 {
		err = fmt.Errorf("invalid connection types %q", unknown)
	}
	return m, err
}

// This will return true if the connection is of a type present in the given `acts` map.
// Note that so far this is used only for CLIENT or LEAF connections.
// But a CLIENT can be standard or websocket (and other types in the future).
func (c *client) connectionTypeAllowed(acts map[string]struct{}) bool {
	// Empty means all type of clients are allowed
	if len(acts) == 0 {
		return true
	}
	var want string
	switch c.kind {
	case CLIENT:
		switch c.clientType() {
		case NATS:
			want = jwt.ConnectionTypeStandard
		case WS:
			want = jwt.ConnectionTypeWebsocket
		case MQTT:
			if c.isWebsocket() {
				want = jwt.ConnectionTypeMqttWS
			} else {
				want = jwt.ConnectionTypeMqtt
			}
		}
	case LEAF:
		if c.isWebsocket() {
			want = jwt.ConnectionTypeLeafnodeWS
		} else {
			want = jwt.ConnectionTypeLeafnode
		}
	}
	_, ok := acts[want]
	return ok
}

// isClosed returns true if either closeConnection or connMarkedClosed
// flag have been set, or if `nc` is nil, which may happen in tests.
func (c *client) isClosed() bool {
	return c.flags.isSet(closeConnection) || c.flags.isSet(connMarkedClosed) || c.nc == nil
}

// Logging functionality scoped to a client or route.
func (c *client) Error(err error) {
	c.srv.Errors(c, err)
}

func (c *client) Errorf(format string, v ...interface{}) {
	format = fmt.Sprintf("%s - %s", c, format)
	c.srv.Errorf(format, v...)
}

func (c *client) Debugf(format string, v ...interface{}) {
	format = fmt.Sprintf("%s - %s", c, format)
	c.srv.Debugf(format, v...)
}

func (c *client) Noticef(format string, v ...interface{}) {
	format = fmt.Sprintf("%s - %s", c, format)
	c.srv.Noticef(format, v...)
}

func (c *client) Tracef(format string, v ...interface{}) {
	format = fmt.Sprintf("%s - %s", c, format)
	c.srv.Tracef(format, v...)
}

func (c *client) Warnf(format string, v ...interface{}) {
	format = fmt.Sprintf("%s - %s", c, format)
	c.srv.Warnf(format, v...)
}

func (c *client) RateLimitWarnf(format string, v ...interface{}) {
	// Do the check before adding the client info to the format...
	statement := fmt.Sprintf(format, v...)
	if _, loaded := c.srv.rateLimitLogging.LoadOrStore(statement, time.Now()); loaded {
		return
	}
	c.Warnf("%s", statement)
}

// Set the very first PING to a lower interval to capture the initial RTT.
// After that the PING interval will be set to the user defined value.
// Client lock should be held.
func (c *client) setFirstPingTimer() {
	s := c.srv
	if s == nil {
		return
	}
	opts := s.getOpts()
	d := opts.PingInterval

	if !opts.DisableShortFirstPing {
		if c.kind != CLIENT {
			if d > firstPingInterval {
				d = firstPingInterval
			}
			if c.kind == GATEWAY {
				d = adjustPingIntervalForGateway(d)
			}
		} else if d > firstClientPingInterval {
			d = firstClientPingInterval
		}
	}
	// We randomize the first one by an offset up to 20%, e.g. 2m ~= max 24s.
	addDelay := rand.Int63n(int64(d / 5))
	d += time.Duration(addDelay)
	c.ping.tmr = time.AfterFunc(d, c.processPingTimer)
}
