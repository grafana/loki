package kfake

import (
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// TODO
//
// * Add raft and make the brokers independent
//
// * Support multiple replicas -- we just pass this through

type (

	// Cluster is a mock Kafka broker cluster.
	Cluster struct {
		cfg cfg

		controller *broker
		bs         []*broker

		coordinatorGen atomic.Uint64

		adminCh      chan func()
		reqCh        chan *clientReq
		wakeCh       chan *slept
		watchFetchCh chan *watchFetch

		controlMu      sync.Mutex
		control        map[int16]map[*controlCtx]struct{}
		currentBroker  *broker
		currentControl *controlCtx
		sleeping       map[*clientConn]*bsleep
		controlSleep   chan sleepChs

		data   data
		pids   pids
		groups groups
		sasls  sasls
		bcfgs  map[string]*string

		die  chan struct{}
		dead atomic.Bool
	}

	broker struct {
		c     *Cluster
		ln    net.Listener
		node  int32
		bsIdx int
	}

	controlFn func(kmsg.Request) (kmsg.Response, error, bool)

	controlCtx struct {
		key     int16
		fn      controlFn
		keep    bool
		drop    bool
		lastReq map[*clientConn]*clientReq // used to not re-run requests that slept, see doc comments below
	}

	controlResp struct {
		kresp   kmsg.Response
		err     error
		handled bool
	}
)

// MustCluster is like NewCluster, but panics on error.
func MustCluster(opts ...Opt) *Cluster {
	c, err := NewCluster(opts...)
	if err != nil {
		panic(err)
	}
	return c
}

// NewCluster returns a new mocked Kafka cluster.
func NewCluster(opts ...Opt) (*Cluster, error) {
	cfg := cfg{
		nbrokers:        3,
		logger:          new(nopLogger),
		clusterID:       "kfake",
		defaultNumParts: 10,

		minSessionTimeout: 6 * time.Second,
		maxSessionTimeout: 5 * time.Minute,

		sasls: make(map[struct{ m, u string }]string),
	}
	for _, opt := range opts {
		opt.apply(&cfg)
	}
	if len(cfg.ports) > 0 {
		cfg.nbrokers = len(cfg.ports)
	}

	c := &Cluster{
		cfg: cfg,

		adminCh:      make(chan func()),
		reqCh:        make(chan *clientReq, 20),
		wakeCh:       make(chan *slept, 10),
		watchFetchCh: make(chan *watchFetch, 20),
		control:      make(map[int16]map[*controlCtx]struct{}),
		controlSleep: make(chan sleepChs, 1),

		sleeping: make(map[*clientConn]*bsleep),

		data: data{
			id2t:      make(map[uuid]string),
			t2id:      make(map[string]uuid),
			treplicas: make(map[string]int),
			tcfgs:     make(map[string]map[string]*string),
		},
		bcfgs: make(map[string]*string),

		die: make(chan struct{}),
	}
	c.data.c = c
	c.groups.c = c
	var err error
	defer func() {
		if err != nil {
			c.Close()
		}
	}()

	for mu, p := range cfg.sasls {
		switch mu.m {
		case saslPlain:
			if c.sasls.plain == nil {
				c.sasls.plain = make(map[string]string)
			}
			c.sasls.plain[mu.u] = p
		case saslScram256:
			if c.sasls.scram256 == nil {
				c.sasls.scram256 = make(map[string]scramAuth)
			}
			c.sasls.scram256[mu.u] = newScramAuth(saslScram256, p)
		case saslScram512:
			if c.sasls.scram512 == nil {
				c.sasls.scram512 = make(map[string]scramAuth)
			}
			c.sasls.scram512[mu.u] = newScramAuth(saslScram512, p)
		default:
			return nil, fmt.Errorf("unknown SASL mechanism %v", mu.m)
		}
	}
	cfg.sasls = nil

	if cfg.enableSASL && c.sasls.empty() {
		c.sasls.scram256 = map[string]scramAuth{
			"admin": newScramAuth(saslScram256, "admin"),
		}
	}

	for i := 0; i < cfg.nbrokers; i++ {
		var port int
		if len(cfg.ports) > 0 {
			port = cfg.ports[i]
		}
		var ln net.Listener
		ln, err = newListener(port, c.cfg.tls)
		if err != nil {
			return nil, err
		}
		b := &broker{
			c:     c,
			ln:    ln,
			node:  int32(i),
			bsIdx: len(c.bs),
		}
		c.bs = append(c.bs, b)
		go b.listen()
	}
	c.controller = c.bs[len(c.bs)-1]
	go c.run()

	seedTopics := make(map[string]int32)
	for _, sts := range cfg.seedTopics {
		p := sts.p
		if p < 1 {
			p = int32(cfg.defaultNumParts)
		}
		for _, t := range sts.ts {
			seedTopics[t] = p
		}
	}
	for t, p := range seedTopics {
		c.data.mkt(t, int(p), -1, nil)
	}
	return c, nil
}

// ListenAddrs returns the hostports that the cluster is listening on.
func (c *Cluster) ListenAddrs() []string {
	var addrs []string
	c.admin(func() {
		for _, b := range c.bs {
			addrs = append(addrs, b.ln.Addr().String())
		}
	})
	return addrs
}

// Close shuts down the cluster.
func (c *Cluster) Close() {
	if c.dead.Swap(true) {
		return
	}
	close(c.die)
	for _, b := range c.bs {
		b.ln.Close()
	}
}

func newListener(port int, tc *tls.Config) (net.Listener, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	if tc != nil {
		l = tls.NewListener(l, tc)
	}
	return l, nil
}

func (b *broker) listen() {
	defer b.ln.Close()
	for {
		conn, err := b.ln.Accept()
		if err != nil {
			return
		}

		cc := &clientConn{
			c:      b.c,
			b:      b,
			conn:   conn,
			respCh: make(chan clientResp, 2),
		}
		go cc.read()
		go cc.write()
	}
}

func (c *Cluster) run() {
outer:
	for {
		var (
			creq    *clientReq
			w       *watchFetch
			s       *slept
			kreq    kmsg.Request
			kresp   kmsg.Response
			err     error
			handled bool
		)

		select {
		case <-c.die:
			return

		case admin := <-c.adminCh:
			admin()
			continue

		case creq = <-c.reqCh:
			if c.cfg.sleepOutOfOrder {
				break
			}
			// If we have any sleeping request on this node,
			// we enqueue the new live request to the end and
			// wait for the sleeping request to finish.
			bs := c.sleeping[creq.cc]
			if bs.enqueue(&slept{
				creq:    creq,
				waiting: true,
			}) {
				continue
			}

		case s = <-c.wakeCh:
			// On wakeup, we know we are handling a control
			// function that was slept, or a request that was
			// waiting for a control function to finish sleeping.
			creq = s.creq
			if s.waiting {
				break
			}

			// We continue a previously sleeping request, and
			// handle results similar to tryControl.
			//
			// Control flow is weird here, but is described more
			// fully in the finish/resleep/etc methods.
			c.continueSleptControl(s)
		inner:
			for {
				select {
				case admin := <-c.adminCh:
					admin()
					continue inner
				case res := <-s.res:
					c.finishSleptControl(s)
					cctx := s.cctx
					s = nil
					kresp, err, handled = res.kresp, res.err, res.handled
					c.maybePopControl(handled, cctx)
					if handled {
						goto afterControl
					}
					break inner
				case sleepChs := <-c.controlSleep:
					c.resleepSleptControl(s, sleepChs)
					continue outer
				}
			}

		case w = <-c.watchFetchCh:
			if w.cleaned {
				continue // already cleaned up, this is an extraneous timer fire
			}
			w.cleanup(c)
			creq = w.creq
		}

		kresp, err, handled = c.tryControl(creq)
		if handled {
			goto afterControl
		}

		if c.cfg.enableSASL {
			if allow := c.handleSASL(creq); !allow {
				err = errors.New("not allowed given SASL state")
				goto afterControl
			}
		}

		kreq = creq.kreq
		switch k := kmsg.Key(kreq.Key()); k {
		case kmsg.Produce:
			kresp, err = c.handleProduce(creq.cc.b, kreq)
		case kmsg.Fetch:
			kresp, err = c.handleFetch(creq, w)
		case kmsg.ListOffsets:
			kresp, err = c.handleListOffsets(creq.cc.b, kreq)
		case kmsg.Metadata:
			kresp, err = c.handleMetadata(kreq)
		case kmsg.OffsetCommit:
			kresp, err = c.handleOffsetCommit(creq)
		case kmsg.OffsetFetch:
			kresp, err = c.handleOffsetFetch(creq)
		case kmsg.FindCoordinator:
			kresp, err = c.handleFindCoordinator(kreq)
		case kmsg.JoinGroup:
			kresp, err = c.handleJoinGroup(creq)
		case kmsg.Heartbeat:
			kresp, err = c.handleHeartbeat(creq)
		case kmsg.LeaveGroup:
			kresp, err = c.handleLeaveGroup(creq)
		case kmsg.SyncGroup:
			kresp, err = c.handleSyncGroup(creq)
		case kmsg.DescribeGroups:
			kresp, err = c.handleDescribeGroups(creq)
		case kmsg.ListGroups:
			kresp, err = c.handleListGroups(creq)
		case kmsg.SASLHandshake:
			kresp, err = c.handleSASLHandshake(creq)
		case kmsg.ApiVersions:
			kresp, err = c.handleApiVersions(kreq)
		case kmsg.CreateTopics:
			kresp, err = c.handleCreateTopics(creq.cc.b, kreq)
		case kmsg.DeleteTopics:
			kresp, err = c.handleDeleteTopics(creq.cc.b, kreq)
		case kmsg.DeleteRecords:
			kresp, err = c.handleDeleteRecords(creq.cc.b, kreq)
		case kmsg.InitProducerID:
			kresp, err = c.handleInitProducerID(kreq)
		case kmsg.OffsetForLeaderEpoch:
			kresp, err = c.handleOffsetForLeaderEpoch(creq.cc.b, kreq)
		case kmsg.DescribeConfigs:
			kresp, err = c.handleDescribeConfigs(creq.cc.b, kreq)
		case kmsg.AlterConfigs:
			kresp, err = c.handleAlterConfigs(creq.cc.b, kreq)
		case kmsg.AlterReplicaLogDirs:
			kresp, err = c.handleAlterReplicaLogDirs(creq.cc.b, kreq)
		case kmsg.DescribeLogDirs:
			kresp, err = c.handleDescribeLogDirs(creq.cc.b, kreq)
		case kmsg.SASLAuthenticate:
			kresp, err = c.handleSASLAuthenticate(creq)
		case kmsg.CreatePartitions:
			kresp, err = c.handleCreatePartitions(creq.cc.b, kreq)
		case kmsg.DeleteGroups:
			kresp, err = c.handleDeleteGroups(creq)
		case kmsg.IncrementalAlterConfigs:
			kresp, err = c.handleIncrementalAlterConfigs(creq.cc.b, kreq)
		case kmsg.OffsetDelete:
			kresp, err = c.handleOffsetDelete(creq)
		case kmsg.DescribeUserSCRAMCredentials:
			kresp, err = c.handleDescribeUserSCRAMCredentials(kreq)
		case kmsg.AlterUserSCRAMCredentials:
			kresp, err = c.handleAlterUserSCRAMCredentials(creq.cc.b, kreq)
		default:
			err = fmt.Errorf("unhandled key %v", k)
		}

	afterControl:
		// If s is non-nil, this is either a previously slept control
		// that finished but was not handled, or a previously slept
		// waiting request. In either case, we need to signal to the
		// sleep dequeue loop to continue.
		if s != nil {
			s.continueDequeue <- struct{}{}
		}
		if kresp == nil && err == nil { // produce request with no acks, or otherwise hijacked request (group, sleep)
			continue
		}

		select {
		case creq.cc.respCh <- clientResp{kresp: kresp, corr: creq.corr, err: err, seq: creq.seq}:
		case <-c.die:
			return
		}
	}
}

// Control is a function to call on any client request the cluster handles.
//
// If the control function returns true, then either the response is written
// back to the client or, if there the control function returns an error, the
// client connection is closed. If both returns are nil, then the cluster will
// loop continuing to read from the client and the client will likely have a
// read timeout at some point.
//
// Controlling a request drops the control function from the cluster, meaning
// that a control function can only control *one* request. To keep the control
// function handling more requests, you can call KeepControl within your
// control function. Alternatively, if you want to just run some logic in your
// control function but then have the cluster handle the request as normal,
// you can call DropControl to drop a control function that was not handled.
//
// It is safe to add new control functions within a control function.
//
// Control functions are run serially unless you use SleepControl, multiple
// control functions are "in progress", and you run Cluster.Close. Closing a
// Cluster awakens all sleeping control functions.
func (c *Cluster) Control(fn func(kmsg.Request) (kmsg.Response, error, bool)) {
	c.ControlKey(-1, fn)
}

// Control is a function to call on a specific request key that the cluster
// handles.
//
// If the control function returns true, then either the response is written
// back to the client or, if there the control function returns an error, the
// client connection is closed. If both returns are nil, then the cluster will
// loop continuing to read from the client and the client will likely have a
// read timeout at some point.
//
// Controlling a request drops the control function from the cluster, meaning
// that a control function can only control *one* request. To keep the control
// function handling more requests, you can call KeepControl within your
// control function. Alternatively, if you want to just run some logic in your
// control function but then have the cluster handle the request as normal,
// you can call DropControl to drop a control function that was not handled.
//
// It is safe to add new control functions within a control function.
//
// Control functions are run serially unless you use SleepControl, multiple
// control functions are "in progress", and you run Cluster.Close. Closing a
// Cluster awakens all sleeping control functions.
func (c *Cluster) ControlKey(key int16, fn func(kmsg.Request) (kmsg.Response, error, bool)) {
	c.controlMu.Lock()
	defer c.controlMu.Unlock()
	m := c.control[key]
	if m == nil {
		m = make(map[*controlCtx]struct{})
		c.control[key] = m
	}
	m[&controlCtx{
		key:     key,
		fn:      fn,
		lastReq: make(map[*clientConn]*clientReq),
	}] = struct{}{}
}

// KeepControl marks the currently running control function to be kept even if
// you handle the request and return true. This can be used to continuously
// control requests without needing to re-add control functions manually.
func (c *Cluster) KeepControl() {
	c.controlMu.Lock()
	defer c.controlMu.Unlock()
	if c.currentControl != nil {
		c.currentControl.keep = true
	}
}

// DropControl allows you to drop the current control function. This takes
// precedence over KeepControl. The use of this function is you can run custom
// control logic *once*, drop the control function, and return that the
// function was not handled -- thus allowing other control functions to run, or
// allowing the kfake cluster to process the request as normal.
func (c *Cluster) DropControl() {
	c.controlMu.Lock()
	defer c.controlMu.Unlock()
	if c.currentControl != nil {
		c.currentControl.drop = true
	}
}

// SleepControl sleeps the current control function until wakeup returns. This
// yields to run any other connection.
//
// Note that per protocol, requests on the same connection must be replied to
// in order. Many clients write multiple requests to the same connection, so
// if you sleep until a different request runs, you may sleep forever -- you
// must know the semantics of your client to know whether requests run on
// different connections (or, ensure you are writing to different brokers).
//
// For example, franz-go uses a dedicated connection for:
//   - produce requests
//   - fetch requests
//   - join&sync requests
//   - requests with a Timeout field
//   - all other request
//
// So, for franz-go, there are up to five separate connections depending
// on what you are doing.
//
// You can run SleepControl multiple times in the same control function. If you
// sleep a request you are controlling, and another request of the same key
// comes in, it will run the same control function and may also sleep (i.e.,
// you must have logic if you want to avoid sleeping on the same request).
func (c *Cluster) SleepControl(wakeup func()) {
	c.controlMu.Lock()
	if c.currentControl == nil {
		c.controlMu.Unlock()
		return
	}
	c.controlMu.Unlock()

	sleepChs := sleepChs{
		clientWait: make(chan struct{}, 1),
		clientCont: make(chan struct{}, 1),
	}
	go func() {
		wakeup()
		sleepChs.clientWait <- struct{}{}
	}()

	c.controlSleep <- sleepChs
	select {
	case <-sleepChs.clientCont:
	case <-c.die:
	}
}

// CurrentNode is solely valid from within a control function; it returns
// the broker id that the request was received by.
// If there's no request currently inflight, this returns -1.
func (c *Cluster) CurrentNode() int32 {
	c.controlMu.Lock()
	defer c.controlMu.Unlock()
	if b := c.currentBroker; b != nil {
		return b.node
	}
	return -1
}

func (c *Cluster) tryControl(creq *clientReq) (kresp kmsg.Response, err error, handled bool) {
	c.controlMu.Lock()
	defer c.controlMu.Unlock()
	if len(c.control) == 0 {
		return nil, nil, false
	}
	kresp, err, handled = c.tryControlKey(creq.kreq.Key(), creq)
	if !handled {
		kresp, err, handled = c.tryControlKey(-1, creq)
	}
	return kresp, err, handled
}

func (c *Cluster) tryControlKey(key int16, creq *clientReq) (kmsg.Response, error, bool) {
	for cctx := range c.control[key] {
		if cctx.lastReq[creq.cc] == creq {
			continue
		}
		cctx.lastReq[creq.cc] = creq
		res := c.runControl(cctx, creq)
		for {
			select {
			case admin := <-c.adminCh:
				admin()
				continue
			case res := <-res:
				c.maybePopControl(res.handled, cctx)
				return res.kresp, res.err, res.handled
			case sleepChs := <-c.controlSleep:
				c.beginSleptControl(&slept{
					cctx:     cctx,
					sleepChs: sleepChs,
					res:      res,
					creq:     creq,
				})
				return nil, nil, true
			}
		}
	}
	return nil, nil, false
}

func (c *Cluster) runControl(cctx *controlCtx, creq *clientReq) chan controlResp {
	res := make(chan controlResp, 1)
	c.currentBroker = creq.cc.b
	c.currentControl = cctx
	// We unlock before entering a control function so that the control
	// function can modify / add more control. We re-lock when exiting the
	// control function. This does pose some weird control flow issues
	// w.r.t. sleeping requests. Here, we have to re-lock before sending
	// down res, otherwise we risk unlocking an unlocked mu in
	// finishSleepControl.
	c.controlMu.Unlock()
	go func() {
		kresp, err, handled := cctx.fn(creq.kreq)
		c.controlMu.Lock()
		c.currentControl = nil
		c.currentBroker = nil
		res <- controlResp{kresp, err, handled}
	}()
	return res
}

func (c *Cluster) beginSleptControl(s *slept) {
	// Control flow gets really weird here. We unlocked when entering the
	// control function, so we have to re-lock now so that tryControl can
	// unlock us safely.
	bs := c.sleeping[s.creq.cc]
	if bs == nil {
		bs = &bsleep{
			c:       c,
			set:     make(map[*slept]struct{}),
			setWake: make(chan *slept, 1),
		}
		c.sleeping[s.creq.cc] = bs
	}
	bs.enqueue(s)
	c.controlMu.Lock()
	c.currentControl = nil
	c.currentBroker = nil
}

func (c *Cluster) continueSleptControl(s *slept) {
	// When continuing a slept control, we are in the main run loop and are
	// not currently under the control mu. We need to re-set the current
	// broker and current control before resuming.
	c.controlMu.Lock()
	c.currentBroker = s.creq.cc.b
	c.currentControl = s.cctx
	c.controlMu.Unlock()
	s.sleepChs.clientCont <- struct{}{}
}

func (c *Cluster) finishSleptControl(s *slept) {
	// When finishing a slept control, the control function exited and
	// grabbed the control mu. We clear the control, unlock, and allow the
	// slept control to be dequeued.
	c.currentControl = nil
	c.currentBroker = nil
	c.controlMu.Unlock()
	s.continueDequeue <- struct{}{}
}

func (c *Cluster) resleepSleptControl(s *slept, sleepChs sleepChs) {
	// A control function previously slept and is now again sleeping. We
	// need to clear the control broker / etc, update the sleep channels,
	// and allow the sleep dequeueing to continue. The control function
	// will not be deqeueued in the loop because we updated sleepChs with
	// a non-nil clientWait.
	c.controlMu.Lock()
	c.currentBroker = nil
	c.currentControl = nil
	c.controlMu.Unlock()
	s.sleepChs = sleepChs
	s.continueDequeue <- struct{}{}
	// For OOO requests, we need to manually trigger a goroutine to
	// watch for the sleep to end.
	s.bs.maybeWaitOOOWake(s)
}

func (c *Cluster) maybePopControl(handled bool, cctx *controlCtx) {
	if handled && !cctx.keep || cctx.drop {
		delete(c.control[cctx.key], cctx)
	}
}

// bsleep manages sleeping requests on a connection to a broker, or
// non-sleeping requests that are waiting for sleeping requests to finish.
type bsleep struct {
	c       *Cluster
	mu      sync.Mutex
	queue   []*slept
	set     map[*slept]struct{}
	setWake chan *slept
}

type slept struct {
	bs       *bsleep
	cctx     *controlCtx
	sleepChs sleepChs
	res      <-chan controlResp
	creq     *clientReq
	waiting  bool

	continueDequeue chan struct{}
}

type sleepChs struct {
	clientWait chan struct{}
	clientCont chan struct{}
}

// enqueue has a few potential behaviors.
//
// (1) If s is waiting, this is a new request enqueueing to the back of an
// existing queue, where we are waiting for the head request to finish
// sleeping. Easy case.
//
// (2) If s is not waiting, this is a sleeping request. If the queue is empty,
// this is the first sleeping request on a node. We enqueue and start our wait
// goroutine. Easy.
//
// (3) If s is not waiting, but our queue is non-empty, this must be from a
// convoluted scenario:
//
//	(a) the user has SleepOutOfOrder configured,
//	(b) or, there was a request in front of us that slept, we were waiting,
//	    and now we ourselves are sleeping
//	(c) or, we are sleeping for the second time in a single control
func (bs *bsleep) enqueue(s *slept) bool {
	if bs == nil {
		return false // Do not enqueue, nothing sleeping
	}
	s.continueDequeue = make(chan struct{}, 1)
	s.bs = bs
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if s.waiting {
		if bs.c.cfg.sleepOutOfOrder {
			panic("enqueueing a waiting request even though we are sleeping out of order")
		}
		if !bs.empty() {
			bs.keep(s) // Case (1)
			return true
		}
		return false // We do not enqueue, do not wait: nothing sleeping ahead of us
	}
	if bs.empty() {
		bs.keep(s)
		go bs.wait() // Case (2)
		return true
	}
	var q0 *slept
	if !bs.c.cfg.sleepOutOfOrder {
		q0 = bs.queue[0] // Case (3b) or (3c) -- just update values below
	} else {
		// Case (3a), out of order sleep: we need to check the entire
		// queue to see if this request was already sleeping and, if
		// so, update the values. If it was not already sleeping, we
		// "keep" the new sleeping item.
		bs.keep(s)
		return true
	}
	if q0.creq != s.creq {
		panic("internal error: sleeping request not head request")
	}
	// We do not update continueDequeue because it is actively being read,
	// we just reuse the old value.
	q0.cctx = s.cctx
	q0.sleepChs = s.sleepChs
	q0.res = s.res
	q0.waiting = s.waiting
	return true
}

// keep stores a sleeping request to be managed. For out of order control, the
// log is a bit more complicated and we need to watch for the control sleep
// finishing here, and forward the "I'm done sleeping" notification to waitSet.
func (bs *bsleep) keep(s *slept) {
	if !bs.c.cfg.sleepOutOfOrder {
		bs.queue = append(bs.queue, s)
		return
	}
	bs.set[s] = struct{}{}
	bs.maybeWaitOOOWake(s)
}

func (bs *bsleep) maybeWaitOOOWake(s *slept) {
	if !bs.c.cfg.sleepOutOfOrder {
		return
	}
	go func() {
		select {
		case <-bs.c.die:
		case <-s.sleepChs.clientWait:
			select {
			case <-bs.c.die:
			case bs.setWake <- s:
			}
		}
	}()
}

func (bs *bsleep) empty() bool {
	return len(bs.queue) == 0 && len(bs.set) == 0
}

func (bs *bsleep) wait() {
	if bs.c.cfg.sleepOutOfOrder {
		bs.waitSet()
	} else {
		bs.waitQueue()
	}
}

// For out of order control, all control functions run concurrently, serially.
// Whenever they wake up, they send themselves down setWake. waitSet manages
// handling the wake up and interacting with the serial manage goroutine to
// run everything properly.
func (bs *bsleep) waitSet() {
	for {
		bs.mu.Lock()
		if len(bs.set) == 0 {
			bs.mu.Unlock()
			return
		}
		bs.mu.Unlock()

		// Wait for a control function to awaken.
		var q *slept
		select {
		case <-bs.c.die:
			return
		case q = <-bs.setWake:
			q.sleepChs.clientWait = nil
		}

		// Now, schedule ourselves with the run loop.
		select {
		case <-bs.c.die:
			return
		case bs.c.wakeCh <- q:
		}

		// Wait for this control function to finish its loop in the run
		// function. Once it does, if clientWait is non-nil, the
		// control function went back to sleep. If it is nil, the
		// control function is done and we remove this from tracking.
		select {
		case <-bs.c.die:
			return
		case <-q.continueDequeue:
		}
		if q.sleepChs.clientWait == nil {
			bs.mu.Lock()
			delete(bs.set, q)
			bs.mu.Unlock()
		}
	}
}

// For in-order control functions, the concept is slightly simpler but the
// logic flow is the same. We wait for the head control function to wake up,
// try to run it, and then wait for it to finish. The logic of this function is
// the same as waitSet, minus the middle part where we wait for something to
// wake up.
func (bs *bsleep) waitQueue() {
	for {
		bs.mu.Lock()
		if len(bs.queue) == 0 {
			bs.mu.Unlock()
			return
		}
		q0 := bs.queue[0]
		bs.mu.Unlock()

		if q0.sleepChs.clientWait != nil {
			select {
			case <-bs.c.die:
				return
			case <-q0.sleepChs.clientWait:
				q0.sleepChs.clientWait = nil
			}
		}

		select {
		case <-bs.c.die:
			return
		case bs.c.wakeCh <- q0:
		}

		select {
		case <-bs.c.die:
			return
		case <-q0.continueDequeue:
		}
		if q0.sleepChs.clientWait == nil {
			bs.mu.Lock()
			bs.queue = bs.queue[1:]
			bs.mu.Unlock()
		}
	}
}

// Various administrative requests can be passed into the cluster to simulate
// real-world operations. These are performed synchronously in the goroutine
// that handles client requests.

func (c *Cluster) admin(fn func()) {
	ofn := fn
	wait := make(chan struct{})
	fn = func() { ofn(); close(wait) }
	c.adminCh <- fn
	<-wait
}

// MoveTopicPartition simulates the rebalancing of a partition to an alternative
// broker. This returns an error if the topic, partition, or node does not exit.
func (c *Cluster) MoveTopicPartition(topic string, partition int32, nodeID int32) error {
	var err error
	c.admin(func() {
		var br *broker
		for _, b := range c.bs {
			if b.node == nodeID {
				br = b
				break
			}
		}
		if br == nil {
			err = fmt.Errorf("node %d not found", nodeID)
			return
		}
		pd, ok := c.data.tps.getp(topic, partition)
		if !ok {
			err = errors.New("topic/partition not found")
			return
		}
		pd.leader = br
		pd.epoch++
	})
	return err
}

// CoordinatorFor returns the node ID of the group or transaction coordinator
// for the given key.
func (c *Cluster) CoordinatorFor(key string) int32 {
	var n int32
	c.admin(func() {
		l := len(c.bs)
		if l == 0 {
			n = -1
			return
		}
		n = c.coordinator(key).node
	})
	return n
}

// RehashCoordinators simulates group and transacational ID coordinators moving
// around. All group and transactional IDs are rekeyed. This forces clients to
// reload coordinators.
func (c *Cluster) RehashCoordinators() {
	c.coordinatorGen.Add(1)
}

// AddNode adds a node to the cluster. If nodeID is -1, the next node ID is
// used. If port is 0 or negative, a random port is chosen. This returns the
// added node ID and the port used, or an error if the node already exists or
// the port cannot be listened to.
func (c *Cluster) AddNode(nodeID int32, port int) (int32, int, error) {
	var err error
	c.admin(func() {
		if nodeID >= 0 {
			for _, b := range c.bs {
				if b.node == nodeID {
					err = fmt.Errorf("node %d already exists", nodeID)
					return
				}
			}
		} else if len(c.bs) > 0 {
			// We go one higher than the max current node ID. We
			// need to search all nodes because a person may have
			// added and removed a bunch, with manual ID overrides.
			nodeID = c.bs[0].node
			for _, b := range c.bs[1:] {
				if b.node > nodeID {
					nodeID = b.node
				}
			}
			nodeID++
		} else {
			nodeID = 0
		}
		if port < 0 {
			port = 0
		}
		var ln net.Listener
		if ln, err = newListener(port, c.cfg.tls); err != nil {
			return
		}
		_, strPort, _ := net.SplitHostPort(ln.Addr().String())
		port, _ = strconv.Atoi(strPort)
		b := &broker{
			c:     c,
			ln:    ln,
			node:  nodeID,
			bsIdx: len(c.bs),
		}
		c.bs = append(c.bs, b)
		c.cfg.nbrokers++
		c.shufflePartitionsLocked()
		go b.listen()
	})
	return nodeID, port, err
}

// RemoveNode removes a ndoe from the cluster. This returns an error if the
// node does not exist.
func (c *Cluster) RemoveNode(nodeID int32) error {
	var err error
	c.admin(func() {
		for i, b := range c.bs {
			if b.node == nodeID {
				if len(c.bs) == 1 {
					err = errors.New("cannot remove all brokers")
					return
				}
				b.ln.Close()
				c.cfg.nbrokers--
				c.bs[i] = c.bs[len(c.bs)-1]
				c.bs[i].bsIdx = i
				c.bs = c.bs[:len(c.bs)-1]
				c.shufflePartitionsLocked()
				return
			}
		}
		err = fmt.Errorf("node %d not found", nodeID)
	})
	return err
}

// ShufflePartitionLeaders simulates a leader election for all partitions: all
// partitions have a randomly selected new leader and their internal epochs are
// bumped.
func (c *Cluster) ShufflePartitionLeaders() {
	c.admin(func() {
		c.shufflePartitionsLocked()
	})
}

func (c *Cluster) shufflePartitionsLocked() {
	c.data.tps.each(func(_ string, _ int32, p *partData) {
		var leader *broker
		if len(c.bs) == 0 {
			leader = c.noLeader()
		} else {
			leader = c.bs[rand.Intn(len(c.bs))]
		}
		p.leader = leader
		p.epoch++
	})
}
