package bloomgateway

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"strings"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/ring"
	ringclient "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/bloomutils"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/queue"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/discovery"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	// BlocksOwnerRead is the operation used to check the authoritative owners of a block
	// (replicas included) that are available for queries (a bloom gateway is available for
	// queries only when ACTIVE).
	BlocksOwnerRead = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)
	// groupedChunksRefPool pooling slice of logproto.GroupedChunkRefs [64, 128, 256, ..., 65536]
	groupedChunksRefPool = queue.NewSlicePool[*logproto.GroupedChunkRefs](1<<6, 1<<16, 2)
	// ringGetBuffersPool pooling for ringGetBuffers to avoid calling ring.MakeBuffersForGet() for each request
	ringGetBuffersPool = sync.Pool{
		New: func() interface{} {
			descs, hosts, zones := ring.MakeBuffersForGet()
			return &ringGetBuffers{
				Descs: descs,
				Hosts: hosts,
				Zones: zones,
			}
		},
	}
)

type ringGetBuffers struct {
	Descs []ring.InstanceDesc
	Hosts []string
	Zones []string
}

func (buf *ringGetBuffers) Reset() {
	buf.Descs = buf.Descs[:0]
	buf.Hosts = buf.Hosts[:0]
	buf.Zones = buf.Zones[:0]
}

// GRPCPool represents a pool of gRPC connections to different bloom gateway instances.
// Interfaces are inlined for simplicity to automatically satisfy interface functions.
type GRPCPool struct {
	grpc_health_v1.HealthClient
	logproto.BloomGatewayClient
	io.Closer
}

// NewBloomGatewayGRPCPool instantiates a new pool of GRPC connections for the Bloom Gateway
// Internally, it also instantiates a protobuf bloom gateway client and a health client.
func NewBloomGatewayGRPCPool(address string, opts []grpc.DialOption) (*GRPCPool, error) {
	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "new grpc pool dial")
	}

	return &GRPCPool{
		Closer:             conn,
		HealthClient:       grpc_health_v1.NewHealthClient(conn),
		BloomGatewayClient: logproto.NewBloomGatewayClient(conn),
	}, nil
}

// IndexGatewayClientConfig configures the Index Gateway client used to
// communicate with the Index Gateway server.
type ClientConfig struct {
	// PoolConfig defines the behavior of the gRPC connection pool used to communicate
	// with the Bloom Gateway.
	PoolConfig PoolConfig `yaml:"pool_config,omitempty" doc:"description=Configures the behavior of the connection pool."`

	// GRPCClientConfig configures the gRPC connection between the Bloom Gateway client and the server.
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`

	// Ring is the Bloom Gateway ring used to find the appropriate Bloom Gateway instance
	// this client should talk to.
	Ring ring.ReadRing `yaml:"-"`

	// Cache configures the cache used to store the results of the Bloom Gateway server.
	Cache        CacheConfig `yaml:"results_cache,omitempty"`
	CacheResults bool        `yaml:"cache_results"`

	// Client sharding using DNS disvovery and jumphash
	Addresses string `yaml:"addresses,omitempty"`
}

// RegisterFlags registers flags for the Bloom Gateway client configuration.
func (i *ClientConfig) RegisterFlags(f *flag.FlagSet) {
	i.RegisterFlagsWithPrefix("bloom-gateway-client.", f)
}

// RegisterFlagsWithPrefix registers flags for the Bloom Gateway client configuration with a common prefix.
func (i *ClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	i.GRPCClientConfig.RegisterFlagsWithPrefix(prefix+"grpc", f)
	i.Cache.RegisterFlagsWithPrefix(prefix+"cache.", f)
	i.PoolConfig.RegisterFlagsWithPrefix(prefix+"pool.", f)
	f.BoolVar(&i.CacheResults, prefix+"cache_results", false, "Flag to control whether to cache bloom gateway client requests/responses.")
	f.StringVar(&i.Addresses, prefix+"addresses", "", "Comma separated addresses list in DNS Service Discovery format: https://grafana.com/docs/mimir/latest/configure/about-dns-service-discovery/#supported-discovery-modes")
}

func (i *ClientConfig) Validate() error {
	if err := i.GRPCClientConfig.Validate(); err != nil {
		return errors.Wrap(err, "grpc client config")
	}

	if err := i.PoolConfig.Validate(); err != nil {
		return errors.Wrap(err, "pool config")
	}

	if i.CacheResults {
		if err := i.Cache.Validate(); err != nil {
			return errors.Wrap(err, "cache config")
		}
	}

	return nil
}

type Client interface {
	FilterChunks(ctx context.Context, tenant string, from, through model.Time, groups []*logproto.GroupedChunkRefs, plan plan.QueryPlan) ([]*logproto.GroupedChunkRefs, error)
}

type GatewayClient struct {
	cfg         ClientConfig
	ctx         context.Context
	limits      Limits
	logger      log.Logger
	metrics     *clientMetrics
	pool        *ringclient.Pool
	dnsProvider *discovery.DNS
	ring        ring.ReadRing
}

func NewClient(
	cfg ClientConfig,
	limits Limits,
	registerer prometheus.Registerer,
	logger log.Logger,
	metricsNamespace string,
	cacheGen resultscache.CacheGenNumberLoader,
	retentionEnabled bool,
) (*GatewayClient, error) {

	latency := promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: "bloom_gateway",
		Name:      "request_duration_seconds",
		Help:      "Time (in seconds) spent serving requests when using the bloom gateway",
		Buckets:   instrument.DefBuckets,
	}, []string{"operation", "status_code"})

	clients := promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: "bloom_gateway",
		Name:      "clients",
		Help:      "The current number of bloom gateway clients.",
	})

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(latency))
	if err != nil {
		return nil, err
	}

	var c cache.Cache
	if cfg.CacheResults {
		c, err = cache.New(cfg.Cache.CacheConfig, registerer, logger, stats.BloomFilterCache, constants.Loki)
		if err != nil {
			return nil, errors.Wrap(err, "new bloom gateway cache")
		}
		if cfg.Cache.Compression == "snappy" {
			c = cache.NewSnappy(c, logger)
		}
	}

	poolFactory := func(addr string) (ringclient.PoolClient, error) {
		pool, err := NewBloomGatewayGRPCPool(addr, dialOpts)
		if err != nil {
			return nil, errors.Wrap(err, "new bloom gateway grpc pool")
		}

		if cfg.CacheResults {
			pool.BloomGatewayClient = NewBloomGatewayClientCacheMiddleware(
				logger,
				pool.BloomGatewayClient,
				c,
				limits,
				cacheGen,
				retentionEnabled,
			)
		}

		return pool, nil
	}

	dnsProvider := discovery.NewDNS(logger, cfg.PoolConfig.CheckInterval, cfg.Addresses, nil)
	// Make an attempt to do one DNS lookup so we can start with addresses
	dnsProvider.RunOnce()
	discovery := func() ([]string, error) {
		return dnsProvider.Addresses(), nil
	}

	pool := ringclient.NewPool(
		"bloom-gateway",
		ringclient.PoolConfig(cfg.PoolConfig),
		discovery,
		ringclient.PoolAddrFunc(poolFactory),
		clients,
		logger,
	)

	ctx := context.Background()
	services.StartAndAwaitRunning(ctx, pool)

	return &GatewayClient{
		cfg:         cfg,
		ctx:         ctx,
		logger:      logger,
		limits:      limits,
		metrics:     newClientMetrics(registerer),
		pool:        pool,
		dnsProvider: dnsProvider,
	}, nil
}

func (c *GatewayClient) Stop() {
	c.dnsProvider.Stop()
	services.StopAndAwaitTerminated(c.ctx, c.pool)
}

func JoinFunc[S ~[]E, E any](elems S, sep string, f func(e E) string) string {
	res := make([]string, len(elems))
	for i := range elems {
		res[i] = f(elems[i])
	}
	return strings.Join(res, sep)
}

func shuffleAddrs(addrs []string) []string {
	rand.Shuffle(len(addrs), func(i, j int) {
		addrs[i], addrs[j] = addrs[j], addrs[i]
	})
	return addrs
}

// FilterChunkRefs implements Client
func (c *GatewayClient) FilterChunks(ctx context.Context, tenant string, from, through model.Time, groups []*logproto.GroupedChunkRefs, plan plan.QueryPlan) ([]*logproto.GroupedChunkRefs, error) {
	if !c.limits.BloomGatewayEnabled(tenant) || len(groups) == 0 {
		return groups, nil
	}

	subRing := GetShuffleShardingSubring(c.ring, tenant, c.limits)
	rs, err := subRing.GetAllHealthy(BlocksOwnerRead)
	if err != nil {
		return nil, errors.Wrap(err, "bloom gateway get healthy instances")
	}

	servers, err := replicationSetsWithBounds(subRing, rs.Instances)

	if err != nil {
		return nil, errors.Wrap(err, "bloom gateway get replication sets")
	}

	servers = partitionByReplicationSet(groups, servers)
	if len(servers) > 0 {
		// cache locality score (higher is better):
		// `% keyspace / % instances`. Ideally converges to 1 (querying x% of keyspace requires x% of instances),
		// but can be less if the keyspace is not evenly distributed across instances. Ideal operation will see the range of
		// `1-2/num_instances` -> `1`, where the former represents slight
		// overlap on instances to the left and right of the range.
		firstFp, lastFp := groups[0].Fingerprint, groups[len(groups)-1].Fingerprint
		pctKeyspace := float64(lastFp-firstFp) / float64(math.MaxUint64)
		pctInstances := float64(len(servers)) / float64(len(rs.Instances))
		cacheLocalityScore := pctKeyspace / pctInstances
		c.metrics.cacheLocalityScore.Observe(cacheLocalityScore)
	}

	results := make([][]*logproto.GroupedChunkRefs, len(servers))
	count := 0
	err = concurrency.ForEachJob(ctx, len(servers), len(servers), func(ctx context.Context, i int) error {
		rs := servers[i]

		// randomize order of addresses so we don't hotspot the first server in the list
		addrs := shuffleAddrs(rs.rs.GetAddresses())
		level.Info(c.logger).Log(
			"msg", "do FilterChunkRefs for addresses",
			"progress", fmt.Sprintf("%d/%d", i+1, len(servers)),
			"bounds", JoinFunc(rs.ranges, ",", func(e v1.FingerprintBounds) string { return e.String() }),
			"addrs", strings.Join(addrs, ","),
			"from", from.Time(),
			"through", through.Time(),
			"num_refs", len(rs.groups),
			"plan", plan.String(),
			"plan_hash", plan.Hash(),
		)

		return c.doForAddrs(addrs, func(client logproto.BloomGatewayClient) error {
			req := &logproto.FilterChunkRefRequest{
				From:    from,
				Through: through,
				Refs:    rs.groups,
				Plan:    plan,
			}
			resp, err := client.FilterChunkRefs(ctx, req)
			if err != nil {
				return err
			}
			results[i] = resp.ChunkRefs
			count += len(resp.ChunkRefs)
			return nil
		})
	})

	if err != nil {
		return nil, err
	}
	return flatten(results, count), nil
}

func flatten(input [][]*logproto.GroupedChunkRefs, n int) []*logproto.GroupedChunkRefs {
	result := make([]*logproto.GroupedChunkRefs, 0, n)
	for _, res := range input {
		result = append(result, res...)
	}
	return result
}

// doForAddrs sequetially calls the provided callback function fn for each
// address in given slice addrs until the callback function does not return an
// error.
// TODO(owen-d): parallelism
func (c *GatewayClient) doForAddrs(addrs []string, fn func(logproto.BloomGatewayClient) error) error {
	var err error
	var poolClient ringclient.PoolClient

	for _, addr := range addrs {
		poolClient, err = c.pool.GetClientFor(addr)
		if err != nil {
			level.Error(c.logger).Log("msg", "failed to get client for instance", "addr", addr, "err", err)
			continue
		}
		err = fn(poolClient.(logproto.BloomGatewayClient))
		if err != nil {
			level.Error(c.logger).Log("msg", "client do failed for instance", "addr", addr, "err", err)
			continue
		}
		return nil
	}
	return err
}

func mapTokenRangeToFingerprintRange(r bloomutils.Range[uint32]) v1.FingerprintBounds {
	minFp := uint64(r.Min) << 32
	maxFp := uint64(r.Max) << 32
	return v1.NewBounds(
		model.Fingerprint(minFp),
		model.Fingerprint(maxFp|math.MaxUint32),
	)
}

type rsWithRanges struct {
	rs     ring.ReplicationSet
	ranges []v1.FingerprintBounds
	groups []*logproto.GroupedChunkRefs
}

func replicationSetsWithBounds(subRing ring.ReadRing, instances []ring.InstanceDesc) ([]rsWithRanges, error) {
	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()

	servers := make([]rsWithRanges, 0, len(instances))
	for _, inst := range instances {
		tr, err := bloomutils.TokenRangesForInstance(inst.Id, instances)
		if err != nil {
			return nil, errors.Wrap(err, "bloom gateway get ring")
		}

		if len(tr) == 0 {
			level.Warn(util_log.Logger).Log(
				"subroutine", "replicationSetsWithBounds",
				"msg", "instance has no token ranges - should not be possible",
				"instance", inst.Id,
				"n_instances", len(instances),
			)
			continue
		}

		// NB(owen-d): this will send requests to the wrong nodes if RF>1 since it only checks the
		// first token when assigning replicasets
		rs, err := subRing.Get(tr[0], BlocksOwnerRead, bufDescs, bufHosts, bufZones)
		if err != nil {
			return nil, errors.Wrap(err, "bloom gateway get ring")
		}

		bounds := make([]v1.FingerprintBounds, 0, len(tr)/2)
		for i := 0; i < len(tr); i += 2 {
			b := v1.NewBounds(
				model.Fingerprint(uint64(tr[i])<<32),
				model.Fingerprint(uint64(tr[i+1])<<32|math.MaxUint32),
			)
			bounds = append(bounds, b)
		}

		servers = append(servers, rsWithRanges{
			rs:     rs,
			ranges: bounds,
		})
	}
	return servers, nil
}

func partitionByReplicationSet(fingerprints []*logproto.GroupedChunkRefs, rs []rsWithRanges) (result []rsWithRanges) {
	for _, inst := range rs {
		for _, bounds := range inst.ranges {
			min, _ := slices.BinarySearchFunc(fingerprints, bounds, func(g *logproto.GroupedChunkRefs, b v1.FingerprintBounds) int {
				if g.Fingerprint < uint64(b.Min) {
					return -1
				} else if g.Fingerprint > uint64(b.Min) {
					return 1
				}
				return 0
			})

			max, _ := slices.BinarySearchFunc(fingerprints, bounds, func(g *logproto.GroupedChunkRefs, b v1.FingerprintBounds) int {
				if g.Fingerprint <= uint64(b.Max) {
					return -1
				} else if g.Fingerprint > uint64(b.Max) {
					return 1
				}
				return 0
			})

			// fingerprint is out of boundaries
			if min == len(fingerprints) || max == 0 {
				continue
			}

			inst.groups = append(inst.groups, fingerprints[min:max]...)
		}

		if len(inst.groups) > 0 {
			result = append(result, inst)
		}
	}

	return result
}

// GetShuffleShardingSubring returns the subring to be used for a given user.
// This function should be used both by index gateway servers and clients in
// order to guarantee the same logic is used.
func GetShuffleShardingSubring(ring ring.ReadRing, tenantID string, limits Limits) ring.ReadRing {
	shardSize := limits.BloomGatewayShardSize(tenantID)

	// A shard size of 0 means shuffle sharding is disabled for this specific user,
	// so we just return the full ring so that indexes will be sharded across all index gateways.
	// Since we set the shard size to replication factor if shard size is 0, this
	// can only happen if both the shard size and the replication factor are set
	// to 0.
	if shardSize <= 0 {
		return ring
	}

	return ring.ShuffleShard(tenantID, shardSize)
}
