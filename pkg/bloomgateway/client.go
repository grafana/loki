package bloomgateway

import (
	"context"
	"flag"
	"io"
	"sort"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/grpcclient"
	ringclient "github.com/grafana/dskit/ring/client"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"
	"golang.org/x/exp/slices"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/queue"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/discovery"
)

var (
	// groupedChunksRefPool pooling slice of logproto.GroupedChunkRefs [64, 128, 256, ..., 65536]
	groupedChunksRefPool = queue.NewSlicePool[*logproto.GroupedChunkRefs](1<<6, 1<<16, 2)
)

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
	// nolint:staticcheck // grpc.Dial() has been deprecated; we'll address it before upgrading to gRPC 2.
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

	if i.Addresses == "" {
		return errors.New("addresses requires a list of comma separated strings in DNS service discovery format with at least one item")
	}

	return nil
}

type Client interface {
	FilterChunks(ctx context.Context, tenant string, interval bloomshipper.Interval, blocks []blockWithSeries, plan plan.QueryPlan) ([]*logproto.GroupedChunkRefs, error)
}

// clientPool is a minimal interface that is satisfied by the JumpHashClientPool.
// It does only expose functions that are used by the GatewayClient
// and is required to mock the JumpHashClientPool in tests.
type clientPool interface {
	GetClientFor(string) (ringclient.PoolClient, error)
	Addr(string) (string, error)
	Stop()
}

type GatewayClient struct {
	cfg         ClientConfig
	logger      log.Logger
	metrics     *clientMetrics
	pool        clientPool
	dnsProvider *discovery.DNS
}

func NewClient(
	cfg ClientConfig,
	limits Limits,
	registerer prometheus.Registerer,
	logger log.Logger,
	cacheGen resultscache.CacheGenNumberLoader,
	retentionEnabled bool,
) (*GatewayClient, error) {
	metrics := newClientMetrics(registerer)

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(metrics.requestLatency))
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

	clientPool := ringclient.NewPool(
		"bloom-gateway",
		ringclient.PoolConfig(cfg.PoolConfig),
		func() ([]string, error) { return dnsProvider.Addresses(), nil },
		ringclient.PoolAddrFunc(poolFactory),
		metrics.clients,
		logger,
	)

	pool := NewJumpHashClientPool(clientPool, dnsProvider, cfg.PoolConfig.CheckInterval, logger)
	pool.Start()

	return &GatewayClient{
		cfg:         cfg,
		logger:      logger,
		metrics:     metrics,
		pool:        pool,
		dnsProvider: dnsProvider, // keep reference so we can stop it when the client is closed
	}, nil
}

func (c *GatewayClient) Close() {
	c.pool.Stop()
	c.dnsProvider.Stop()
}

// FilterChunkRefs implements Client
func (c *GatewayClient) FilterChunks(ctx context.Context, _ string, interval bloomshipper.Interval, blocks []blockWithSeries, plan plan.QueryPlan) ([]*logproto.GroupedChunkRefs, error) {
	// no block and therefore no series with chunks
	if len(blocks) == 0 {
		return nil, nil
	}

	pos := make(map[string]int)
	servers := make([]addrWithGroups, 0, len(blocks))
	for _, blockWithSeries := range blocks {
		addr, err := c.pool.Addr(blockWithSeries.block.String())

		// the client should return the full, unfiltered list of chunks instead of an error
		if err != nil {
			level.Error(c.logger).Log("msg", "failed to resolve server address for block", "block", blockWithSeries.block, "err", err)
			var series [][]*logproto.GroupedChunkRefs
			for i := range blocks {
				series = append(series, blocks[i].series)
			}
			return mergeSeries(series, nil)
		}

		if idx, found := pos[addr]; found {
			servers[idx].groups = append(servers[idx].groups, blockWithSeries.series...)
			servers[idx].blocks = append(servers[idx].blocks, blockWithSeries.block.String())
		} else {
			pos[addr] = len(servers)
			servers = append(servers, addrWithGroups{
				addr:   addr,
				blocks: []string{blockWithSeries.block.String()},
				groups: blockWithSeries.series,
			})
		}
	}

	results := make([][]*logproto.GroupedChunkRefs, len(servers))
	count := atomic.NewInt64(0)
	err := concurrency.ForEachJob(ctx, len(servers), len(servers), func(ctx context.Context, i int) error {
		rs := servers[i]

		sort.Slice(rs.groups, func(i, j int) bool {
			return rs.groups[i].Fingerprint < rs.groups[j].Fingerprint
		})

		return c.doForAddrs([]string{rs.addr}, func(client logproto.BloomGatewayClient) error {
			req := &logproto.FilterChunkRefRequest{
				From:    interval.Start,
				Through: interval.End,
				Refs:    rs.groups,
				Blocks:  rs.blocks,
				Plan:    plan,
			}
			resp, err := client.FilterChunkRefs(ctx, req)
			if err != nil {
				// We don't want a single bloom-gw failure to fail the entire query,
				// so instrument & move on
				level.Error(c.logger).Log(
					"msg", "filter failed for instance, skipping",
					"addr", rs.addr,
					"series", len(rs.groups),
					"blocks", len(rs.blocks),
					"err", err,
				)
				// filter none of the results on failed request
				c.metrics.clientRequests.WithLabelValues(typeError).Inc()
				results[i] = rs.groups
			} else {
				c.metrics.clientRequests.WithLabelValues(typeSuccess).Inc()
				results[i] = resp.ChunkRefs
			}

			count.Add(int64(len(results[i])))
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	buf := make([]*logproto.GroupedChunkRefs, 0, int(count.Load()))
	return mergeSeries(results, buf)
}

// mergeSeries combines responses from multiple FilterChunkRefs calls and deduplicates
// chunks from series that appear in multiple responses.
// To avoid allocations, an optional slice can be passed as second argument.
func mergeSeries(input [][]*logproto.GroupedChunkRefs, buf []*logproto.GroupedChunkRefs) ([]*logproto.GroupedChunkRefs, error) {
	// clear provided buffer
	buf = buf[:0]

	iters := make([]iter.PeekIterator[*logproto.GroupedChunkRefs], 0, len(input))
	for _, inp := range input {
		sort.Slice(inp, func(i, j int) bool { return inp[i].Fingerprint < inp[j].Fingerprint })
		iters = append(iters, iter.NewPeekIter(iter.NewSliceIter(inp)))
	}

	heapIter := v1.NewHeapIterator[*logproto.GroupedChunkRefs](
		func(a, b *logproto.GroupedChunkRefs) bool { return a.Fingerprint < b.Fingerprint },
		iters...,
	)

	dedupeIter := iter.NewDedupingIter[*logproto.GroupedChunkRefs, *logproto.GroupedChunkRefs](
		// eq
		func(a, b *logproto.GroupedChunkRefs) bool { return a.Fingerprint == b.Fingerprint },
		// from
		iter.Identity[*logproto.GroupedChunkRefs],
		// merge
		func(a, b *logproto.GroupedChunkRefs) *logproto.GroupedChunkRefs {
			// TODO(chaudum): Check if we can assume sorted shortrefs here
			if !slices.IsSortedFunc(a.Refs, func(a, b *logproto.ShortRef) int { return a.Cmp(b) }) {
				slices.SortFunc(a.Refs, func(a, b *logproto.ShortRef) int { return a.Cmp(b) })
			}
			if !slices.IsSortedFunc(b.Refs, func(a, b *logproto.ShortRef) int { return a.Cmp(b) }) {
				slices.SortFunc(b.Refs, func(a, b *logproto.ShortRef) int { return a.Cmp(b) })
			}
			return &logproto.GroupedChunkRefs{
				Fingerprint: a.Fingerprint,
				Tenant:      a.Tenant,
				Refs:        mergeChunkSets(a.Refs, b.Refs),
			}
		},
		// iterator
		iter.NewPeekIter(heapIter),
	)

	return iter.CollectInto(dedupeIter, buf)
}

// mergeChunkSets merges and deduplicates two sorted slices of shortRefs
func mergeChunkSets(s1, s2 []*logproto.ShortRef) (result []*logproto.ShortRef) {
	var i, j int
	for i < len(s1) && j < len(s2) {
		a, b := s1[i], s2[j]

		if a.Equal(b) {
			result = append(result, a)
			i++
			j++
			continue
		}

		if a.Less(b) {
			result = append(result, a)
			i++
			continue
		}

		result = append(result, b)
		j++
	}

	if i < len(s1) {
		result = append(result, s1[i:]...)
	}
	if j < len(s2) {
		result = append(result, s2[j:]...)
	}

	return result
}

// doForAddrs sequetially calls the provided callback function fn for each
// address in given slice addrs until the callback function does not return an
// error.
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

type addrWithGroups struct {
	addr   string
	blocks []string
	groups []*logproto.GroupedChunkRefs
}
