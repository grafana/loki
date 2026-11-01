// Package kfake provides a fake Kafka broker for testing.
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
		control        map[int16][]*controlCtx
		currentBroker  *broker
		currentControl *controlCtx
		sleeping       map[*clientConn]*bsleep
		controlSleep   chan sleepChs

		data               data
		pids               pids
		groups             groups
		sasls              sasls
		acls               clusterACLs
		bcfgs              atomic.Pointer[map[string]*string]
		quotas             map[string]quotaEntry
		telem              map[[16]byte]int32
		telemNextID        int32
		features           map[string]int16 // KIP-584 finalized feature levels, see 18_api_versions.go
		fetchSessions      fetchSessions
		groupConfigs       map[string]map[string]*string // group -> config key -> config value
		shareGroups        shareGroups
		compactTicker      *time.Ticker
		offsetExpireTicker *time.Ticker

		// storageDir is the root directory for segment and index files.
		// Always set: "/kfake" for in-memory (memFS) or the user's
		// DataDir path for on-disk persistence. State files (topics.json,
		// groups.log, pids.log, etc.) only write when persist().
		storageDir         string
		fs                 fs
		groupsLogMu        sync.Mutex
		groupsLogFile      file
		pidsLogFile        file
		groupsLogSize      atomic.Int64
		pidsLogSize        atomic.Int64
		needsGroupsCompact atomic.Bool

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

// persist returns true if the cluster is configured with a DataDir
// for on-disk state persistence. Segment I/O always happens (via storageDir),
// but state files (topics.json, groups.log, etc.) are only written when
// persistence is enabled.
func (c *Cluster) persist() bool { return c.cfg.dataDir != "" }

func (b *broker) hostport() (string, int32) {
	h, p, _ := net.SplitHostPort(b.ln.Addr().String())
	p32, _ := strconv.Atoi(p)
	return h, int32(p32)
}

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
		listenFn:        net.Listen,

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
	if cfg.injectFS != nil && cfg.dataDir == "" {
		cfg.dataDir = "/kfake"
	}

	c := &Cluster{
		cfg: cfg,

		adminCh:      make(chan func()),
		reqCh:        make(chan *clientReq, 20),
		wakeCh:       make(chan *slept, 10),
		watchFetchCh: make(chan *watchFetch, 20),
		control:      make(map[int16][]*controlCtx),
		controlSleep: make(chan sleepChs, 1),

		sleeping: make(map[*clientConn]*bsleep),

		data: data{
			id2t:      make(map[uuid]string),
			t2id:      make(map[string]uuid),
			treplicas: make(map[string]int),
			tcfgs:     make(map[string]map[string]*string),
			tnorms:    make(map[string]string),
		},
		// bcfgs initialized below via storeBcfgs
		quotas:   make(map[string]quotaEntry),
		telem:    make(map[[16]byte]int32),
		features: defaultFinalizedFeatures(),

		die: make(chan struct{}),
	}
	if cfg.injectFS != nil {
		c.fs = cfg.injectFS
		c.storageDir = cfg.dataDir
	} else if cfg.dataDir != "" {
		c.fs = osFS{}
		c.storageDir = cfg.dataDir
	} else {
		c.fs = newMemFS()
		c.storageDir = "/kfake"
	}
	{
		m := make(map[string]*string, len(cfg.brokerConfigs))
		for k, v := range cfg.brokerConfigs {
			if v == "" {
				m[k] = nil
			} else {
				v := v
				m[k] = &v
			}
		}
		c.storeBcfgs(m)
	}
	c.data.c = c
	c.groups.c = c
	c.shareGroups.c = c
	c.shareGroups.gs = make(map[string]*shareGroup)
	c.shareGroups.sweepCh = make(chan *shareGroup, 16)
	c.shareGroups.sessions = make(map[shareSessionKey]*shareSession)
	c.shareGroups.connWatch = make(map[*clientConn]struct{})
	c.shareGroups.disconnCh = make(chan *clientConn, 16)
	c.shareGroups.watchFetchCh = make(chan *watchShareFetch, 16)
	c.pids.c = c
	c.pids.ids = make(map[int64]*pidinfo)
	c.pids.byTxid = make(map[string]*pidinfo)
	c.pids.txs = make(map[*pidinfo]struct{})
	c.pids.txTimer = time.NewTimer(0)
	<-c.pids.txTimer.C
	var err error
	defer func() {
		if err != nil {
			for _, b := range c.bs {
				b.ln.Close()
			}
			c.closeOpenFiles()
			close(c.die)
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
		ln, err = newListener(port, c.cfg.tls, c.cfg.listenFn)
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
		defer func() { go b.listen() }() //nolint:gocritic,revive // intentional - each broker's listener starts after loop iter
	}
	c.controller = c.bs[len(c.bs)-1]

	// Try to load persisted state from disk (after brokers are created,
	// since partition leader assignment needs c.bs)
	var loaded bool
	if c.persist() {
		loaded, err = c.loadFromDisk()
		if err != nil {
			return nil, fmt.Errorf("loading persisted state: %w", err)
		}
	}

	if !loaded {
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

		for _, a := range cfg.seedACLs {
			c.acls.add(a)
		}

		// Persist the initial state so crash recovery can find
		// meta.json, topics.json, etc.
		if c.persist() {
			if err = c.saveToDisk(); err != nil {
				return nil, fmt.Errorf("persisting initial state: %w", err)
			}
		}
	}

	go c.run()

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

	// Shutdown sequence:
	//  1. dead=true rejects duplicate Close calls (above).
	//  2. Close listeners to stop accepting new connections.
	//  3. If persistence is enabled, send a shutdown function
	//     through adminCh. This runs single-threaded inside
	//     run(), ensuring no concurrent state mutations during
	//     save. The shutdown function closes c.die, causing
	//     run() to exit immediately after.
	for _, b := range c.bs {
		b.ln.Close()
	}

	if c.persist() {
		// Send through adminCh so it runs single-threaded.
		// The send must block - a non-blocking default would
		// race with run() handling a request.
		done := make(chan struct{})
		c.adminCh <- func() {
			c.drainReqChForShutdown()
			if err := c.saveToDisk(); err != nil {
				c.cfg.logger.Logf(LogLevelError, "persist to disk: %v", err)
			}
			if err := c.saveSessionState(); err != nil {
				c.cfg.logger.Logf(LogLevelError, "save session state: %v", err)
			}
			c.closeOpenFiles()
			// Close c.die inside the admin function so run()
			// exits immediately - prevents processing requests
			// after saveToDisk which would create state not
			// captured by the saved seq windows.
			close(c.die)
			close(done)
		}
		<-done
		return
	}

	close(c.die)
}

// drainReqChForShutdown processes any pending OffsetCommit requests in
// c.reqCh, dispatching them to the appropriate group goroutines. This
// is called at the start of the shutdown admin function, before
// saveToDisk. Without this, Go's select in run() may pick adminCh over
// reqCh, causing committed offsets to be lost across restarts.
//
// TxnOffsetCommitRequest is not handled here: transactional offset
// staging is processed inline in run() (not dispatched to a group
// goroutine), so any in-flight TxnOffsetCommit simply fails on the
// client side and the client must abort/retry the transaction.
func (c *Cluster) drainReqChForShutdown() {
	for {
		select {
		case creq := <-c.reqCh:
			if _, ok := creq.kreq.(*kmsg.OffsetCommitRequest); ok {
				c.groups.handleOffsetCommit(creq)
			}
		default:
			return
		}
	}
}

func newListener(port int, tc *tls.Config, fn func(network, address string) (net.Listener, error)) (net.Listener, error) {
	l, err := fn("tcp", fmt.Sprintf("127.0.0.1:%d", port))
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

		mute := make(chan bool, 1)
		mute <- true // first request proceeds immediately
		cc := &clientConn{
			c:      b.c,
			b:      b,
			conn:   conn,
			respCh: make(chan clientResp, 2),
			done:   make(chan struct{}),
			mute:   mute,
		}
		go cc.read()
		go cc.write()
	}
}

func (c *Cluster) run() {
	defer func() {
		c.pids.txTimer.Stop()
		if c.compactTicker != nil {
			c.compactTicker.Stop()
		}
		c.offsetExpireTicker.Stop()
	}()
	c.offsetExpireTicker = time.NewTicker(time.Duration(c.offsetsRetentionCheckIntervalMs()) * time.Millisecond)
outer:
	for {
		var (
			creq    *clientReq
			w       *watchFetch
			wsf     *watchShareFetch
			s       *slept
			kreq    kmsg.Request
			kresp   kmsg.Response
			err     error
			handled bool
		)

		// Drain ready watchers before the main select so that
		// completed long-polls are dispatched promptly. Under
		// heavy parallel load (many tests with -race), the
		// main select's random pick can starve watchers in
		// favor of reqCh, causing fetch timeouts.
		for {
			select {
			case w = <-c.watchFetchCh:
				if w.cleaned {
					w = nil
					continue
				}
				w.cleanup()
				creq = w.creq
			case wsf = <-c.shareGroups.watchFetchCh:
				if wsf.cleaned {
					wsf = nil
					continue
				}
				wsf.cleanup()
				creq = wsf.creq
			default:
				goto mainSelect
			}
			break
		}
		goto handleReq

	mainSelect:
		select {
		case <-c.die:
			return

		case <-c.pids.txTimer.C:
			c.pids.handleTimeout()
			continue

		case <-c.compactTickerC():
			c.compactAll()
			continue

		case <-c.offsetExpireTicker.C:
			c.expireGroupOffsets()
			continue

		case sg := <-c.shareGroups.sweepCh:
			sg.fireAllShareWatchers()
			continue

		case cc := <-c.shareGroups.disconnCh:
			delete(c.shareGroups.connWatch, cc)
			c.shareGroups.cleanupSessionsForConn(cc)
			continue

		case admin := <-c.adminCh:
			admin()
			// If the admin function closed c.die (shutdown persist),
			// exit immediately. We can't rely on the outer select
			// because Go's select is non-deterministic - it could
			// pick reqCh over c.die even when both are ready.
			select {
			case <-c.die:
				return
			default:
			}
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
			w.cleanup()
			creq = w.creq

		case wsf = <-c.shareGroups.watchFetchCh:
			if wsf.cleaned {
				continue
			}
			wsf.cleanup()
			creq = wsf.creq
		}

	handleReq:
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
			kresp, err = c.handleProduce(creq)
		case kmsg.Fetch:
			kresp, err = c.handleFetch(creq, w)
		case kmsg.ListOffsets:
			kresp, err = c.handleListOffsets(creq)
		case kmsg.Metadata:
			kresp, err = c.handleMetadata(creq)
		case kmsg.OffsetCommit:
			kresp, err = c.handleOffsetCommit(creq)
		case kmsg.OffsetFetch:
			kresp, err = c.handleOffsetFetch(creq)
		case kmsg.FindCoordinator:
			kresp, err = c.handleFindCoordinator(creq)
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
			kresp, err = c.handleCreateTopics(creq)
		case kmsg.DeleteTopics:
			kresp, err = c.handleDeleteTopics(creq)
		case kmsg.DeleteRecords:
			kresp, err = c.handleDeleteRecords(creq)
		case kmsg.InitProducerID:
			kresp, err = c.handleInitProducerID(creq)
		case kmsg.OffsetForLeaderEpoch:
			kresp, err = c.handleOffsetForLeaderEpoch(creq)
		case kmsg.AddPartitionsToTxn:
			kresp, err = c.handleAddPartitionsToTxn(creq)
		case kmsg.AddOffsetsToTxn:
			kresp, err = c.handleAddOffsetsToTxn(creq)
		case kmsg.EndTxn:
			kresp, err = c.handleEndTxn(creq)
		case kmsg.WriteTxnMarkers:
			kresp, err = c.handleWriteTxnMarkers(creq)
		case kmsg.TxnOffsetCommit:
			kresp, err = c.handleTxnOffsetCommit(creq)
		case kmsg.DescribeACLs:
			kresp, err = c.handleDescribeACLs(creq)
		case kmsg.CreateACLs:
			kresp, err = c.handleCreateACLs(creq)
		case kmsg.DeleteACLs:
			kresp, err = c.handleDeleteACLs(creq)
		case kmsg.DescribeConfigs:
			kresp, err = c.handleDescribeConfigs(creq)
		case kmsg.AlterConfigs:
			kresp, err = c.handleAlterConfigs(creq)
		case kmsg.AlterReplicaLogDirs:
			kresp, err = c.handleAlterReplicaLogDirs(creq)
		case kmsg.DescribeLogDirs:
			kresp, err = c.handleDescribeLogDirs(creq)
		case kmsg.SASLAuthenticate:
			kresp, err = c.handleSASLAuthenticate(creq)
		case kmsg.CreatePartitions:
			kresp, err = c.handleCreatePartitions(creq)
		case kmsg.DeleteGroups:
			kresp, err = c.handleDeleteGroups(creq)
		case kmsg.ElectLeaders:
			kresp, err = c.handleElectLeaders(creq)
		case kmsg.IncrementalAlterConfigs:
			kresp, err = c.handleIncrementalAlterConfigs(creq)
		case kmsg.AlterPartitionAssignments:
			kresp, err = c.handleAlterPartitionAssignments(creq)
		case kmsg.ListPartitionReassignments:
			kresp, err = c.handleListPartitionReassignments(creq)
		case kmsg.OffsetDelete:
			kresp, err = c.handleOffsetDelete(creq)
		case kmsg.DescribeUserSCRAMCredentials:
			kresp, err = c.handleDescribeUserSCRAMCredentials(creq)
		case kmsg.AlterUserSCRAMCredentials:
			kresp, err = c.handleAlterUserSCRAMCredentials(creq)
		case kmsg.UpdateFeatures:
			kresp, err = c.handleUpdateFeatures(creq)
		case kmsg.DescribeCluster:
			kresp, err = c.handleDescribeCluster(creq)
		case kmsg.DescribeProducers:
			kresp, err = c.handleDescribeProducers(creq)
		case kmsg.DescribeTransactions:
			kresp, err = c.handleDescribeTransactions(creq)
		case kmsg.ListTransactions:
			kresp, err = c.handleListTransactions(creq)
		case kmsg.DescribeClientQuotas:
			kresp, err = c.handleDescribeClientQuotas(creq)
		case kmsg.AlterClientQuotas:
			kresp, err = c.handleAlterClientQuotas(creq)
		case kmsg.GetTelemetrySubscriptions:
			kresp, err = c.handleGetTelemetrySubscriptions(creq)
		case kmsg.PushTelemetry:
			kresp, err = c.handlePushTelemetry(creq)
		case kmsg.ListConfigResources:
			kresp, err = c.handleListConfigResources(creq)
		case kmsg.DescribeTopicPartitions:
			kresp, err = c.handleDescribeTopicPartitions(creq)
		case kmsg.ConsumerGroupHeartbeat:
			kresp, err = c.handleConsumerGroupHeartbeat(creq)
		case kmsg.ConsumerGroupDescribe:
			kresp, err = c.handleConsumerGroupDescribe(creq)
		case kmsg.ShareGroupHeartbeat:
			kresp, err = c.handleShareGroupHeartbeat(creq)
		case kmsg.ShareGroupDescribe:
			kresp, err = c.handleShareGroupDescribe(creq)
		case kmsg.ShareFetch:
			kresp, err = c.handleShareFetch(creq, wsf)
		case kmsg.ShareAcknowledge:
			kresp, err = c.handleShareAcknowledge(creq)
		case kmsg.DescribeShareGroupOffsets:
			kresp, err = c.handleDescribeShareGroupOffsets(creq)
		case kmsg.AlterShareGroupOffsets:
			kresp, err = c.handleAlterShareGroupOffsets(creq)
		case kmsg.DeleteShareGroupOffsets:
			kresp, err = c.handleDeleteShareGroupOffsets(creq)
		default:
			err = fmt.Errorf("unhandled key %v", k)
		}
		c.pids.updateTimer()

	afterControl:
		if c.needsGroupsCompact.Load() {
			c.compactGroupsLog()
		}
		// If s is non-nil, this is either a previously slept control
		// that finished but was not handled, or a previously slept
		// waiting request. In either case, we need to signal to the
		// sleep dequeue loop to continue.
		if s != nil {
			s.continueDequeue <- struct{}{}
		}
		if kresp == nil && err == nil {
			// Group requests (JoinGroup, SyncGroup, Heartbeat, etc.)
			// are dispatched to goroutines that send the response
			// later via creq.reply(). The mute stays held until
			// cc.write() processes the response.
			//
			// acks=0 produce requests have no response at all.
			// Unmute immediately so cc.read() can proceed.
			if req, ok := kreq.(*kmsg.ProduceRequest); ok && req.Acks == 0 {
				creq.cc.unmute(true)
			}
			continue
		}

		select {
		case creq.cc.respCh <- clientResp{kresp: kresp, corr: creq.corr, err: err, seq: creq.seq}:
		case <-creq.cc.done:
		case <-c.die:
			return
		}
	}
}

// Control is a function to call on any client request the cluster handles.
// See [Cluster.ControlKey] for more details.
func (c *Cluster) Control(fn func(kmsg.Request) (kmsg.Response, error, bool)) {
	c.ControlKey(-1, fn)
}

// ControlKey is a function to call on a specific request key that the cluster
// handles. If the key is -1, then the control function is run on all requests.
// For all possible keys, see [kmsg.Key], for example [kmsg.Produce].
//
// If the control function returns true (handled), then either the response is
// written back to the client or, if the control function returns an error, the
// client connection is closed. If both returns are nil, then the cluster will
// loop continuing to read from the client and the client will likely have a
// read timeout at some point.
//
// If the control function returns false (not handled), the next control
// function for this key runs. If no control function handles the request,
// the cluster processes it normally. This allows control functions that just
// observe or count requests without intercepting them.
//
// Multiple control functions for the same key run in FIFO order (the order
// they were added).
//
// Handling a request drops the control function from the cluster, meaning
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
	c.control[key] = append(c.control[key], &controlCtx{
		key:     key,
		fn:      fn,
		lastReq: make(map[*clientConn]*clientReq),
	})
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

// DropControl removes the current control function. This takes precedence
// over KeepControl, allowing you to keep a control function by default but
// forcefully drop it when a specific condition is met.
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

func (c *Cluster) tryControl(creq *clientReq) (kresp kmsg.Response, err error, handled bool) { //nolint:revive // control handler signature
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

func (c *Cluster) tryControlKey(key int16, creq *clientReq) (kmsg.Response, error, bool) { //nolint:revive // control handler signature
	for _, cctx := range c.control[key] {
		if cctx.lastReq[creq.cc] == creq {
			continue
		}
		cctx.lastReq[creq.cc] = creq
		res := c.runControl(cctx, creq)
	inner:
		for {
			select {
			case admin := <-c.adminCh:
				admin()
				continue
			case res := <-res:
				c.maybePopControl(res.handled, cctx)
				if res.handled {
					return res.kresp, res.err, true
				}
				break inner
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
		s := c.control[cctx.key]
		for i, v := range s {
			if v == cctx {
				c.control[cctx.key] = append(s[:i], s[i+1:]...)
				break
			}
		}
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
	if bs.c.cfg.sleepOutOfOrder {
		// Case (3a), out of order sleep: we need to check the entire
		// queue to see if this request was already sleeping and, if
		// so, update the values. If it was not already sleeping, we
		// "keep" the new sleeping item.
		bs.keep(s)
		return true
	}
	q0 := bs.queue[0] // Case (3b) or (3c) -- just update values below
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
func (c *Cluster) MoveTopicPartition(topic string, partition, nodeID int32) error {
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

// LeaderFor returns the node ID of the topic partition. If the partition
// does not exist, this returns -1.
func (c *Cluster) LeaderFor(topic string, partition int32) int32 {
	n := int32(-1)
	c.admin(func() {
		pd, ok := c.data.tps.getp(topic, partition)
		if !ok {
			return
		}
		n = pd.leader.node
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
		if ln, err = newListener(port, c.cfg.tls, c.cfg.listenFn); err != nil {
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
			if b.node != nodeID {
				continue
			}
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

// SetFollowers sets the node IDs of brokers that can also serve fetch requests
// for a partition. Setting followers to an empty or nil slice reverts to the
// default of only the leader being able to serve fetch requests.
func (c *Cluster) SetFollowers(topic string, partition int32, followers []int32) {
	c.admin(func() {
		pd, ok := c.data.tps.getp(topic, partition)
		if !ok {
			return
		}
		pd.followers = append([]int32(nil), followers...)
	})
}

// Compact triggers log compaction on all topics with cleanup.policy=compact.
// Records with duplicate keys are deduplicated, keeping only the latest value.
// Tombstones (nil value) older than delete.retention.ms are removed.
func (c *Cluster) Compact() {
	c.admin(func() {
		c.compactAll()
	})
}

// ApplyRetention enforces retention.ms and retention.bytes on all topics,
// removing batches that are expired or exceed the size limit.
func (c *Cluster) ApplyRetention() {
	c.admin(func() {
		c.applyRetentionAll()
	})
}

// compactAll compacts and applies retention on all eligible topics.
// Must be called from Cluster.run().
func (c *Cluster) compactAll() {
	for t := range c.data.tps {
		for _, pd := range c.data.tps[t] {
			if c.data.isCompactTopic(t) {
				c.compact(pd, t)
			}
			c.applyRetention(pd, t)
		}
	}
}

// applyRetentionAll applies retention on all topics.
// Must be called from Cluster.run().
func (c *Cluster) applyRetentionAll() {
	for t := range c.data.tps {
		for _, pd := range c.data.tps[t] {
			c.applyRetention(pd, t)
		}
	}
}

func (c *Cluster) compactTickerC() <-chan time.Time {
	if c.compactTicker != nil {
		return c.compactTicker.C
	}
	return nil
}

func (c *Cluster) compactIntervalMs() int64 {
	if v, ok := c.loadBcfgs()["log.cleaner.backoff.ms"]; ok && v != nil {
		if n, err := strconv.ParseInt(*v, 10, 64); err == nil {
			return n
		}
	}
	return 3600000
}

// refreshCompactTicker starts or stops the compaction ticker based on whether
// any topic has cleanup.policy=compact or has retention configs explicitly set.
// Must be called from Cluster.run().
func (c *Cluster) refreshCompactTicker() {
	needsTicker := false
	for t := range c.data.tps {
		if c.data.isCompactTopic(t) {
			needsTicker = true
			break
		}
		if c.data.hasRetentionConfig(t) {
			needsTicker = true
			break
		}
	}
	if needsTicker && c.compactTicker == nil {
		c.compactTicker = time.NewTicker(time.Duration(c.compactIntervalMs()) * time.Millisecond)
	} else if !needsTicker && c.compactTicker != nil {
		c.compactTicker.Stop()
		c.compactTicker = nil
	}
}

// expireGroupOffsets iterates all groups and expires stale offsets.
// Groups with all offsets expired and no members are auto-deleted.
// Must be called from Cluster.run().
func (c *Cluster) expireGroupOffsets() {
	retentionMs := c.offsetsRetentionMs()
	for name, g := range c.groups.gs {
		unstable := c.pids.hasUnstableOffsets(name)
		var shouldDelete bool
		if !g.waitControl(func() {
			shouldDelete = g.expireOffsets(retentionMs, unstable)
			if shouldDelete {
				g.quitOnce()
			}
		}) {
			continue
		}
		select {
		case <-g.quitCh:
			delete(c.groups.gs, name)
		default:
		}
	}
}
