// Package kgo provides a pure Go efficient Kafka client for Kafka 0.8+ with
// support for transactions, regex topic consuming, the latest partition
// strategies, and more. This client supports all client related KIPs.
//
// This client aims to be simple to use while still interacting with Kafka in a
// near ideal way. For more overview of the entire client itself, please see
// the README on the project's Github page.
package kgo

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"hash/crc32"
	"math/rand"
	"net"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/sasl"
)

var crc32c = crc32.MakeTable(crc32.Castagnoli) // record crc's use Castagnoli table; for consuming/producing

// Client issues requests and handles responses to a Kafka cluster.
type Client struct {
	cfg  cfg
	opts []Opt

	ctx       context.Context
	ctxCancel func()

	rng func(func(*rand.Rand))

	brokersMu    sync.RWMutex
	brokers      []*broker    // ordered by broker ID
	seeds        atomic.Value // []*broker, seed brokers, also ordered by ID
	anyBrokerOrd []int32      // shuffled brokers, for random ordering
	anySeedIdx   int32
	stopBrokers  bool // set to true on close to stop updateBrokers

	// A sink and a source is created once per node ID and persists
	// forever. We expect the list to be small.
	//
	// The mutex only exists to allow consumer session stopping to read
	// sources to notify when starting a session; all writes happen in the
	// metadata loop.
	sinksAndSourcesMu sync.Mutex
	sinksAndSources   map[int32]sinkAndSource

	reqFormatter  *kmsg.RequestFormatter
	connTimeouter connTimeouter

	bufPool bufPool // for to brokers to share underlying reusable request buffers
	prsPool prsPool // for sinks to reuse []promisedNumberedRecord

	controllerIDMu sync.Mutex
	controllerID   int32

	// The following two ensure that we only have one fetchBrokerMetadata
	// at once. This avoids unnecessary broker metadata requests and
	// metadata trampling.
	fetchingBrokersMu sync.Mutex
	fetchingBrokers   *struct {
		done chan struct{}
		err  error
	}

	producer producer
	consumer consumer

	compressor   *compressor
	decompressor *decompressor

	coordinatorsMu sync.Mutex
	coordinators   map[coordinatorKey]*coordinatorLoad

	updateMetadataCh     chan string
	updateMetadataNowCh  chan string // like above, but with high priority
	blockingMetadataFnCh chan func()
	metawait             metawait
	metadone             chan struct{}

	mappedMetaMu sync.Mutex
	mappedMeta   map[string]mappedMetadataTopic
}

func (cl *Client) idempotent() bool { return !cl.cfg.disableIdempotency }

type sinkAndSource struct {
	sink   *sink
	source *source
}

func (cl *Client) allSinksAndSources(fn func(sns sinkAndSource)) {
	cl.sinksAndSourcesMu.Lock()
	defer cl.sinksAndSourcesMu.Unlock()

	for _, sns := range cl.sinksAndSources {
		fn(sns)
	}
}

type hostport struct {
	host string
	port int32
}

// ValidateOpts returns an error if the options are invalid.
func ValidateOpts(opts ...Opt) error {
	_, _, _, err := validateCfg(opts...)
	return err
}

func parseSeeds(addrs []string) ([]hostport, error) {
	seeds := make([]hostport, 0, len(addrs))
	for _, seedBroker := range addrs {
		hp, err := parseBrokerAddr(seedBroker)
		if err != nil {
			return nil, err
		}
		seeds = append(seeds, hp)
	}
	return seeds, nil
}

// This function validates the configuration and returns a few things that we
// initialize while validating. The difference between this and NewClient
// initialization is all NewClient initialization is infallible.
func validateCfg(opts ...Opt) (cfg, []hostport, *compressor, error) {
	cfg := defaultCfg()
	for _, opt := range opts {
		opt.apply(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return cfg, nil, nil, err
	}
	seeds, err := parseSeeds(cfg.seedBrokers)
	if err != nil {
		return cfg, nil, nil, err
	}
	compressor, err := newCompressor(cfg.compression...)
	if err != nil {
		return cfg, nil, nil, err
	}
	return cfg, seeds, compressor, nil
}

func namefn(fn any) string {
	v := reflect.ValueOf(fn)
	if v.Type().Kind() != reflect.Func {
		return ""
	}
	name := runtime.FuncForPC(v.Pointer()).Name()
	dot := strings.LastIndexByte(name, '.')
	if dot >= 0 {
		return name[dot+1:]
	}
	return name
}

// OptValue returns the value for the given configuration option. If the
// given option does not exist, this returns nil. This function takes either a
// raw Opt, or an Opt function name.
//
// If a configuration option has multiple inputs, this function returns only
// the first input. If the function is a boolean function (such as
// BlockRebalanceOnPoll), this function returns the value of the internal bool.
// Variadic option inputs are returned as a single slice.  Options that are
// internally stored as a pointer (ClientID, TransactionalID, and InstanceID)
// are returned as their string input; you can see if the option is internally
// nil by looking at the second value returned from OptValues.
//
//	var (
//		cl, _ := NewClient(
//			InstanceID("foo"),
//			ConsumeTopics("foo", "bar"),
//		)
//		iid    = cl.OptValue(InstanceID)           // iid is "foo"
//		gid    = cl.OptValue(ConsumerGroup)        // gid is "" since groups are not used
//		topics = cl.OptValue("ConsumeTopics")      // topics is []string{"foo", "bar"}; string lookup for the option works
//		bpoll  = cl.OptValue(BlockRebalanceOnPoll) // bpoll is false
//		t      = cl.OptValue(SessionTimeout)       // t is 45s, the internal default
//		td     = t.(time.Duration)                 // safe conversion since SessionTimeout's input is a time.Duration
//		unk    = cl.OptValue("Unknown"),           // unk is nil
//	)
func (cl *Client) OptValue(opt any) any {
	vs := cl.OptValues(opt)
	if len(vs) > 0 {
		return vs[0]
	}
	return nil
}

// OptValues returns all values for options. This method is useful for
// options that have multiple inputs (notably, SoftwareNameAndVersion). This is
// also useful for options that are internally stored as a pointer (ClientID,
// TransactionalID, and InstanceID) -- this function will return the string
// value of the option but also whether the option is non-nil. Boolean options
// are returned as a single-element slice with the bool value. Variadic inputs
// are returned as a signle slice. If the input option does not exist, this
// returns nil.
//
//	var (
//		cl, _    = NewClient(
//			InstanceID("foo"),
//			ConsumeTopics("foo", "bar"),
//		)
//		idValues = cl.OptValues(InstanceID)           // idValues is []any{"foo", true}
//		tValues  = cl.OptValues(SessionTimeout)       // tValues is []any{45 * time.Second}
//		topics   = cl.OptValues(ConsumeTopics)        // topics is []any{[]string{"foo", "bar"}
//		bpoll    = cl.OptValues(BlockRebalanceOnPoll) // bpoll is []any{false}
//		unknown  = cl.OptValues("Unknown")            // unknown is nil
//	)
func (cl *Client) OptValues(opt any) []any {
	name := namefn(opt)
	if s, ok := opt.(string); ok {
		name = s
	}
	cfg := &cl.cfg

	switch name {
	case namefn(ClientID):
		if cfg.id != nil {
			return []any{*cfg.id, true}
		}
		return []any{"", false}
	case namefn(SoftwareNameAndVersion):
		return []any{cfg.softwareName, cfg.softwareVersion}
	case namefn(WithLogger):
		if _, wrapped := cfg.logger.(*wrappedLogger); wrapped {
			return []any{cfg.logger.(*wrappedLogger).inner}
		}
		return []any{nil}
	case namefn(RequestTimeoutOverhead):
		return []any{cfg.requestTimeoutOverhead}
	case namefn(ConnIdleTimeout):
		return []any{cfg.connIdleTimeout}
	case namefn(Dialer):
		return []any{cfg.dialFn}
	case namefn(DialTLSConfig):
		return []any{cfg.dialTLS}
	case namefn(DialTLS):
		return []any{cfg.dialTLS != nil}
	case namefn(SeedBrokers):
		return []any{cfg.seedBrokers}
	case namefn(MaxVersions):
		return []any{cfg.maxVersions}
	case namefn(MinVersions):
		return []any{cfg.minVersions}
	case namefn(RetryBackoffFn):
		return []any{cfg.retryBackoff}
	case namefn(RequestRetries):
		return []any{cfg.retries}
	case namefn(RetryTimeout):
		return []any{cfg.retryTimeout(0)}
	case namefn(RetryTimeoutFn):
		return []any{cfg.retryTimeout}
	case namefn(AllowAutoTopicCreation):
		return []any{cfg.allowAutoTopicCreation}
	case namefn(BrokerMaxWriteBytes):
		return []any{cfg.maxBrokerWriteBytes}
	case namefn(BrokerMaxReadBytes):
		return []any{cfg.maxBrokerReadBytes}
	case namefn(MetadataMaxAge):
		return []any{cfg.metadataMaxAge}
	case namefn(MetadataMinAge):
		return []any{cfg.metadataMinAge}
	case namefn(SASL):
		return []any{cfg.sasls}
	case namefn(WithHooks):
		return []any{cfg.hooks}
	case namefn(ConcurrentTransactionsBackoff):
		return []any{cfg.txnBackoff}
	case namefn(ConsiderMissingTopicDeletedAfter):
		return []any{cfg.missingTopicDelete}

	case namefn(DefaultProduceTopic):
		return []any{cfg.defaultProduceTopic}
	case namefn(RequiredAcks):
		return []any{cfg.acks}
	case namefn(DisableIdempotentWrite):
		return []any{cfg.disableIdempotency}
	case namefn(MaxProduceRequestsInflightPerBroker):
		return []any{cfg.maxProduceInflight}
	case namefn(ProducerBatchCompression):
		return []any{cfg.compression}
	case namefn(ProducerBatchMaxBytes):
		return []any{cfg.maxRecordBatchBytes}
	case namefn(MaxBufferedRecords):
		return []any{cfg.maxBufferedRecords}
	case namefn(MaxBufferedBytes):
		return []any{cfg.maxBufferedBytes}
	case namefn(RecordPartitioner):
		return []any{cfg.partitioner}
	case namefn(ProduceRequestTimeout):
		return []any{cfg.produceTimeout}
	case namefn(RecordRetries):
		return []any{cfg.recordRetries}
	case namefn(UnknownTopicRetries):
		return []any{cfg.maxUnknownFailures}
	case namefn(StopProducerOnDataLossDetected):
		return []any{cfg.stopOnDataLoss}
	case namefn(ProducerOnDataLossDetected):
		return []any{cfg.onDataLoss}
	case namefn(ProducerLinger):
		return []any{cfg.linger}
	case namefn(ManualFlushing):
		return []any{cfg.manualFlushing}
	case namefn(RecordDeliveryTimeout):
		return []any{cfg.recordTimeout}
	case namefn(TransactionalID):
		if cfg.txnID != nil {
			return []any{cfg.txnID, true}
		}
		return []any{"", false}
	case namefn(TransactionTimeout):
		return []any{cfg.txnTimeout}

	case namefn(ConsumePartitions):
		return []any{cfg.partitions}
	case namefn(ConsumePreferringLagFn):
		return []any{cfg.preferLagFn}
	case namefn(ConsumeRegex):
		return []any{cfg.regex}
	case namefn(ConsumeResetOffset):
		return []any{cfg.resetOffset}
	case namefn(ConsumeTopics):
		return []any{cfg.topics}
	case namefn(DisableFetchSessions):
		return []any{cfg.disableFetchSessions}
	case namefn(FetchIsolationLevel):
		return []any{cfg.isolationLevel}
	case namefn(FetchMaxBytes):
		return []any{int32(cfg.maxBytes)}
	case namefn(FetchMaxPartitionBytes):
		return []any{int32(cfg.maxPartBytes)}
	case namefn(FetchMaxWait):
		return []any{time.Duration(cfg.maxWait) * time.Millisecond}
	case namefn(FetchMinBytes):
		return []any{cfg.minBytes}
	case namefn(KeepControlRecords):
		return []any{cfg.keepControl}
	case namefn(MaxConcurrentFetches):
		return []any{cfg.maxConcurrentFetches}
	case namefn(Rack):
		return []any{cfg.rack}
	case namefn(KeepRetryableFetchErrors):
		return []any{cfg.keepRetryableFetchErrors}

	case namefn(AdjustFetchOffsetsFn):
		return []any{cfg.adjustOffsetsBeforeAssign}
	case namefn(AutoCommitCallback):
		return []any{cfg.commitCallback}
	case namefn(AutoCommitInterval):
		return []any{cfg.autocommitInterval}
	case namefn(AutoCommitMarks):
		return []any{cfg.autocommitMarks}
	case namefn(Balancers):
		return []any{cfg.balancers}
	case namefn(BlockRebalanceOnPoll):
		return []any{cfg.blockRebalanceOnPoll}
	case namefn(ConsumerGroup):
		return []any{cfg.group}
	case namefn(DisableAutoCommit):
		return []any{cfg.autocommitDisable}
	case namefn(GreedyAutoCommit):
		return []any{cfg.autocommitGreedy}
	case namefn(GroupProtocol):
		return []any{cfg.protocol}
	case namefn(HeartbeatInterval):
		return []any{cfg.heartbeatInterval}
	case namefn(InstanceID):
		if cfg.instanceID != nil {
			return []any{*cfg.instanceID, true}
		}
		return []any{"", false}
	case namefn(OnOffsetsFetched):
		return []any{cfg.onFetched}
	case namefn(OnPartitionsAssigned):
		return []any{cfg.onAssigned}
	case namefn(OnPartitionsLost):
		return []any{cfg.onLost}
	case namefn(OnPartitionsRevoked):
		return []any{cfg.onRevoked}
	case namefn(RebalanceTimeout):
		return []any{cfg.rebalanceTimeout}
	case namefn(RequireStableFetchOffsets):
		return []any{cfg.requireStable}
	case namefn(SessionTimeout):
		return []any{cfg.sessionTimeout}
	default:
		return nil
	}
}

// NewClient returns a new Kafka client with the given options or an error if
// the options are invalid. Connections to brokers are lazily created only when
// requests are written to them.
//
// By default, the client uses the latest stable request versions when talking
// to Kafka. If you use a broker older than 0.10.0, then you need to manually
// set a MaxVersions option. Otherwise, there is usually no harm in defaulting
// to the latest API versions, although occasionally Kafka introduces new
// required parameters that do not have zero value defaults.
//
// NewClient also launches a goroutine which periodically updates the cached
// topic metadata.
func NewClient(opts ...Opt) (*Client, error) {
	cfg, seeds, compressor, err := validateCfg(opts...)
	if err != nil {
		return nil, err
	}

	if cfg.retryTimeout == nil {
		cfg.retryTimeout = func(key int16) time.Duration {
			switch key {
			case ((*kmsg.JoinGroupRequest)(nil)).Key(),
				((*kmsg.SyncGroupRequest)(nil)).Key(),
				((*kmsg.HeartbeatRequest)(nil)).Key():
				return cfg.sessionTimeout
			}
			return 30 * time.Second
		}
	}

	if cfg.dialFn == nil {
		dialer := &net.Dialer{Timeout: cfg.dialTimeout}
		cfg.dialFn = dialer.DialContext
		if cfg.dialTLS != nil {
			cfg.dialFn = func(ctx context.Context, network, host string) (net.Conn, error) {
				c := cfg.dialTLS.Clone()
				if c.ServerName == "" {
					server, _, err := net.SplitHostPort(host)
					if err != nil {
						return nil, fmt.Errorf("unable to split host:port for dialing: %w", err)
					}
					c.ServerName = server
				}
				return (&tls.Dialer{
					NetDialer: dialer,
					Config:    c,
				}).DialContext(ctx, network, host)
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	cl := &Client{
		cfg:       cfg,
		opts:      opts,
		ctx:       ctx,
		ctxCancel: cancel,

		rng: func() func(func(*rand.Rand)) {
			var mu sync.Mutex
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			return func(fn func(*rand.Rand)) {
				mu.Lock()
				defer mu.Unlock()
				fn(rng)
			}
		}(),

		controllerID: unknownControllerID,

		sinksAndSources: make(map[int32]sinkAndSource),

		reqFormatter:  kmsg.NewRequestFormatter(),
		connTimeouter: connTimeouter{def: cfg.requestTimeoutOverhead},

		bufPool: newBufPool(),
		prsPool: newPrsPool(),

		compressor:   compressor,
		decompressor: newDecompressor(),

		coordinators: make(map[coordinatorKey]*coordinatorLoad),

		updateMetadataCh:     make(chan string, 1),
		updateMetadataNowCh:  make(chan string, 1),
		blockingMetadataFnCh: make(chan func()),
		metadone:             make(chan struct{}),
	}

	// Before we start any goroutines below, we must notify any interested
	// hooks of our existence.
	cl.cfg.hooks.each(func(h Hook) {
		if h, ok := h.(HookNewClient); ok {
			h.OnNewClient(cl)
		}
	})

	cl.producer.init(cl)
	cl.consumer.init(cl)
	cl.metawait.init()

	if cfg.id != nil {
		cl.reqFormatter = kmsg.NewRequestFormatter(kmsg.FormatterClientID(*cfg.id))
	}

	seedBrokers := make([]*broker, 0, len(seeds))
	for i, seed := range seeds {
		b := cl.newBroker(unknownSeedID(i), seed.host, seed.port, nil)
		seedBrokers = append(seedBrokers, b)
	}
	cl.seeds.Store(seedBrokers)
	go cl.updateMetadataLoop()
	go cl.reapConnectionsLoop()

	return cl, nil
}

// Opts returns the options that were used to create this client. This can be
// as a base to generate a new client, where you can add override options to
// the end of the original input list. If you want to know a specific option
// value, you can use OptValue or OptValues.
func (cl *Client) Opts() []Opt {
	return cl.opts
}

func (cl *Client) loadSeeds() []*broker {
	return cl.seeds.Load().([]*broker)
}

// Ping returns whether any broker is reachable, iterating over any discovered
// broker or seed broker until one returns a successful response to an
// ApiVersions request. No discovered broker nor seed broker is attempted more
// than once. If all requests fail, this returns final error.
func (cl *Client) Ping(ctx context.Context) error {
	req := kmsg.NewPtrApiVersionsRequest()
	req.ClientSoftwareName = cl.cfg.softwareName
	req.ClientSoftwareVersion = cl.cfg.softwareVersion

	cl.brokersMu.RLock()
	brokers := append([]*broker(nil), cl.brokers...)
	cl.brokersMu.RUnlock()

	var lastErr error
	for _, brs := range [2][]*broker{
		brokers,
		cl.loadSeeds(),
	} {
		for _, br := range brs {
			_, err := br.waitResp(ctx, req)
			if lastErr = err; lastErr == nil {
				return nil
			}
		}
	}
	return lastErr
}

// PurgeTopicsFromClient internally removes all internal information about the
// input topics. If you you want to purge information for only consuming or
// only producing, see the related functions [PurgeTopicsFromConsuming] and
// [PurgeTopicsFromProducing].
//
// For producing, this clears all knowledge that these topics have ever been
// produced to. Producing to the topic again may result in out of order
// sequence number errors, or, if idempotency is disabled and the sequence
// numbers align, may result in invisibly discarded records at the broker.
// Purging a topic that was previously produced to may be useful to free up
// resources if you are producing to many disparate and short lived topic in
// the lifetime of this client and you do not plan to produce to the topic
// anymore. You may want to flush buffered records before purging if records
// for a topic you are purging are currently in flight.
//
// For consuming, this removes all concept of the topic from being consumed.
// This is different from PauseFetchTopics, which literally pauses the fetching
// of topics but keeps the topic information around for resuming fetching
// later. Purging a topic that was being consumed can be useful if you know the
// topic no longer exists, or if you are consuming via regex and know that some
// previously consumed topics no longer exist, or if you simply do not want to
// ever consume from a topic again. If you are group consuming, this function
// will likely cause a rebalance.
//
// For admin requests, this deletes the topic from the cached metadata map for
// sharded requests. Metadata for sharded admin requests is only cached for
// MetadataMinAge anyway, but the map is not cleaned up one the metadata
// expires. This function ensures the map is purged.
func (cl *Client) PurgeTopicsFromClient(topics ...string) {
	if len(topics) == 0 {
		return
	}
	sort.Strings(topics)           // for logging in the functions
	cl.blockingMetadataFn(func() { // make reasoning about concurrency easier
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			cl.producer.purgeTopics(topics)
		}()
		go func() {
			defer wg.Done()
			cl.consumer.purgeTopics(topics)
		}()
		wg.Wait()
	})
	cl.mappedMetaMu.Lock()
	for _, t := range topics {
		delete(cl.mappedMeta, t)
	}
	cl.mappedMetaMu.Unlock()
}

// PurgeTopicsFromProducing internally removes all internal information for
// producing about the input topics. This runs the producer bit of logic that
// is documented in [PurgeTopicsFromClient]; see that function for more
// details.
func (cl *Client) PurgeTopicsFromProducing(topics ...string) {
	if len(topics) == 0 {
		return
	}
	sort.Strings(topics)
	cl.blockingMetadataFn(func() {
		cl.producer.purgeTopics(topics)
	})
}

// PurgeTopicsFromConsuming internally removes all internal information for
// consuming about the input topics. This runs the consumer bit of logic that
// is documented in [PurgeTopicsFromClient]; see that function for more
// details.
func (cl *Client) PurgeTopicsFromConsuming(topics ...string) {
	if len(topics) == 0 {
		return
	}
	sort.Strings(topics)
	cl.blockingMetadataFn(func() {
		cl.consumer.purgeTopics(topics)
	})
}

// Parse broker IP/host and port from a string, using the default Kafka port if
// unspecified. Supported address formats:
//
// - IPv4 host/IP without port: "127.0.0.1", "localhost"
// - IPv4 host/IP with port: "127.0.0.1:1234", "localhost:1234"
// - IPv6 IP without port:  "[2001:1000:2000::1]", "::1"
// - IPv6 IP with port: "[2001:1000:2000::1]:1234"
func parseBrokerAddr(addr string) (hostport, error) {
	const defaultKafkaPort = 9092

	// Bracketed IPv6
	if strings.IndexByte(addr, '[') == 0 {
		parts := strings.Split(addr[1:], "]")
		if len(parts) != 2 {
			return hostport{}, fmt.Errorf("invalid addr: %s", addr)
		}
		// No port specified -> use default
		if len(parts[1]) == 0 {
			return hostport{parts[0], defaultKafkaPort}, nil
		}
		port, err := strconv.ParseInt(parts[1][1:], 10, 32)
		if err != nil {
			return hostport{}, fmt.Errorf("unable to parse port from addr: %w", err)
		}
		return hostport{parts[0], int32(port)}, nil
	}

	// IPv4 with no port
	if strings.IndexByte(addr, ':') == -1 {
		return hostport{addr, defaultKafkaPort}, nil
	}

	// Either a IPv6 literal ("::1"), IP:port or host:port
	// Try to parse as IP:port or host:port
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return hostport{addr, defaultKafkaPort}, nil //nolint:nilerr // ipv6 literal -- use default kafka port
	}
	port, err := strconv.ParseInt(p, 10, 32)
	if err != nil {
		return hostport{}, fmt.Errorf("unable to parse port from addr: %w", err)
	}
	return hostport{h, int32(port)}, nil
}

type connTimeouter struct {
	def                  time.Duration
	joinMu               sync.Mutex
	lastRebalanceTimeout time.Duration
}

func (c *connTimeouter) timeouts(req kmsg.Request) (r, w time.Duration) {
	def := c.def
	millis := func(m int32) time.Duration { return time.Duration(m) * time.Millisecond }
	switch t := req.(type) {
	default:
		if timeoutRequest, ok := req.(kmsg.TimeoutRequest); ok {
			timeoutMillis := timeoutRequest.Timeout()
			return def + millis(timeoutMillis), def
		}
		return def, def

	case *produceRequest:
		return def + millis(t.timeout), def
	case *fetchRequest:
		return def + millis(t.maxWait), def
	case *kmsg.FetchRequest:
		return def + millis(t.MaxWaitMillis), def

	// Join and sync can take a long time. Sync has no notion of
	// timeouts, but since the flow of requests should be first
	// join, then sync, we can stash the timeout from the join.

	case *kmsg.JoinGroupRequest:
		c.joinMu.Lock()
		c.lastRebalanceTimeout = millis(t.RebalanceTimeoutMillis)
		c.joinMu.Unlock()

		return def + millis(t.RebalanceTimeoutMillis), def
	case *kmsg.SyncGroupRequest:
		read := def
		c.joinMu.Lock()
		if c.lastRebalanceTimeout != 0 {
			read = c.lastRebalanceTimeout
		}
		c.joinMu.Unlock()

		return read, def
	}
}

func (cl *Client) reinitAnyBrokerOrd() {
	cl.anyBrokerOrd = append(cl.anyBrokerOrd[:0], make([]int32, len(cl.brokers))...)
	for i := range cl.anyBrokerOrd {
		cl.anyBrokerOrd[i] = int32(i)
	}
	cl.rng(func(r *rand.Rand) {
		r.Shuffle(len(cl.anyBrokerOrd), func(i, j int) {
			cl.anyBrokerOrd[i], cl.anyBrokerOrd[j] = cl.anyBrokerOrd[j], cl.anyBrokerOrd[i]
		})
	})
}

// broker returns a random broker from all brokers ever known.
func (cl *Client) broker() *broker {
	cl.brokersMu.Lock()
	defer cl.brokersMu.Unlock()

	// Every time we loop through all discovered brokers, we issue one
	// request to the next seed. This ensures that if all discovered
	// brokers are down, we will *eventually* loop through seeds and
	// hopefully have a reachable seed.
	var b *broker

	if len(cl.anyBrokerOrd) > 0 {
		b = cl.brokers[cl.anyBrokerOrd[0]]
		cl.anyBrokerOrd = cl.anyBrokerOrd[1:]
		return b
	}

	seeds := cl.loadSeeds()
	cl.anySeedIdx %= int32(len(seeds))
	b = seeds[cl.anySeedIdx]
	cl.anySeedIdx++

	// If we have brokers, we ranged past discovered brokers.
	// We now reset the anyBrokerOrd to begin ranging through
	// discovered brokers again. If there are still no brokers,
	// this reinit will do nothing and we will keep looping seeds.
	cl.reinitAnyBrokerOrd()
	return b
}

func (cl *Client) waitTries(ctx context.Context, backoff time.Duration) bool {
	after := time.NewTimer(backoff)
	defer after.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-cl.ctx.Done():
		return false
	case <-after.C:
		return true
	}
}

// A broker may sometimes indicate it supports offset for leader epoch v2+ when
// it does not. We need to catch that and avoid issuing offset for leader
// epoch, because we will just loop continuously failing. We do not catch every
// case, such as when a person explicitly assigns offsets with epochs, but we
// catch a few areas that would be returned from a broker itself.
//
// This function is always used *after* at least one request has been issued.
//
// NOTE: This is a weak check; we check if any broker in the cluster supports
// the request. We use this function in three locations:
//
//  1. When using the LeaderEpoch returned in a metadata response. This guards
//     against buggy brokers that return 0 rather than -1 even if they do not
//     support OffsetForLeaderEpoch. If any support, the cluster is in the
//     middle of an upgrade and we can start using the epoch.
//  2. When deciding whether to keep LeaderEpoch from fetched offsets.
//     Realistically, clients should only commit epochs if the cluster supports
//     them.
//  3. When receiving OffsetOutOfRange when follower fetching and we fetched
//     past the end.
//
// In any of these cases, if we OffsetForLeaderEpoch against a broker that does
// not support (even though one in the cluster does), we will loop fail until
// the rest of the cluster is upgraded and supports the request.
func (cl *Client) supportsOffsetForLeaderEpoch() bool {
	return cl.supportsKeyVersion(int16(kmsg.OffsetForLeaderEpoch), 2)
}

// A broker may not support some requests we want to make. This function checks
// support. This should only be used *after* at least one successful response.
func (cl *Client) supportsKeyVersion(key, version int16) bool {
	cl.brokersMu.RLock()
	defer cl.brokersMu.RUnlock()

	for _, brokers := range [][]*broker{
		cl.brokers,
		cl.loadSeeds(),
	} {
		for _, b := range brokers {
			if v := b.loadVersions(); v != nil && v.versions[key] >= version {
				return true
			}
		}
	}
	return false
}

// fetchBrokerMetadata issues a metadata request solely for broker information.
func (cl *Client) fetchBrokerMetadata(ctx context.Context) error {
	cl.fetchingBrokersMu.Lock()
	wait := cl.fetchingBrokers
	if wait != nil {
		cl.fetchingBrokersMu.Unlock()
		<-wait.done
		return wait.err
	}
	wait = &struct {
		done chan struct{}
		err  error
	}{done: make(chan struct{})}
	cl.fetchingBrokers = wait
	cl.fetchingBrokersMu.Unlock()

	defer func() {
		cl.fetchingBrokersMu.Lock()
		defer cl.fetchingBrokersMu.Unlock()
		cl.fetchingBrokers = nil
		close(wait.done)
	}()

	_, _, wait.err = cl.fetchMetadata(ctx, kmsg.NewPtrMetadataRequest(), true)
	return wait.err
}

func (cl *Client) fetchMetadataForTopics(ctx context.Context, all bool, topics []string) (*broker, *kmsg.MetadataResponse, error) {
	req := kmsg.NewPtrMetadataRequest()
	req.AllowAutoTopicCreation = cl.cfg.allowAutoTopicCreation
	if all {
		req.Topics = nil
	} else if len(topics) == 0 {
		req.Topics = []kmsg.MetadataRequestTopic{}
	} else {
		for _, topic := range topics {
			reqTopic := kmsg.NewMetadataRequestTopic()
			reqTopic.Topic = kmsg.StringPtr(topic)
			req.Topics = append(req.Topics, reqTopic)
		}
	}
	return cl.fetchMetadata(ctx, req, true)
}

func (cl *Client) fetchMetadata(ctx context.Context, req *kmsg.MetadataRequest, limitRetries bool) (*broker, *kmsg.MetadataResponse, error) {
	r := cl.retryable()

	// We limit retries for internal metadata refreshes, because these do
	// not need to retry forever and are usually blocking *other* requests.
	// e.g., producing bumps load errors when metadata returns, so 3
	// failures here will correspond to 1 bumped error count. To make the
	// number more accurate, we should *never* retry here, but this is
	// pretty intolerant of immediately-temporary network issues. Rather,
	// we use a small count of 3 retries, which with the default backoff,
	// will be <2s of retrying. This is still intolerant of temporary
	// failures, but it does allow recovery from a dns issue / bad path.
	if limitRetries {
		r.limitRetries = 3
	}

	meta, err := req.RequestWith(ctx, r)
	if err == nil {
		if meta.ControllerID >= 0 {
			cl.controllerIDMu.Lock()
			cl.controllerID = meta.ControllerID
			cl.controllerIDMu.Unlock()
		}
		cl.updateBrokers(meta.Brokers)
	}
	return r.last, meta, err
}

// updateBrokers is called with the broker portion of every metadata response.
// All metadata responses contain all known live brokers, so we can always
// use the response.
func (cl *Client) updateBrokers(brokers []kmsg.MetadataResponseBroker) {
	sort.Slice(brokers, func(i, j int) bool { return brokers[i].NodeID < brokers[j].NodeID })
	newBrokers := make([]*broker, 0, len(brokers))

	cl.brokersMu.Lock()
	defer cl.brokersMu.Unlock()

	if cl.stopBrokers {
		return
	}

	for len(brokers) > 0 && len(cl.brokers) > 0 {
		ob := cl.brokers[0]
		nb := brokers[0]

		switch {
		case ob.meta.NodeID < nb.NodeID:
			ob.stopForever()
			cl.brokers = cl.brokers[1:]

		case ob.meta.NodeID == nb.NodeID:
			if !ob.meta.equals(nb) {
				ob.stopForever()
				ob = cl.newBroker(nb.NodeID, nb.Host, nb.Port, nb.Rack)
			}
			newBrokers = append(newBrokers, ob)
			cl.brokers = cl.brokers[1:]
			brokers = brokers[1:]

		case ob.meta.NodeID > nb.NodeID:
			newBrokers = append(newBrokers, cl.newBroker(nb.NodeID, nb.Host, nb.Port, nb.Rack))
			brokers = brokers[1:]
		}
	}

	for len(cl.brokers) > 0 {
		ob := cl.brokers[0]
		ob.stopForever()
		cl.brokers = cl.brokers[1:]
	}

	for len(brokers) > 0 {
		nb := brokers[0]
		newBrokers = append(newBrokers, cl.newBroker(nb.NodeID, nb.Host, nb.Port, nb.Rack))
		brokers = brokers[1:]
	}

	cl.brokers = newBrokers
	cl.reinitAnyBrokerOrd()
}

// CloseAllowingRebalance allows rebalances, leaves any group, and closes all
// connections and goroutines. This function is only useful if you are using
// the BlockRebalanceOnPoll option. Close itself does not allow rebalances and
// will hang if you polled, did not allow rebalances, and want to close. Close
// does not automatically allow rebalances because leaving a group causes a
// revoke, and the client does not assume that the final revoke is concurrency
// safe. The CloseAllowingRebalance function exists a a shortcut to opt into
// allowing rebalance while closing.
func (cl *Client) CloseAllowingRebalance() {
	cl.AllowRebalance()
	cl.Close()
}

// Close leaves any group and closes all connections and goroutines. This
// function waits for the group to be left. If you want to force leave a group
// immediately and ensure a speedy shutdown you can use LeaveGroupContext first
// (and then Close will be immediate).
//
// If you are group consuming and have overridden the default
// OnPartitionsRevoked, you must manually commit offsets before closing the
// client.
//
// If you are using the BlockRebalanceOnPoll option and have polled, this
// function does not automatically allow rebalancing. You must AllowRebalance
// before calling this function. Internally, this function leaves the group,
// and leaving a group causes a rebalance so that you can get one final
// notification of revoked partitions. If you want to automatically allow
// rebalancing, use CloseAllowingRebalance.
func (cl *Client) Close() {
	cl.close(cl.ctx)
}

func (cl *Client) close(ctx context.Context) (rerr error) {
	defer cl.cfg.hooks.each(func(h Hook) {
		if h, ok := h.(HookClientClosed); ok {
			h.OnClientClosed(cl)
		}
	})

	c := &cl.consumer
	c.kill.Store(true)
	if c.g != nil {
		rerr = cl.LeaveGroupContext(ctx)
	} else if c.d != nil {
		c.mu.Lock()                                           // lock for assign
		c.assignPartitions(nil, assignInvalidateAll, nil, "") // we do not use a log message when not in a group
		c.mu.Unlock()
	}

	// After the above, consumers cannot consume anymore. LeaveGroup
	// internally assigns nil, which uses noConsumerSession, which prevents
	// loopFetch from starting. Assigning also waits for the prior session
	// to be complete, meaning loopFetch cannot be running.

	sessCloseCtx, sessCloseCancel := context.WithTimeout(ctx, time.Second)
	var wg sync.WaitGroup
	cl.allSinksAndSources(func(sns sinkAndSource) {
		if sns.source.session.id != 0 {
			sns := sns
			wg.Add(1)
			go func() {
				defer wg.Done()
				sns.source.killSessionOnClose(sessCloseCtx)
			}()
		}
	})
	wg.Wait()
	sessCloseCancel()

	// Now we kill the client context and all brokers, ensuring all
	// requests fail. This will finish all producer callbacks and
	// stop the metadata loop.
	cl.ctxCancel()
	cl.brokersMu.Lock()
	cl.stopBrokers = true
	for _, broker := range cl.brokers {
		broker.stopForever()
	}
	cl.brokersMu.Unlock()
	for _, broker := range cl.loadSeeds() {
		broker.stopForever()
	}

	// Wait for metadata to quit so we know no more erroring topic
	// partitions will be created. After metadata has quit, we can
	// safely stop sinks and sources, as no more will be made.
	<-cl.metadone

	for _, sns := range cl.sinksAndSources {
		sns.sink.maybeDrain()     // awaken anything in backoff
		sns.source.maybeConsume() // same
	}

	cl.failBufferedRecords(ErrClientClosed)

	// We need one final poll: if any sources buffered a fetch, then the
	// manageFetchConcurrency loop only exits when all fetches have been
	// drained, because draining a fetch is what decrements an "active"
	// fetch. PollFetches with `nil` is instant.
	cl.PollFetches(nil)

	for _, s := range cl.cfg.sasls {
		if closing, ok := s.(sasl.ClosingMechanism); ok {
			closing.Close()
		}
	}

	return rerr
}

// Request issues a request to Kafka, waiting for and returning the response.
// If a retryable network error occurs, or if a retryable group / transaction
// coordinator error occurs, the request is retried. All other errors are
// returned.
//
// If the request is an admin request, this will issue it to the Kafka
// controller. If the controller ID is unknown, this will attempt to fetch it.
// If the fetch errors, this will return an unknown controller error.
//
// If the request is a group or transaction coordinator request, this will
// issue the request to the appropriate group or transaction coordinator.
//
// For transaction requests, the request is issued to the transaction
// coordinator. However, if the request is an init producer ID request and the
// request has no transactional ID, the request goes to any broker.
//
// Some requests need to be split and sent to many brokers. For these requests,
// it is *highly* recommended to use RequestSharded. Not all responses from
// many brokers can be cleanly merged. However, for the requests that are
// split, this does attempt to merge them in a sane way.
//
// The following requests are split:
//
//	ListOffsets
//	OffsetFetch (if using v8+ for Kafka 3.0+)
//	FindCoordinator (if using v4+ for Kafka 3.0+)
//	DescribeGroups
//	ListGroups
//	DeleteRecords
//	OffsetForLeaderEpoch
//	DescribeConfigs
//	AlterConfigs
//	AlterReplicaLogDirs
//	DescribeLogDirs
//	DeleteGroups
//	IncrementalAlterConfigs
//	DescribeProducers
//	DescribeTransactions
//	ListTransactions
//
// Kafka 3.0 introduced batch OffsetFetch and batch FindCoordinator requests.
// This function is forward and backward compatible: old requests will be
// batched as necessary, and batched requests will be split as necessary. It is
// recommended to always use batch requests for simplicity.
//
// In short, this method tries to do the correct thing depending on what type
// of request is being issued.
//
// The passed context can be used to cancel a request and return early. Note
// that if the request was written to Kafka but the context canceled before a
// response is received, Kafka may still operate on the received request.
//
// If using this function to issue kmsg.ProduceRequest's, you must configure
// the client with the same RequiredAcks option that you use in the request.
// If you are issuing produce requests with 0 acks, you must configure the
// client with the same timeout you use in the request. The client will
// internally rewrite the incoming request's acks to match the client's
// configuration, and it will rewrite the timeout millis if the acks is 0. It
// is strongly recommended to not issue raw kmsg.ProduceRequest's.
func (cl *Client) Request(ctx context.Context, req kmsg.Request) (kmsg.Response, error) {
	resps, merge := cl.shardedRequest(ctx, req)
	// If there is no merge function, only one request was issued directly
	// to a broker. Return the resp and err directly.
	if merge == nil {
		return resps[0].Resp, resps[0].Err
	}
	return merge(resps)
}

func (cl *Client) retryable() *retryable {
	return cl.retryableBrokerFn(func() (*broker, error) { return cl.broker(), nil })
}

func (cl *Client) retryableBrokerFn(fn func() (*broker, error)) *retryable {
	return &retryable{cl: cl, br: fn}
}

func (cl *Client) shouldRetry(tries int, err error) bool {
	return (kerr.IsRetriable(err) || isRetryableBrokerErr(err)) && int64(tries) < cl.cfg.retries
}

func (cl *Client) shouldRetryNext(tries int, err error) bool {
	return isSkippableBrokerErr(err) && int64(tries) < cl.cfg.retries
}

type retryable struct {
	cl   *Client
	br   func() (*broker, error)
	last *broker

	// If non-zero, limitRetries may specify a smaller # of retries than
	// the client RequestRetries number. This is used for internal requests
	// that can fail / do not need to retry forever.
	limitRetries int

	// parseRetryErr, if non-nil, can delete stale cached brokers. We do
	// *not* return the error from this function to the caller, but we do
	// use it to potentially retry. It is not necessary, but also not
	// harmful, to return the input error.
	parseRetryErr func(kmsg.Response, error) error
}

type failDial struct{ fails int8 }

// The controller and group/txn coordinators are cached. If dialing the broker
// repeatedly fails, we need to forget our cache to force a re-load: the broker
// may have completely died.
func (d *failDial) isRepeatedDialFail(err error) bool {
	if isAnyDialErr(err) {
		d.fails++
		if d.fails == 3 {
			d.fails = 0
			return true
		}
	}
	return false
}

func (r *retryable) Request(ctx context.Context, req kmsg.Request) (kmsg.Response, error) {
	tries := 0
	tryStart := time.Now()
	retryTimeout := r.cl.cfg.retryTimeout(req.Key())

	next, nextErr := r.br()
start:
	tries++
	br, err := next, nextErr
	r.last = br
	var resp kmsg.Response
	var retryErr error
	if err == nil {
		resp, err = r.last.waitResp(ctx, req)
		if r.parseRetryErr != nil {
			retryErr = r.parseRetryErr(resp, err)
		}
	}

	if err != nil || retryErr != nil {
		if r.limitRetries == 0 || tries < r.limitRetries {
			backoff := r.cl.cfg.retryBackoff(tries)
			if retryTimeout == 0 || time.Now().Add(backoff).Sub(tryStart) <= retryTimeout {
				// If this broker / request had a retryable error, we can
				// just retry now. If the error is *not* retryable but
				// is a broker-specific network error, and the next
				// broker is different than the current, we also retry.
				if r.cl.shouldRetry(tries, err) || r.cl.shouldRetry(tries, retryErr) {
					r.cl.cfg.logger.Log(LogLevelDebug, "retrying request",
						"tries", tries,
						"backoff", backoff,
						"request_error", err,
						"response_error", retryErr,
					)
					if r.cl.waitTries(ctx, backoff) {
						next, nextErr = r.br()
						goto start
					}
				} else if r.cl.shouldRetryNext(tries, err) {
					next, nextErr = r.br()
					if next != br && r.cl.waitTries(ctx, backoff) {
						goto start
					}
				}
			}
		}
	}
	return resp, err
}

// ResponseShard ties together a request with either the response it received
// or an error that prevented a response from being received.
type ResponseShard struct {
	// Meta contains the broker that this request was issued to, or an
	// unknown (node ID -1) metadata if the request could not be issued.
	//
	// Requests can fail to even be issued if an appropriate broker cannot
	// be loaded of if the client cannot understand the request.
	Meta BrokerMetadata

	// Req is the request that was issued to this broker.
	Req kmsg.Request

	// Resp is the response received from the broker, if any.
	Resp kmsg.Response

	// Err, if non-nil, is the error that prevented a response from being
	// received or the request from being issued.
	Err error
}

// RequestSharded performs the same logic as Request, but returns all responses
// from any broker that the request was split to. This always returns at least
// one shard. If the request does not need to be issued (describing no groups),
// this issues the request to a random broker just to ensure that one shard
// exists.
//
// There are only a few requests that are strongly recommended to explicitly
// use RequestSharded; the rest can by default use Request. These few requests
// are mentioned in the documentation for Request.
//
// If, in the process of splitting a request, some topics or partitions are
// found to not exist, or Kafka replies that a request should go to a broker
// that does not exist, all those non-existent pieces are grouped into one
// request to the first seed broker. This will show up as a seed broker node ID
// (min int32) and the response will likely contain purely errors.
//
// The response shards are ordered by broker metadata.
func (cl *Client) RequestSharded(ctx context.Context, req kmsg.Request) []ResponseShard {
	resps, _ := cl.shardedRequest(ctx, req)
	sort.Slice(resps, func(i, j int) bool {
		l := &resps[i].Meta
		r := &resps[j].Meta

		if l.NodeID < r.NodeID {
			return true
		}
		if r.NodeID < l.NodeID {
			return false
		}
		if l.Host < r.Host {
			return true
		}
		if r.Host < l.Host {
			return false
		}
		if l.Port < r.Port {
			return true
		}
		if r.Port < l.Port {
			return false
		}
		if l.Rack == nil {
			return true
		}
		if r.Rack == nil {
			return false
		}
		return *l.Rack < *r.Rack
	})
	return resps
}

type shardMerge func([]ResponseShard) (kmsg.Response, error)

func (cl *Client) shardedRequest(ctx context.Context, req kmsg.Request) ([]ResponseShard, shardMerge) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer cancel()
		select {
		case <-done:
		case <-ctx.Done():
		case <-cl.ctx.Done():
		}
	}()

	// First, handle any sharded request. This comes before the conditional
	// below because this handles two group requests, which we do not want
	// to fall into the handleCoordinatorReq logic.
	switch t := req.(type) {
	case *kmsg.ListOffsetsRequest, // key 2
		*kmsg.OffsetFetchRequest,             // key 9
		*kmsg.FindCoordinatorRequest,         // key 10
		*kmsg.DescribeGroupsRequest,          // key 15
		*kmsg.ListGroupsRequest,              // key 16
		*kmsg.DeleteRecordsRequest,           // key 21
		*kmsg.OffsetForLeaderEpochRequest,    // key 23
		*kmsg.AddPartitionsToTxnRequest,      // key 24
		*kmsg.WriteTxnMarkersRequest,         // key 27
		*kmsg.DescribeConfigsRequest,         // key 32
		*kmsg.AlterConfigsRequest,            // key 33
		*kmsg.AlterReplicaLogDirsRequest,     // key 34
		*kmsg.DescribeLogDirsRequest,         // key 35
		*kmsg.DeleteGroupsRequest,            // key 42
		*kmsg.IncrementalAlterConfigsRequest, // key 44
		*kmsg.DescribeProducersRequest,       // key 61
		*kmsg.DescribeTransactionsRequest,    // key 65
		*kmsg.ListTransactionsRequest:        // key 66
		return cl.handleShardedReq(ctx, req)

	case *kmsg.MetadataRequest:
		// We hijack any metadata request so as to populate our
		// own brokers and controller ID.
		br, resp, err := cl.fetchMetadata(ctx, t, false)
		return shards(shard(br, req, resp, err)), nil

	case kmsg.AdminRequest:
		return shards(cl.handleAdminReq(ctx, t)), nil

	case kmsg.GroupCoordinatorRequest,
		kmsg.TxnCoordinatorRequest:
		return shards(cl.handleCoordinatorReq(ctx, t)), nil

	case *kmsg.ApiVersionsRequest:
		// As of v3, software name and version are required.
		// If they are missing, we use the config options.
		if t.ClientSoftwareName == "" && t.ClientSoftwareVersion == "" {
			dup := *t
			dup.ClientSoftwareName = cl.cfg.softwareName
			dup.ClientSoftwareVersion = cl.cfg.softwareVersion
			req = &dup
		}
	}

	// All other requests not handled above can be issued to any broker
	// with the default retryable logic.
	r := cl.retryable()
	resp, err := r.Request(ctx, req)
	return shards(shard(r.last, req, resp, err)), nil
}

func shard(br *broker, req kmsg.Request, resp kmsg.Response, err error) ResponseShard {
	if br == nil { // the broker could be nil if loading the broker failed.
		return ResponseShard{unknownBrokerMetadata, req, resp, err}
	}
	return ResponseShard{br.meta, req, resp, err}
}

func shards(shard ...ResponseShard) []ResponseShard {
	return shard
}

func findBroker(candidates []*broker, node int32) *broker {
	n := sort.Search(len(candidates), func(n int) bool { return candidates[n].meta.NodeID >= node })
	var b *broker
	if n < len(candidates) {
		c := candidates[n]
		if c.meta.NodeID == node {
			b = c
		}
	}
	return b
}

// brokerOrErr returns the broker for ID or the error if the broker does not
// exist.
//
// If tryLoad is true and the broker does not exist, this attempts a broker
// metadata load once before failing. If the metadata load fails, this returns
// that error.
func (cl *Client) brokerOrErr(ctx context.Context, id int32, err error) (*broker, error) {
	if id < 0 {
		return nil, err
	}

	tryLoad := ctx != nil
	tries := 0
start:
	var broker *broker
	if id < 0 {
		broker = findBroker(cl.loadSeeds(), id)
	} else {
		cl.brokersMu.RLock()
		broker = findBroker(cl.brokers, id)
		cl.brokersMu.RUnlock()
	}

	if broker == nil {
		if tryLoad {
			if loadErr := cl.fetchBrokerMetadata(ctx); loadErr != nil {
				return nil, loadErr
			}
			// We will retry loading up to two times, if we load broker
			// metadata twice successfully but neither load has the broker
			// we are looking for, then we say our broker does not exist.
			tries++
			if tries < 2 {
				goto start
			}
		}
		return nil, err
	}
	return broker, nil
}

// controller returns the controller broker, forcing a broker load if
// necessary.
func (cl *Client) controller(ctx context.Context) (b *broker, err error) {
	get := func() int32 {
		cl.controllerIDMu.Lock()
		defer cl.controllerIDMu.Unlock()
		return cl.controllerID
	}

	defer func() {
		if ec := (*errUnknownController)(nil); errors.As(err, &ec) {
			cl.forgetControllerID(ec.id)
		}
	}()

	var id int32
	if id = get(); id < 0 {
		if err := cl.fetchBrokerMetadata(ctx); err != nil {
			return nil, err
		}
		if id = get(); id < 0 {
			return nil, &errUnknownController{id}
		}
	}

	return cl.brokerOrErr(nil, id, &errUnknownController{id})
}

// forgetControllerID is called once an admin requests sees NOT_CONTROLLER.
func (cl *Client) forgetControllerID(id int32) {
	cl.controllerIDMu.Lock()
	defer cl.controllerIDMu.Unlock()
	if cl.controllerID == id {
		cl.controllerID = unknownControllerID
	}
}

const (
	coordinatorTypeGroup int8 = 0
	coordinatorTypeTxn   int8 = 1
)

type coordinatorKey struct {
	name string
	typ  int8
}

type coordinatorLoad struct {
	loadWait chan struct{}
	node     int32
	err      error
}

func (cl *Client) loadCoordinator(ctx context.Context, typ int8, key string) (*broker, error) {
	berr := cl.loadCoordinators(ctx, typ, key)[key]
	return berr.b, berr.err
}

func (cl *Client) loadCoordinators(ctx context.Context, typ int8, keys ...string) map[string]brokerOrErr {
	mch := make(chan map[string]brokerOrErr, 1)
	go func() { mch <- cl.doLoadCoordinators(ctx, typ, keys...) }()
	select {
	case m := <-mch:
		return m
	case <-ctx.Done():
		m := make(map[string]brokerOrErr, len(keys))
		for _, k := range keys {
			m[k] = brokerOrErr{nil, ctx.Err()}
		}
		return m
	}
}

// doLoadCoordinators uses the caller context to cancel loading metadata
// (brokerOrErr), but we use the client context to actually issue the request.
// There should be only one direct call to doLoadCoordinators, just above in
// loadCoordinator. It is possible for two requests to be loading the same
// coordinator (in fact, that's the point of this function -- collapse these
// requests). We do not want the first request canceling it's context to cause
// errors for the second request.
//
// It is ok to leave FindCoordinator running even if the caller quits. Worst
// case, we just cache things for some time in the future; yay.
func (cl *Client) doLoadCoordinators(ctx context.Context, typ int8, keys ...string) map[string]brokerOrErr {
	m := make(map[string]brokerOrErr, len(keys))
	if len(keys) == 0 {
		return m
	}

	toRequest := make(map[string]bool, len(keys)) // true == bypass the cache
	for _, key := range keys {
		toRequest[key] = false
	}

	// For each of these keys, we have two cases:
	//
	// 1) The key is cached. It is either loading or loaded. We do not
	// request the key ourselves; we wait for the load to finish.
	//
	// 2) The key is not cached, and we request it.
	//
	// If a key is cached but the coordinator no longer exists for us, we
	// re-request to refresh the coordinator by setting toRequest[key] to
	// true (bypass cache).
	//
	// If we ever request a key ourselves, we do not request it again. We
	// ensure this by deleting from toRequest. We also delete if the key
	// was cached with no error.
	//
	// We could have some keys cached and some that need to be requested.
	// We issue a request but do not request what is cached.
	//
	// Lastly, we only ever trigger one metadata update, which happens if
	// we have an unknown coordinator after we load coordinators.
	var hasLoadedBrokers bool
	for len(toRequest) > 0 {
		var loadWait chan struct{}
		load2key := make(map[*coordinatorLoad][]string)

		cl.coordinatorsMu.Lock()
		for key, bypassCache := range toRequest {
			c, ok := cl.coordinators[coordinatorKey{key, typ}]
			if !ok || bypassCache {
				if loadWait == nil {
					loadWait = make(chan struct{})
				}
				c = &coordinatorLoad{
					loadWait: loadWait,
					err:      errors.New("coordinator was not returned in broker response"),
				}
				cl.coordinators[coordinatorKey{key, typ}] = c
			}
			load2key[c] = append(load2key[c], key)
		}
		cl.coordinatorsMu.Unlock()

		if loadWait == nil { // all coordinators were cached
			hasLoadedBrokers = cl.waitCoordinatorLoad(ctx, typ, load2key, !hasLoadedBrokers, toRequest, m)
			continue
		}

		key2load := make(map[string]*coordinatorLoad)
		req := kmsg.NewPtrFindCoordinatorRequest()
		req.CoordinatorType = typ
		for c, keys := range load2key {
			if c.loadWait == loadWait { // if this is our wait, this is ours to request
				req.CoordinatorKeys = append(req.CoordinatorKeys, keys...)
				for _, key := range keys {
					key2load[key] = c
					delete(toRequest, key)
				}
			}
		}

		cl.cfg.logger.Log(LogLevelDebug, "prepared to issue find coordinator request",
			"coordinator_type", typ,
			"coordinator_keys", req.CoordinatorKeys,
		)

		shards := cl.RequestSharded(cl.ctx, req)

		for _, shard := range shards {
			if shard.Err != nil {
				req := shard.Req.(*kmsg.FindCoordinatorRequest)
				for _, key := range req.CoordinatorKeys {
					c, ok := key2load[key]
					if ok {
						c.err = shard.Err
					}
				}
			} else {
				resp := shard.Resp.(*kmsg.FindCoordinatorResponse)
				for _, rc := range resp.Coordinators {
					c, ok := key2load[rc.Key]
					if ok {
						c.err = kerr.ErrorForCode(rc.ErrorCode)
						c.node = rc.NodeID
					}
				}
			}
		}

		// For anything we loaded, if it has a load failure (including
		// not being replied to), we remove the key from the cache.  We
		// do not want to cache erroring values.
		//
		// We range key2load, which contains only coordinators we are
		// responsible for loading.
		cl.coordinatorsMu.Lock()
		for key, c := range key2load {
			if c.err != nil {
				ck := coordinatorKey{key, typ}
				if loading, ok := cl.coordinators[ck]; ok && loading == c {
					delete(cl.coordinators, ck)
				}
			}
		}
		cl.coordinatorsMu.Unlock()

		close(loadWait)
		hasLoadedBrokers = cl.waitCoordinatorLoad(ctx, typ, load2key, !hasLoadedBrokers, toRequest, m)
	}
	return m
}

// After some prep work, we wait for coordinators to load. We update toRequest
// values with true if the caller should bypass cache and re-load these
// coordinators.
//
// This returns if we load brokers, and populates m with results.
func (cl *Client) waitCoordinatorLoad(ctx context.Context, typ int8, load2key map[*coordinatorLoad][]string, shouldLoadBrokers bool, toRequest map[string]bool, m map[string]brokerOrErr) bool {
	var loadedBrokers bool
	for c, keys := range load2key {
		<-c.loadWait
		for _, key := range keys {
			if c.err != nil {
				delete(toRequest, key)
				m[key] = brokerOrErr{nil, c.err}
				continue
			}

			var brokerCtx context.Context
			if shouldLoadBrokers && !loadedBrokers {
				brokerCtx = ctx
				loadedBrokers = true
			}

			b, err := cl.brokerOrErr(brokerCtx, c.node, &errUnknownCoordinator{c.node, coordinatorKey{key, typ}})
			if err != nil {
				if _, exists := toRequest[key]; exists {
					toRequest[key] = true
					continue
				}
				// If the key does not exist, we just loaded this
				// coordinator and also the brokers. We do not
				// re-request.
			}
			delete(toRequest, key)
			m[key] = brokerOrErr{b, err}
		}
	}
	return loadedBrokers
}

func (cl *Client) maybeDeleteStaleCoordinator(name string, typ int8, err error) bool {
	switch {
	case errors.Is(err, kerr.CoordinatorNotAvailable),
		errors.Is(err, kerr.CoordinatorLoadInProgress),
		errors.Is(err, kerr.NotCoordinator):
		cl.deleteStaleCoordinator(name, typ)
		return true
	}
	return false
}

func (cl *Client) deleteStaleCoordinator(name string, typ int8) {
	cl.coordinatorsMu.Lock()
	defer cl.coordinatorsMu.Unlock()
	k := coordinatorKey{name, typ}
	v := cl.coordinators[k]
	if v == nil {
		return
	}
	select {
	case <-v.loadWait:
		delete(cl.coordinators, k)
	default:
		// We are actively reloading this coordinator.
	}
}

type brokerOrErr struct {
	b   *broker
	err error
}

func (cl *Client) handleAdminReq(ctx context.Context, req kmsg.Request) ResponseShard {
	// Loading a controller can perform some wait; we accept that and do
	// not account for the retries or the time to load the controller as
	// part of the retries / time to issue the req.
	r := cl.retryableBrokerFn(func() (*broker, error) {
		return cl.controller(ctx)
	})

	// The only request that can break mapped metadata is CreatePartitions,
	// because our mapping will still be "valid" but behind the scenes,
	// more partitions exist. If CreatePartitions is going through this
	// client, we preemptively delete any mapping for these topics.
	if t, ok := req.(*kmsg.CreatePartitionsRequest); ok {
		var topics []string
		for i := range t.Topics {
			topics = append(topics, t.Topics[i].Topic)
		}
		cl.maybeDeleteMappedMetadata(false, topics...)
	}

	var d failDial
	r.parseRetryErr = func(resp kmsg.Response, err error) error {
		if err != nil {
			if d.isRepeatedDialFail(err) {
				cl.forgetControllerID(r.last.meta.NodeID)
			}
			return err
		}
		var code int16
		switch t := resp.(type) {
		case *kmsg.CreateTopicsResponse:
			if len(t.Topics) > 0 {
				code = t.Topics[0].ErrorCode
			}
		case *kmsg.DeleteTopicsResponse:
			if len(t.Topics) > 0 {
				code = t.Topics[0].ErrorCode
			}
		case *kmsg.CreatePartitionsResponse:
			if len(t.Topics) > 0 {
				code = t.Topics[0].ErrorCode
			}
		case *kmsg.ElectLeadersResponse:
			if len(t.Topics) > 0 && len(t.Topics[0].Partitions) > 0 {
				code = t.Topics[0].Partitions[0].ErrorCode
			}
		case *kmsg.AlterPartitionAssignmentsResponse:
			code = t.ErrorCode
		case *kmsg.ListPartitionReassignmentsResponse:
			code = t.ErrorCode
		case *kmsg.AlterUserSCRAMCredentialsResponse:
			if len(t.Results) > 0 {
				code = t.Results[0].ErrorCode
			}
		case *kmsg.VoteResponse:
			code = t.ErrorCode
		case *kmsg.BeginQuorumEpochResponse:
			code = t.ErrorCode
		case *kmsg.EndQuorumEpochResponse:
			code = t.ErrorCode
		case *kmsg.DescribeQuorumResponse:
			code = t.ErrorCode
		case *kmsg.AlterPartitionResponse:
			code = t.ErrorCode
		case *kmsg.UpdateFeaturesResponse:
			code = t.ErrorCode
		case *kmsg.EnvelopeResponse:
			code = t.ErrorCode
		}
		if err := kerr.ErrorForCode(code); errors.Is(err, kerr.NotController) {
			// There must be a last broker if we were able to issue
			// the request and get a response.
			cl.forgetControllerID(r.last.meta.NodeID)
			return err
		}
		return nil
	}

	resp, err := r.Request(ctx, req)
	return shard(r.last, req, resp, err)
}

// handleCoordinatorReq issues simple (non-shardable) group or txn requests.
func (cl *Client) handleCoordinatorReq(ctx context.Context, req kmsg.Request) ResponseShard {
	switch t := req.(type) {
	default:
		// All group requests should be listed below, so if it isn't,
		// then we do not know what this request is.
		return shard(nil, req, nil, errors.New("client is too old; this client does not know what to do with this request"))

	/////////
	// TXN // -- all txn reqs are simple
	/////////

	case *kmsg.InitProducerIDRequest:
		if t.TransactionalID != nil {
			return cl.handleCoordinatorReqSimple(ctx, coordinatorTypeTxn, *t.TransactionalID, req)
		}
		// InitProducerID can go to any broker if the transactional ID
		// is nil. By using handleReqWithCoordinator, we get the
		// retryable-error parsing, even though we are not actually
		// using a defined txn coordinator. This is fine; by passing no
		// names, we delete no coordinator.
		coordinator, resp, err := cl.handleReqWithCoordinator(ctx, func() (*broker, error) { return cl.broker(), nil }, coordinatorTypeTxn, "", req)
		return shard(coordinator, req, resp, err)
	case *kmsg.AddOffsetsToTxnRequest:
		return cl.handleCoordinatorReqSimple(ctx, coordinatorTypeTxn, t.TransactionalID, req)
	case *kmsg.EndTxnRequest:
		return cl.handleCoordinatorReqSimple(ctx, coordinatorTypeTxn, t.TransactionalID, req)

	///////////
	// GROUP // -- most group reqs are simple
	///////////

	case *kmsg.OffsetCommitRequest:
		return cl.handleCoordinatorReqSimple(ctx, coordinatorTypeGroup, t.Group, req)
	case *kmsg.TxnOffsetCommitRequest:
		return cl.handleCoordinatorReqSimple(ctx, coordinatorTypeGroup, t.Group, req)
	case *kmsg.JoinGroupRequest:
		return cl.handleCoordinatorReqSimple(ctx, coordinatorTypeGroup, t.Group, req)
	case *kmsg.HeartbeatRequest:
		return cl.handleCoordinatorReqSimple(ctx, coordinatorTypeGroup, t.Group, req)
	case *kmsg.LeaveGroupRequest:
		return cl.handleCoordinatorReqSimple(ctx, coordinatorTypeGroup, t.Group, req)
	case *kmsg.SyncGroupRequest:
		return cl.handleCoordinatorReqSimple(ctx, coordinatorTypeGroup, t.Group, req)
	case *kmsg.OffsetDeleteRequest:
		return cl.handleCoordinatorReqSimple(ctx, coordinatorTypeGroup, t.Group, req)
	}
}

// handleCoordinatorReqSimple issues a request that contains a single group or
// txn to its coordinator.
//
// The error is inspected to see if it is a retryable error and, if so, the
// coordinator is deleted.
func (cl *Client) handleCoordinatorReqSimple(ctx context.Context, typ int8, name string, req kmsg.Request) ResponseShard {
	coordinator, resp, err := cl.handleReqWithCoordinator(ctx, func() (*broker, error) {
		return cl.loadCoordinator(ctx, typ, name)
	}, typ, name, req)
	return shard(coordinator, req, resp, err)
}

// handleReqWithCoordinator actually issues a request to a coordinator and
// does retry handling.
//
// This avoids retries on the two group requests that need to be sharded.
func (cl *Client) handleReqWithCoordinator(
	ctx context.Context,
	coordinator func() (*broker, error),
	typ int8,
	name string, // group ID or the transactional id
	req kmsg.Request,
) (*broker, kmsg.Response, error) {
	r := cl.retryableBrokerFn(coordinator)
	var d failDial
	r.parseRetryErr = func(resp kmsg.Response, err error) error {
		if err != nil {
			if d.isRepeatedDialFail(err) {
				cl.deleteStaleCoordinator(name, typ)
			}
			return err
		}
		var code int16
		switch t := resp.(type) {
		// TXN
		case *kmsg.InitProducerIDResponse:
			code = t.ErrorCode
		case *kmsg.AddOffsetsToTxnResponse:
			code = t.ErrorCode
		case *kmsg.EndTxnResponse:
			code = t.ErrorCode

		// GROUP
		case *kmsg.OffsetCommitResponse:
			if len(t.Topics) > 0 && len(t.Topics[0].Partitions) > 0 {
				code = t.Topics[0].Partitions[0].ErrorCode
			}
		case *kmsg.TxnOffsetCommitResponse:
			if len(t.Topics) > 0 && len(t.Topics[0].Partitions) > 0 {
				code = t.Topics[0].Partitions[0].ErrorCode
			}
		case *kmsg.JoinGroupResponse:
			code = t.ErrorCode
		case *kmsg.HeartbeatResponse:
			code = t.ErrorCode
		case *kmsg.LeaveGroupResponse:
			code = t.ErrorCode
		case *kmsg.SyncGroupResponse:
			code = t.ErrorCode
		}

		// ListGroups, OffsetFetch, DeleteGroups, DescribeGroups, and
		// DescribeTransactions handled in sharding.

		if err := kerr.ErrorForCode(code); cl.maybeDeleteStaleCoordinator(name, typ, err) {
			return err
		}
		return nil
	}

	resp, err := r.Request(ctx, req)
	return r.last, resp, err
}

// Broker returns a handle to a specific broker to directly issue requests to.
// Note that there is no guarantee that this broker exists; if it does not,
// requests will fail with with an unknown broker error.
func (cl *Client) Broker(id int) *Broker {
	return &Broker{
		id: int32(id),
		cl: cl,
	}
}

// DiscoveredBrokers returns all brokers that were discovered from prior
// metadata responses. This does not actually issue a metadata request to load
// brokers; if you wish to ensure this returns all brokers, be sure to manually
// issue a metadata request before this. This also does not include seed
// brokers, which are internally saved under special internal broker IDs (but,
// it does include those brokers under their normal IDs as returned from a
// metadata response).
func (cl *Client) DiscoveredBrokers() []*Broker {
	cl.brokersMu.RLock()
	defer cl.brokersMu.RUnlock()

	var bs []*Broker
	for _, broker := range cl.brokers {
		bs = append(bs, &Broker{id: broker.meta.NodeID, cl: cl})
	}
	return bs
}

// SeedBrokers returns the all seed brokers.
func (cl *Client) SeedBrokers() []*Broker {
	var bs []*Broker
	for _, broker := range cl.loadSeeds() {
		bs = append(bs, &Broker{id: broker.meta.NodeID, cl: cl})
	}
	return bs
}

// UpdateSeedBrokers updates the client's list of seed brokers. Over the course
// of a long period of time, your might replace all brokers that you originally
// specified as seeds. This command allows you to replace the client's list of
// seeds.
//
// This returns an error if any of the input addrs is not a host:port. If the
// input list is empty, the function returns without replacing the seeds.
func (cl *Client) UpdateSeedBrokers(addrs ...string) error {
	if len(addrs) == 0 {
		return nil
	}
	seeds, err := parseSeeds(addrs)
	if err != nil {
		return err
	}

	seedBrokers := make([]*broker, 0, len(seeds))
	for i, seed := range seeds {
		b := cl.newBroker(unknownSeedID(i), seed.host, seed.port, nil)
		seedBrokers = append(seedBrokers, b)
	}

	// We lock to guard against concurrently updating seeds; we do not need
	// the lock for what this usually guards.
	cl.brokersMu.Lock()
	old := cl.loadSeeds()
	cl.seeds.Store(seedBrokers)
	cl.brokersMu.Unlock()

	for _, b := range old {
		b.stopForever()
	}

	return nil
}

// Broker pairs a broker ID with a client to directly issue requests to a
// specific broker.
type Broker struct {
	id int32
	cl *Client
}

// Request issues a request to a broker. If the broker does not exist in the
// client, this returns an unknown broker error. Requests are not retried.
//
// The passed context can be used to cancel a request and return early.
// Note that if the request is not canceled before it is written to Kafka,
// you may just end up canceling and not receiving the response to what Kafka
// inevitably does.
//
// It is more beneficial to always use RetriableRequest.
func (b *Broker) Request(ctx context.Context, req kmsg.Request) (kmsg.Response, error) {
	return b.request(ctx, false, req)
}

// RetriableRequest issues a request to a broker the same as Broker, but
// retries in the face of retryable broker connection errors. This does not
// retry on response internal errors.
func (b *Broker) RetriableRequest(ctx context.Context, req kmsg.Request) (kmsg.Response, error) {
	return b.request(ctx, true, req)
}

func (b *Broker) request(ctx context.Context, retry bool, req kmsg.Request) (kmsg.Response, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var resp kmsg.Response
	var err error
	done := make(chan struct{})

	go func() {
		defer close(done)

		if !retry {
			var br *broker
			br, err = b.cl.brokerOrErr(ctx, b.id, errUnknownBroker)
			if err == nil {
				resp, err = br.waitResp(ctx, req)
			}
		} else {
			resp, err = b.cl.retryableBrokerFn(func() (*broker, error) {
				return b.cl.brokerOrErr(ctx, b.id, errUnknownBroker)
			}).Request(ctx, req)
		}
	}()

	select {
	case <-done:
		return resp, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.cl.ctx.Done():
		return nil, b.cl.ctx.Err()
	}
}

//////////////////////
// REQUEST SHARDING //
//////////////////////

// Below here lies all logic to handle requests that need to be split and sent
// to many brokers. A lot of the logic for each sharding function is very
// similar, but each sharding function uses slightly different types.

// issueShard is a request that has been split and is ready to be sent to the
// given broker ID.
type issueShard struct {
	req    kmsg.Request
	broker int32
	any    bool

	// if non-nil, we could not map this request shard to any broker, and
	// this error is the reason.
	err error
}

// sharder splits a request.
type sharder interface {
	// shard splits a request and returns the requests to issue tied to the
	// brokers to issue the requests to. This can return an error if there
	// is some pre-loading that needs to happen. If an error is returned,
	// the request that was intended for splitting is failed wholesale.
	//
	// Due to sharded requests not being retryable if a response is
	// received, to avoid stale coordinator errors, this function should
	// not use any previously cached metadata.
	//
	// This takes the last error if the request is being retried, which is
	// currently only useful for errBrokerTooOld.
	shard(context.Context, kmsg.Request, error) ([]issueShard, bool, error)

	// onResp is called on a successful response to investigate the
	// response and potentially perform cleanup, and potentially returns an
	// error signifying to retry. See onShardRespErr below for more
	// details.
	onResp(kmsg.Request, kmsg.Response) error

	// merge is a function that can be used to merge sharded responses into
	// one response. This is used by the client.Request method.
	merge([]ResponseShard) (kmsg.Response, error)
}

// handleShardedReq splits and issues requests to brokers, recursively
// splitting as necessary if requests fail and need remapping.
func (cl *Client) handleShardedReq(ctx context.Context, req kmsg.Request) ([]ResponseShard, shardMerge) {
	// First, determine our sharder.
	var sharder sharder
	switch req.(type) {
	case *kmsg.ListOffsetsRequest:
		sharder = &listOffsetsSharder{cl}
	case *kmsg.OffsetFetchRequest:
		sharder = &offsetFetchSharder{cl}
	case *kmsg.FindCoordinatorRequest:
		sharder = &findCoordinatorSharder{cl}
	case *kmsg.DescribeGroupsRequest:
		sharder = &describeGroupsSharder{cl}
	case *kmsg.ListGroupsRequest:
		sharder = &listGroupsSharder{cl}
	case *kmsg.DeleteRecordsRequest:
		sharder = &deleteRecordsSharder{cl}
	case *kmsg.OffsetForLeaderEpochRequest:
		sharder = &offsetForLeaderEpochSharder{cl}
	case *kmsg.AddPartitionsToTxnRequest:
		sharder = &addPartitionsToTxnSharder{cl}
	case *kmsg.WriteTxnMarkersRequest:
		sharder = &writeTxnMarkersSharder{cl}
	case *kmsg.DescribeConfigsRequest:
		sharder = &describeConfigsSharder{cl}
	case *kmsg.AlterConfigsRequest:
		sharder = &alterConfigsSharder{cl}
	case *kmsg.AlterReplicaLogDirsRequest:
		sharder = &alterReplicaLogDirsSharder{cl}
	case *kmsg.DescribeLogDirsRequest:
		sharder = &describeLogDirsSharder{cl}
	case *kmsg.DeleteGroupsRequest:
		sharder = &deleteGroupsSharder{cl}
	case *kmsg.IncrementalAlterConfigsRequest:
		sharder = &incrementalAlterConfigsSharder{cl}
	case *kmsg.DescribeProducersRequest:
		sharder = &describeProducersSharder{cl}
	case *kmsg.DescribeTransactionsRequest:
		sharder = &describeTransactionsSharder{cl}
	case *kmsg.ListTransactionsRequest:
		sharder = &listTransactionsSharder{cl}
	}

	// If a request fails, we re-shard it (in case it needs to be split
	// again). reqTry tracks how many total tries a request piece has had;
	// we quit at either the max configured tries or max configured time.
	type reqTry struct {
		tries   int
		req     kmsg.Request
		lastErr error
	}

	var (
		shardsMu sync.Mutex
		shards   []ResponseShard

		addShard = func(shard ResponseShard) {
			shardsMu.Lock()
			defer shardsMu.Unlock()
			shards = append(shards, shard)
		}

		start        = time.Now()
		retryTimeout = cl.cfg.retryTimeout(req.Key())

		wg    sync.WaitGroup
		issue func(reqTry)
	)

	l := cl.cfg.logger
	debug := l.Level() >= LogLevelDebug

	// issue is called to progressively split and issue requests.
	//
	// This recursively calls itself if a request fails and can be retried.
	// We avoid stack problems because this calls itself in a goroutine.
	issue = func(try reqTry) {
		issues, reshardable, err := sharder.shard(ctx, try.req, try.lastErr)
		if err != nil {
			l.Log(LogLevelDebug, "unable to shard request", "req", kmsg.Key(try.req.Key()).Name(), "previous_tries", try.tries, "err", err)
			addShard(shard(nil, try.req, nil, err)) // failure to shard means data loading failed; this request is failed
			return
		}

		// If the request actually does not need to be issued, we issue
		// it to a random broker. There is no benefit to this, but at
		// least we will return one shard.
		if len(issues) == 0 {
			issues = []issueShard{{
				req: try.req,
				any: true,
			}}
			reshardable = true
		}

		if debug {
			var key int16
			var brokerAnys []string
			for _, issue := range issues {
				key = issue.req.Key()
				if issue.err != nil {
					brokerAnys = append(brokerAnys, "err")
				} else if issue.any {
					brokerAnys = append(brokerAnys, "any")
				} else {
					brokerAnys = append(brokerAnys, fmt.Sprintf("%d", issue.broker))
				}
			}
			l.Log(LogLevelDebug, "sharded request", "req", kmsg.Key(key).Name(), "destinations", brokerAnys)
		}

		for i := range issues {
			myIssue := issues[i]
			myUnderlyingReq := myIssue.req
			var isPinned bool
			if pinned, ok := myIssue.req.(*pinReq); ok {
				myUnderlyingReq = pinned.Request
				isPinned = true
			}

			if myIssue.err != nil {
				addShard(shard(nil, myUnderlyingReq, nil, myIssue.err))
				continue
			}

			tries := try.tries
			wg.Add(1)
			go func() {
				defer wg.Done()
			start:
				tries++

				broker := cl.broker()
				var err error
				if !myIssue.any {
					broker, err = cl.brokerOrErr(ctx, myIssue.broker, errUnknownBroker)
				}
				if err != nil {
					addShard(shard(nil, myUnderlyingReq, nil, err)) // failure to load a broker is a failure to issue a request
					return
				}

				resp, err := broker.waitResp(ctx, myIssue.req)
				var errIsFromResp bool
				if err == nil {
					err = sharder.onResp(myUnderlyingReq, resp) // perform some potential cleanup, and potentially receive an error to retry
					if ke := (*kerr.Error)(nil); errors.As(err, &ke) {
						errIsFromResp = true
					}
				}

				// If we failed to issue the request, we *maybe* will retry.
				// We could have failed to even issue the request or receive
				// a response, which is retryable.
				//
				// If a pinned req fails with errBrokerTooOld, we always retry
				// immediately. The request was not even issued. However, as a
				// safety, we only do this 3 times to avoid some super weird
				// pathological spin loop.
				backoff := cl.cfg.retryBackoff(tries)
				if err != nil &&
					(reshardable && isPinned && errors.Is(err, errBrokerTooOld) && tries <= 3) ||
					(retryTimeout == 0 || time.Now().Add(backoff).Sub(start) <= retryTimeout) && cl.shouldRetry(tries, err) && cl.waitTries(ctx, backoff) {
					// Non-reshardable re-requests just jump back to the
					// top where the broker is loaded. This is the case on
					// requests where the original request is split to
					// dedicated brokers; we do not want to re-shard that.
					if !reshardable {
						l.Log(LogLevelDebug, "sharded request failed, reissuing without resharding", "req", kmsg.Key(myIssue.req.Key()).Name(), "time_since_start", time.Since(start), "tries", try.tries, "err", err)
						goto start
					}
					l.Log(LogLevelDebug, "sharded request failed, resharding and reissuing", "req", kmsg.Key(myIssue.req.Key()).Name(), "time_since_start", time.Since(start), "tries", try.tries, "err", err)
					issue(reqTry{tries, myUnderlyingReq, err})
					return
				}

				// If we pulled an error out of the response body in an attempt
				// to possibly retry, the request was NOT an error that we want
				// to bubble as a shard error. The request was successful, we
				// have a response. Before we add the shard, strip the error.
				// The end user can parse the response ErrorCode.
				if errIsFromResp {
					err = nil
				}
				addShard(shard(broker, myUnderlyingReq, resp, err)) // the error was not retryable
			}()
		}
	}

	issue(reqTry{0, req, nil})
	wg.Wait()

	return shards, sharder.merge
}

// For sharded errors, we prefer to keep retryable errors rather than
// non-retryable errors. We keep the non-retryable if everything is
// non-retryable.
//
// We favor retryable because retryable means we used a stale cache value; we
// clear the stale entries on failure and the retry uses fresh data. The
// request will be split and remapped, and the non-retryable errors will be
// encountered again.
func onRespShardErr(err *error, newKerr error) {
	if newKerr == nil || *err != nil && kerr.IsRetriable(*err) {
		return
	}
	*err = newKerr
}

// a convenience function for when a request needs to be issued identically to
// all brokers.
func (cl *Client) allBrokersShardedReq(ctx context.Context, fn func() kmsg.Request) ([]issueShard, bool, error) {
	if err := cl.fetchBrokerMetadata(ctx); err != nil {
		return nil, false, err
	}

	var issues []issueShard
	cl.brokersMu.RLock()
	for _, broker := range cl.brokers {
		issues = append(issues, issueShard{
			req:    fn(),
			broker: broker.meta.NodeID,
		})
	}
	cl.brokersMu.RUnlock()

	return issues, false, nil // we do NOT re-shard these requests request
}

// a convenience function for saving the first ResponseShard error.
func firstErrMerger(sresps []ResponseShard, merge func(kresp kmsg.Response)) error {
	var firstErr error
	for _, sresp := range sresps {
		if sresp.Err != nil {
			if firstErr == nil {
				firstErr = sresp.Err
			}
			continue
		}
		merge(sresp.Resp)
	}
	return firstErr
}

type mappedMetadataTopic struct {
	t    kmsg.MetadataResponseTopic
	ps   map[int32]kmsg.MetadataResponseTopicPartition
	when time.Time
}

// For NOT_LEADER_FOR_PARTITION:
// We always delete stale metadata. It's possible that a leader rebalance
// happened immediately after we requested metadata; we should not pin to
// the stale metadata for 1s.
//
// For UNKNOWN_TOPIC_OR_PARTITION:
// We only delete stale metadata if it is older than the min age or 1s,
// whichever is smaller. We use 1s even if min age is larger, because we want
// to encourage larger min age for caching purposes. More obvious would be to
// *always* evict the cache here, but if we *just* requested metadata, then
// evicting the cache would cause churn for a topic that genuinely does not
// exist.
func (cl *Client) maybeDeleteMappedMetadata(unknownTopic bool, ts ...string) (shouldRetry bool) {
	if len(ts) == 0 {
		return
	}

	var min time.Duration
	if unknownTopic {
		min = time.Second
		if cl.cfg.metadataMinAge < min {
			min = cl.cfg.metadataMinAge
		}
	}

	cl.mappedMetaMu.Lock()
	defer cl.mappedMetaMu.Unlock()
	for _, t := range ts {
		tcached, exists := cl.mappedMeta[t]
		if exists && (min == 0 || time.Since(tcached.when) > min) {
			shouldRetry = true
			delete(cl.mappedMeta, t)
		}
	}
	return shouldRetry
}

// We only cache for metadata min age. We could theoretically cache forever,
// but an out of band CreatePartitions can result in our metadata being stale
// and us never knowing. So, we choose metadata min age. There are only a few
// requests that are sharded and use metadata, and the one this benefits most
// is ListOffsets. Likely, ListOffsets for the same topic will be issued back
// to back, so not caching for so long is ok.
func (cl *Client) fetchCachedMappedMetadata(ts ...string) (map[string]mappedMetadataTopic, []string) {
	cl.mappedMetaMu.Lock()
	defer cl.mappedMetaMu.Unlock()
	if cl.mappedMeta == nil {
		return nil, ts
	}
	cached := make(map[string]mappedMetadataTopic)
	needed := ts[:0]

	for _, t := range ts {
		tcached, exists := cl.mappedMeta[t]
		if exists && time.Since(tcached.when) < cl.cfg.metadataMinAge {
			cached[t] = tcached
		} else {
			needed = append(needed, t)
			delete(cl.mappedMeta, t)
		}
	}
	return cached, needed
}

// fetchMappedMetadata provides a convenience type of working with metadata;
// this is garbage heavy, so it is only used in one off requests in this
// package.
func (cl *Client) fetchMappedMetadata(ctx context.Context, topics []string, useCache bool) (map[string]mappedMetadataTopic, error) {
	var r map[string]mappedMetadataTopic
	needed := topics
	if useCache {
		r, needed = cl.fetchCachedMappedMetadata(topics...)
		if len(needed) == 0 {
			return r, nil
		}
	}
	if r == nil {
		r = make(map[string]mappedMetadataTopic)
	}

	_, meta, err := cl.fetchMetadataForTopics(ctx, false, needed)
	if err != nil {
		return nil, err
	}

	// Cache the mapped metadata, and also store each topic in the results.
	cl.storeCachedMappedMetadata(meta, func(entry mappedMetadataTopic) {
		r[*entry.t.Topic] = entry
	})

	return r, nil
}

// storeCachedMappedMetadata caches the fetched metadata in the Client, and calls the onEachTopic callback
// function for each topic in the MetadataResponse.
func (cl *Client) storeCachedMappedMetadata(meta *kmsg.MetadataResponse, onEachTopic func(_ mappedMetadataTopic)) {
	cl.mappedMetaMu.Lock()
	defer cl.mappedMetaMu.Unlock()
	if cl.mappedMeta == nil {
		cl.mappedMeta = make(map[string]mappedMetadataTopic)
	}
	when := time.Now()
	for _, topic := range meta.Topics {
		if topic.Topic == nil {
			// We do not request with topic IDs, so we should not
			// receive topic IDs in the response.
			continue
		}
		t := mappedMetadataTopic{
			t:    topic,
			ps:   make(map[int32]kmsg.MetadataResponseTopicPartition),
			when: when,
		}
		cl.mappedMeta[*topic.Topic] = t
		for _, partition := range topic.Partitions {
			t.ps[partition.Partition] = partition
		}

		if onEachTopic != nil {
			onEachTopic(t)
		}
	}
	if len(meta.Topics) != len(cl.mappedMeta) {
		for topic, mapped := range cl.mappedMeta {
			if mapped.when.Equal(when) {
				continue
			}
			if time.Since(mapped.when) > cl.cfg.metadataMinAge {
				delete(cl.mappedMeta, topic)
			}
		}
	}
}

func unknownOrCode(exists bool, code int16) error {
	if !exists {
		return kerr.UnknownTopicOrPartition
	}
	return kerr.ErrorForCode(code)
}

func noLeader(l int32) error {
	if l < 0 {
		return kerr.LeaderNotAvailable
	}
	return nil
}

// This is a helper for the sharded requests below; if mapping metadata fails
// to load topics or partitions, we group the failures by error.
//
// We use a lot of reflect magic to make the actual usage much nicer.
type unknownErrShards struct {
	// load err => topic => mystery slice type
	//
	// The mystery type is basically just []Partition, where Partition can
	// be any kmsg type.
	mapped map[error]map[string]reflect.Value
}

// err stores a new failing partition with its failing error.
//
// partition's type is equal to the arg1 type of l.fn.
func (l *unknownErrShards) err(err error, topic string, partition any) {
	if l.mapped == nil {
		l.mapped = make(map[error]map[string]reflect.Value)
	}
	t := l.mapped[err]
	if t == nil {
		t = make(map[string]reflect.Value)
		l.mapped[err] = t
	}
	slice, ok := t[topic]
	if !ok {
		// We make a slice of the input partition type.
		slice = reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(partition)), 0, 1)
	}

	t[topic] = reflect.Append(slice, reflect.ValueOf(partition))
}

// errs takes an input slice of partitions and stores each with its failing
// error.
//
// partitions is a slice where each element has type of arg1 of l.fn.
func (l *unknownErrShards) errs(err error, topic string, partitions any) {
	v := reflect.ValueOf(partitions)
	for i := 0; i < v.Len(); i++ {
		l.err(err, topic, v.Index(i).Interface())
	}
}

// Returns issueShards for each error stored in l.
//
// This takes a factory function: the first return is a new kmsg.Request, the
// second is a function that adds a topic and its partitions to that request.
//
// Thus, fn is of type func() (kmsg.Request, func(string, []P))
func (l *unknownErrShards) collect(mkreq, mergeParts any) []issueShard {
	if len(l.mapped) == 0 {
		return nil
	}

	var shards []issueShard

	factory := reflect.ValueOf(mkreq)
	perTopic := reflect.ValueOf(mergeParts)
	for err, topics := range l.mapped {
		req := factory.Call(nil)[0]

		var ntopics, npartitions int
		for topic, partitions := range topics {
			ntopics++
			npartitions += partitions.Len()
			perTopic.Call([]reflect.Value{req, reflect.ValueOf(topic), partitions})
		}

		shards = append(shards, issueShard{
			req: req.Interface().(kmsg.Request),
			err: err,
		})
	}

	return shards
}

// handles sharding ListOffsetsRequest
type listOffsetsSharder struct{ *Client }

func (cl *listOffsetsSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.ListOffsetsRequest)

	// For listing offsets, we need the broker leader for each partition we
	// are listing. Thus, we first load metadata for the topics.
	//
	// Metadata loading performs retries; if we fail here, the we do not
	// issue sharded requests.
	var need []string
	for _, topic := range req.Topics {
		need = append(need, topic.Topic)
	}
	mapping, err := cl.fetchMappedMetadata(ctx, need, true)
	if err != nil {
		return nil, false, err
	}

	brokerReqs := make(map[int32]map[string][]kmsg.ListOffsetsRequestTopicPartition)
	var unknowns unknownErrShards

	// For any topic or partition that had an error load, we blindly issue
	// a load to the first seed broker. We expect the list to fail, but it
	// is the best we could do.
	for _, topic := range req.Topics {
		t := topic.Topic
		tmapping, exists := mapping[t]
		if err := unknownOrCode(exists, tmapping.t.ErrorCode); err != nil {
			unknowns.errs(err, t, topic.Partitions)
			continue
		}
		for _, partition := range topic.Partitions {
			p, exists := tmapping.ps[partition.Partition]
			if err := unknownOrCode(exists, p.ErrorCode); err != nil {
				unknowns.err(err, t, partition)
				continue
			}
			if err := noLeader(p.Leader); err != nil {
				unknowns.err(err, t, partition)
				continue
			}

			brokerReq := brokerReqs[p.Leader]
			if brokerReq == nil {
				brokerReq = make(map[string][]kmsg.ListOffsetsRequestTopicPartition)
				brokerReqs[p.Leader] = brokerReq
			}
			brokerReq[t] = append(brokerReq[t], partition)
		}
	}

	mkreq := func() *kmsg.ListOffsetsRequest {
		r := kmsg.NewPtrListOffsetsRequest()
		r.ReplicaID = req.ReplicaID
		r.IsolationLevel = req.IsolationLevel
		return r
	}

	var issues []issueShard
	for brokerID, brokerReq := range brokerReqs {
		req := mkreq()
		for topic, parts := range brokerReq {
			reqTopic := kmsg.NewListOffsetsRequestTopic()
			reqTopic.Topic = topic
			reqTopic.Partitions = parts
			req.Topics = append(req.Topics, reqTopic)
		}
		issues = append(issues, issueShard{
			req:    req,
			broker: brokerID,
		})
	}

	return append(issues, unknowns.collect(mkreq, func(r *kmsg.ListOffsetsRequest, topic string, parts []kmsg.ListOffsetsRequestTopicPartition) {
		reqTopic := kmsg.NewListOffsetsRequestTopic()
		reqTopic.Topic = topic
		reqTopic.Partitions = parts
		r.Topics = append(r.Topics, reqTopic)
	})...), true, nil // this is reshardable
}

func (cl *listOffsetsSharder) onResp(_ kmsg.Request, kresp kmsg.Response) error {
	var (
		resp         = kresp.(*kmsg.ListOffsetsResponse)
		del          []string
		retErr       error
		unknownTopic bool
	)

	for i := range resp.Topics {
		t := &resp.Topics[i]
		for j := range t.Partitions {
			p := &t.Partitions[j]
			err := kerr.ErrorForCode(p.ErrorCode)
			if err == kerr.UnknownTopicOrPartition || err == kerr.NotLeaderForPartition {
				del = append(del, t.Topic)
				unknownTopic = unknownTopic || err == kerr.UnknownTopicOrPartition
			}
			onRespShardErr(&retErr, err)
		}
	}
	if cl.maybeDeleteMappedMetadata(unknownTopic, del...) {
		return retErr
	}
	return nil
}

func (*listOffsetsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrListOffsetsResponse()
	topics := make(map[string][]kmsg.ListOffsetsResponseTopicPartition)

	firstErr := firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.ListOffsetsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis

		for _, topic := range resp.Topics {
			topics[topic.Topic] = append(topics[topic.Topic], topic.Partitions...)
		}
	})
	for topic, partitions := range topics {
		respTopic := kmsg.NewListOffsetsResponseTopic()
		respTopic.Topic = topic
		respTopic.Partitions = partitions
		merged.Topics = append(merged.Topics, respTopic)
	}
	return merged, firstErr
}

// handles sharding OffsetFetchRequest
type offsetFetchSharder struct{ *Client }

func offsetFetchReqToGroup(req *kmsg.OffsetFetchRequest) kmsg.OffsetFetchRequestGroup {
	g := kmsg.NewOffsetFetchRequestGroup()
	g.Group = req.Group
	for _, topic := range req.Topics {
		reqTopic := kmsg.NewOffsetFetchRequestGroupTopic()
		reqTopic.Topic = topic.Topic
		reqTopic.Partitions = topic.Partitions
		g.Topics = append(g.Topics, reqTopic)
	}
	return g
}

func offsetFetchGroupToReq(requireStable bool, group kmsg.OffsetFetchRequestGroup) *kmsg.OffsetFetchRequest {
	req := kmsg.NewPtrOffsetFetchRequest()
	req.RequireStable = requireStable
	req.Group = group.Group
	for _, topic := range group.Topics {
		reqTopic := kmsg.NewOffsetFetchRequestTopic()
		reqTopic.Topic = topic.Topic
		reqTopic.Partitions = topic.Partitions
		req.Topics = append(req.Topics, reqTopic)
	}
	return req
}

func offsetFetchRespToGroup(req *kmsg.OffsetFetchRequest, resp *kmsg.OffsetFetchResponse) kmsg.OffsetFetchResponseGroup {
	g := kmsg.NewOffsetFetchResponseGroup()
	g.Group = req.Group
	g.ErrorCode = resp.ErrorCode
	for _, topic := range resp.Topics {
		t := kmsg.NewOffsetFetchResponseGroupTopic()
		t.Topic = topic.Topic
		for _, partition := range topic.Partitions {
			p := kmsg.NewOffsetFetchResponseGroupTopicPartition()
			p.Partition = partition.Partition
			p.Offset = partition.Offset
			p.LeaderEpoch = partition.LeaderEpoch
			p.Metadata = partition.Metadata
			p.ErrorCode = partition.ErrorCode
			t.Partitions = append(t.Partitions, p)
		}
		g.Topics = append(g.Topics, t)
	}
	return g
}

func offsetFetchRespGroupIntoResp(g kmsg.OffsetFetchResponseGroup, into *kmsg.OffsetFetchResponse) {
	into.ErrorCode = g.ErrorCode
	into.Topics = into.Topics[:0]
	for _, topic := range g.Topics {
		t := kmsg.NewOffsetFetchResponseTopic()
		t.Topic = topic.Topic
		for _, partition := range topic.Partitions {
			p := kmsg.NewOffsetFetchResponseTopicPartition()
			p.Partition = partition.Partition
			p.Offset = partition.Offset
			p.LeaderEpoch = partition.LeaderEpoch
			p.Metadata = partition.Metadata
			p.ErrorCode = partition.ErrorCode
			t.Partitions = append(t.Partitions, p)
		}
		into.Topics = append(into.Topics, t)
	}
}

func (cl *offsetFetchSharder) shard(ctx context.Context, kreq kmsg.Request, lastErr error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.OffsetFetchRequest)

	// We always try batching and only split at the end if lastErr
	// indicates too old. We convert to batching immediately.
	dup := *req
	req = &dup

	if len(req.Groups) == 0 {
		req.Groups = append(req.Groups, offsetFetchReqToGroup(req))
	}
	groups := make([]string, 0, len(req.Groups))
	for i := range req.Groups {
		groups = append(groups, req.Groups[i].Group)
	}

	coordinators := cl.loadCoordinators(ctx, coordinatorTypeGroup, groups...)

	// Loading coordinators can have each group fail with its unique error,
	// or with a kerr.Error that can be merged. Unique errors get their own
	// failure shard, while kerr.Error's get merged.
	type unkerr struct {
		err   error
		group kmsg.OffsetFetchRequestGroup
	}
	var (
		brokerReqs = make(map[int32]*kmsg.OffsetFetchRequest)
		kerrs      = make(map[*kerr.Error][]kmsg.OffsetFetchRequestGroup)
		unkerrs    []unkerr
	)

	newReq := func(groups ...kmsg.OffsetFetchRequestGroup) *kmsg.OffsetFetchRequest {
		newReq := kmsg.NewPtrOffsetFetchRequest()
		newReq.RequireStable = req.RequireStable
		newReq.Groups = groups
		return newReq
	}

	for _, group := range req.Groups {
		berr := coordinators[group.Group]
		var ke *kerr.Error
		switch {
		case berr.err == nil:
			brokerReq := brokerReqs[berr.b.meta.NodeID]
			if brokerReq == nil {
				brokerReq = newReq()
				brokerReqs[berr.b.meta.NodeID] = brokerReq
			}
			brokerReq.Groups = append(brokerReq.Groups, group)
		case errors.As(berr.err, &ke):
			kerrs[ke] = append(kerrs[ke], group)
		default:
			unkerrs = append(unkerrs, unkerr{berr.err, group})
		}
	}

	splitReq := errors.Is(lastErr, errBrokerTooOld)

	var issues []issueShard
	for id, req := range brokerReqs {
		if splitReq {
			for _, group := range req.Groups {
				req := offsetFetchGroupToReq(req.RequireStable, group)
				issues = append(issues, issueShard{
					req:    &pinReq{Request: req, pinMax: true, max: 7},
					broker: id,
				})
			}
		} else if len(req.Groups) == 1 {
			single := offsetFetchGroupToReq(req.RequireStable, req.Groups[0])
			single.Groups = req.Groups
			issues = append(issues, issueShard{
				req:    single,
				broker: id,
			})
		} else {
			issues = append(issues, issueShard{
				req:    &pinReq{Request: req, pinMin: len(req.Groups) > 1, min: 8},
				broker: id,
			})
		}
	}
	for _, unkerr := range unkerrs {
		issues = append(issues, issueShard{
			req: newReq(unkerr.group),
			err: unkerr.err,
		})
	}
	for kerr, groups := range kerrs {
		issues = append(issues, issueShard{
			req: newReq(groups...),
			err: kerr,
		})
	}

	return issues, true, nil // reshardable to load correct coordinators
}

func (cl *offsetFetchSharder) onResp(kreq kmsg.Request, kresp kmsg.Response) error {
	req := kreq.(*kmsg.OffsetFetchRequest)
	resp := kresp.(*kmsg.OffsetFetchResponse)

	switch len(resp.Groups) {
	case 0:
		// Requested no groups: move top level into batch for v0-v7 to
		// v8 forward compat.
		resp.Groups = append(resp.Groups, offsetFetchRespToGroup(req, resp))
	case 1:
		// Requested 1 group v8+: set top level for v0-v7 back-compat.
		offsetFetchRespGroupIntoResp(resp.Groups[0], resp)
	default:
	}

	var retErr error
	for i := range resp.Groups {
		group := &resp.Groups[i]
		err := kerr.ErrorForCode(group.ErrorCode)
		cl.maybeDeleteStaleCoordinator(group.Group, coordinatorTypeGroup, err)
		onRespShardErr(&retErr, err)
	}

	// For a final bit of extra fun, v0 and v1 do not have a top level
	// error code but instead a per-partition error code. If the
	// coordinator is loading &c, then all per-partition error codes are
	// the same so we only need to look at the first partition.
	if resp.Version < 2 && len(resp.Topics) > 0 && len(resp.Topics[0].Partitions) > 0 {
		code := resp.Topics[0].Partitions[0].ErrorCode
		err := kerr.ErrorForCode(code)
		cl.maybeDeleteStaleCoordinator(req.Group, coordinatorTypeGroup, err)
		onRespShardErr(&retErr, err)
	}

	return retErr
}

func (*offsetFetchSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrOffsetFetchResponse()
	return merged, firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.OffsetFetchResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		merged.Groups = append(merged.Groups, resp.Groups...)

		// Old requests only support one group; *either* the commit
		// used multiple groups and they are expecting the batch
		// response, *or* the commit used one group and we always merge
		// that one group into the old format.
		if len(resp.Groups) == 1 {
			offsetFetchRespGroupIntoResp(resp.Groups[0], merged)
		}
	})
}

// handles sharding FindCoordinatorRequest
type findCoordinatorSharder struct{ *Client }

func findCoordinatorRespCoordinatorIntoResp(c kmsg.FindCoordinatorResponseCoordinator, into *kmsg.FindCoordinatorResponse) {
	into.NodeID = c.NodeID
	into.Host = c.Host
	into.Port = c.Port
	into.ErrorCode = c.ErrorCode
	into.ErrorMessage = c.ErrorMessage
}

func (*findCoordinatorSharder) shard(_ context.Context, kreq kmsg.Request, lastErr error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.FindCoordinatorRequest)

	// We always try batching and only split at the end if lastErr
	// indicates too old. We convert to batching immediately.
	dup := *req
	req = &dup

	uniq := make(map[string]struct{}, len(req.CoordinatorKeys))
	if len(req.CoordinatorKeys) == 0 {
		uniq[req.CoordinatorKey] = struct{}{}
	} else {
		for _, key := range req.CoordinatorKeys {
			uniq[key] = struct{}{}
		}
	}
	req.CoordinatorKeys = req.CoordinatorKeys[:0]
	for key := range uniq {
		req.CoordinatorKeys = append(req.CoordinatorKeys, key)
	}
	if len(req.CoordinatorKeys) == 1 {
		req.CoordinatorKey = req.CoordinatorKeys[0]
	}

	splitReq := errors.Is(lastErr, errBrokerTooOld)
	if !splitReq {
		// With only one key, we do not need to split nor pin this.
		if len(req.CoordinatorKeys) <= 1 {
			return []issueShard{{req: req, any: true}}, false, nil
		}
		return []issueShard{{
			req: &pinReq{Request: req, pinMin: true, min: 4},
			any: true,
		}}, true, nil // this is "reshardable", in that we will split the request next
	}

	var issues []issueShard
	for _, key := range req.CoordinatorKeys {
		sreq := kmsg.NewPtrFindCoordinatorRequest()
		sreq.CoordinatorType = req.CoordinatorType
		sreq.CoordinatorKey = key
		issues = append(issues, issueShard{
			req: &pinReq{Request: sreq, pinMax: true, max: 3},
			any: true,
		})
	}
	return issues, false, nil // not reshardable
}

func (*findCoordinatorSharder) onResp(kreq kmsg.Request, kresp kmsg.Response) error {
	req := kreq.(*kmsg.FindCoordinatorRequest)
	resp := kresp.(*kmsg.FindCoordinatorResponse)

	switch len(resp.Coordinators) {
	case 0:
		// Convert v3 and prior to v4+
		rc := kmsg.NewFindCoordinatorResponseCoordinator()
		rc.Key = req.CoordinatorKey
		rc.NodeID = resp.NodeID
		rc.Host = resp.Host
		rc.Port = resp.Port
		rc.ErrorCode = resp.ErrorCode
		rc.ErrorMessage = resp.ErrorMessage
		resp.Coordinators = append(resp.Coordinators, rc)
	case 1:
		// Convert v4 to v3 and prior
		findCoordinatorRespCoordinatorIntoResp(resp.Coordinators[0], resp)
	}

	return nil
}

func (*findCoordinatorSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrFindCoordinatorResponse()
	return merged, firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.FindCoordinatorResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		merged.Coordinators = append(merged.Coordinators, resp.Coordinators...)

		if len(resp.Coordinators) == 1 {
			findCoordinatorRespCoordinatorIntoResp(resp.Coordinators[0], merged)
		}
	})
}

// handles sharding DescribeGroupsRequest
type describeGroupsSharder struct{ *Client }

func (cl *describeGroupsSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.DescribeGroupsRequest)

	coordinators := cl.loadCoordinators(ctx, coordinatorTypeGroup, req.Groups...)
	type unkerr struct {
		err   error
		group string
	}
	var (
		brokerReqs = make(map[int32]*kmsg.DescribeGroupsRequest)
		kerrs      = make(map[*kerr.Error][]string)
		unkerrs    []unkerr
	)

	newReq := func(groups ...string) *kmsg.DescribeGroupsRequest {
		newReq := kmsg.NewPtrDescribeGroupsRequest()
		newReq.IncludeAuthorizedOperations = req.IncludeAuthorizedOperations
		newReq.Groups = groups
		return newReq
	}

	for _, group := range req.Groups {
		berr := coordinators[group]
		var ke *kerr.Error
		switch {
		case berr.err == nil:
			brokerReq := brokerReqs[berr.b.meta.NodeID]
			if brokerReq == nil {
				brokerReq = newReq()
				brokerReqs[berr.b.meta.NodeID] = brokerReq
			}
			brokerReq.Groups = append(brokerReq.Groups, group)
		case errors.As(berr.err, &ke):
			kerrs[ke] = append(kerrs[ke], group)
		default:
			unkerrs = append(unkerrs, unkerr{berr.err, group})
		}
	}

	var issues []issueShard
	for id, req := range brokerReqs {
		issues = append(issues, issueShard{
			req:    req,
			broker: id,
		})
	}
	for _, unkerr := range unkerrs {
		issues = append(issues, issueShard{
			req: newReq(unkerr.group),
			err: unkerr.err,
		})
	}
	for kerr, groups := range kerrs {
		issues = append(issues, issueShard{
			req: newReq(groups...),
			err: kerr,
		})
	}

	return issues, true, nil // reshardable to load correct coordinators
}

func (cl *describeGroupsSharder) onResp(_ kmsg.Request, kresp kmsg.Response) error { // cleanup any stale groups
	resp := kresp.(*kmsg.DescribeGroupsResponse)
	var retErr error
	for i := range resp.Groups {
		group := &resp.Groups[i]
		err := kerr.ErrorForCode(group.ErrorCode)
		cl.maybeDeleteStaleCoordinator(group.Group, coordinatorTypeGroup, err)
		onRespShardErr(&retErr, err)
	}
	return retErr
}

func (*describeGroupsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrDescribeGroupsResponse()
	return merged, firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.DescribeGroupsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		merged.Groups = append(merged.Groups, resp.Groups...)
	})
}

// handles sharding ListGroupsRequest
type listGroupsSharder struct{ *Client }

func (cl *listGroupsSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.ListGroupsRequest)
	return cl.allBrokersShardedReq(ctx, func() kmsg.Request {
		dup := *req
		return &dup
	})
}

func (*listGroupsSharder) onResp(_ kmsg.Request, kresp kmsg.Response) error {
	resp := kresp.(*kmsg.ListGroupsResponse)
	return kerr.ErrorForCode(resp.ErrorCode)
}

func (*listGroupsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrListGroupsResponse()
	return merged, firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.ListGroupsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		if merged.ErrorCode == 0 {
			merged.ErrorCode = resp.ErrorCode
		}
		merged.Groups = append(merged.Groups, resp.Groups...)
	})
}

// handle sharding DeleteRecordsRequest
type deleteRecordsSharder struct{ *Client }

func (cl *deleteRecordsSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.DeleteRecordsRequest)

	var need []string
	for _, topic := range req.Topics {
		need = append(need, topic.Topic)
	}
	mapping, err := cl.fetchMappedMetadata(ctx, need, true)
	if err != nil {
		return nil, false, err
	}

	brokerReqs := make(map[int32]map[string][]kmsg.DeleteRecordsRequestTopicPartition)
	var unknowns unknownErrShards

	for _, topic := range req.Topics {
		t := topic.Topic
		tmapping, exists := mapping[t]
		if err := unknownOrCode(exists, tmapping.t.ErrorCode); err != nil {
			unknowns.errs(err, t, topic.Partitions)
			continue
		}
		for _, partition := range topic.Partitions {
			p, exists := tmapping.ps[partition.Partition]
			if err := unknownOrCode(exists, p.ErrorCode); err != nil {
				unknowns.err(err, t, partition)
				continue
			}
			if err := noLeader(p.Leader); err != nil {
				unknowns.err(err, t, partition)
				continue
			}

			brokerReq := brokerReqs[p.Leader]
			if brokerReq == nil {
				brokerReq = make(map[string][]kmsg.DeleteRecordsRequestTopicPartition)
				brokerReqs[p.Leader] = brokerReq
			}
			brokerReq[t] = append(brokerReq[t], partition)
		}
	}

	mkreq := func() *kmsg.DeleteRecordsRequest {
		r := kmsg.NewPtrDeleteRecordsRequest()
		r.TimeoutMillis = req.TimeoutMillis
		return r
	}

	var issues []issueShard
	for brokerID, brokerReq := range brokerReqs {
		req := mkreq()
		for topic, parts := range brokerReq {
			reqTopic := kmsg.NewDeleteRecordsRequestTopic()
			reqTopic.Topic = topic
			reqTopic.Partitions = parts
			req.Topics = append(req.Topics, reqTopic)
		}
		issues = append(issues, issueShard{
			req:    req,
			broker: brokerID,
		})
	}

	return append(issues, unknowns.collect(mkreq, func(r *kmsg.DeleteRecordsRequest, topic string, parts []kmsg.DeleteRecordsRequestTopicPartition) {
		reqTopic := kmsg.NewDeleteRecordsRequestTopic()
		reqTopic.Topic = topic
		reqTopic.Partitions = parts
		r.Topics = append(r.Topics, reqTopic)
	})...), true, nil // this is reshardable
}

func (cl *deleteRecordsSharder) onResp(_ kmsg.Request, kresp kmsg.Response) error {
	var (
		resp         = kresp.(*kmsg.DeleteRecordsResponse)
		del          []string
		retErr       error
		unknownTopic bool
	)
	for i := range resp.Topics {
		t := &resp.Topics[i]
		for j := range t.Partitions {
			p := &t.Partitions[j]
			err := kerr.ErrorForCode(p.ErrorCode)
			if err == kerr.UnknownTopicOrPartition || err == kerr.NotLeaderForPartition {
				del = append(del, t.Topic)
				unknownTopic = unknownTopic || err == kerr.UnknownTopicOrPartition
			}
			onRespShardErr(&retErr, err)
		}
	}
	if cl.maybeDeleteMappedMetadata(unknownTopic, del...) {
		return retErr
	}
	return nil
}

func (*deleteRecordsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrDeleteRecordsResponse()
	topics := make(map[string][]kmsg.DeleteRecordsResponseTopicPartition)

	firstErr := firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.DeleteRecordsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis

		for _, topic := range resp.Topics {
			topics[topic.Topic] = append(topics[topic.Topic], topic.Partitions...)
		}
	})
	for topic, partitions := range topics {
		respTopic := kmsg.NewDeleteRecordsResponseTopic()
		respTopic.Topic = topic
		respTopic.Partitions = partitions
		merged.Topics = append(merged.Topics, respTopic)
	}
	return merged, firstErr
}

// handle sharding OffsetForLeaderEpochRequest
type offsetForLeaderEpochSharder struct{ *Client }

func (cl *offsetForLeaderEpochSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.OffsetForLeaderEpochRequest)

	var need []string
	for _, topic := range req.Topics {
		need = append(need, topic.Topic)
	}
	mapping, err := cl.fetchMappedMetadata(ctx, need, true)
	if err != nil {
		return nil, false, err
	}

	brokerReqs := make(map[int32]map[string][]kmsg.OffsetForLeaderEpochRequestTopicPartition)
	var unknowns unknownErrShards

	for _, topic := range req.Topics {
		t := topic.Topic
		tmapping, exists := mapping[t]
		if err := unknownOrCode(exists, tmapping.t.ErrorCode); err != nil {
			unknowns.errs(err, t, topic.Partitions)
			continue
		}
		for _, partition := range topic.Partitions {
			p, exists := tmapping.ps[partition.Partition]
			if err := unknownOrCode(exists, p.ErrorCode); err != nil {
				unknowns.err(err, t, partition)
				continue
			}
			if err := noLeader(p.Leader); err != nil {
				unknowns.err(err, t, partition)
				continue
			}

			brokerReq := brokerReqs[p.Leader]
			if brokerReq == nil {
				brokerReq = make(map[string][]kmsg.OffsetForLeaderEpochRequestTopicPartition)
				brokerReqs[p.Leader] = brokerReq
			}
			brokerReq[topic.Topic] = append(brokerReq[topic.Topic], partition)
		}
	}

	mkreq := func() *kmsg.OffsetForLeaderEpochRequest {
		r := kmsg.NewPtrOffsetForLeaderEpochRequest()
		r.ReplicaID = req.ReplicaID
		return r
	}

	var issues []issueShard
	for brokerID, brokerReq := range brokerReqs {
		req := mkreq()
		for topic, parts := range brokerReq {
			reqTopic := kmsg.NewOffsetForLeaderEpochRequestTopic()
			reqTopic.Topic = topic
			reqTopic.Partitions = parts
			req.Topics = append(req.Topics, reqTopic)
		}
		issues = append(issues, issueShard{
			req:    req,
			broker: brokerID,
		})
	}

	return append(issues, unknowns.collect(mkreq, func(r *kmsg.OffsetForLeaderEpochRequest, topic string, parts []kmsg.OffsetForLeaderEpochRequestTopicPartition) {
		reqTopic := kmsg.NewOffsetForLeaderEpochRequestTopic()
		reqTopic.Topic = topic
		reqTopic.Partitions = parts
		r.Topics = append(r.Topics, reqTopic)
	})...), true, nil // this is reshardable
}

func (cl *offsetForLeaderEpochSharder) onResp(_ kmsg.Request, kresp kmsg.Response) error {
	var (
		resp         = kresp.(*kmsg.OffsetForLeaderEpochResponse)
		del          []string
		retErr       error
		unknownTopic bool
	)
	for i := range resp.Topics {
		t := &resp.Topics[i]
		for j := range t.Partitions {
			p := &t.Partitions[j]
			err := kerr.ErrorForCode(p.ErrorCode)
			if err == kerr.UnknownTopicOrPartition || err == kerr.NotLeaderForPartition {
				del = append(del, t.Topic)
				unknownTopic = unknownTopic || err == kerr.UnknownTopicOrPartition
			}
			onRespShardErr(&retErr, err)
		}
	}
	if cl.maybeDeleteMappedMetadata(unknownTopic, del...) {
		return retErr
	}
	return nil
}

func (*offsetForLeaderEpochSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrOffsetForLeaderEpochResponse()
	topics := make(map[string][]kmsg.OffsetForLeaderEpochResponseTopicPartition)

	firstErr := firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.OffsetForLeaderEpochResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis

		for _, topic := range resp.Topics {
			topics[topic.Topic] = append(topics[topic.Topic], topic.Partitions...)
		}
	})
	for topic, partitions := range topics {
		respTopic := kmsg.NewOffsetForLeaderEpochResponseTopic()
		respTopic.Topic = topic
		respTopic.Partitions = partitions
		merged.Topics = append(merged.Topics, respTopic)
	}
	return merged, firstErr
}

// handle sharding AddPartitionsToTXn, where v4+ switched to batch requests
type addPartitionsToTxnSharder struct{ *Client }

func addPartitionsReqToTxn(req *kmsg.AddPartitionsToTxnRequest) {
	t := kmsg.NewAddPartitionsToTxnRequestTransaction()
	t.TransactionalID = req.TransactionalID
	t.ProducerID = req.ProducerID
	t.ProducerEpoch = req.ProducerEpoch
	for i := range req.Topics {
		rt := &req.Topics[i]
		tt := kmsg.NewAddPartitionsToTxnRequestTransactionTopic()
		tt.Topic = rt.Topic
		tt.Partitions = rt.Partitions
		t.Topics = append(t.Topics, tt)
	}
	req.Transactions = append(req.Transactions, t)
}

func addPartitionsTxnToReq(req *kmsg.AddPartitionsToTxnRequest) {
	if len(req.Transactions) != 1 {
		return
	}
	t0 := &req.Transactions[0]
	req.TransactionalID = t0.TransactionalID
	req.ProducerID = t0.ProducerID
	req.ProducerEpoch = t0.ProducerEpoch
	for _, tt := range t0.Topics {
		rt := kmsg.NewAddPartitionsToTxnRequestTopic()
		rt.Topic = tt.Topic
		rt.Partitions = tt.Partitions
		req.Topics = append(req.Topics, rt)
	}
}

func addPartitionsTxnToResp(resp *kmsg.AddPartitionsToTxnResponse) {
	if len(resp.Transactions) == 0 {
		return
	}
	t0 := &resp.Transactions[0]
	for _, tt := range t0.Topics {
		rt := kmsg.NewAddPartitionsToTxnResponseTopic()
		rt.Topic = tt.Topic
		for _, tp := range tt.Partitions {
			rp := kmsg.NewAddPartitionsToTxnResponseTopicPartition()
			rp.Partition = tp.Partition
			rp.ErrorCode = tp.ErrorCode
			rt.Partitions = append(rt.Partitions, rp)
		}
		resp.Topics = append(resp.Topics, rt)
	}
}

func (cl *addPartitionsToTxnSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.AddPartitionsToTxnRequest)

	if len(req.Transactions) == 0 {
		addPartitionsReqToTxn(req)
	}
	txnIDs := make([]string, 0, len(req.Transactions))
	for i := range req.Transactions {
		txnIDs = append(txnIDs, req.Transactions[i].TransactionalID)
	}
	coordinators := cl.loadCoordinators(ctx, coordinatorTypeTxn, txnIDs...)

	type unkerr struct {
		err error
		txn kmsg.AddPartitionsToTxnRequestTransaction
	}
	var (
		brokerReqs = make(map[int32]*kmsg.AddPartitionsToTxnRequest)
		kerrs      = make(map[*kerr.Error][]kmsg.AddPartitionsToTxnRequestTransaction)
		unkerrs    []unkerr
	)

	newReq := func(txns ...kmsg.AddPartitionsToTxnRequestTransaction) *kmsg.AddPartitionsToTxnRequest {
		req := kmsg.NewPtrAddPartitionsToTxnRequest()
		req.Transactions = txns
		addPartitionsTxnToReq(req)
		return req
	}

	for _, txn := range req.Transactions {
		berr := coordinators[txn.TransactionalID]
		var ke *kerr.Error
		switch {
		case berr.err == nil:
			brokerReq := brokerReqs[berr.b.meta.NodeID]
			if brokerReq == nil {
				brokerReq = newReq(txn)
				brokerReqs[berr.b.meta.NodeID] = brokerReq
			} else {
				brokerReq.Transactions = append(brokerReq.Transactions, txn)
			}
		case errors.As(berr.err, &ke):
			kerrs[ke] = append(kerrs[ke], txn)
		default:
			unkerrs = append(unkerrs, unkerr{berr.err, txn})
		}
	}

	var issues []issueShard
	for id, req := range brokerReqs {
		if len(req.Transactions) <= 1 || len(req.Transactions) == 1 && !req.Transactions[0].VerifyOnly {
			issues = append(issues, issueShard{
				req:    &pinReq{Request: req, pinMax: true, max: 3},
				broker: id,
			})
		} else {
			issues = append(issues, issueShard{
				req:    req,
				broker: id,
			})
		}
	}
	for _, unkerr := range unkerrs {
		issues = append(issues, issueShard{
			req: newReq(unkerr.txn),
			err: unkerr.err,
		})
	}
	for kerr, txns := range kerrs {
		issues = append(issues, issueShard{
			req: newReq(txns...),
			err: kerr,
		})
	}

	return issues, true, nil // reshardable to load correct coordinators
}

func (cl *addPartitionsToTxnSharder) onResp(kreq kmsg.Request, kresp kmsg.Response) error {
	req := kreq.(*kmsg.AddPartitionsToTxnRequest)
	resp := kresp.(*kmsg.AddPartitionsToTxnResponse)

	// We default to the top level error, which is used in v4+. For v3
	// (case 0), we use the per-partition error, which is the same for
	// every partition on not_coordinator errors.
	code := resp.ErrorCode
	if code == 0 && len(resp.Transactions) == 0 {
		// Convert v3 and prior to v4+
		resptxn := kmsg.NewAddPartitionsToTxnResponseTransaction()
		resptxn.TransactionalID = req.TransactionalID
		for _, rt := range resp.Topics {
			respt := kmsg.NewAddPartitionsToTxnResponseTransactionTopic()
			respt.Topic = rt.Topic
			for _, rp := range rt.Partitions {
				respp := kmsg.NewAddPartitionsToTxnResponseTransactionTopicPartition()
				respp.Partition = rp.Partition
				respp.ErrorCode = rp.ErrorCode
				code = rp.ErrorCode // v3 and prior has per-partition errors, not top level
				respt.Partitions = append(respt.Partitions, respp)
			}
			resptxn.Topics = append(resptxn.Topics, respt)
		}
		resp.Transactions = append(resp.Transactions, resptxn)
	} else {
		// Convert v4 to v3 and prior: either we have a top level error
		// code or we have at least one transaction.
		//
		// If the code is non-zero, we convert it to per-partition error
		// codes; v3 does not have a top level err.
		addPartitionsTxnToResp(resp)
		if code != 0 {
			for _, reqt := range req.Topics {
				respt := kmsg.NewAddPartitionsToTxnResponseTopic()
				respt.Topic = reqt.Topic
				for _, reqp := range reqt.Partitions {
					respp := kmsg.NewAddPartitionsToTxnResponseTopicPartition()
					respp.Partition = reqp
					respp.ErrorCode = resp.ErrorCode
					respt.Partitions = append(respt.Partitions, respp)
				}
				resp.Topics = append(resp.Topics, respt)
			}
		}
	}
	if err := kerr.ErrorForCode(code); cl.maybeDeleteStaleCoordinator(req.TransactionalID, coordinatorTypeTxn, err) {
		return err
	}
	return nil
}

func (*addPartitionsToTxnSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrAddPartitionsToTxnResponse()

	firstErr := firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.AddPartitionsToTxnResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		merged.ErrorCode = resp.ErrorCode
		merged.Transactions = append(merged.Transactions, resp.Transactions...)
	})
	addPartitionsTxnToResp(merged)
	return merged, firstErr
}

// handle sharding WriteTxnMarkersRequest
type writeTxnMarkersSharder struct{ *Client }

func (cl *writeTxnMarkersSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.WriteTxnMarkersRequest)

	var need []string
	for _, marker := range req.Markers {
		for _, topic := range marker.Topics {
			need = append(need, topic.Topic)
		}
	}
	mapping, err := cl.fetchMappedMetadata(ctx, need, true)
	if err != nil {
		return nil, false, err
	}

	type pidEpochCommit struct {
		pid    int64
		epoch  int16
		commit bool
	}

	brokerReqs := make(map[int32]map[pidEpochCommit]map[string][]int32)
	unknown := make(map[error]map[pidEpochCommit]map[string][]int32) // err => pec => topic => partitions

	addreq := func(b int32, pec pidEpochCommit, t string, p int32) {
		pecs := brokerReqs[b]
		if pecs == nil {
			pecs = make(map[pidEpochCommit]map[string][]int32)
			brokerReqs[b] = pecs
		}
		ts := pecs[pec]
		if ts == nil {
			ts = make(map[string][]int32)
			pecs[pec] = ts
		}
		ts[t] = append(ts[t], p)
	}
	addunk := func(err error, pec pidEpochCommit, t string, p int32) {
		pecs := unknown[err]
		if pecs == nil {
			pecs = make(map[pidEpochCommit]map[string][]int32)
			unknown[err] = pecs
		}
		ts := pecs[pec]
		if ts == nil {
			ts = make(map[string][]int32)
			pecs[pec] = ts
		}
		ts[t] = append(ts[t], p)
	}

	for _, marker := range req.Markers {
		pec := pidEpochCommit{
			marker.ProducerID,
			marker.ProducerEpoch,
			marker.Committed,
		}
		for _, topic := range marker.Topics {
			t := topic.Topic
			tmapping, exists := mapping[t]
			if err := unknownOrCode(exists, tmapping.t.ErrorCode); err != nil {
				for _, partition := range topic.Partitions {
					addunk(err, pec, t, partition)
				}
				continue
			}
			for _, partition := range topic.Partitions {
				p, exists := tmapping.ps[partition]
				if err := unknownOrCode(exists, p.ErrorCode); err != nil {
					addunk(err, pec, t, partition)
					continue
				}
				if err := noLeader(p.Leader); err != nil {
					addunk(err, pec, t, partition)
					continue
				}
				addreq(p.Leader, pec, t, partition)
			}
		}
	}

	mkreq := kmsg.NewPtrWriteTxnMarkersRequest

	var issues []issueShard
	for brokerID, brokerReq := range brokerReqs {
		req := mkreq()
		for pec, topics := range brokerReq {
			rm := kmsg.NewWriteTxnMarkersRequestMarker()
			rm.ProducerID = pec.pid
			rm.ProducerEpoch = pec.epoch
			rm.Committed = pec.commit
			for topic, parts := range topics {
				rt := kmsg.NewWriteTxnMarkersRequestMarkerTopic()
				rt.Topic = topic
				rt.Partitions = parts
				rm.Topics = append(rm.Topics, rt)
			}
			req.Markers = append(req.Markers, rm)
		}
		issues = append(issues, issueShard{
			req:    req,
			broker: brokerID,
		})
	}

	for err, errReq := range unknown {
		req := mkreq()
		for pec, topics := range errReq {
			rm := kmsg.NewWriteTxnMarkersRequestMarker()
			rm.ProducerID = pec.pid
			rm.ProducerEpoch = pec.epoch
			rm.Committed = pec.commit
			for topic, parts := range topics {
				rt := kmsg.NewWriteTxnMarkersRequestMarkerTopic()
				rt.Topic = topic
				rt.Partitions = parts
				rm.Topics = append(rm.Topics, rt)
			}
			req.Markers = append(req.Markers, rm)
		}
		issues = append(issues, issueShard{
			req: req,
			err: err,
		})
	}
	return issues, true, nil // this is reshardable
}

func (cl *writeTxnMarkersSharder) onResp(_ kmsg.Request, kresp kmsg.Response) error {
	var (
		resp         = kresp.(*kmsg.WriteTxnMarkersResponse)
		del          []string
		retErr       error
		unknownTopic bool
	)
	for i := range resp.Markers {
		m := &resp.Markers[i]
		for j := range m.Topics {
			t := &m.Topics[j]
			for k := range t.Partitions {
				p := &t.Partitions[k]
				err := kerr.ErrorForCode(p.ErrorCode)
				if err == kerr.UnknownTopicOrPartition || err == kerr.NotLeaderForPartition {
					del = append(del, t.Topic)
					unknownTopic = unknownTopic || err == kerr.UnknownTopicOrPartition
				}
				onRespShardErr(&retErr, err)
			}
		}
	}
	if cl.maybeDeleteMappedMetadata(unknownTopic, del...) {
		return retErr
	}
	return nil
}

func (*writeTxnMarkersSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrWriteTxnMarkersResponse()
	markers := make(map[int64]map[string][]kmsg.WriteTxnMarkersResponseMarkerTopicPartition)

	firstErr := firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.WriteTxnMarkersResponse)
		merged.Version = resp.Version
		for _, marker := range resp.Markers {
			topics := markers[marker.ProducerID]
			if topics == nil {
				topics = make(map[string][]kmsg.WriteTxnMarkersResponseMarkerTopicPartition)
				markers[marker.ProducerID] = topics
			}
			for _, topic := range marker.Topics {
				topics[topic.Topic] = append(topics[topic.Topic], topic.Partitions...)
			}
		}
	})
	for pid, topics := range markers {
		respMarker := kmsg.NewWriteTxnMarkersResponseMarker()
		respMarker.ProducerID = pid
		for topic, partitions := range topics {
			respTopic := kmsg.NewWriteTxnMarkersResponseMarkerTopic()
			respTopic.Topic = topic
			respTopic.Partitions = append(respTopic.Partitions, partitions...)
			respMarker.Topics = append(respMarker.Topics, respTopic)
		}
		merged.Markers = append(merged.Markers, respMarker)
	}
	return merged, firstErr
}

// handle sharding DescribeConfigsRequest
type describeConfigsSharder struct{ *Client }

func (*describeConfigsSharder) shard(_ context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.DescribeConfigsRequest)

	brokerReqs := make(map[int32][]kmsg.DescribeConfigsRequestResource)
	var any []kmsg.DescribeConfigsRequestResource

	for i := range req.Resources {
		resource := req.Resources[i]
		switch resource.ResourceType {
		case kmsg.ConfigResourceTypeBroker:
		case kmsg.ConfigResourceTypeBrokerLogger:
		default:
			any = append(any, resource)
			continue
		}
		id, err := strconv.ParseInt(resource.ResourceName, 10, 32)
		if err != nil || id < 0 {
			any = append(any, resource)
			continue
		}
		brokerReqs[int32(id)] = append(brokerReqs[int32(id)], resource)
	}

	var issues []issueShard
	for brokerID, brokerReq := range brokerReqs {
		newReq := kmsg.NewPtrDescribeConfigsRequest()
		newReq.Resources = brokerReq
		newReq.IncludeSynonyms = req.IncludeSynonyms
		newReq.IncludeDocumentation = req.IncludeDocumentation

		issues = append(issues, issueShard{
			req:    newReq,
			broker: brokerID,
		})
	}

	if len(any) > 0 {
		newReq := kmsg.NewPtrDescribeConfigsRequest()
		newReq.Resources = any
		newReq.IncludeSynonyms = req.IncludeSynonyms
		newReq.IncludeDocumentation = req.IncludeDocumentation
		issues = append(issues, issueShard{
			req: newReq,
			any: true,
		})
	}

	return issues, false, nil // this is not reshardable, but the any block can go anywhere
}

func (*describeConfigsSharder) onResp(kmsg.Request, kmsg.Response) error { return nil } // configs: topics not mapped, nothing retryable

func (*describeConfigsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrDescribeConfigsResponse()
	return merged, firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.DescribeConfigsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		merged.Resources = append(merged.Resources, resp.Resources...)
	})
}

// handle sharding AlterConfigsRequest
type alterConfigsSharder struct{ *Client }

func (*alterConfigsSharder) shard(_ context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.AlterConfigsRequest)

	brokerReqs := make(map[int32][]kmsg.AlterConfigsRequestResource)
	var any []kmsg.AlterConfigsRequestResource

	for i := range req.Resources {
		resource := req.Resources[i]
		switch resource.ResourceType {
		case kmsg.ConfigResourceTypeBroker:
		case kmsg.ConfigResourceTypeBrokerLogger:
		default:
			any = append(any, resource)
			continue
		}
		id, err := strconv.ParseInt(resource.ResourceName, 10, 32)
		if err != nil || id < 0 {
			any = append(any, resource)
			continue
		}
		brokerReqs[int32(id)] = append(brokerReqs[int32(id)], resource)
	}

	var issues []issueShard
	for brokerID, brokerReq := range brokerReqs {
		newReq := kmsg.NewPtrAlterConfigsRequest()
		newReq.Resources = brokerReq
		newReq.ValidateOnly = req.ValidateOnly

		issues = append(issues, issueShard{
			req:    newReq,
			broker: brokerID,
		})
	}

	if len(any) > 0 {
		newReq := kmsg.NewPtrAlterConfigsRequest()
		newReq.Resources = any
		newReq.ValidateOnly = req.ValidateOnly
		issues = append(issues, issueShard{
			req: newReq,
			any: true,
		})
	}

	return issues, false, nil // this is not reshardable, but the any block can go anywhere
}

func (*alterConfigsSharder) onResp(kmsg.Request, kmsg.Response) error { return nil } // configs: topics not mapped, nothing retryable

func (*alterConfigsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrAlterConfigsResponse()
	return merged, firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.AlterConfigsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		merged.Resources = append(merged.Resources, resp.Resources...)
	})
}

// handles sharding AlterReplicaLogDirsRequest
type alterReplicaLogDirsSharder struct{ *Client }

func (cl *alterReplicaLogDirsSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.AlterReplicaLogDirsRequest)

	needMap := make(map[string]struct{})
	for _, dir := range req.Dirs {
		for _, topic := range dir.Topics {
			needMap[topic.Topic] = struct{}{}
		}
	}
	var need []string
	for topic := range needMap {
		need = append(need, topic)
	}
	mapping, err := cl.fetchMappedMetadata(ctx, need, false) // bypass cache, tricky to manage response
	if err != nil {
		return nil, false, err
	}

	brokerReqs := make(map[int32]map[string]map[string][]int32) // broker => dir => topic => partitions
	unknowns := make(map[error]map[string]map[string][]int32)   // err => dir => topic => partitions

	addBroker := func(broker int32, dir, topic string, partition int32) {
		brokerDirs := brokerReqs[broker]
		if brokerDirs == nil {
			brokerDirs = make(map[string]map[string][]int32)
			brokerReqs[broker] = brokerDirs
		}
		dirTopics := brokerDirs[dir]
		if dirTopics == nil {
			dirTopics = make(map[string][]int32)
			brokerDirs[dir] = dirTopics
		}
		dirTopics[topic] = append(dirTopics[topic], partition)
	}

	addUnknown := func(err error, dir, topic string, partition int32) {
		dirs := unknowns[err]
		if dirs == nil {
			dirs = make(map[string]map[string][]int32)
			unknowns[err] = dirs
		}
		dirTopics := dirs[dir]
		if dirTopics == nil {
			dirTopics = make(map[string][]int32)
			dirs[dir] = dirTopics
		}
		dirTopics[topic] = append(dirTopics[topic], partition)
	}

	for _, dir := range req.Dirs {
		for _, topic := range dir.Topics {
			t := topic.Topic
			tmapping, exists := mapping[t]
			if err := unknownOrCode(exists, tmapping.t.ErrorCode); err != nil {
				for _, partition := range topic.Partitions {
					addUnknown(err, dir.Dir, t, partition)
				}
				continue
			}
			for _, partition := range topic.Partitions {
				p, exists := tmapping.ps[partition]
				if err := unknownOrCode(exists, p.ErrorCode); err != nil {
					addUnknown(err, dir.Dir, t, partition)
					continue
				}

				for _, replica := range p.Replicas {
					addBroker(replica, dir.Dir, t, partition)
				}
			}
		}
	}

	var issues []issueShard
	for brokerID, brokerReq := range brokerReqs {
		req := kmsg.NewPtrAlterReplicaLogDirsRequest()
		for dir, topics := range brokerReq {
			rd := kmsg.NewAlterReplicaLogDirsRequestDir()
			rd.Dir = dir
			for topic, partitions := range topics {
				rdTopic := kmsg.NewAlterReplicaLogDirsRequestDirTopic()
				rdTopic.Topic = topic
				rdTopic.Partitions = partitions
				rd.Topics = append(rd.Topics, rdTopic)
			}
			req.Dirs = append(req.Dirs, rd)
		}

		issues = append(issues, issueShard{
			req:    req,
			broker: brokerID,
		})
	}

	for err, dirs := range unknowns {
		req := kmsg.NewPtrAlterReplicaLogDirsRequest()
		for dir, topics := range dirs {
			rd := kmsg.NewAlterReplicaLogDirsRequestDir()
			rd.Dir = dir
			for topic, partitions := range topics {
				rdTopic := kmsg.NewAlterReplicaLogDirsRequestDirTopic()
				rdTopic.Topic = topic
				rdTopic.Partitions = partitions
				rd.Topics = append(rd.Topics, rdTopic)
			}
			req.Dirs = append(req.Dirs, rd)
		}

		issues = append(issues, issueShard{
			req: req,
			err: err,
		})
	}

	return issues, true, nil // this is reshardable
}

func (*alterReplicaLogDirsSharder) onResp(kmsg.Request, kmsg.Response) error { return nil } // topic / partitions: not retried

// merge does not make sense for this function, but we provide a one anyway.
func (*alterReplicaLogDirsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrAlterReplicaLogDirsResponse()
	topics := make(map[string][]kmsg.AlterReplicaLogDirsResponseTopicPartition)

	firstErr := firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.AlterReplicaLogDirsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis

		for _, topic := range resp.Topics {
			topics[topic.Topic] = append(topics[topic.Topic], topic.Partitions...)
		}
	})
	for topic, partitions := range topics {
		respTopic := kmsg.NewAlterReplicaLogDirsResponseTopic()
		respTopic.Topic = topic
		respTopic.Partitions = partitions
		merged.Topics = append(merged.Topics, respTopic)
	}
	return merged, firstErr
}

// handles sharding DescribeLogDirsRequest
type describeLogDirsSharder struct{ *Client }

func (cl *describeLogDirsSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.DescribeLogDirsRequest)

	// If req.Topics is nil, the request is to describe all logdirs. Thus,
	// we will issue the request to all brokers (similar to ListGroups).
	if req.Topics == nil {
		return cl.allBrokersShardedReq(ctx, func() kmsg.Request {
			dup := *req
			return &dup
		})
	}

	var need []string
	for _, topic := range req.Topics {
		need = append(need, topic.Topic)
	}
	mapping, err := cl.fetchMappedMetadata(ctx, need, false) // bypass cache, tricky to manage response
	if err != nil {
		return nil, false, err
	}

	brokerReqs := make(map[int32]map[string][]int32)
	var unknowns unknownErrShards

	for _, topic := range req.Topics {
		t := topic.Topic
		tmapping, exists := mapping[t]
		if err := unknownOrCode(exists, tmapping.t.ErrorCode); err != nil {
			unknowns.errs(err, t, topic.Partitions)
			continue
		}
		for _, partition := range topic.Partitions {
			p, exists := tmapping.ps[partition]
			if err := unknownOrCode(exists, p.ErrorCode); err != nil {
				unknowns.err(err, t, partition)
				continue
			}

			for _, replica := range p.Replicas {
				brokerReq := brokerReqs[replica]
				if brokerReq == nil {
					brokerReq = make(map[string][]int32)
					brokerReqs[replica] = brokerReq
				}
				brokerReq[topic.Topic] = append(brokerReq[topic.Topic], partition)
			}
		}
	}

	mkreq := kmsg.NewPtrDescribeLogDirsRequest

	var issues []issueShard
	for brokerID, brokerReq := range brokerReqs {
		req := mkreq()
		for topic, parts := range brokerReq {
			reqTopic := kmsg.NewDescribeLogDirsRequestTopic()
			reqTopic.Topic = topic
			reqTopic.Partitions = parts
			req.Topics = append(req.Topics, reqTopic)
		}
		issues = append(issues, issueShard{
			req:    req,
			broker: brokerID,
		})
	}

	return append(issues, unknowns.collect(mkreq, func(r *kmsg.DescribeLogDirsRequest, topic string, parts []int32) {
		reqTopic := kmsg.NewDescribeLogDirsRequestTopic()
		reqTopic.Topic = topic
		reqTopic.Partitions = parts
		r.Topics = append(r.Topics, reqTopic)
	})...), true, nil // this is reshardable
}

func (*describeLogDirsSharder) onResp(kmsg.Request, kmsg.Response) error { return nil } // topic / configs: not retried

// merge does not make sense for this function, but we provide one anyway.
// We lose the error code for directories.
func (*describeLogDirsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrDescribeLogDirsResponse()
	dirs := make(map[string]map[string][]kmsg.DescribeLogDirsResponseDirTopicPartition)

	firstErr := firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.DescribeLogDirsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis

		for _, dir := range resp.Dirs {
			mergeDir := dirs[dir.Dir]
			if mergeDir == nil {
				mergeDir = make(map[string][]kmsg.DescribeLogDirsResponseDirTopicPartition)
				dirs[dir.Dir] = mergeDir
			}
			for _, topic := range dir.Topics {
				mergeDir[topic.Topic] = append(mergeDir[topic.Topic], topic.Partitions...)
			}
		}
	})
	for dir, topics := range dirs {
		md := kmsg.NewDescribeLogDirsResponseDir()
		md.Dir = dir
		for topic, partitions := range topics {
			mdTopic := kmsg.NewDescribeLogDirsResponseDirTopic()
			mdTopic.Topic = topic
			mdTopic.Partitions = partitions
			md.Topics = append(md.Topics, mdTopic)
		}
		merged.Dirs = append(merged.Dirs, md)
	}
	return merged, firstErr
}

// handles sharding DeleteGroupsRequest
type deleteGroupsSharder struct{ *Client }

func (cl *deleteGroupsSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.DeleteGroupsRequest)

	coordinators := cl.loadCoordinators(ctx, coordinatorTypeGroup, req.Groups...)
	type unkerr struct {
		err   error
		group string
	}
	var (
		brokerReqs = make(map[int32]*kmsg.DeleteGroupsRequest)
		kerrs      = make(map[*kerr.Error][]string)
		unkerrs    []unkerr
	)

	newReq := func(groups ...string) *kmsg.DeleteGroupsRequest {
		newReq := kmsg.NewPtrDeleteGroupsRequest()
		newReq.Groups = groups
		return newReq
	}

	for _, group := range req.Groups {
		berr := coordinators[group]
		var ke *kerr.Error
		switch {
		case berr.err == nil:
			brokerReq := brokerReqs[berr.b.meta.NodeID]
			if brokerReq == nil {
				brokerReq = newReq()
				brokerReqs[berr.b.meta.NodeID] = brokerReq
			}
			brokerReq.Groups = append(brokerReq.Groups, group)
		case errors.As(berr.err, &ke):
			kerrs[ke] = append(kerrs[ke], group)
		default:
			unkerrs = append(unkerrs, unkerr{berr.err, group})
		}
	}

	var issues []issueShard
	for id, req := range brokerReqs {
		issues = append(issues, issueShard{
			req:    req,
			broker: id,
		})
	}
	for _, unkerr := range unkerrs {
		issues = append(issues, issueShard{
			req: newReq(unkerr.group),
			err: unkerr.err,
		})
	}
	for kerr, groups := range kerrs {
		issues = append(issues, issueShard{
			req: newReq(groups...),
			err: kerr,
		})
	}

	return issues, true, nil // reshardable to load correct coordinators
}

func (cl *deleteGroupsSharder) onResp(_ kmsg.Request, kresp kmsg.Response) error {
	resp := kresp.(*kmsg.DeleteGroupsResponse)
	var retErr error
	for i := range resp.Groups {
		group := &resp.Groups[i]
		err := kerr.ErrorForCode(group.ErrorCode)
		cl.maybeDeleteStaleCoordinator(group.Group, coordinatorTypeGroup, err)
		onRespShardErr(&retErr, err)
	}
	return retErr
}

func (*deleteGroupsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrDeleteGroupsResponse()
	return merged, firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.DeleteGroupsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		merged.Groups = append(merged.Groups, resp.Groups...)
	})
}

// handle sharding IncrementalAlterConfigsRequest
type incrementalAlterConfigsSharder struct{ *Client }

func (*incrementalAlterConfigsSharder) shard(_ context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.IncrementalAlterConfigsRequest)

	brokerReqs := make(map[int32][]kmsg.IncrementalAlterConfigsRequestResource)
	var any []kmsg.IncrementalAlterConfigsRequestResource

	for i := range req.Resources {
		resource := req.Resources[i]
		switch resource.ResourceType {
		case kmsg.ConfigResourceTypeBroker:
		case kmsg.ConfigResourceTypeBrokerLogger:
		default:
			any = append(any, resource)
			continue
		}
		id, err := strconv.ParseInt(resource.ResourceName, 10, 32)
		if err != nil || id < 0 {
			any = append(any, resource)
			continue
		}
		brokerReqs[int32(id)] = append(brokerReqs[int32(id)], resource)
	}

	var issues []issueShard
	for brokerID, brokerReq := range brokerReqs {
		newReq := kmsg.NewPtrIncrementalAlterConfigsRequest()
		newReq.Resources = brokerReq
		newReq.ValidateOnly = req.ValidateOnly

		issues = append(issues, issueShard{
			req:    newReq,
			broker: brokerID,
		})
	}

	if len(any) > 0 {
		newReq := kmsg.NewPtrIncrementalAlterConfigsRequest()
		newReq.Resources = any
		newReq.ValidateOnly = req.ValidateOnly
		issues = append(issues, issueShard{
			req: newReq,
			any: true,
		})
	}

	return issues, false, nil // this is not reshardable, but the any block can go anywhere
}

func (*incrementalAlterConfigsSharder) onResp(kmsg.Request, kmsg.Response) error { return nil } // configs: topics not mapped, nothing retryable

func (*incrementalAlterConfigsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrIncrementalAlterConfigsResponse()
	return merged, firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.IncrementalAlterConfigsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		merged.Resources = append(merged.Resources, resp.Resources...)
	})
}

// handle sharding DescribeProducersRequest
type describeProducersSharder struct{ *Client }

func (cl *describeProducersSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.DescribeProducersRequest)

	var need []string
	for _, topic := range req.Topics {
		need = append(need, topic.Topic)
	}
	mapping, err := cl.fetchMappedMetadata(ctx, need, true)
	if err != nil {
		return nil, false, err
	}

	brokerReqs := make(map[int32]map[string][]int32) // broker => topic => partitions
	var unknowns unknownErrShards

	for _, topic := range req.Topics {
		t := topic.Topic
		tmapping, exists := mapping[t]
		if err := unknownOrCode(exists, tmapping.t.ErrorCode); err != nil {
			unknowns.errs(err, t, topic.Partitions)
			continue
		}
		for _, partition := range topic.Partitions {
			p, exists := tmapping.ps[partition]
			if err := unknownOrCode(exists, p.ErrorCode); err != nil {
				unknowns.err(err, t, partition)
				continue
			}

			brokerReq := brokerReqs[p.Leader]
			if brokerReq == nil {
				brokerReq = make(map[string][]int32)
				brokerReqs[p.Leader] = brokerReq
			}
			brokerReq[topic.Topic] = append(brokerReq[topic.Topic], partition)
		}
	}

	mkreq := kmsg.NewPtrDescribeProducersRequest

	var issues []issueShard
	for brokerID, brokerReq := range brokerReqs {
		req := mkreq()
		for topic, parts := range brokerReq {
			reqTopic := kmsg.NewDescribeProducersRequestTopic()
			reqTopic.Topic = topic
			reqTopic.Partitions = parts
			req.Topics = append(req.Topics, reqTopic)
		}
		issues = append(issues, issueShard{
			req:    req,
			broker: brokerID,
		})
	}

	return append(issues, unknowns.collect(mkreq, func(r *kmsg.DescribeProducersRequest, topic string, parts []int32) {
		reqTopic := kmsg.NewDescribeProducersRequestTopic()
		reqTopic.Topic = topic
		reqTopic.Partitions = parts
		r.Topics = append(r.Topics, reqTopic)
	})...), true, nil // this is reshardable
}

func (cl *describeProducersSharder) onResp(_ kmsg.Request, kresp kmsg.Response) error {
	var (
		resp         = kresp.(*kmsg.DescribeProducersResponse)
		del          []string
		retErr       error
		unknownTopic bool
	)
	for i := range resp.Topics {
		t := &resp.Topics[i]
		for j := range t.Partitions {
			p := &t.Partitions[j]
			err := kerr.ErrorForCode(p.ErrorCode)
			if err == kerr.UnknownTopicOrPartition || err == kerr.NotLeaderForPartition {
				del = append(del, t.Topic)
				unknownTopic = unknownTopic || err == kerr.UnknownTopicOrPartition
			}
			onRespShardErr(&retErr, err)
		}
	}
	if cl.maybeDeleteMappedMetadata(unknownTopic, del...) {
		return retErr
	}
	return nil
}

func (*describeProducersSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrDescribeProducersResponse()
	topics := make(map[string][]kmsg.DescribeProducersResponseTopicPartition)
	firstErr := firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.DescribeProducersResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis

		for _, topic := range resp.Topics {
			topics[topic.Topic] = append(topics[topic.Topic], topic.Partitions...)
		}
	})
	for topic, partitions := range topics {
		respTopic := kmsg.NewDescribeProducersResponseTopic()
		respTopic.Topic = topic
		respTopic.Partitions = partitions
		merged.Topics = append(merged.Topics, respTopic)
	}
	return merged, firstErr
}

// handles sharding DescribeTransactionsRequest
type describeTransactionsSharder struct{ *Client }

func (cl *describeTransactionsSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.DescribeTransactionsRequest)

	coordinators := cl.loadCoordinators(ctx, coordinatorTypeTxn, req.TransactionalIDs...)
	type unkerr struct {
		err   error
		txnID string
	}
	var (
		brokerReqs = make(map[int32]*kmsg.DescribeTransactionsRequest)
		kerrs      = make(map[*kerr.Error][]string)
		unkerrs    []unkerr
	)

	newReq := func(txnIDs ...string) *kmsg.DescribeTransactionsRequest {
		r := kmsg.NewPtrDescribeTransactionsRequest()
		r.TransactionalIDs = txnIDs
		return r
	}

	for _, txnID := range req.TransactionalIDs {
		berr := coordinators[txnID]
		var ke *kerr.Error
		switch {
		case berr.err == nil:
			brokerReq := brokerReqs[berr.b.meta.NodeID]
			if brokerReq == nil {
				brokerReq = newReq()
				brokerReqs[berr.b.meta.NodeID] = brokerReq
			}
			brokerReq.TransactionalIDs = append(brokerReq.TransactionalIDs, txnID)
		case errors.As(berr.err, &ke):
			kerrs[ke] = append(kerrs[ke], txnID)
		default:
			unkerrs = append(unkerrs, unkerr{berr.err, txnID})
		}
	}

	var issues []issueShard
	for id, req := range brokerReqs {
		issues = append(issues, issueShard{
			req:    req,
			broker: id,
		})
	}
	for _, unkerr := range unkerrs {
		issues = append(issues, issueShard{
			req: newReq(unkerr.txnID),
			err: unkerr.err,
		})
	}
	for kerr, txnIDs := range kerrs {
		issues = append(issues, issueShard{
			req: newReq(txnIDs...),
			err: kerr,
		})
	}

	return issues, true, nil // reshardable to load correct coordinators
}

func (cl *describeTransactionsSharder) onResp(_ kmsg.Request, kresp kmsg.Response) error { // cleanup any stale coordinators
	resp := kresp.(*kmsg.DescribeTransactionsResponse)
	var retErr error
	for i := range resp.TransactionStates {
		txnState := &resp.TransactionStates[i]
		err := kerr.ErrorForCode(txnState.ErrorCode)
		cl.maybeDeleteStaleCoordinator(txnState.TransactionalID, coordinatorTypeTxn, err)
		onRespShardErr(&retErr, err)
	}
	return retErr
}

func (*describeTransactionsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrDescribeTransactionsResponse()
	return merged, firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.DescribeTransactionsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		merged.TransactionStates = append(merged.TransactionStates, resp.TransactionStates...)
	})
}

// handles sharding ListTransactionsRequest
type listTransactionsSharder struct{ *Client }

func (cl *listTransactionsSharder) shard(ctx context.Context, kreq kmsg.Request, _ error) ([]issueShard, bool, error) {
	req := kreq.(*kmsg.ListTransactionsRequest)
	return cl.allBrokersShardedReq(ctx, func() kmsg.Request {
		dup := *req
		return &dup
	})
}

func (*listTransactionsSharder) onResp(_ kmsg.Request, kresp kmsg.Response) error {
	resp := kresp.(*kmsg.ListTransactionsResponse)
	return kerr.ErrorForCode(resp.ErrorCode)
}

func (*listTransactionsSharder) merge(sresps []ResponseShard) (kmsg.Response, error) {
	merged := kmsg.NewPtrListTransactionsResponse()

	unknownStates := make(map[string]struct{})

	firstErr := firstErrMerger(sresps, func(kresp kmsg.Response) {
		resp := kresp.(*kmsg.ListTransactionsResponse)
		merged.Version = resp.Version
		merged.ThrottleMillis = resp.ThrottleMillis
		if merged.ErrorCode == 0 {
			merged.ErrorCode = resp.ErrorCode
		}
		for _, state := range resp.UnknownStateFilters {
			unknownStates[state] = struct{}{}
		}
		merged.TransactionStates = append(merged.TransactionStates, resp.TransactionStates...)
	})
	for unknownState := range unknownStates {
		merged.UnknownStateFilters = append(merged.UnknownStateFilters, unknownState)
	}

	return merged, firstErr
}
