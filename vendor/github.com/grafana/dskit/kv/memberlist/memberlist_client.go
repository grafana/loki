package memberlist

import (
	"bytes"
	"context"
	crypto_rand "crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	math_rand "math/rand"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/hashicorp/memberlist"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/services"
)

const (
	maxCasRetries              = 10          // max retries in CAS operation
	noChangeDetectedRetrySleep = time.Second // how long to sleep after no change was detected in CAS
	notifyMsgQueueSize         = 1024        // size of buffered channels to handle memberlist messages
)

// Client implements kv.Client interface, by using memberlist.KV
type Client struct {
	kv    *KV // reference to singleton memberlist-based KV
	codec codec.Codec
}

// NewClient creates new client instance. Supplied codec must already be registered in KV.
func NewClient(kv *KV, codec codec.Codec) (*Client, error) {
	c := kv.GetCodec(codec.CodecID())
	if c == nil {
		return nil, fmt.Errorf("codec not registered in KV: %s", codec.CodecID())
	}

	return &Client{
		kv:    kv,
		codec: codec,
	}, nil
}

// List is part of kv.Client interface.
func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	err := c.awaitKVRunningOrStopping(ctx)
	if err != nil {
		return nil, err
	}

	return c.kv.List(prefix), nil
}

// Get is part of kv.Client interface.
func (c *Client) Get(ctx context.Context, key string) (interface{}, error) {
	err := c.awaitKVRunningOrStopping(ctx)
	if err != nil {
		return nil, err
	}

	return c.kv.Get(key, c.codec)
}

// Delete is part of kv.Client interface.
func (c *Client) Delete(_ context.Context, _ string) error {
	return errors.New("memberlist does not support Delete")
}

// CAS is part of kv.Client interface
func (c *Client) CAS(ctx context.Context, key string, f func(in interface{}) (out interface{}, retry bool, err error)) error {
	err := c.awaitKVRunningOrStopping(ctx)
	if err != nil {
		return err
	}

	return c.kv.CAS(ctx, key, c.codec, f)
}

// WatchKey is part of kv.Client interface.
func (c *Client) WatchKey(ctx context.Context, key string, f func(interface{}) bool) {
	err := c.awaitKVRunningOrStopping(ctx)
	if err != nil {
		return
	}

	c.kv.WatchKey(ctx, key, c.codec, f)
}

// WatchPrefix calls f whenever any value stored under prefix changes.
// Part of kv.Client interface.
func (c *Client) WatchPrefix(ctx context.Context, prefix string, f func(string, interface{}) bool) {
	err := c.awaitKVRunningOrStopping(ctx)
	if err != nil {
		return
	}

	c.kv.WatchPrefix(ctx, prefix, c.codec, f)
}

// We want to use KV in Running and Stopping states.
func (c *Client) awaitKVRunningOrStopping(ctx context.Context) error {
	s := c.kv.State()
	switch s {
	case services.Running, services.Stopping:
		return nil
	case services.New, services.Starting:
		err := c.kv.AwaitRunning(ctx)
		if ns := c.kv.State(); ns == services.Stopping {
			return nil
		}
		return err
	default:
		return fmt.Errorf("unexpected state: %v", s)
	}
}

// KVConfig is a config for memberlist.KV
type KVConfig struct {
	// Memberlist options.
	NodeName            string        `yaml:"node_name" category:"advanced"`
	RandomizeNodeName   bool          `yaml:"randomize_node_name" category:"advanced"`
	StreamTimeout       time.Duration `yaml:"stream_timeout" category:"advanced"`
	RetransmitMult      int           `yaml:"retransmit_factor" category:"advanced"`
	PushPullInterval    time.Duration `yaml:"pull_push_interval" category:"advanced"`
	GossipInterval      time.Duration `yaml:"gossip_interval" category:"advanced"`
	GossipNodes         int           `yaml:"gossip_nodes" category:"advanced"`
	GossipToTheDeadTime time.Duration `yaml:"gossip_to_dead_nodes_time" category:"advanced"`
	DeadNodeReclaimTime time.Duration `yaml:"dead_node_reclaim_time" category:"advanced"`
	EnableCompression   bool          `yaml:"compression_enabled" category:"advanced"`

	// ip:port to advertise other cluster members. Used for NAT traversal
	AdvertiseAddr string `yaml:"advertise_addr"`
	AdvertisePort int    `yaml:"advertise_port"`

	ClusterLabel                     string `yaml:"cluster_label" category:"advanced"`
	ClusterLabelVerificationDisabled bool   `yaml:"cluster_label_verification_disabled" category:"advanced"`

	// List of members to join
	JoinMembers      flagext.StringSlice `yaml:"join_members"`
	MinJoinBackoff   time.Duration       `yaml:"min_join_backoff" category:"advanced"`
	MaxJoinBackoff   time.Duration       `yaml:"max_join_backoff" category:"advanced"`
	MaxJoinRetries   int                 `yaml:"max_join_retries" category:"advanced"`
	AbortIfJoinFails bool                `yaml:"abort_if_cluster_join_fails"`
	RejoinInterval   time.Duration       `yaml:"rejoin_interval" category:"advanced"`

	// Remove LEFT ingesters from ring after this timeout.
	LeftIngestersTimeout time.Duration `yaml:"left_ingesters_timeout" category:"advanced"`

	// Timeout used when leaving the memberlist cluster.
	LeaveTimeout time.Duration `yaml:"leave_timeout" category:"advanced"`

	// How much space to use to keep received and sent messages in memory (for troubleshooting).
	MessageHistoryBufferBytes int `yaml:"message_history_buffer_bytes" category:"advanced"`

	TCPTransport TCPTransportConfig `yaml:",inline"`

	MetricsNamespace string `yaml:"-"`

	// Codecs to register. Codecs need to be registered before joining other members.
	Codecs []codec.Codec `yaml:"-"`
}

// RegisterFlagsWithPrefix registers flags.
func (cfg *KVConfig) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	mlDefaults := defaultMemberlistConfig()

	// "Defaults to hostname" -- memberlist sets it to hostname by default.
	f.StringVar(&cfg.NodeName, prefix+"memberlist.nodename", "", "Name of the node in memberlist cluster. Defaults to hostname.") // memberlist.DefaultLANConfig will put hostname here.
	f.BoolVar(&cfg.RandomizeNodeName, prefix+"memberlist.randomize-node-name", true, "Add random suffix to the node name.")
	f.DurationVar(&cfg.StreamTimeout, prefix+"memberlist.stream-timeout", mlDefaults.TCPTimeout, "The timeout for establishing a connection with a remote node, and for read/write operations.")
	f.IntVar(&cfg.RetransmitMult, prefix+"memberlist.retransmit-factor", mlDefaults.RetransmitMult, "Multiplication factor used when sending out messages (factor * log(N+1)).")
	f.Var(&cfg.JoinMembers, prefix+"memberlist.join", "Other cluster members to join. Can be specified multiple times. It can be an IP, hostname or an entry specified in the DNS Service Discovery format.")
	f.DurationVar(&cfg.MinJoinBackoff, prefix+"memberlist.min-join-backoff", 1*time.Second, "Min backoff duration to join other cluster members.")
	f.DurationVar(&cfg.MaxJoinBackoff, prefix+"memberlist.max-join-backoff", 1*time.Minute, "Max backoff duration to join other cluster members.")
	f.IntVar(&cfg.MaxJoinRetries, prefix+"memberlist.max-join-retries", 10, "Max number of retries to join other cluster members.")
	f.BoolVar(&cfg.AbortIfJoinFails, prefix+"memberlist.abort-if-join-fails", cfg.AbortIfJoinFails, "If this node fails to join memberlist cluster, abort.")
	f.DurationVar(&cfg.RejoinInterval, prefix+"memberlist.rejoin-interval", 0, "If not 0, how often to rejoin the cluster. Occasional rejoin can help to fix the cluster split issue, and is harmless otherwise. For example when using only few components as a seed nodes (via -memberlist.join), then it's recommended to use rejoin. If -memberlist.join points to dynamic service that resolves to all gossiping nodes (eg. Kubernetes headless service), then rejoin is not needed.")
	f.DurationVar(&cfg.LeftIngestersTimeout, prefix+"memberlist.left-ingesters-timeout", 5*time.Minute, "How long to keep LEFT ingesters in the ring.")
	f.DurationVar(&cfg.LeaveTimeout, prefix+"memberlist.leave-timeout", 20*time.Second, "Timeout for leaving memberlist cluster.")
	f.DurationVar(&cfg.GossipInterval, prefix+"memberlist.gossip-interval", mlDefaults.GossipInterval, "How often to gossip.")
	f.IntVar(&cfg.GossipNodes, prefix+"memberlist.gossip-nodes", mlDefaults.GossipNodes, "How many nodes to gossip to.")
	f.DurationVar(&cfg.PushPullInterval, prefix+"memberlist.pullpush-interval", mlDefaults.PushPullInterval, "How often to use pull/push sync.")
	f.DurationVar(&cfg.GossipToTheDeadTime, prefix+"memberlist.gossip-to-dead-nodes-time", mlDefaults.GossipToTheDeadTime, "How long to keep gossiping to dead nodes, to give them chance to refute their death.")
	f.DurationVar(&cfg.DeadNodeReclaimTime, prefix+"memberlist.dead-node-reclaim-time", mlDefaults.DeadNodeReclaimTime, "How soon can dead node's name be reclaimed with new address. 0 to disable.")
	f.IntVar(&cfg.MessageHistoryBufferBytes, prefix+"memberlist.message-history-buffer-bytes", 0, "How much space to use for keeping received and sent messages in memory for troubleshooting (two buffers). 0 to disable.")
	f.BoolVar(&cfg.EnableCompression, prefix+"memberlist.compression-enabled", mlDefaults.EnableCompression, "Enable message compression. This can be used to reduce bandwidth usage at the cost of slightly more CPU utilization.")
	f.StringVar(&cfg.AdvertiseAddr, prefix+"memberlist.advertise-addr", mlDefaults.AdvertiseAddr, "Gossip address to advertise to other members in the cluster. Used for NAT traversal.")
	f.IntVar(&cfg.AdvertisePort, prefix+"memberlist.advertise-port", mlDefaults.AdvertisePort, "Gossip port to advertise to other members in the cluster. Used for NAT traversal.")
	f.StringVar(&cfg.ClusterLabel, prefix+"memberlist.cluster-label", mlDefaults.Label, "The cluster label is an optional string to include in outbound packets and gossip streams. Other members in the memberlist cluster will discard any message whose label doesn't match the configured one, unless the 'cluster-label-verification-disabled' configuration option is set to true.")
	f.BoolVar(&cfg.ClusterLabelVerificationDisabled, prefix+"memberlist.cluster-label-verification-disabled", mlDefaults.SkipInboundLabelCheck, "When true, memberlist doesn't verify that inbound packets and gossip streams have the cluster label matching the configured one. This verification should be disabled while rolling out the change to the configured cluster label in a live memberlist cluster.")

	cfg.TCPTransport.RegisterFlagsWithPrefix(f, prefix)
}

func (cfg *KVConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "")
}

func generateRandomSuffix(logger log.Logger) string {
	suffix := make([]byte, 4)
	_, err := crypto_rand.Read(suffix)
	if err != nil {
		level.Error(logger).Log("msg", "failed to generate random suffix", "err", err)
		return "error"
	}
	return fmt.Sprintf("%2x", suffix)
}

// KV implements Key-Value store on top of memberlist library. KV store has API similar to kv.Client,
// except methods also need explicit codec for each operation.
// KV is a Service. It needs to be started first, and is only usable once it enters Running state.
// If joining of the cluster if configured, it is done in Running state, and if join fails and Abort flag is set, service
// fails.
type KV struct {
	services.NamedService

	cfg        KVConfig
	logger     log.Logger
	registerer prometheus.Registerer

	// dns discovery provider
	provider DNSProvider

	// Protects access to memberlist and broadcasts fields.
	delegateReady atomic.Bool
	memberlist    *memberlist.Memberlist
	broadcasts    *memberlist.TransmitLimitedQueue

	// KV Store.
	storeMu sync.Mutex
	store   map[string]ValueDesc

	// Codec registry
	codecs map[string]codec.Codec

	// Key watchers
	watchersMu     sync.Mutex
	watchers       map[string][]chan string
	prefixWatchers map[string][]chan string

	// Buffers with sent and received messages. Used for troubleshooting only.
	// New messages are appended, old messages (based on configured size limit) removed from the front.
	messagesMu           sync.Mutex
	sentMessages         []Message
	sentMessagesSize     int
	receivedMessages     []Message
	receivedMessagesSize int
	messageCounter       int // Used to give each message in the sentMessages and receivedMessages a unique ID, for UI.

	// Per-key value update workers
	workersMu       sync.Mutex
	workersChannels map[string]chan valueUpdate

	// closed on shutdown
	shutdown chan struct{}

	// metrics
	numberOfReceivedMessages            prometheus.Counter
	totalSizeOfReceivedMessages         prometheus.Counter
	numberOfInvalidReceivedMessages     prometheus.Counter
	numberOfDroppedMessages             prometheus.Counter
	numberOfPulls                       prometheus.Counter
	numberOfPushes                      prometheus.Counter
	totalSizeOfPulls                    prometheus.Counter
	totalSizeOfPushes                   prometheus.Counter
	numberOfBroadcastMessagesInQueue    prometheus.GaugeFunc
	totalSizeOfBroadcastMessagesInQueue prometheus.Gauge
	numberOfBroadcastMessagesDropped    prometheus.Counter
	casAttempts                         prometheus.Counter
	casFailures                         prometheus.Counter
	casSuccesses                        prometheus.Counter
	watchPrefixDroppedNotifications     *prometheus.CounterVec

	storeValuesDesc        *prometheus.Desc
	storeTombstones        *prometheus.GaugeVec
	storeRemovedTombstones *prometheus.CounterVec

	memberlistMembersCount prometheus.GaugeFunc
	memberlistHealthScore  prometheus.GaugeFunc

	// make this configurable for tests. Default value is fine for normal usage
	// where updates are coming from network, but when running tests with many
	// goroutines using same KV, default can be too low.
	maxCasRetries int
}

// Message describes incoming or outgoing message, and local state after applying incoming message, or state when sending message.
// Fields are exported for templating to work.
type Message struct {
	ID   int       // Unique local ID of the message.
	Time time.Time // Time when message was sent or received.
	Size int       // Message size
	Pair KeyValuePair

	// Following values are computed on the receiving node, based on local state.
	Version uint     // For sent message, which version the message reflects. For received message, version after applying the message.
	Changes []string // List of changes in this message (as computed by *this* node).
}

// ValueDesc stores the value along with it's codec and local version.
type ValueDesc struct {
	// We store the decoded value here to prevent decoding the entire state for every
	// update we receive. Whilst the updates are small and fast to decode,
	// the total state can be quite large.
	// The CAS function is passed a deep copy because it modifies in-place.
	value Mergeable

	// Version (local only) is used to keep track of what we're gossiping about, and invalidate old messages.
	Version uint

	// ID of codec used to write this value. Only used when sending full state.
	CodecID string
}

func (v ValueDesc) Clone() (result ValueDesc) {
	result = v
	if v.value != nil {
		result.value = v.value.Clone()
	}
	return
}

type valueUpdate struct {
	value       []byte
	codec       codec.Codec
	messageSize int
}

func (v ValueDesc) String() string {
	return fmt.Sprintf("version: %d, codec: %s", v.Version, v.CodecID)
}

var (
	// if merge fails because of CAS version mismatch, this error is returned. CAS operation reacts on it
	errVersionMismatch  = errors.New("version mismatch")
	errNoChangeDetected = errors.New("no change detected")
	errTooManyRetries   = errors.New("too many retries")
)

// NewKV creates new gossip-based KV service. Note that service needs to be started, until then it doesn't initialize
// gossiping part. Only after service is in Running state, it is really gossiping. Starting the service will also
// trigger connecting to the existing memberlist cluster. If that fails and AbortIfJoinFails is true, error is returned
// and service enters Failed state.
func NewKV(cfg KVConfig, logger log.Logger, dnsProvider DNSProvider, registerer prometheus.Registerer) *KV {
	cfg.TCPTransport.MetricsNamespace = cfg.MetricsNamespace

	mlkv := &KV{
		cfg:             cfg,
		logger:          logger,
		registerer:      registerer,
		provider:        dnsProvider,
		store:           make(map[string]ValueDesc),
		codecs:          make(map[string]codec.Codec),
		watchers:        make(map[string][]chan string),
		prefixWatchers:  make(map[string][]chan string),
		workersChannels: make(map[string]chan valueUpdate),
		shutdown:        make(chan struct{}),
		maxCasRetries:   maxCasRetries,
	}

	mlkv.createAndRegisterMetrics()

	for _, c := range cfg.Codecs {
		mlkv.codecs[c.CodecID()] = c
	}

	mlkv.NamedService = services.NewBasicService(mlkv.starting, mlkv.running, mlkv.stopping).WithName("memberlist_kv")

	return mlkv
}

func defaultMemberlistConfig() *memberlist.Config {
	return memberlist.DefaultLANConfig()
}

func (m *KV) buildMemberlistConfig() (*memberlist.Config, error) {
	tr, err := NewTCPTransport(m.cfg.TCPTransport, m.logger, m.registerer)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %v", err)
	}

	mlCfg := defaultMemberlistConfig()
	mlCfg.Delegate = m

	mlCfg.TCPTimeout = m.cfg.StreamTimeout
	mlCfg.RetransmitMult = m.cfg.RetransmitMult
	mlCfg.PushPullInterval = m.cfg.PushPullInterval
	mlCfg.GossipInterval = m.cfg.GossipInterval
	mlCfg.GossipNodes = m.cfg.GossipNodes
	mlCfg.GossipToTheDeadTime = m.cfg.GossipToTheDeadTime
	mlCfg.DeadNodeReclaimTime = m.cfg.DeadNodeReclaimTime
	mlCfg.EnableCompression = m.cfg.EnableCompression

	mlCfg.AdvertiseAddr = m.cfg.AdvertiseAddr
	mlCfg.AdvertisePort = m.cfg.AdvertisePort

	mlCfg.Label = m.cfg.ClusterLabel
	mlCfg.SkipInboundLabelCheck = m.cfg.ClusterLabelVerificationDisabled

	if m.cfg.NodeName != "" {
		mlCfg.Name = m.cfg.NodeName
	}
	if m.cfg.RandomizeNodeName {
		mlCfg.Name = mlCfg.Name + "-" + generateRandomSuffix(m.logger)
	}

	mlCfg.LogOutput = newMemberlistLoggerAdapter(m.logger, false)
	mlCfg.Transport = tr

	// Memberlist uses UDPBufferSize to figure out how many messages it can put into single "packet".
	// As we don't use UDP for sending packets, we can use higher value here.
	mlCfg.UDPBufferSize = 10 * 1024 * 1024

	// For our use cases, we don't need a very fast detection of dead nodes. Since we use a TCP transport
	// and we open a new TCP connection for each packet, we prefer to reduce the probe frequency and increase
	// the timeout compared to defaults.
	mlCfg.ProbeInterval = 5 * time.Second // Probe a random node every this interval. This setting is also the total timeout for the direct + indirect probes.
	mlCfg.ProbeTimeout = 2 * time.Second  // Timeout for the direct probe.

	// Since we use a custom transport based on TCP, having TCP-based fallbacks doesn't give us any benefit.
	// On the contrary, if we keep TCP pings enabled, each node will effectively run 2x pings against a dead
	// node, because the TCP-based fallback will always trigger.
	mlCfg.DisableTcpPings = true

	level.Info(m.logger).Log("msg", "Using memberlist cluster label and node name", "cluster_label", mlCfg.Label, "node", mlCfg.Name)

	return mlCfg, nil
}

func (m *KV) starting(ctx context.Context) error {
	mlCfg, err := m.buildMemberlistConfig()
	if err != nil {
		return err
	}

	// Wait for memberlist and broadcasts fields creation because
	// memberlist may start calling delegate methods if it
	// receives traffic.
	// See https://godoc.org/github.com/hashicorp/memberlist#Delegate
	//
	// Note: We cannot check for Starting state, as we want to use delegate during cluster joining process
	// that happens in Starting state.
	list, err := memberlist.Create(mlCfg)
	if err != nil {
		return fmt.Errorf("failed to create memberlist: %v", err)
	}
	// Finish delegate initialization.
	m.memberlist = list
	m.broadcasts = &memberlist.TransmitLimitedQueue{
		NumNodes:       list.NumMembers,
		RetransmitMult: mlCfg.RetransmitMult,
	}
	m.delegateReady.Store(true)

	// Try to fast-join memberlist cluster in Starting state, so that we don't start with empty KV store.
	if len(m.cfg.JoinMembers) > 0 {
		m.fastJoinMembersOnStartup(ctx)
	}

	return nil
}

var errFailedToJoinCluster = errors.New("failed to join memberlist cluster on startup")

func (m *KV) running(ctx context.Context) error {
	ok := m.joinMembersOnStartup(ctx)
	if !ok && m.cfg.AbortIfJoinFails {
		return errFailedToJoinCluster
	}

	var tickerChan <-chan time.Time
	if m.cfg.RejoinInterval > 0 && len(m.cfg.JoinMembers) > 0 {
		t := time.NewTicker(m.cfg.RejoinInterval)
		defer t.Stop()

		tickerChan = t.C
	}

	logger := log.With(m.logger, "phase", "periodic_rejoin")
	for {
		select {
		case <-tickerChan:
			const numAttempts = 1 // don't retry if resolution fails, we will try again next time
			reached, err := m.joinMembersWithRetries(ctx, numAttempts, logger)
			if err == nil {
				level.Info(logger).Log("msg", "re-joined memberlist cluster", "reached_nodes", reached)
			} else {
				// Don't report error from rejoin, otherwise KV service would be stopped completely.
				level.Warn(logger).Log("msg", "re-joining memberlist cluster failed", "err", err, "next_try_in", m.cfg.RejoinInterval)
			}

		case <-ctx.Done():
			return nil
		}
	}
}

// GetCodec returns codec for given ID or nil.
func (m *KV) GetCodec(codecID string) codec.Codec {
	return m.codecs[codecID]
}

// GetListeningPort returns port used for listening for memberlist communication. Useful when BindPort is set to 0.
// This call is only valid after KV service has been started.
func (m *KV) GetListeningPort() int {
	return int(m.memberlist.LocalNode().Port)
}

// JoinMembers joins the cluster with given members.
// See https://godoc.org/github.com/hashicorp/memberlist#Memberlist.Join
// This call is only valid after KV service has been started and is still running.
func (m *KV) JoinMembers(members []string) (int, error) {
	if m.State() != services.Running {
		return 0, fmt.Errorf("service not Running")
	}
	return m.memberlist.Join(members)
}

// fastJoinMembersOnStartup attempts to reach small subset of nodes (computed as RetransmitMult * log10(number of discovered members + 1)).
func (m *KV) fastJoinMembersOnStartup(ctx context.Context) {
	startTime := time.Now()

	nodes := m.discoverMembers(ctx, m.cfg.JoinMembers)
	math_rand.Shuffle(len(nodes), func(i, j int) {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	})

	// This is the same formula as used by memberlist for number of nodes that a single message should be gossiped to.
	toJoin := m.cfg.RetransmitMult * int(math.Ceil(math.Log10(float64(len(nodes)+1))))

	level.Info(m.logger).Log("msg", "memberlist fast-join starting", "nodes_found", len(nodes), "to_join", toJoin)

	totalJoined := 0
	for toJoin > 0 && len(nodes) > 0 && ctx.Err() == nil {
		reached, err := m.memberlist.Join(nodes[0:1]) // Try to join single node only.
		if err != nil {
			level.Debug(m.logger).Log("msg", "fast-joining node failed", "node", nodes[0], "err", err)
		}

		totalJoined += reached
		toJoin -= reached

		nodes = nodes[1:]
	}

	l := level.Info(m.logger)
	// Warn, if we didn't join any node.
	if totalJoined == 0 {
		l = level.Warn(m.logger)
	}
	l.Log("msg", "memberlist fast-join finished", "joined_nodes", totalJoined, "elapsed_time", time.Since(startTime))
}

// The joinMembersOnStartup method resolves the addresses of the given join_members hosts and asks memberlist to join to them.
// This method cannot be called before KV.running state as it may wait for K8S DNS to resolve the service addresses of members
// running this very method. Which means the service needs to be READY for K8S to add it to DNS.
func (m *KV) joinMembersOnStartup(ctx context.Context) bool {
	if len(m.cfg.JoinMembers) == 0 {
		return true
	}

	logger := log.With(m.logger, "phase", "startup")
	level.Info(logger).Log("msg", "joining memberlist cluster", "join_members", strings.Join(m.cfg.JoinMembers, ","))
	startTime := time.Now()
	reached, err := m.joinMembersWithRetries(ctx, m.cfg.MaxJoinRetries, logger)
	if err != nil {
		level.Error(logger).Log("msg", "joining memberlist cluster failed", "err", err, "elapsed_time", time.Since(startTime))
		return false
	}
	level.Info(logger).Log("msg", "joining memberlist cluster succeeded", "reached_nodes", reached, "elapsed_time", time.Since(startTime))
	return true
}

// joinMembersWithRetries joins m.cfg.JoinMembers 100 at a time. After each batch of 100 it rediscoveres the members.
// This helps when the list of members is big and by the time we reach the end the originally resolved addresses may be obsolete.
// joinMembersWithRetries returns an error iff it couldn't successfully join any node OR the context was cancelled.
func (m *KV) joinMembersWithRetries(ctx context.Context, numAttempts int, logger log.Logger) (int, error) {
	var (
		cfg = backoff.Config{
			MinBackoff: m.cfg.MinJoinBackoff,
			MaxBackoff: m.cfg.MaxJoinBackoff,
			MaxRetries: numAttempts,
		}
		boff               = backoff.New(ctx, cfg)
		err                error
		successfullyJoined = 0
	)

	for ; boff.Ongoing(); boff.Wait() {
		successfullyJoined, err = m.joinMembersInBatches(ctx)
		if successfullyJoined > 0 {
			// If there are _some_ successful joins, then we can consider the join done.
			// Mimicking the Join semantics we return an error only when we couldn't join any node at all
			err = nil
			break
		}
		level.Warn(logger).Log("msg", "joining memberlist cluster", "attempts", boff.NumRetries()+1, "max_attempts", numAttempts, "err", err)
	}
	if err == nil && boff.Err() != nil {
		err = fmt.Errorf("joining memberlist: %w", boff.Err())
	}

	return successfullyJoined, err
}

// joinMembersInBatches joins m.cfg.JoinMembers and re-resolves the address of m.cfg.JoinMembers after joining 100 nodes.
// joinMembersInBatches returns the number of nodes joined. joinMembersInBatches returns an error only when the
// number of joined nodes is 0.
func (m *KV) joinMembersInBatches(ctx context.Context) (int, error) {
	const batchSize = 100
	var (
		attemptedNodes     = make(map[string]bool)
		successfullyJoined = 0
		lastErr            error
		batch              = make([]string, batchSize)
		nodes              []string
	)
	for moreAvailableNodes := true; ctx.Err() == nil && moreAvailableNodes; {
		// Rediscover nodes and try to join a subset of them with each batch.
		// When the list of nodes is large by the time we reach the end of the list some of the
		// IPs can be unreachable.
		newlyResolved := m.discoverMembers(ctx, m.cfg.JoinMembers)
		if len(newlyResolved) > 0 {
			// If the resolution fails we keep using the nodes list from the last resolution.
			// If that failed too, then we fail the join attempt.
			nodes = newlyResolved
		}

		// Prepare batch
		batch = batch[:0]
		moreAvailableNodes = false
		for _, n := range nodes {
			if attemptedNodes[n] {
				continue
			}
			if len(batch) >= batchSize {
				moreAvailableNodes = true
				break
			}
			batch = append(batch, n)
			attemptedNodes[n] = true
		}

		// Join batch
		joinedInBatch, err := m.joinMembersBatch(ctx, batch)
		if err != nil {
			lastErr = err
		}
		successfullyJoined += joinedInBatch
	}
	if successfullyJoined > 0 {
		return successfullyJoined, nil
	}
	if successfullyJoined == 0 && lastErr == nil {
		return 0, errors.New("found no nodes to join")
	}
	return 0, lastErr
}

// joinMembersBatch returns an error only if it couldn't successfully join any nodes or if ctx is cancelled.
func (m *KV) joinMembersBatch(ctx context.Context, nodes []string) (successfullyJoined int, lastErr error) {
	for nodeIdx := range nodes {
		if ctx.Err() != nil {
			return successfullyJoined, fmt.Errorf("joining batch: %w", context.Cause(ctx))
		}
		// Attempt to join a single node.
		// The cost of calling Join shouldn't be different between passing all nodes in one invocation versus passing a single node per invocation.
		reached, err := m.memberlist.Join(nodes[nodeIdx : nodeIdx+1])
		successfullyJoined += reached
		if err != nil {
			lastErr = err
		}
	}
	if successfullyJoined > 0 {
		lastErr = nil
	}
	return successfullyJoined, lastErr
}

// Provides a dns-based member disovery to join a memberlist cluster w/o knowning members' addresses upfront.
func (m *KV) discoverMembers(ctx context.Context, members []string) []string {
	if len(members) == 0 {
		return nil
	}

	var ms, resolve []string

	for _, member := range members {
		if strings.Contains(member, "+") {
			resolve = append(resolve, member)
		} else {
			// No DNS SRV record to lookup, just append member
			ms = append(ms, member)
		}
	}

	err := m.provider.Resolve(ctx, resolve)
	if err != nil {
		level.Error(m.logger).Log("msg", "failed to resolve members", "addrs", strings.Join(resolve, ","), "err", err)
	}

	ms = append(ms, m.provider.Addresses()...)

	return ms
}

// While Stopping, we try to leave memberlist cluster and then shutdown memberlist client.
// We do this in order to send out last messages, typically that ingester has LEFT the ring.
func (m *KV) stopping(_ error) error {
	level.Info(m.logger).Log("msg", "leaving memberlist cluster")

	// Wait until broadcast queue is empty, but don't wait for too long.
	// Also don't wait if there is just one node left.
	// Problem is that broadcast queue is also filled up by state changes received from other nodes,
	// so it may never be empty in a busy cluster. However, we generally only care about messages
	// generated on this node via CAS, and those are disabled now (via casBroadcastsEnabled), and should be able
	// to get out in this timeout.

	waitTimeout := time.Now().Add(10 * time.Second)
	for m.broadcasts.NumQueued() > 0 && m.memberlist.NumMembers() > 1 && time.Now().Before(waitTimeout) {
		time.Sleep(250 * time.Millisecond)
	}

	if cnt := m.broadcasts.NumQueued(); cnt > 0 {
		level.Warn(m.logger).Log("msg", "broadcast messages left in queue", "count", cnt, "nodes", m.memberlist.NumMembers())
	}

	err := m.memberlist.Leave(m.cfg.LeaveTimeout)
	if err != nil {
		level.Error(m.logger).Log("msg", "error when leaving memberlist cluster", "err", err)
	}

	close(m.shutdown)

	err = m.memberlist.Shutdown()
	if err != nil {
		level.Error(m.logger).Log("msg", "error when shutting down memberlist client", "err", err)
	}
	return nil
}

// List returns all known keys under a given prefix.
// No communication with other nodes in the cluster is done here.
func (m *KV) List(prefix string) []string {
	m.storeMu.Lock()
	defer m.storeMu.Unlock()

	var keys []string
	for k := range m.store {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys
}

// Get returns current value associated with given key.
// No communication with other nodes in the cluster is done here.
func (m *KV) Get(key string, codec codec.Codec) (interface{}, error) {
	val, _, err := m.get(key, codec)
	return val, err
}

// Returns current value with removed tombstones.
func (m *KV) get(key string, _ codec.Codec) (out interface{}, version uint, err error) {
	m.storeMu.Lock()
	v := m.store[key].Clone()
	m.storeMu.Unlock()

	if v.value != nil {
		// remove ALL tombstones before returning to client.
		// No need for clients to see them.
		_, _ = v.value.RemoveTombstones(time.Time{})
	}

	return v.value, v.Version, nil
}

// WatchKey watches for value changes for given key. When value changes, 'f' function is called with the
// latest value. Notifications that arrive while 'f' is running are coalesced into one subsequent 'f' call.
//
// Watching ends when 'f' returns false, context is done, or this client is shut down.
func (m *KV) WatchKey(ctx context.Context, key string, codec codec.Codec, f func(interface{}) bool) {
	// keep one extra notification, to avoid missing notification if we're busy running the function
	w := make(chan string, 1)

	// register watcher
	m.watchersMu.Lock()
	m.watchers[key] = append(m.watchers[key], w)
	m.watchersMu.Unlock()

	defer func() {
		// unregister watcher on exit
		m.watchersMu.Lock()
		defer m.watchersMu.Unlock()

		removeWatcherChannel(key, w, m.watchers)
	}()

	for {
		select {
		case <-w:
			// value changed
			val, _, err := m.get(key, codec)
			if err != nil {
				level.Warn(m.logger).Log("msg", "failed to decode value while watching for changes", "key", key, "err", err)
				continue
			}

			if !f(val) {
				return
			}

		case <-m.shutdown:
			// stop watching on shutdown
			return

		case <-ctx.Done():
			return
		}
	}
}

// WatchPrefix watches for any change of values stored under keys with given prefix. When change occurs,
// function 'f' is called with key and current value.
// Each change of the key results in one notification. If there are too many pending notifications ('f' is slow),
// some notifications may be lost.
//
// Watching ends when 'f' returns false, context is done, or this client is shut down.
func (m *KV) WatchPrefix(ctx context.Context, prefix string, codec codec.Codec, f func(string, interface{}) bool) {
	// we use bigger buffer here, since keys are interesting and we don't want to lose them.
	w := make(chan string, 16)

	// register watcher
	m.watchersMu.Lock()
	m.prefixWatchers[prefix] = append(m.prefixWatchers[prefix], w)
	m.watchersMu.Unlock()

	defer func() {
		// unregister watcher on exit
		m.watchersMu.Lock()
		defer m.watchersMu.Unlock()

		removeWatcherChannel(prefix, w, m.prefixWatchers)
	}()

	for {
		select {
		case key := <-w:
			val, _, err := m.get(key, codec)
			if err != nil {
				level.Warn(m.logger).Log("msg", "failed to decode value while watching for changes", "key", key, "err", err)
				continue
			}

			if !f(key, val) {
				return
			}

		case <-m.shutdown:
			// stop watching on shutdown
			return

		case <-ctx.Done():
			return
		}
	}
}

func removeWatcherChannel(k string, w chan string, watchers map[string][]chan string) {
	ws := watchers[k]
	for ix, kw := range ws {
		if kw == w {
			ws = append(ws[:ix], ws[ix+1:]...)
			break
		}
	}

	if len(ws) > 0 {
		watchers[k] = ws
	} else {
		delete(watchers, k)
	}
}

func (m *KV) notifyWatchers(key string) {
	m.watchersMu.Lock()
	defer m.watchersMu.Unlock()

	for _, kw := range m.watchers[key] {
		select {
		case kw <- key:
			// notification sent.
		default:
			// cannot send notification to this watcher at the moment
			// but since this is a buffered channel, it means that
			// there is already a pending notification anyway
		}
	}

	for p, ws := range m.prefixWatchers {
		if strings.HasPrefix(key, p) {
			for _, pw := range ws {
				select {
				case pw <- key:
					// notification sent.
				default:
					c, _ := m.watchPrefixDroppedNotifications.GetMetricWithLabelValues(p)
					if c != nil {
						c.Inc()
					}

					level.Warn(m.logger).Log("msg", "failed to send notification to prefix watcher", "prefix", p)
				}
			}
		}
	}
}

// CAS implements Compare-And-Set/Swap operation.
//
// CAS expects that value returned by 'f' function implements Mergeable interface. If it doesn't, CAS fails immediately.
//
// This method combines Compare-And-Swap with Merge: it calls 'f' function to get a new state, and then merges this
// new state into current state, to find out what the change was. Resulting updated current state is then CAS-ed to
// KV store, and change is broadcast to cluster peers. Merge function is called with CAS flag on, so that it can
// detect removals. If Merge doesn't result in any change (returns nil), then operation fails and is retried again.
// After too many failed retries, this method returns error.
func (m *KV) CAS(ctx context.Context, key string, codec codec.Codec, f func(in interface{}) (out interface{}, retry bool, err error)) error {
	var lastError error

outer:
	for retries := m.maxCasRetries; retries > 0; retries-- {
		m.casAttempts.Inc()

		if lastError == errNoChangeDetected {
			// We only get here, if 'f' reports some change, but Merge function reports no change. This can happen
			// with Ring's merge function, which depends on timestamps (and not the tokens) with 1-second resolution.
			// By waiting for one second, we hope that Merge will be able to detect change from 'f' function.

			select {
			case <-time.After(noChangeDetectedRetrySleep):
				// ok
			case <-ctx.Done():
				lastError = ctx.Err()
				break outer
			}
		}

		change, newver, retry, err := m.trySingleCas(key, codec, f)
		if err != nil {
			level.Debug(m.logger).Log("msg", "CAS attempt failed", "err", err, "retry", retry)

			lastError = err
			if !retry {
				break
			}
			continue
		}

		if change != nil {
			m.casSuccesses.Inc()
			m.notifyWatchers(key)

			if m.State() == services.Running {
				m.broadcastNewValue(key, change, newver, codec)
			} else {
				level.Warn(m.logger).Log("msg", "skipped broadcasting CAS update because memberlist KV is shutting down", "key", key)
			}
		}

		return nil
	}

	if lastError == errVersionMismatch {
		// this is more likely error than version mismatch.
		lastError = errTooManyRetries
	}

	m.casFailures.Inc()
	return fmt.Errorf("failed to CAS-update key %s: %v", key, lastError)
}

// returns change, error (or nil, if CAS succeeded), and whether to retry or not.
// returns errNoChangeDetected if merge failed to detect change in f's output.
func (m *KV) trySingleCas(key string, codec codec.Codec, f func(in interface{}) (out interface{}, retry bool, err error)) (Mergeable, uint, bool, error) {
	val, ver, err := m.get(key, codec)
	if err != nil {
		return nil, 0, false, fmt.Errorf("failed to get value: %v", err)
	}

	out, retry, err := f(val)
	if err != nil {
		return nil, 0, retry, fmt.Errorf("fn returned error: %v", err)
	}

	if out == nil {
		// no change to be done
		return nil, 0, false, nil
	}

	// Don't even try
	r, ok := out.(Mergeable)
	if !ok || r == nil {
		return nil, 0, retry, fmt.Errorf("invalid type: %T, expected Mergeable", out)
	}

	// To support detection of removed items from value, we will only allow CAS operation to
	// succeed if version hasn't changed, i.e. state hasn't changed since running 'f'.
	change, newver, err := m.mergeValueForKey(key, r, ver, codec)
	if err == errVersionMismatch {
		return nil, 0, retry, err
	}

	if err != nil {
		return nil, 0, retry, fmt.Errorf("merge failed: %v", err)
	}

	if newver == 0 {
		// CAS method reacts on this error
		return nil, 0, retry, errNoChangeDetected
	}

	return change, newver, retry, nil
}

func (m *KV) broadcastNewValue(key string, change Mergeable, version uint, codec codec.Codec) {
	data, err := codec.Encode(change)
	if err != nil {
		level.Error(m.logger).Log("msg", "failed to encode change", "key", key, "version", version, "err", err)
		m.numberOfBroadcastMessagesDropped.Inc()
		return
	}

	kvPair := KeyValuePair{Key: key, Value: data, Codec: codec.CodecID()}
	pairData, err := kvPair.Marshal()
	if err != nil {
		level.Error(m.logger).Log("msg", "failed to serialize KV pair", "key", key, "version", version, "err", err)
		m.numberOfBroadcastMessagesDropped.Inc()
		return
	}

	m.addSentMessage(Message{
		Time:    time.Now(),
		Size:    len(pairData),
		Pair:    kvPair,
		Version: version,
		Changes: change.MergeContent(),
	})

	m.queueBroadcast(key, change.MergeContent(), version, pairData)
}

// NodeMeta is method from Memberlist Delegate interface
func (m *KV) NodeMeta(_ int) []byte {
	// we can send local state from here (512 bytes only)
	// if state is updated, we need to tell memberlist to distribute it.
	return nil
}

// NotifyMsg is method from Memberlist Delegate interface
// Called when single message is received, i.e. what our broadcastNewValue has sent.
func (m *KV) NotifyMsg(msg []byte) {
	if !m.delegateReady.Load() {
		return
	}

	m.numberOfReceivedMessages.Inc()
	m.totalSizeOfReceivedMessages.Add(float64(len(msg)))

	kvPair := KeyValuePair{}
	err := kvPair.Unmarshal(msg)
	if err != nil {
		level.Warn(m.logger).Log("msg", "failed to unmarshal received KV Pair", "err", err)
		m.numberOfInvalidReceivedMessages.Inc()
		return
	}

	if len(kvPair.Key) == 0 {
		level.Warn(m.logger).Log("msg", "received an invalid KV Pair (empty key)")
		m.numberOfInvalidReceivedMessages.Inc()
		return
	}

	codec := m.GetCodec(kvPair.GetCodec())
	if codec == nil {
		m.numberOfInvalidReceivedMessages.Inc()
		level.Error(m.logger).Log("msg", "failed to decode received value, unknown codec", "codec", kvPair.GetCodec())
		return
	}

	ch := m.getKeyWorkerChannel(kvPair.Key)
	select {
	case ch <- valueUpdate{value: kvPair.Value, codec: codec, messageSize: len(msg)}:
	default:
		m.numberOfDroppedMessages.Inc()
		level.Warn(m.logger).Log("msg", "notify queue full, dropping message", "key", kvPair.Key)
	}
}

func (m *KV) getKeyWorkerChannel(key string) chan<- valueUpdate {
	m.workersMu.Lock()
	defer m.workersMu.Unlock()

	ch := m.workersChannels[key]
	if ch == nil {
		// spawn a key associated worker goroutine to process updates in background
		ch = make(chan valueUpdate, notifyMsgQueueSize)
		go m.processValueUpdate(ch, key)

		m.workersChannels[key] = ch
	}
	return ch
}

func (m *KV) processValueUpdate(workerCh <-chan valueUpdate, key string) {
	for {
		select {
		case update := <-workerCh:
			// we have a value update! Let's merge it with our current version for given key
			mod, version, err := m.mergeBytesValueForKey(key, update.value, update.codec)

			changes := []string(nil)
			if mod != nil {
				changes = mod.MergeContent()
			}

			m.addReceivedMessage(Message{
				Time: time.Now(),
				Size: update.messageSize,
				Pair: KeyValuePair{
					Key:   key,
					Value: update.value,
					Codec: update.codec.CodecID(),
				},
				Version: version,
				Changes: changes,
			})

			if err != nil {
				level.Error(m.logger).Log("msg", "failed to store received value", "key", key, "err", err)
			} else if version > 0 {
				m.notifyWatchers(key)

				// Don't resend original message, but only changes.
				m.broadcastNewValue(key, mod, version, update.codec)
			}

		case <-m.shutdown:
			// stop running on shutdown
			return
		}
	}
}

func (m *KV) queueBroadcast(key string, content []string, version uint, message []byte) {
	l := len(message)

	b := ringBroadcast{
		key:     key,
		content: content,
		version: version,
		msg:     message,
		finished: func(b ringBroadcast) {
			m.totalSizeOfBroadcastMessagesInQueue.Sub(float64(l))
		},
		logger: m.logger,
	}

	m.totalSizeOfBroadcastMessagesInQueue.Add(float64(l))
	m.broadcasts.QueueBroadcast(b)
}

// GetBroadcasts is method from Memberlist Delegate interface
// It returns all pending broadcasts (within the size limit)
func (m *KV) GetBroadcasts(overhead, limit int) [][]byte {
	if !m.delegateReady.Load() {
		return nil
	}

	return m.broadcasts.GetBroadcasts(overhead, limit)
}

// LocalState is method from Memberlist Delegate interface
//
// This is "pull" part of push/pull sync (either periodic, or when new node joins the cluster).
// Here we dump our entire state -- all keys and their values. There is no limit on message size here,
// as Memberlist uses 'stream' operations for transferring this state.
func (m *KV) LocalState(_ bool) []byte {
	if !m.delegateReady.Load() {
		return nil
	}

	m.numberOfPulls.Inc()

	m.storeMu.Lock()
	defer m.storeMu.Unlock()

	// For each Key/Value pair in our store, we write
	// [4-bytes length of marshalled KV pair] [marshalled KV pair]

	buf := bytes.Buffer{}
	sent := time.Now()

	kvPair := KeyValuePair{}
	for key, val := range m.store {
		if val.value == nil {
			continue
		}

		codec := m.GetCodec(val.CodecID)
		if codec == nil {
			level.Error(m.logger).Log("msg", "failed to encode remote state: unknown codec for key", "codec", val.CodecID, "key", key)
			continue
		}

		encoded, err := codec.Encode(val.value)
		if err != nil {
			level.Error(m.logger).Log("msg", "failed to encode remote state", "err", err)
			continue
		}

		kvPair.Reset()
		kvPair.Key = key
		kvPair.Value = encoded
		kvPair.Codec = val.CodecID

		ser, err := kvPair.Marshal()
		if err != nil {
			level.Error(m.logger).Log("msg", "failed to serialize KV Pair", "err", err)
			continue
		}

		if uint(len(ser)) > math.MaxUint32 {
			level.Error(m.logger).Log("msg", "value too long", "key", key, "value_length", len(encoded))
			continue
		}

		err = binary.Write(&buf, binary.BigEndian, uint32(len(ser)))
		if err != nil {
			level.Error(m.logger).Log("msg", "failed to write uint32 to buffer?", "err", err)
			continue
		}
		buf.Write(ser)

		m.addSentMessage(Message{
			Time:    sent,
			Size:    len(ser),
			Pair:    kvPair, // Makes a copy of kvPair.
			Version: val.Version,
		})
	}

	m.totalSizeOfPulls.Add(float64(buf.Len()))
	return buf.Bytes()
}

// MergeRemoteState is a method from the Memberlist Delegate interface.
//
// This is 'push' part of push/pull sync. We merge incoming KV store (all keys and values) with ours.
//
// Data is full state of remote KV store, as generated by LocalState method (run on another node).
func (m *KV) MergeRemoteState(data []byte, _ bool) {
	if !m.delegateReady.Load() {
		return
	}

	received := time.Now()

	m.numberOfPushes.Inc()
	m.totalSizeOfPushes.Add(float64(len(data)))

	kvPair := KeyValuePair{}

	var err error
	// Data contains individual KV pairs (encoded as protobuf messages), each prefixed with 4 bytes length of KV pair:
	// [4-bytes length of marshalled KV pair] [marshalled KV pair] [4-bytes length] [KV pair]...
	for len(data) > 0 {
		if len(data) < 4 {
			err = fmt.Errorf("not enough data left for another KV Pair: %d", len(data))
			break
		}

		kvPairLength := binary.BigEndian.Uint32(data)

		data = data[4:]

		if len(data) < int(kvPairLength) {
			err = fmt.Errorf("not enough data left for next KV Pair, expected %d, remaining %d bytes", kvPairLength, len(data))
			break
		}

		kvPair.Reset()
		err = kvPair.Unmarshal(data[:kvPairLength])
		if err != nil {
			err = fmt.Errorf("failed to parse KV Pair: %v", err)
			break
		}

		data = data[kvPairLength:]

		codec := m.GetCodec(kvPair.GetCodec())
		if codec == nil {
			level.Error(m.logger).Log("msg", "failed to parse remote state: unknown codec for key", "codec", kvPair.GetCodec(), "key", kvPair.GetKey())
			continue
		}

		// we have both key and value, try to merge it with our state
		change, newver, err := m.mergeBytesValueForKey(kvPair.Key, kvPair.Value, codec)

		changes := []string(nil)
		if change != nil {
			changes = change.MergeContent()
		}

		m.addReceivedMessage(Message{
			Time:    received,
			Size:    int(kvPairLength),
			Pair:    kvPair, // Makes a copy of kvPair.
			Version: newver,
			Changes: changes,
		})

		if err != nil {
			level.Error(m.logger).Log("msg", "failed to store received value", "key", kvPair.Key, "err", err)
		} else if newver > 0 {
			m.notifyWatchers(kvPair.Key)
			m.broadcastNewValue(kvPair.Key, change, newver, codec)
		}
	}

	if err != nil {
		level.Error(m.logger).Log("msg", "failed to parse remote state", "err", err)
	}
}

func (m *KV) mergeBytesValueForKey(key string, incomingData []byte, codec codec.Codec) (Mergeable, uint, error) {
	decodedValue, err := codec.Decode(incomingData)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to decode value: %v", err)
	}

	incomingValue, ok := decodedValue.(Mergeable)
	if !ok {
		return nil, 0, fmt.Errorf("expected Mergeable, got: %T", decodedValue)
	}

	return m.mergeValueForKey(key, incomingValue, 0, codec)
}

// Merges incoming value with value we have in our store. Returns "a change" that can be sent to other
// cluster members to update their state, and new version of the value.
// If CAS version is specified, then merging will fail if state has changed already, and errVersionMismatch is reported.
// If no modification occurred, new version is 0.
func (m *KV) mergeValueForKey(key string, incomingValue Mergeable, casVersion uint, codec codec.Codec) (Mergeable, uint, error) {
	m.storeMu.Lock()
	defer m.storeMu.Unlock()

	// Note that we do not take a deep copy of curr.value here, it is modified in-place.
	// This is safe because the entire function runs under the store lock; we do not return
	// the full state anywhere as is done elsewhere (i.e. Get/WatchKey/CAS).
	curr := m.store[key]
	// if casVersion is 0, then there was no previous value, so we will just do normal merge, without localCAS flag set.
	if casVersion > 0 && curr.Version != casVersion {
		return nil, 0, errVersionMismatch
	}
	result, change, err := computeNewValue(incomingValue, curr.value, casVersion > 0)
	if err != nil {
		return nil, 0, err
	}

	// No change, don't store it.
	if change == nil || len(change.MergeContent()) == 0 {
		return nil, 0, nil
	}

	if m.cfg.LeftIngestersTimeout > 0 {
		limit := time.Now().Add(-m.cfg.LeftIngestersTimeout)
		total, removed := result.RemoveTombstones(limit)
		m.storeTombstones.WithLabelValues(key).Set(float64(total))
		m.storeRemovedTombstones.WithLabelValues(key).Add(float64(removed))

		// Remove tombstones from change too. If change turns out to be empty after this,
		// we don't need to gossip the change. However, the local value will be always be updated.
		//
		// Note that "result" and "change" may actually be the same Mergeable. That is why we
		// call RemoveTombstones on "result" first, so that we get the correct metrics. Calling
		// RemoveTombstones twice with same limit should be noop.
		change.RemoveTombstones(limit)
		if len(change.MergeContent()) == 0 {
			return nil, 0, nil
		}
	}

	newVersion := curr.Version + 1
	m.store[key] = ValueDesc{
		value:   result,
		Version: newVersion,
		CodecID: codec.CodecID(),
	}

	// The "changes" returned by Merge() can contain references to the "result"
	// state. Therefore, make sure we clone it before releasing the lock.
	change = change.Clone()

	return change, newVersion, nil
}

// returns [result, change, error]
func computeNewValue(incoming Mergeable, oldVal Mergeable, cas bool) (Mergeable, Mergeable, error) {
	if oldVal == nil {
		return incoming, incoming, nil
	}

	// otherwise we have two mergeables, so merge them
	change, err := oldVal.Merge(incoming, cas)
	return oldVal, change, err
}

func (m *KV) storeCopy() map[string]ValueDesc {
	m.storeMu.Lock()
	defer m.storeMu.Unlock()

	result := make(map[string]ValueDesc, len(m.store))
	for k, v := range m.store {
		result[k] = v.Clone()
	}
	return result
}
func (m *KV) addReceivedMessage(msg Message) {
	if m.cfg.MessageHistoryBufferBytes == 0 {
		return
	}

	m.messagesMu.Lock()
	defer m.messagesMu.Unlock()

	m.messageCounter++
	msg.ID = m.messageCounter

	m.receivedMessages, m.receivedMessagesSize = addMessageToBuffer(m.receivedMessages, m.receivedMessagesSize, m.cfg.MessageHistoryBufferBytes, msg)
}

func (m *KV) addSentMessage(msg Message) {
	if m.cfg.MessageHistoryBufferBytes == 0 {
		return
	}

	m.messagesMu.Lock()
	defer m.messagesMu.Unlock()

	m.messageCounter++
	msg.ID = m.messageCounter

	m.sentMessages, m.sentMessagesSize = addMessageToBuffer(m.sentMessages, m.sentMessagesSize, m.cfg.MessageHistoryBufferBytes, msg)
}

func (m *KV) getSentAndReceivedMessages() (sent, received []Message) {
	m.messagesMu.Lock()
	defer m.messagesMu.Unlock()

	// Make copy of both slices.
	return append([]Message(nil), m.sentMessages...), append([]Message(nil), m.receivedMessages...)
}

func (m *KV) deleteSentReceivedMessages() {
	m.messagesMu.Lock()
	defer m.messagesMu.Unlock()

	m.sentMessages = nil
	m.sentMessagesSize = 0
	m.receivedMessages = nil
	m.receivedMessagesSize = 0
}

func addMessageToBuffer(msgs []Message, size int, limit int, msg Message) ([]Message, int) {
	msgs = append(msgs, msg)
	size += msg.Size

	for len(msgs) > 0 && size > limit {
		size -= msgs[0].Size
		msgs = msgs[1:]
	}

	return msgs, size
}
