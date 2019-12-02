package memberlist

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/hashicorp/memberlist"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

const (
	// We encode key length as one byte
	maxKeyLength = 255
)

// Config for memberlist-based Client
type Config struct {
	// Memberlist options.
	NodeName         string        `yaml:"node_name"`
	StreamTimeout    time.Duration `yaml:"stream_timeout"`
	RetransmitMult   int           `yaml:"retransmit_factor"`
	PushPullInterval time.Duration `yaml:"pull_push_interval"`
	GossipInterval   time.Duration `yaml:"gossip_interval"`
	GossipNodes      int           `yaml:"gossip_nodes"`

	// List of members to join
	JoinMembers      flagext.StringSlice `yaml:"join_members"`
	AbortIfJoinFails bool                `yaml:"abort_if_cluster_join_fails"`

	// Remove LEFT ingesters from ring after this timeout.
	LeftIngestersTimeout time.Duration `yaml:"left_ingesters_timeout"`

	// Timeout used when leaving the memberlist cluster.
	LeaveTimeout time.Duration `yaml:"leave_timeout"`

	TCPTransport TCPTransportConfig `yaml:",inline"`

	// Where to put custom metrics. Metrics are not registered, if this is nil.
	MetricsRegisterer prometheus.Registerer
	MetricsNamespace  string
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet, prefix string) {
	// "Defaults to hostname" -- memberlist sets it to hostname by default.
	f.StringVar(&cfg.NodeName, prefix+"memberlist.nodename", "", "Name of the node in memberlist cluster. Defaults to hostname.") // memberlist.DefaultLANConfig will put hostname here.
	f.IntVar(&cfg.RetransmitMult, prefix+"memberlist.retransmit-factor", 0, "Multiplication factor used when sending out messages (factor * log(N+1)).")
	f.Var(&cfg.JoinMembers, prefix+"memberlist.join", "Other cluster members to join. Can be specified multiple times. Memberlist store is EXPERIMENTAL.")
	f.BoolVar(&cfg.AbortIfJoinFails, prefix+"memberlist.abort-if-join-fails", true, "If this node fails to join memberlist cluster, abort.")
	f.DurationVar(&cfg.LeftIngestersTimeout, prefix+"memberlist.left-ingesters-timeout", 5*time.Minute, "How long to keep LEFT ingesters in the ring.")
	f.DurationVar(&cfg.LeaveTimeout, prefix+"memberlist.leave-timeout", 5*time.Second, "Timeout for leaving memberlist cluster.")
	f.DurationVar(&cfg.GossipInterval, prefix+"memberlist.gossip-interval", 0, "How often to gossip. Uses memberlist LAN defaults if 0.")
	f.IntVar(&cfg.GossipNodes, prefix+"memberlist.gossip-nodes", 0, "How many nodes to gossip to. Uses memberlist LAN defaults if 0.")
	f.DurationVar(&cfg.PushPullInterval, prefix+"memberlist.pullpush-interval", 0, "How often to use pull/push sync. Uses memberlist LAN defaults if 0.")

	cfg.TCPTransport.RegisterFlags(f, prefix)
}

// Client implements kv.Client interface by using memberlist gossiping library.
type Client struct {
	cfg        Config
	memberlist *memberlist.Memberlist
	broadcasts *memberlist.TransmitLimitedQueue
	codec      codec.Codec

	// KV Store.
	storeMu sync.Mutex
	store   map[string]valueDesc

	// Key watchers
	watchersMu     sync.Mutex
	watchers       map[string][]chan string
	prefixWatchers map[string][]chan string

	// closed on shutdown
	shutdown chan struct{}

	// metrics
	numberOfReceivedMessages            prometheus.Counter
	totalSizeOfReceivedMessages         prometheus.Counter
	numberOfInvalidReceivedMessages     prometheus.Counter
	numberOfPulls                       prometheus.Counter
	numberOfPushes                      prometheus.Counter
	totalSizeOfPulls                    prometheus.Counter
	totalSizeOfPushes                   prometheus.Counter
	numberOfBroadcastMessagesInQueue    prometheus.GaugeFunc
	totalSizeOfBroadcastMessagesInQueue prometheus.Gauge
	casAttempts                         prometheus.Counter
	casFailures                         prometheus.Counter
	casSuccesses                        prometheus.Counter
	watchPrefixDroppedNotifications     *prometheus.CounterVec

	storeValuesDesc *prometheus.Desc
	storeSizesDesc  *prometheus.Desc

	memberlistMembersCount    prometheus.GaugeFunc
	memberlistHealthScore     prometheus.GaugeFunc
	memberlistMembersInfoDesc *prometheus.Desc
}

type valueDesc struct {
	// We store bytes here. Reason is that clients calling CAS function will modify the object in place,
	// but unless CAS succeeds, we don't want those modifications to be visible.
	value []byte

	// version (local only) is used to keep track of what we're gossiping about, and invalidate old messages
	version uint
}

var (
	// if merge fails because of CAS version mismatch, this error is returned. CAS operation reacts on it
	errVersionMismatch = errors.New("version mismatch")
)

// NewMemberlistClient creates new Client instance. If cfg.JoinMembers is set, it will also try to connect
// to these members and join the cluster. If that fails and AbortIfJoinFails is true, error is returned and no
// client is created.
func NewMemberlistClient(cfg Config, codec codec.Codec) (*Client, error) {
	level.Warn(util.Logger).Log("msg", "Using memberlist-based KV store is EXPERIMENTAL and not tested in production")

	cfg.TCPTransport.MetricsRegisterer = cfg.MetricsRegisterer
	cfg.TCPTransport.MetricsNamespace = cfg.MetricsNamespace

	tr, err := NewTCPTransport(cfg.TCPTransport)

	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %v", err)
	}

	mlCfg := memberlist.DefaultLANConfig()

	if cfg.StreamTimeout != 0 {
		mlCfg.TCPTimeout = cfg.StreamTimeout
	}
	if cfg.RetransmitMult != 0 {
		mlCfg.RetransmitMult = cfg.RetransmitMult
	}
	if cfg.PushPullInterval != 0 {
		mlCfg.PushPullInterval = cfg.PushPullInterval
	}
	if cfg.GossipInterval != 0 {
		mlCfg.GossipInterval = cfg.GossipInterval
	}
	if cfg.GossipNodes != 0 {
		mlCfg.GossipNodes = cfg.GossipNodes
	}
	if cfg.NodeName != "" {
		mlCfg.Name = cfg.NodeName
	}

	mlCfg.LogOutput = newMemberlistLoggerAdapter(util.Logger, false)
	mlCfg.Transport = tr

	// Memberlist uses UDPBufferSize to figure out how many messages it can put into single "packet".
	// As we don't use UDP for sending packets, we can use higher value here.
	mlCfg.UDPBufferSize = 10 * 1024 * 1024

	memberlistClient := &Client{
		cfg:            cfg,
		store:          make(map[string]valueDesc),
		watchers:       make(map[string][]chan string),
		prefixWatchers: make(map[string][]chan string),
		codec:          codec,
		shutdown:       make(chan struct{}),
	}

	mlCfg.Delegate = memberlistClient

	list, err := memberlist.Create(mlCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create memberlist: %v", err)
	}

	// finish delegate initialization
	memberlistClient.memberlist = list
	memberlistClient.broadcasts = &memberlist.TransmitLimitedQueue{
		NumNodes:       list.NumMembers,
		RetransmitMult: cfg.RetransmitMult,
	}

	// Almost ready...
	memberlistClient.createAndRegisterMetrics()

	// Join the cluster
	if len(cfg.JoinMembers) > 0 {
		reached, err := memberlistClient.JoinMembers(cfg.JoinMembers)
		if err != nil && cfg.AbortIfJoinFails {
			_ = memberlistClient.memberlist.Shutdown()
			return nil, err
		}

		if err != nil {
			level.Error(util.Logger).Log("msg", "Failed to join memberlist cluster", "err", err)
		} else {
			level.Info(util.Logger).Log("msg", "Joined memberlist cluster", "reached_nodes", reached)
		}
	}

	return memberlistClient, nil
}

// GetListeningPort returns port used for listening for memberlist communication. Useful when BindPort is set to 0.
func (m *Client) GetListeningPort() int {
	return int(m.memberlist.LocalNode().Port)
}

// JoinMembers joins the cluster with given members.
// See https://godoc.org/github.com/hashicorp/memberlist#Memberlist.Join
func (m *Client) JoinMembers(members []string) (int, error) {
	return m.memberlist.Join(members)
}

// Stop tries to leave memberlist cluster and then shutdown memberlist client.
// We do this in order to send out last messages, typically that ingester has LEFT the ring.
func (m *Client) Stop() {
	level.Info(util.Logger).Log("msg", "leaving memberlist cluster")

	// TODO: should we empty our broadcast queue before leaving? That would make sure that we have sent out everything we wanted.

	err := m.memberlist.Leave(m.cfg.LeaveTimeout)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error when leaving memberlist cluster", "err", err)
	}

	close(m.shutdown)

	err = m.memberlist.Shutdown()
	if err != nil {
		level.Error(util.Logger).Log("msg", "error when shutting down memberlist client", "err", err)
	}
}

// Get returns current value associated with given key.
// No communication with other nodes in the cluster is done here. Part of kv.Client interface.
func (m *Client) Get(ctx context.Context, key string) (interface{}, error) {
	val, _, err := m.get(key)
	return val, err
}

// Returns current value with removed tombstones.
func (m *Client) get(key string) (out interface{}, version uint, err error) {
	m.storeMu.Lock()
	v := m.store[key]
	m.storeMu.Unlock()

	out = nil
	if v.value != nil {
		out, err = m.codec.Decode(v.value)
		if err != nil {
			return nil, 0, err
		}

		if mr, ok := out.(Mergeable); ok {
			// remove ALL tombstones before returning to client.
			// No need for clients to see them.
			mr.RemoveTombstones(time.Time{})
		}
	}

	return out, v.version, nil
}

// WatchKey watches for value changes for given key. When value changes, 'f' function is called with the
// latest value. Notifications that arrive while 'f' is running are coalesced into one subsequent 'f' call.
//
// Watching ends when 'f' returns false, context is done, or this client is shut down.
//
// Part of kv.Client interface.
func (m *Client) WatchKey(ctx context.Context, key string, f func(interface{}) bool) {
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
			val, _, err := m.get(key)
			if err != nil {
				level.Warn(util.Logger).Log("msg", "failed to decode value while watching for changes", "key", key, "err", err)
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
//
// Part of kv.Client interface.
func (m *Client) WatchPrefix(ctx context.Context, prefix string, f func(string, interface{}) bool) {
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
			val, _, err := m.get(key)
			if err != nil {
				level.Warn(util.Logger).Log("msg", "failed to decode value while watching for changes", "key", key, "err", err)
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

func (m *Client) notifyWatchers(key string) {
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

					level.Warn(util.Logger).Log("msg", "failed to send notification to prefix watcher", "prefix", p)
				}
			}
		}
	}
}

// CAS is part of kv.Client implementation.
//
// CAS expects that value returned by 'f' function implements Mergeable interface. If it doesn't, CAS fails immediately.
//
// This method combines Compare-And-Swap with Merge: it calls 'f' function to get a new state, and then merges this
// new state into current state, to find out what the change was. Resulting updated current state is then CAS-ed to
// KV store, and change is broadcast to cluster peers. Merge function is called with CAS flag on, so that it can
// detect removals. If Merge doesn't result in any change (returns nil), then operation fails and is retried again.
// After too many failed retries, this method returns error.
func (m *Client) CAS(ctx context.Context, key string, f func(in interface{}) (out interface{}, retry bool, err error)) error {
	if len(key) > maxKeyLength {
		return fmt.Errorf("key too long: %d", len(key))
	}

	sleep := false
	for retries := 10; retries > 0; retries-- {
		m.casAttempts.Inc()

		if sleep {
			// We only get here, if 'f' reports some change, but Merge function reports no change. This can happen
			// with Ring's merge function, which depends on timestamps (and not the tokens) with 1-second resolution.
			// By waiting for one second, we hope that Merge will be able to detect change from 'f' function.

			select {
			case <-time.After(1 * time.Second):
				// ok
			case <-ctx.Done():
				break
			}
		}

		val, ver, err := m.get(key)
		if err != nil {
			return err
		}

		out, retry, err := f(val)
		if err != nil {
			return err
		}

		if out == nil {
			// no change to be done
			return nil
		}

		// Don't even try
		r, ok := out.(Mergeable)
		if !ok {
			return fmt.Errorf("invalid type: %T, expected Mergeable", out)
		}

		// To support detection of removed items from value, we will only allow CAS operation to
		// succeed if version hasn't changed, i.e. state hasn't changed since running 'f'.
		change, newver, err := m.mergeValueForKey(key, r, ver)
		if err != nil && err != errVersionMismatch {
			return err
		}

		if newver == 0 {
			// 'f' didn't do any update that we can merge. Let's give it a new chance?
			if !retry {
				break
			}

			// If merge failed because of version mismatch, don't sleep.
			sleep = (err != errVersionMismatch)
			continue
		}

		m.casSuccesses.Inc()
		m.notifyWatchers(key)
		m.broadcastNewValue(key, change, newver)
		return nil
	}

	m.casFailures.Inc()
	return fmt.Errorf("failed to CAS-update key %s", key)
}

func (m *Client) broadcastNewValue(key string, change Mergeable, version uint) {
	data, err := m.codec.Encode(change)
	if err != nil {
		level.Error(util.Logger).Log("msg", "failed to encode ring", "err", err)
		return
	}

	buf := bytes.Buffer{}
	buf.Write([]byte{byte(len(key))})
	buf.WriteString(key)
	buf.Write(data)

	if buf.Len() > 65535 {
		// Unfortunately, memberlist will happily let us send bigger messages via gossip,
		// but then it will fail to parse them properly, because its own size field is 2-bytes only.
		// (github.com/hashicorp/memberlist@v0.1.4/util.go:167, makeCompoundMessage function)
		//
		// Typically messages are smaller (when dealing with couple of updates only), but can get bigger
		// when broadcasting result of push/pull update.
		level.Debug(util.Logger).Log("msg", "broadcast message too big, not broadcasting", "len", buf.Len())
		return
	}

	m.queueBroadcast(key, change.MergeContent(), version, buf.Bytes())
}

// NodeMeta is method from Memberlist Delegate interface
func (m *Client) NodeMeta(limit int) []byte {
	// we can send local state from here (512 bytes only)
	// if state is updated, we need to tell memberlist to distribute it.
	return nil
}

// NotifyMsg is method from Memberlist Delegate interface
// Called when single message is received, i.e. what our broadcastNewValue has sent.
func (m *Client) NotifyMsg(msg []byte) {
	m.numberOfReceivedMessages.Inc()
	m.totalSizeOfReceivedMessages.Add(float64(len(msg)))

	if len(msg) == 0 {
		level.Warn(util.Logger).Log("msg", "Empty message received")
		m.numberOfInvalidReceivedMessages.Inc()
		return
	}

	keyLen := int(msg[0])
	if len(msg) <= 1+keyLen {
		level.Warn(util.Logger).Log("msg", "Too short message received", "length", len(msg))
		m.numberOfInvalidReceivedMessages.Inc()
		return
	}

	key := string(msg[1 : 1+keyLen])
	data := msg[1+keyLen:]

	// we have a ring update! Let's merge it with our version of the ring for given key
	mod, version, err := m.mergeBytesValueForKey(key, data)
	if err != nil {
		level.Error(util.Logger).Log("msg", "failed to store received value", "key", key, "err", err)
	} else if version > 0 {
		m.notifyWatchers(key)

		// Forward this message
		// Memberlist will modify message once this function returns, so we need to make a copy
		msgCopy := append([]byte(nil), msg...)

		// forward this message further
		m.queueBroadcast(key, mod.MergeContent(), version, msgCopy)
	}
}

func (m *Client) queueBroadcast(key string, content []string, version uint, message []byte) {
	l := len(message)

	b := ringBroadcast{
		key:     key,
		content: content,
		version: version,
		msg:     message,
		finished: func(b ringBroadcast) {
			m.totalSizeOfBroadcastMessagesInQueue.Sub(float64(l))
		},
	}

	m.totalSizeOfBroadcastMessagesInQueue.Add(float64(l))
	m.broadcasts.QueueBroadcast(b)
}

// GetBroadcasts is method from Memberlist Delegate interface
// It returns all pending broadcasts (within the size limit)
func (m *Client) GetBroadcasts(overhead, limit int) [][]byte {
	return m.broadcasts.GetBroadcasts(overhead, limit)
}

// LocalState is method from Memberlist Delegate interface
//
// This is "pull" part of push/pull sync (either periodic, or when new node joins the cluster).
// Here we dump our entire state -- all keys and their values. There is no limit on message size here,
// as Memberlist uses 'stream' operations for transferring this state.
func (m *Client) LocalState(join bool) []byte {
	m.numberOfPulls.Inc()

	m.storeMu.Lock()
	defer m.storeMu.Unlock()

	// For each Key/Value pair in our store, we write:
	// [1 byte key length] [key] [4-bytes value length] [value]

	buf := bytes.Buffer{}
	for key, val := range m.store {
		if val.value == nil {
			continue
		}

		if len(key) > maxKeyLength {
			level.Error(util.Logger).Log("msg", "key too long", "key", key)
			continue
		}
		if uint(len(val.value)) > math.MaxUint32 {
			level.Error(util.Logger).Log("msg", "value too long", "key", key, "value_length", len(val.value))
			continue
		}

		buf.WriteByte(byte(len(key)))
		buf.WriteString(key)
		err := binary.Write(&buf, binary.BigEndian, uint32(len(val.value)))
		if err != nil {
			level.Error(util.Logger).Log("msg", "failed to write uint32 to buffer?", "err", err)
			continue
		}
		buf.Write(val.value)
	}

	m.totalSizeOfPulls.Add(float64(buf.Len()))
	return buf.Bytes()
}

// MergeRemoteState is method from Memberlist Delegate interface
//
// This is 'push' part of push/pull sync. We merge incoming KV store (all keys and values) with ours.
func (m *Client) MergeRemoteState(stateMsg []byte, join bool) {
	m.numberOfPushes.Inc()
	m.totalSizeOfPushes.Add(float64(len(stateMsg)))

	buf := bytes.NewBuffer(stateMsg)

	var err error
	for buf.Len() > 0 {
		keyLen := byte(0)
		keyLen, err = buf.ReadByte()
		if err != nil {
			break
		}

		keyBuf := make([]byte, keyLen)
		l := 0
		l, err = buf.Read(keyBuf)
		if err != nil {
			break
		}

		if l != len(keyBuf) {
			err = fmt.Errorf("cannot read key, expected %d, got %d bytes", keyLen, l)
			break
		}

		key := string(keyBuf)

		// next read the length of the data
		valueLength := uint32(0)
		err = binary.Read(buf, binary.BigEndian, &valueLength)
		if err != nil {
			break
		}

		if buf.Len() < int(valueLength) {
			err = fmt.Errorf("not enough data left for value in key %q, expected %d, remaining %d bytes", key, valueLength, buf.Len())
			break
		}

		valueBuf := make([]byte, valueLength)
		l, err = buf.Read(valueBuf)
		if err != nil {
			break
		}

		if l != len(valueBuf) {
			err = fmt.Errorf("cannot read value for key %q, expected %d, got %d bytes", key, valueLength, l)
			break
		}

		// we have both key and value, try to merge it with our state
		change, newver, err := m.mergeBytesValueForKey(key, valueBuf)
		if err != nil {
			level.Error(util.Logger).Log("msg", "failed to store received value", "key", key, "err", err)
		} else if newver > 0 {
			m.notifyWatchers(key)
			m.broadcastNewValue(key, change, newver)
		}
	}

	if err != nil {
		level.Error(util.Logger).Log("msg", "failed to parse remote state", "err", err)
	}
}

func (m *Client) mergeBytesValueForKey(key string, incomingData []byte) (Mergeable, uint, error) {
	decodedValue, err := m.codec.Decode(incomingData)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to decode value: %v", err)
	}

	incomingValue, ok := decodedValue.(Mergeable)
	if !ok {
		return nil, 0, fmt.Errorf("expected Mergeable, got: %T", decodedValue)
	}

	return m.mergeValueForKey(key, incomingValue, 0)
}

// Merges incoming value with value we have in our store. Returns "a change" that can be sent to other
// cluster members to update their state, and new version of the value.
// If CAS version is specified, then merging will fail if state has changed already, and errVersionMismatch is reported.
// If no modification occurred, new version is 0.
func (m *Client) mergeValueForKey(key string, incomingValue Mergeable, casVersion uint) (Mergeable, uint, error) {
	m.storeMu.Lock()
	defer m.storeMu.Unlock()

	curr := m.store[key]
	if casVersion > 0 && curr.version != casVersion {
		return nil, 0, errVersionMismatch
	}
	result, change, err := computeNewValue(incomingValue, curr.value, m.codec, casVersion > 0)
	if err != nil {
		return nil, 0, err
	}

	// No change, don't store it.
	if change == nil || len(change.MergeContent()) == 0 {
		return nil, 0, nil
	}

	if m.cfg.LeftIngestersTimeout > 0 {
		limit := time.Now().Add(-m.cfg.LeftIngestersTimeout)
		result.RemoveTombstones(limit)
	}

	encoded, err := m.codec.Encode(result)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to encode merged result: %v", err)
	}

	newVersion := curr.version + 1
	m.store[key] = valueDesc{
		value:   encoded,
		version: newVersion,
	}

	return change, newVersion, nil
}

// returns [result, change, error]
func computeNewValue(incoming Mergeable, stored []byte, c codec.Codec, cas bool) (Mergeable, Mergeable, error) {
	if len(stored) == 0 {
		return incoming, incoming, nil
	}

	old, err := c.Decode(stored)
	if err != nil {
		return incoming, incoming, fmt.Errorf("failed to decode stored value: %v", err)
	}

	if old == nil {
		return incoming, incoming, nil
	}

	oldVal, ok := old.(Mergeable)
	if !ok {
		return incoming, incoming, fmt.Errorf("stored value is not Mergeable, got %T", old)
	}

	if oldVal == nil {
		return incoming, incoming, nil
	}

	// otherwise we have two mergeables, so merge them
	change, err := oldVal.Merge(incoming, cas)
	return oldVal, change, err
}
