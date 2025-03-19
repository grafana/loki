package ckit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/hashicorp/go-msgpack/codec"
	"github.com/hashicorp/memberlist"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/ckit/internal/gossiphttp"
	"github.com/grafana/ckit/internal/lamport"
	"github.com/grafana/ckit/internal/messages"
	"github.com/grafana/ckit/internal/queue"
	"github.com/grafana/ckit/peer"
	"github.com/grafana/ckit/shard"
)

var (
	// ErrStopped is returned by invoking methods against Node when it is
	// stopped.
	ErrStopped = errors.New("node stopped")
)

// StateTransitionError is returned when a node requests an invalid state
// transition.
type StateTransitionError struct {
	From, To peer.State
}

// Error implements error.
func (e StateTransitionError) Error() string {
	return fmt.Sprintf("invalid transition from %s to %s", e.From, e.To)
}

// Config configures a Node within the cluster.
type Config struct {
	// Name of the Node. Must be unique across the cluster. Required.
	Name string

	// host:port address other nodes should use to connect to this Node.
	// Required.
	AdvertiseAddr string

	// Optional logger to use.
	Log log.Logger

	// Optional sharder to synchronize cluster changes to. Synchronization of the
	// Sharder happens prior to Observers being notified of changes.
	Sharder shard.Sharder

	// Optional identifier to prevent clusters from accidentally merging.
	// Nodes are prevented from joining a cluster with an explicit label if
	// they do not share the same label.
	Label string

	// EnableTLS optionally specifies whether TLS should be
	// used for communication between peers.
	// Defaults to false.
	EnableTLS bool
}

func (c *Config) validate() error {
	if len(c.Name) == 0 {
		return fmt.Errorf("node name is required")
	}

	if len(c.AdvertiseAddr) == 0 {
		return fmt.Errorf("advertise address is required")
	}

	if c.Log == nil {
		c.Log = log.NewNopLogger()
	}

	return nil
}

// A Node is a participant in a cluster. Nodes keep track of all of their peers
// and emit events to Observers when the cluster state changes.
type Node struct {
	log                  log.Logger
	cfg                  Config
	ml                   *memberlist.Memberlist
	broadcasts           memberlist.TransmitLimitedQueue // Make sure peerMut isn't held when updating
	notifyObserversQueue *queue.Queue
	m                    *metrics
	baseRoute            string
	handler              http.Handler

	// The clock for the node. Nodes have their own clock for the sake of
	// testing; using the global clock could cause clock synchronization issues
	// to be missed if you use multiple in-process nodes.
	clock lamport.Clock

	stateMut   sync.RWMutex
	localState peer.State

	shutdownMut sync.Mutex
	runCancel   context.CancelFunc
	stopped     bool

	observersMut sync.Mutex
	observers    []Observer

	// peerStates is updated any time a messages.State broadcast is received, and
	// may have keys for node names that do not exist in the peers map. These
	// keys get gradually cleaned up during local state synchronization.
	//
	// Use peers for the definitive list of current peers.

	// TODO(rfratto): should this block be replaced with a single struct that
	// supports updating peers in-place?

	peerMut    sync.RWMutex
	peerStates map[string]messages.State // State lookup for a node name
	peers      map[string]peer.Peer      // Current list of peers & their states
	peerCache  []peer.Peer               // Slice version of peers; keep in sync with peers
}

// NewNode creates an unstarted Node to participulate in a cluster. An error
// will be returned if the provided config is invalid.
//
// Before starting the Node, the caller has to wire up the Node's HTTP handlers
// on the base route provided by the Handler method.
//
// If Node is intended to be reachable over non-TLS HTTP/2 connections, then
// the http.Server the routes are registered on must make use of the
// golang.org/x/net/http2/h2c package to enable upgrading incoming plain HTTP
// connections to HTTP/2.
//
// Similarly, if the Node is intended to initiate non-TLS outgoing connections,
// the provided cli should be configured properly (with AllowHTTP set to true
// and using a custom DialTLS function to create a non-TLS net.Conn).
func NewNode(cli *http.Client, cfg Config) (*Node, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	advertiseAddr, advertisePortString, err := net.SplitHostPort(cfg.AdvertiseAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to read advertise address: %w", err)
	}

	advertiseIP, err := net.ResolveIPAddr("ip", advertiseAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup advertise address %q: %w", advertiseAddr, err)
	}

	advertisePort, err := net.LookupPort("tcp", advertisePortString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse advertise port %q: %w", advertisePortString, err)
	}

	httpTransport, transportMetrics, err := gossiphttp.NewTransport(gossiphttp.Options{
		Log:           cfg.Log,
		Client:        cli,
		PacketTimeout: 1 * time.Second,
		UseHTTPS:      cfg.EnableTLS,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build transport: %w", err)
	}

	baseRoute, handler := httpTransport.Handler()

	mlc := memberlist.DefaultLANConfig()
	mlc.Name = cfg.Name
	mlc.Transport = httpTransport
	mlc.AdvertiseAddr = advertiseIP.String()
	mlc.AdvertisePort = advertisePort
	mlc.Label = cfg.Label

	if cfg.Log != nil {
		mlc.Logger = newMemberListLogger(log.With(cfg.Log, "subsystem", "memberlist"))
	} else {
		mlc.LogOutput = io.Discard
	}

	n := &Node{
		log: cfg.Log,
		cfg: cfg,
		m:   newMetrics(mlc.Label),

		notifyObserversQueue: queue.New(1),

		peerStates: make(map[string]messages.State),
		peers:      make(map[string]peer.Peer),

		baseRoute: baseRoute,
		handler:   handler,
	}

	nd := &nodeDelegate{Node: n}
	mlc.Events = nd
	mlc.Delegate = nd
	mlc.Conflict = nd

	ml, err := memberlist.Create(mlc)
	if err != nil {
		return nil, fmt.Errorf("failed to create memberlist: %w", err)
	}

	n.ml = ml
	n.broadcasts.NumNodes = func() int { return len(n.Peers()) }
	n.broadcasts.RetransmitMult = mlc.RetransmitMult

	// Include some extra metrics.
	n.m.Add(
		newMemberlistCollector(ml, mlc.Label),
		transportMetrics,
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "cluster_node_lamport_time",
			Help: "The current lamport time of the node.",
		}, func() float64 {
			return float64(n.clock.Now())
		}),
	)

	return n, nil
}

// Metrics returns a prometheus.Collector that can be used to collect metrics
// about the Node.
func (n *Node) Metrics() prometheus.Collector { return n.m }

// Handler returns the base route and http.Handler used by the Node for
// communicating over HTTP/2.
//
// The base route and handler must be configured properly by registering them
// with an HTTP server before starting the Node.
func (n *Node) Handler() (string, http.Handler) { return n.baseRoute, n.handler }

// Start starts the Node with a set of peers to connect to. Start may be called
// multiple times to add additional peers into the memberlist.
//
// Start may not be called after [Stop] has been called.
func (n *Node) Start(peers []string) error {
	n.shutdownMut.Lock()
	defer n.shutdownMut.Unlock()

	if n.stopped {
		// n.ml can't be re-used after we stopped, so we need to force error the
		// Start.
		//
		// TODO: maybe create a new n.ml instead?
		return ErrStopped
	}

	if n.runCancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		go n.run(ctx)
		n.runCancel = cancel
	}

	_, err := n.ml.Join(peers)
	if err != nil {
		return fmt.Errorf("failed to join memberlist: %w", err)
	}

	// Originally, after calling n.ml.Join, we would broadcast a new state
	// message (with a new lamport time) with the node's local state. The intent
	// was that there might be stale messages about a previous instance of our
	// node with different lamport times, and we'd want to correct them by
	// broadcasting a message.
	//
	// This only appeared necessary due to a bug in how we stale messages about
	// our own node, and resulted in a lot of extra state messages being sent due
	// to nodes calling Start to fix split brain issues.
	//
	// Now, we'll correct invalid state messages about our node upon receipt; see
	// [Node.handleStateMessage] and [nodeDelegate.MergeRemoteState].
	return nil
}

func (n *Node) run(ctx context.Context) {
	var lastPeers []peer.Peer

	for {
		v, err := n.notifyObserversQueue.Dequeue(ctx)
		if err != nil {
			break
		}
		peers := v.([]peer.Peer)

		// Ignore events if the peer set hasn't changed.
		if peersEqual(lastPeers, peers) {
			continue
		}
		lastPeers = peers

		n.notifyObservers(peers)
	}
}

// Stop stops the Node, removing it from the cluster. Callers should first
// first transition to StateTerminating to gracefully leave the cluster.
// Observers will no longer be notified about cluster changes after Stop
// returns.
func (n *Node) Stop() error {
	n.shutdownMut.Lock()
	defer n.shutdownMut.Unlock()

	if n.runCancel != nil {
		n.runCancel()
		n.runCancel = nil
	}

	if n.stopped {
		// n.ml.Leave will panic if being called twice. We'll be defensive and
		// prevent anything from happening here.
		return nil
	}
	n.stopped = true

	// TODO(rfratto): configurable leave timeout
	leaveTimeout := time.Second * 5
	if err := n.ml.Leave(leaveTimeout); err != nil {
		level.Error(n.log).Log("msg", "failed to broadcast leave message to cluster", "err", err)
	}
	return n.ml.Shutdown()
}

// CurrentState returns n's current State. Other nodes may not have the same
// State value for n as the current state propagates throughout the cluster.
func (n *Node) CurrentState() peer.State {
	n.stateMut.RLock()
	defer n.stateMut.RUnlock()

	return n.localState
}

// ChangeState changes the state of the node. ChangeState will block until the
// message has been broadcast or until the provided ctx is canceled. Canceling
// the context does not stop the message from being broadcast; it just stops
// waiting for it.
//
// The "to" state must be valid to move to from the current state. Acceptable
// transitions are:
//
//	StateViewer -> StateParticipant
//	StateParticipant -> StateTerminating
//
// Nodes intended to only be viewers should never transition to another state.
func (n *Node) ChangeState(ctx context.Context, to peer.State) error {
	n.stateMut.Lock()
	defer n.stateMut.Unlock()

	t := stateTransition{From: n.localState, To: to}
	if _, valid := validStateTransitions[t]; !valid {
		return StateTransitionError(t)
	}

	level.Debug(n.log).Log("msg", "changing node state", "from", n.localState, "to", to)
	return n.waitChangeState(ctx, to)
}

type stateTransition struct{ From, To peer.State }

var validStateTransitions = map[stateTransition]struct{}{
	{peer.StateViewer, peer.StateParticipant}:      {},
	{peer.StateParticipant, peer.StateTerminating}: {},
}

func (n *Node) waitChangeState(ctx context.Context, to peer.State) error {
	waitBroadcast := make(chan struct{}, 1)
	afterBroadcast := func() {
		select {
		case waitBroadcast <- struct{}{}:
		default:
		}
	}

	if err := n.changeState(to, afterBroadcast); err != nil {
		return err
	}

	// We need at least one remote peer to broadcast the state change to. If it's
	// just us, we can return immediately.
	n.peerMut.RLock()
	hasPeers := len(n.peers) > 1
	n.peerMut.RUnlock()

	if !hasPeers {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-waitBroadcast:
		return nil
	}
}

func (n *Node) changeState(to peer.State, onDone func()) error {
	n.localState = to
	n.m.nodeInfo.MustSet("state", to.String())

	stateMsg := messages.State{
		NodeName: n.cfg.Name,
		NewState: n.localState,
		Time:     n.clock.Tick(),
	}

	// Treat the stateMsg as if it was received externally to track our own state
	// along with other nodes.
	n.handleStateMessage(stateMsg)

	bcast, err := messages.Broadcast(&stateMsg, onDone)
	if err != nil {
		return err
	}
	n.broadcasts.QueueBroadcast(bcast)
	return nil
}

// handleStateMessage handles a state message from a peer. Returns true if the
// message hasn't been seen before. The final return parameter will be the
// message to broadcast: if msg is a stale message from a previous instance of
// the local node, final will be an updated message reflecting the node's local
// state.
//
// handleStateMessage must be called with n.stateMut held for reading.
func (n *Node) handleStateMessage(msg messages.State) (final messages.State, newMessage bool) {
	n.clock.Observe(msg.Time)

	n.peerMut.Lock()
	defer n.peerMut.Unlock()

	curr, exist := n.peerStates[msg.NodeName]
	if exist && msg.Time <= curr.Time {
		// Ignore a state message if we have the same or a newer one.
		return curr, false
	} else if msg.NodeName == n.cfg.Name {
		level.Debug(n.log).Log("msg", "got stale message about self", "msg", msg)

		// A peer has a newer message about ourselves, likely from a previous
		// instance of the process. We'll ignore the message and replace it with a
		// newer message reflecting our current state.
		msg = messages.State{
			NodeName: n.cfg.Name,
			NewState: n.localState,
			Time:     n.clock.Tick(),
		}
	} else {
		level.Debug(n.log).Log("msg", "handling state message", "msg", msg)
	}

	n.peerStates[msg.NodeName] = msg

	if p, ok := n.peers[msg.NodeName]; ok {
		p.State = msg.NewState
		n.peers[msg.NodeName] = p
		n.handlePeersChanged()
	}

	return msg, true
}

// Peers returns all Peers currently known by n. The Peers list will include
// peers regardless of their current State. The returned slice should not be
// modified.
func (n *Node) Peers() []peer.Peer {
	n.peerMut.RLock()
	defer n.peerMut.RUnlock()
	return n.peerCache
}

// handlePeersChanged should be called when the peers map is updated. The peer
// cache will be updated before notifying observers that peers have changed.
//
// peerMut should be held when this function is called.
func (n *Node) handlePeersChanged() {
	var (
		newPeers = make([]peer.Peer, 0, len(n.peers))

		peerCountByState = make(map[peer.State]int, len(peer.AllStates))
	)

	for _, peer := range n.peers {
		newPeers = append(newPeers, peer)
		peerCountByState[peer.State]++
	}

	// Update the metric based on the peers we just processed.
	for _, state := range peer.AllStates {
		count := peerCountByState[state]
		n.m.nodePeers.WithLabelValues(state.String()).Set(float64(count))
	}

	// Sort the new peers by name.
	sort.Slice(newPeers, func(i, j int) bool {
		return newPeers[i].Name < newPeers[j].Name
	})

	// Notify the sharder first if it's set.
	if n.cfg.Sharder != nil {
		n.cfg.Sharder.SetPeers(newPeers)
	}

	n.peerCache = newPeers
	n.notifyObserversQueue.Enqueue(newPeers)
}

// Observe registers o to be informed when the cluster changes. o will be
// notified when a new peer is discovered, an existing peer shuts down, or the
// state of a peer changes. Observers are invoked in the order they were
// registered.
//
// Observers are notified in the background about the most recent state of the
// cluster, ignoring intermediate changed events that occurred while a
// long-running observer is still processing an older change.
func (n *Node) Observe(o Observer) {
	n.observersMut.Lock()
	defer n.observersMut.Unlock()
	n.observers = append(n.observers, o)
	n.m.nodeObservers.Set(float64(len(n.observers)))
}

func (n *Node) notifyObservers(peers []peer.Peer) {
	n.observersMut.Lock()
	defer n.observersMut.Unlock()

	n.m.nodeUpdating.Set(1)
	defer n.m.nodeUpdating.Set(0)

	timer := prometheus.NewTimer(n.m.nodeUpdateDuration)
	defer timer.ObserveDuration()

	newObservers := make([]Observer, 0, len(n.observers))
	for _, o := range n.observers {
		rereg := o.NotifyPeersChanged(peers)
		if rereg {
			newObservers = append(newObservers, o)
		}
	}

	n.observers = newObservers
	n.m.nodeObservers.Set(float64(len(n.observers)))
}

// nodeDelegate is used to implement memberlist.*Delegate types without
// exposing their methods publicly.
type nodeDelegate struct {
	*Node
}

// Delegate types nodeDelegate should implement
var (
	_ memberlist.Delegate         = (*nodeDelegate)(nil)
	_ memberlist.EventDelegate    = (*nodeDelegate)(nil)
	_ memberlist.ConflictDelegate = (*nodeDelegate)(nil)
)

//
// memberlist.Delegate methods
//

func (nd *nodeDelegate) NodeMeta(limit int) []byte {
	// Nodes don't have any additional metadata to send; return nil.
	return nil
}

func (nd *nodeDelegate) NotifyMsg(raw []byte) {
	buf, ty, err := messages.Parse(raw)
	if err != nil {
		level.Error(nd.log).Log("msg", "failed to parse gossip message", "ty", ty, "err", err)
		return
	}

	switch ty {
	case messages.TypeState:
		nd.m.gossipEventsTotal.WithLabelValues(eventStateChange).Inc()

		var s messages.State
		if err := messages.Decode(buf, &s); err != nil {
			level.Error(nd.log).Log("msg", "failed to decode state message", "err", err)
			return
		}

		// nd.handleStateMessage must be called with stateMut held.
		nd.stateMut.RLock()
		defer nd.stateMut.RUnlock()

		if s, broadcast := nd.handleStateMessage(s); broadcast {
			// We should continue gossiping the message to other peers if we haven't
			// seen it before.
			//
			// We can ignore errors from the broadcast here. It shouldn't fail to
			// encode since we just decoded it successfully, but even if it did fail,
			// messages would still converge eventually using push/pulls.
			bcast, _ := messages.Broadcast(&s, nil)
			nd.broadcasts.QueueBroadcast(bcast)
			nd.m.gossipBroadcastsTotal.WithLabelValues(eventStateChange).Inc()
		}

	default:
		nd.m.gossipEventsTotal.WithLabelValues(eventUnkownMessage).Inc()

		level.Warn(nd.log).Log("msg", "unexpected gossip message", "ty", ty)
	}
}

func (nd *nodeDelegate) GetBroadcasts(overhead, limit int) [][]byte {
	return nd.broadcasts.GetBroadcasts(overhead, limit)
}

func (nd *nodeDelegate) LocalState(join bool) []byte {
	nd.peerMut.RLock()
	defer nd.peerMut.RUnlock()

	nd.m.gossipEventsTotal.WithLabelValues(eventGetLocalState).Inc()

	ls := localState{
		CurrentTime: nd.clock.Now(),
		NodeStates:  make([]messages.State, 0, len(nd.peers)),
	}

	// Our local state will have one NodeState for each peer we're currently
	// aware of. This is different from each state we're aware of, since our
	// nodeStates map may track states for nodes we haven't seen.
	//
	// Returning states for known peers allows MergeRemoteState to clean up
	// nodeStates and remove entries for nodes that neither peer knows about.
	for p := range nd.peers {
		if s, hasState := nd.peerStates[p]; hasState {
			ls.NodeStates = append(ls.NodeStates, s)
			continue
		}

		ls.NodeStates = append(ls.NodeStates, messages.State{
			NodeName: p,
			NewState: peer.StateViewer,
			Time:     lamport.Time(0),
		})
	}

	bb, err := encodeLocalState(&ls)
	if err != nil {
		level.Error(nd.log).Log("msg", "failed to encode local state", "err", err)
		return nil
	}
	return bb
}

func (nd *nodeDelegate) MergeRemoteState(buf []byte, join bool) {
	rs, err := decodeLocalState(buf)
	if err != nil {
		level.Error(nd.log).Log("msg", "failed to decode remote state", "join", join, "err", err)
		return
	}
	nd.clock.Observe(rs.CurrentTime)
	level.Debug(nd.log).Log("msg", "merging remote state", "remote_time", rs.CurrentTime)

	// We'll be doing a full sync of state messages that another peer knows
	// about. After the end of the full sync, we'll want to gossip new messages
	// we've discovered to our peers.
	var newMessages = make([]messages.State, 0, len(rs.NodeStates))
	defer func() {
		// This must be done after we unlock nd.peerMut, since QueueBroadcast will
		// call nd.Peers.
		for _, msg := range newMessages {
			bcast, _ := messages.Broadcast(&msg, nil)
			nd.broadcasts.QueueBroadcast(bcast)
		}
	}()

	nd.peerMut.Lock()
	defer nd.peerMut.Unlock()

	nd.m.gossipEventsTotal.WithLabelValues(eventMergeRemoteState).Inc()

	var (
		peersChanged bool

		// Map of remote states by name to optimize lookups.
		remoteStates = make(map[string]messages.State, len(rs.NodeStates))
	)

	// Merge in node states that the remote peer kept.
	for _, msg := range rs.NodeStates {
		// We don't use handleStateMessage here so we can defer recalculating peers
		// to the end of the merge.
		remoteStates[msg.NodeName] = msg

		curr, exist := nd.peerStates[msg.NodeName]
		if exist && msg.Time <= curr.Time {
			// Ignore a state message if we have a newer one.
			continue
		} else if msg.NodeName == nd.cfg.Name {
			level.Debug(nd.log).Log("msg", "got stale message about self", "msg", msg)

			// Our remote peer has a newer message about ourselves, likely from a
			// previous instance of the process. We'll ignore the message and replace
			// it with a newer message reflecting our current state.
			msg = messages.State{
				NodeName: nd.cfg.Name,
				NewState: nd.CurrentState(),
				Time:     nd.clock.Tick(),
			}
		} else {
			level.Debug(nd.log).Log("msg", "handling state message", "msg", msg)
		}

		nd.peerStates[msg.NodeName] = msg

		if p, ok := nd.peers[msg.NodeName]; ok {
			p.State = msg.NewState
			nd.peers[msg.NodeName] = p
			peersChanged = true
		}

		newMessages = append(newMessages, msg)
	}

	// Clean up stale entries in peerStates.
	for nodeName := range nd.peerStates {
		// If nodeName exists, it is not a stale reference.
		if _, peerExists := nd.peers[nodeName]; peerExists {
			continue
		}

		// We don't know about this peer. If the remote state also doesn't have an
		// entry for this peer, we can treat it as stale and delete it.
		_, peerExistsRemote := remoteStates[nodeName]
		if !peerExistsRemote {
			level.Debug(nd.log).Log("msg", "deleting stale reference to node", "node", nodeName)
			delete(nd.peerStates, nodeName)
		}
	}

	if peersChanged {
		nd.handlePeersChanged()
	}
}

type localState struct {
	// CurrentTime is the current lamport time.
	CurrentTime lamport.Time
	// NodeStates holds the set of states for all peers of a node. States may
	// have a lamport time of 0 for nodes that have not broadcast a state yet.
	NodeStates []messages.State
}

func encodeLocalState(ls *localState) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	var handle codec.MsgpackHandle
	enc := codec.NewEncoder(buf, &handle)
	err := enc.Encode(ls)
	return buf.Bytes(), err
}

func decodeLocalState(buf []byte) (*localState, error) {
	var ls localState

	r := bytes.NewReader(buf)
	var handle codec.MsgpackHandle
	dec := codec.NewDecoder(r, &handle)
	err := dec.Decode(&ls)
	return &ls, err
}

//
// memberlist.EventDelegate methods
//

func (nd *nodeDelegate) NotifyJoin(node *memberlist.Node) {
	nd.peerMut.Lock()
	defer nd.peerMut.Unlock()

	nd.m.gossipEventsTotal.WithLabelValues(eventNodeJoin).Inc()
	nd.updatePeer(nd.nodeToPeer(node))
}

// nodeToPeer converts a memberlist Node to a Peer. Should only be called with
// peerMut held.
func (nd *nodeDelegate) nodeToPeer(node *memberlist.Node) peer.Peer {
	return peer.Peer{
		Name:  node.Name,
		Addr:  node.Address(),
		Self:  node.Name == nd.cfg.Name,
		State: nd.peerStates[node.Name].NewState,
	}
}

func (nd *nodeDelegate) NotifyLeave(node *memberlist.Node) {
	nd.peerMut.Lock()
	defer nd.peerMut.Unlock()

	nd.m.gossipEventsTotal.WithLabelValues(eventNodeLeave).Inc()
	nd.removePeer(node.Name)
}

func (nd *nodeDelegate) NotifyUpdate(node *memberlist.Node) {
	nd.peerMut.Lock()
	defer nd.peerMut.Unlock()

	nd.m.gossipEventsTotal.WithLabelValues(eventNodeUpdate).Inc()
	nd.updatePeer(nd.nodeToPeer(node))
}

func (nd *nodeDelegate) updatePeer(p peer.Peer) {
	nd.peers[p.Name] = p
	nd.handlePeersChanged()
}

func (nd *nodeDelegate) removePeer(name string) {
	delete(nd.peers, name)
	nd.handlePeersChanged()
}

//
// memberlist.ConflictDelegate methods
//

func (nd *nodeDelegate) NotifyConflict(existing, other *memberlist.Node) {
	nd.m.gossipEventsTotal.WithLabelValues(eventNodeConflict).Inc()
}
