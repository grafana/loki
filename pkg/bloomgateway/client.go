package bloomgateway

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/ring"
	ringclient "github.com/grafana/dskit/ring/client"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
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
	// It is defined at the distributors YAML section and reused here.
	PoolConfig clientpool.PoolConfig `yaml:"pool_config,omitempty" doc:"description=Configures the behavior of the connection pool."`

	// GRPCClientConfig configures the gRPC connection between the Bloom Gateway client and the server.
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`

	// LogGatewayRequests configures if requests sent to the gateway should be logged or not.
	// The log messages are of type debug and contain the address of the gateway and the relevant tenant.
	LogGatewayRequests bool `yaml:"log_gateway_requests"`

	// Ring is the Bloom Gateway ring used to find the appropriate Bloom Gateway instance
	// this client should talk to.
	Ring ring.ReadRing `yaml:"-"`
}

// RegisterFlags registers flags for the Bloom Gateway client configuration.
func (i *ClientConfig) RegisterFlags(f *flag.FlagSet) {
	i.RegisterFlagsWithPrefix("bloom-gateway-client.", f)
}

// RegisterFlagsWithPrefix registers flags for the Bloom Gateway client configuration with a common prefix.
func (i *ClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	i.GRPCClientConfig.RegisterFlagsWithPrefix(prefix+"grpc", f)
	f.BoolVar(&i.LogGatewayRequests, prefix+"log-gateway-requests", false, "Flag to control whether requests sent to the gateway should be logged or not.")
}

type Client interface {
	FilterChunks(ctx context.Context, tenant string, from, through model.Time, groups []*logproto.GroupedChunkRefs, filters ...*logproto.LineFilterExpression) ([]*logproto.GroupedChunkRefs, error)
}

type GatewayClient struct {
	cfg    ClientConfig
	limits Limits
	logger log.Logger
	pool   *ringclient.Pool
	ring   ring.ReadRing
}

func NewGatewayClient(cfg ClientConfig, limits Limits, registerer prometheus.Registerer, logger log.Logger) (*GatewayClient, error) {
	latency := promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Subsystem: "bloom_gateway",
		Name:      "request_duration_seconds",
		Help:      "Time (in seconds) spent serving requests when using the bloom gateway",
		Buckets:   instrument.DefBuckets,
	}, []string{"operation", "status_code"})

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(latency))
	if err != nil {
		return nil, err
	}

	poolFactory := func(addr string) (ringclient.PoolClient, error) {
		pool, err := NewBloomGatewayGRPCPool(addr, dialOpts)
		if err != nil {
			return nil, errors.Wrap(err, "new bloom gateway grpc pool")
		}
		return pool, nil
	}

	c := &GatewayClient{
		cfg:    cfg,
		logger: logger,
		limits: limits,
		pool:   clientpool.NewPool("bloom-gateway", cfg.PoolConfig, cfg.Ring, ringclient.PoolAddrFunc(poolFactory), logger),
	}

	return c, nil
}

func shuffleAddrs(addrs []string) []string {
	rand.Shuffle(len(addrs), func(i, j int) {
		addrs[i], addrs[j] = addrs[j], addrs[i]
	})
	return addrs
}

// FilterChunkRefs implements Client
func (c *GatewayClient) FilterChunks(ctx context.Context, tenant string, from, through model.Time, groups []*logproto.GroupedChunkRefs, filters ...*logproto.LineFilterExpression) ([]*logproto.GroupedChunkRefs, error) {
	// Get the addresses of corresponding bloom gateways for each series.
	fingerprints, addrs, err := c.serverAddrsForFingerprints(tenant, groups)
	if err != nil {
		return nil, err
	}

	// Group chunk refs by addresses of one or more bloom gateways.
	// All chunk refs of series that belong to one and the same bloom gateway are set in one batch.
	streamsByAddr := c.groupStreamsByAddr(groups, addrs)

	// TODO(chaudum): We might over-allocate for the filtered responses here?
	filteredChunkRefs := make([]*logproto.GroupedChunkRefs, 0, len(fingerprints))

	for _, item := range streamsByAddr {
		// randomize order of addresses so we don't hotspot the first server in the list
		addrs := shuffleAddrs(item.addrs)
		err := c.doForAddrs(addrs, func(client logproto.BloomGatewayClient) error {
			req := &logproto.FilterChunkRefRequest{
				From:    from,
				Through: through,
				Refs:    item.refs,
				Filters: filters,
			}
			resp, err := client.FilterChunkRefs(ctx, req)
			if err != nil {
				return err
			}
			filteredChunkRefs = append(filteredChunkRefs, resp.ChunkRefs...)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return filteredChunkRefs, nil
}

// isEqualStringElements checks if two string slices contain the same elements.
// The order of the elements is ignored.
func isEqualStringElements(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, s := range a {
		if !util.StringsContain(b, s) {
			return false
		}
	}
	return true
}

// listContainsAddrs checks if a slice of chunkRefAddrs contains an element
// whos field addrs contains the same addresses as the given slice of
// addresses.
// It returns the index of the element, if found, and a boolean whether the
// given list contains the given addrs.
func listContainsAddrs(list []chunkRefsByAddrs, addrs []string) (int, bool) {
	for i, r := range list {
		if isEqualStringElements(r.addrs, addrs) {
			return i, true
		}
	}
	return -1, false
}

type chunkRefsByAddrs struct {
	addrs []string
	refs  []*logproto.GroupedChunkRefs
}

func (c *GatewayClient) groupStreamsByAddr(groups []*logproto.GroupedChunkRefs, addresses [][]string) []chunkRefsByAddrs {
	res := make([]chunkRefsByAddrs, 0, len(addresses))
	for i := 0; i < len(addresses); i++ {
		addrs := addresses[i]
		refs := groups[i]
		if idx, ok := listContainsAddrs(res, addrs); ok {
			res[idx].refs = append(res[idx].refs, refs)
		} else {
			res = append(res, chunkRefsByAddrs{addrs: addrs, refs: []*logproto.GroupedChunkRefs{refs}})
		}
	}
	return res
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
			level.Error(c.logger).Log("msg", fmt.Sprintf("failed to get client for instance %s", addr), "err", err)
			continue
		}
		err = fn(poolClient.(logproto.BloomGatewayClient))
		if err != nil {
			level.Error(c.logger).Log("msg", fmt.Sprintf("client do failed for instance %s", addr), "err", err)
			continue
		}
		return nil
	}
	return err
}

// serverAddrsForFingerprints returns a slices of server address slices for
// each fingerprint of given fingerprints.
// The indexes of the returned slices correspond to each other.
// Returns an error in case the bloom gateway ring could not get the
// corresponding replica set for a given fingerprint.
// Warning: This function becomes inefficient when the number of fingerprints is very large.
func (c *GatewayClient) serverAddrsForFingerprints(tenantID string, groups []*logproto.GroupedChunkRefs) ([]uint64, [][]string, error) {
	subRing := GetShuffleShardingSubring(c.ring, tenantID, c.limits)

	rs, err := subRing.GetAllHealthy(BlocksRead)
	if err != nil {
		return nil, nil, errors.Wrap(err, "bloom gateway get healthy instances")
	}

	var numTokens int
	for _, instanceDesc := range rs.Instances {
		numTokens += len(instanceDesc.Tokens)
	}

	numFingerprints := len(groups)
	if numFingerprints > int(float64(numTokens)*math.Log2(float64(numFingerprints))) {
		// TODO(chaudum): Implement algorithm in O(n * m * log(k) + n) instead of O(k) by iterating over ring tokens
		// and finding corresponding fingerprint ranges using binary search.
		// n .. number of instances
		// m .. number of tokens per instance
		// k .. number of fingerprints
		level.Warn(c.logger).Log("msg", "using an inefficient algorithm to determin server addresses for fingerprints", "fingerprints", numFingerprints, "tokens", numTokens)
	}

	fingerprints := make([]uint64, numFingerprints)
	addresses := make([][]string, numFingerprints)
	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()

	for idx, key := range groups {
		rs, err = subRing.Get(uint32(key.Fingerprint), BlocksRead, bufDescs, bufHosts, bufZones)
		if err != nil {
			return nil, nil, errors.Wrap(err, "bloom gateway get ring")
		}
		fingerprints[idx] = key.Fingerprint
		addresses[idx] = rs.GetAddresses()
	}

	return fingerprints, addresses, nil
}
