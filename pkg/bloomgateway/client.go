package bloomgateway

import (
	"context"
	"flag"
	"fmt"
	"io"

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
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/util"
)

// BloomGatewayGRPCPool represents a pool of gRPC connections to different index gateway instances.
type BloomGatewayGRPCPool struct {
	grpc_health_v1.HealthClient
	logproto.BloomGatewayClient
	io.Closer
}

// NewBloomGatewayGRPCPool instantiates a new pool of GRPC connections for the Bloom Gateway
// Internally, it also instantiates a protobuf bloom gateway client and a health client.
func NewBloomGatewayGRPCPool(address string, opts []grpc.DialOption) (*BloomGatewayGRPCPool, error) {
	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "new grpc pool dial")
	}

	return &BloomGatewayGRPCPool{
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
	PoolConfig clientpool.PoolConfig `yaml:"-"`

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
	FilterChunks(ctx context.Context, tenant string, from, through model.Time, fingerprints []uint64, chunkRefs [][]*logproto.ChunkRef, filters ...*logproto.LineFilterExpression) ([]uint64, [][]*logproto.ChunkRef, error)
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
		pool:   clientpool.NewPool(cfg.PoolConfig, cfg.Ring, poolFactory, logger),
	}

	return c, nil
}

// FilterChunkRefs implements Client
func (c *GatewayClient) FilterChunks(ctx context.Context, tenant string, from, through model.Time, fingerprints []uint64, chunkRefs [][]*logproto.ChunkRef, filters ...*logproto.LineFilterExpression) ([]uint64, [][]*logproto.ChunkRef, error) {
	// Get the addresses of corresponding bloom gateways for each series.
	_, addrs, err := c.serverAddrsForFingerprints(tenant, fingerprints)
	if err != nil {
		return nil, nil, err
	}

	// Group chunk refs by addresses of one or more bloom gateways.
	// All chunk refs of series that belong to one and the same bloom gateway are set in one batch.
	streamsByAddr := c.groupStreamsByAddr(fingerprints, chunkRefs, addrs)

	filteredChunkRefs := make([][]*logproto.ChunkRef, 0, len(fingerprints))
	filteredFingerprints := make([]uint64, 0, len(fingerprints))

	for _, item := range streamsByAddr {
		err := c.doForAddrs(item.addrs, func(client logproto.BloomGatewayClient) error {
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
			for _, res := range resp.Chunks {
				chunkRefs := make([]*logproto.ChunkRef, 0, len(res.ChunkIDs))
				for _, chunkID := range res.ChunkIDs {
					chk, err := chunk.ParseExternalKey(tenant, chunkID)
					if err != nil {
						// What to do in this case???
						continue
					}
					chunkRefs = append(chunkRefs, &chk.ChunkRef)
				}
				filteredFingerprints = append(filteredFingerprints, res.Fingerprint)
				filteredChunkRefs = append(filteredChunkRefs, chunkRefs)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return fingerprints, filteredChunkRefs, nil
}

func IsEqualAddresses(a, b []string) bool {
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

func ListContainsAddrs(list []chunkRefsByAddrs, addrs []string) (int, bool) {
	for i, r := range list {
		if IsEqualAddresses(r.addrs, addrs) {
			return i, true
		}
	}
	return -1, false
}

type chunkRefsByAddrs struct {
	addrs []string
	refs  []*logproto.ChunkRef
}

func (c *GatewayClient) groupStreamsByAddr(streams []uint64, chunks [][]*logproto.ChunkRef, addresses [][]string) []chunkRefsByAddrs {
	res := make([]chunkRefsByAddrs, 0, len(addresses))
	for i := 0; i < len(addresses); i++ {
		addrs := addresses[i]
		refs := chunks[i]
		if idx, ok := ListContainsAddrs(res, addrs); ok {
			res[idx].refs = append(res[idx].refs, refs...)
		} else {
			res = append(res, chunkRefsByAddrs{addrs: addrs, refs: refs})
		}
	}
	return res
}

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

func (c *GatewayClient) serverAddrsForFingerprints(tenantID string, fingerprints []uint64) ([]uint64, [][]string, error) {
	subRing := GetShuffleShardingSubring(c.ring, tenantID, c.limits)

	addresses := make([][]string, len(fingerprints))
	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()
	var rs ring.ReplicationSet
	var err error

	for idx, key := range fingerprints {
		rs, err = subRing.Get(uint32(key), BlocksRead, bufDescs, bufHosts, bufZones)
		if err != nil {
			return nil, nil, errors.Wrap(err, "bloom gateway get ring")
		}
		addresses[idx] = rs.GetAddresses()
	}

	return fingerprints, addresses, nil
}
