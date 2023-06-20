package gatewayclient

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	util_math "github.com/grafana/loki/pkg/util/math"
)

const (
	maxQueriesPerGrpc      = 100
	maxConcurrentGrpcCalls = 10
)

// IndexGatewayClientConfig configures the Index Gateway client used to
// communicate with the Index Gateway server.
type IndexGatewayClientConfig struct {
	// Mode sets in which mode the client will operate. It is actually defined at the
	// index_gateway YAML section and reused here.
	Mode indexgateway.Mode `yaml:"-"`

	// PoolConfig defines the behavior of the gRPC connection pool used to communicate
	// with the Index Gateway.
	//
	// Only relevant for the ring mode.
	// It is defined at the distributors YAML section and reused here.
	PoolConfig clientpool.PoolConfig `yaml:"-"`

	// Ring is the Index Gateway ring used to find the appropriate Index Gateway instance
	// this client should talk to.
	//
	// Only relevant for the ring mode.
	Ring ring.ReadRing `yaml:"-"`

	// GRPCClientConfig configures the gRPC connection between the Index Gateway client and the server.
	//
	// Used by both, ring and simple mode.
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`

	// Address of the Index Gateway instance responsible for retaining the index for all tenants.
	//
	// Only relevant for the simple mode.
	Address string `yaml:"server_address,omitempty"`

	// Forcefully disable the use of the index gateway client for the storage.
	// This is mainly useful for the index-gateway component which should always use the storage.
	Disabled bool `yaml:"-"`

	// LogGatewayRequests configures if requests sent to the gateway should be logged or not.
	// The log messages are of type debug and contain the address of the gateway and the relevant tenant.
	LogGatewayRequests bool `yaml:"log_gateway_requests"`
}

// RegisterFlagsWithPrefix register client-specific flags with the given prefix.
//
// Flags that are used by both, client and server, are defined in the indexgateway package.
func (i *IndexGatewayClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	i.GRPCClientConfig.RegisterFlagsWithPrefix(prefix+".grpc", f)
	f.StringVar(&i.Address, prefix+".server-address", "", "Hostname or IP of the Index Gateway gRPC server running in simple mode.")
	f.BoolVar(&i.LogGatewayRequests, prefix+".log-gateway-requests", false, "Whether requests sent to the gateway should be logged or not.")
}

func (i *IndexGatewayClientConfig) RegisterFlags(f *flag.FlagSet) {
	i.RegisterFlagsWithPrefix("index-gateway-client", f)
}

type GatewayClient struct {
	cfg IndexGatewayClientConfig

	storeGatewayClientRequestDuration *prometheus.HistogramVec

	conn       *grpc.ClientConn
	grpcClient logproto.IndexGatewayClient

	pool *ring_client.Pool

	ring ring.ReadRing

	stringBufPool   *sync.Pool
	instanceBufPool *sync.Pool
}

// NewGatewayClient instantiates a new client used to communicate with an Index Gateway instance.
//
// If it is configured to be in ring mode, a pool of GRPC connections to all Index Gateway instances is created.
// Otherwise, it creates a single GRPC connection to an Index Gateway instance running in simple mode.
func NewGatewayClient(cfg IndexGatewayClientConfig, r prometheus.Registerer, logger log.Logger) (*GatewayClient, error) {
	latency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "index_gateway_request_duration_seconds",
		Help:      "Time (in seconds) spent serving requests when using the index gateway",
		Buckets:   instrument.DefBuckets,
	}, []string{"operation", "status_code"})
	if r != nil {
		err := r.Register(latency)
		if err != nil {
			alreadyErr, ok := err.(prometheus.AlreadyRegisteredError)
			if !ok {
				return nil, err
			}
			latency = alreadyErr.ExistingCollector.(*prometheus.HistogramVec)
		}
	}

	sgClient := &GatewayClient{
		cfg:                               cfg,
		storeGatewayClientRequestDuration: latency,
		ring:                              cfg.Ring,
	}

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(sgClient.storeGatewayClientRequestDuration))
	if err != nil {
		return nil, errors.Wrap(err, "index gateway grpc dial option")
	}

	if sgClient.cfg.Mode == indexgateway.RingMode {
		factory := func(addr string) (ring_client.PoolClient, error) {
			igPool, err := NewIndexGatewayGRPCPool(addr, dialOpts)
			if err != nil {
				return nil, errors.Wrap(err, "new index gateway grpc pool")
			}

			return igPool, nil
		}

		// Replication factor plus additional room for JOINING/LEAVING instances
		// See also ring.GetBufferSize
		bufSize := cfg.Ring.ReplicationFactor() * 3 / 2
		sgClient.stringBufPool = &sync.Pool{
			New: func() any {
				return make([]string, 0, bufSize)
			},
		}
		sgClient.instanceBufPool = &sync.Pool{
			New: func() any {
				return make([]ring.InstanceDesc, 0, bufSize)
			},
		}

		sgClient.pool = clientpool.NewPool(cfg.PoolConfig, sgClient.ring, factory, logger)
	} else {
		sgClient.conn, err = grpc.Dial(cfg.Address, dialOpts...)
		if err != nil {
			return nil, errors.Wrap(err, "index gateway grpc dial")
		}

		sgClient.grpcClient = logproto.NewIndexGatewayClient(sgClient.conn)
	}

	return sgClient, nil
}

// Stop stops the execution of this gateway client.
//
// If it is in simple mode, the single GRPC connection is closed. Otherwise, nothing happens.
func (s *GatewayClient) Stop() {
	if s.cfg.Mode == indexgateway.SimpleMode {
		s.conn.Close()
	}
}

func (s *GatewayClient) QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	if len(queries) <= maxQueriesPerGrpc {
		return s.doQueries(ctx, queries, callback)
	}

	jobsCount := len(queries) / maxQueriesPerGrpc
	if len(queries)%maxQueriesPerGrpc != 0 {
		jobsCount++
	}
	return concurrency.ForEachJob(ctx, jobsCount, maxConcurrentGrpcCalls, func(ctx context.Context, idx int) error {
		return s.doQueries(ctx, queries[idx*maxQueriesPerGrpc:util_math.Min((idx+1)*maxQueriesPerGrpc, len(queries))], callback)
	})
}

func (s *GatewayClient) QueryIndex(_ context.Context, _ *logproto.QueryIndexRequest, _ ...grpc.CallOption) (logproto.IndexGateway_QueryIndexClient, error) {
	panic("not implemented")
}

func (s *GatewayClient) GetChunkRef(ctx context.Context, in *logproto.GetChunkRefRequest, opts ...grpc.CallOption) (*logproto.GetChunkRefResponse, error) {
	if s.cfg.Mode == indexgateway.RingMode {
		var (
			resp *logproto.GetChunkRefResponse
			err  error
		)
		err = s.ringModeDo(ctx, func(client logproto.IndexGatewayClient) error {
			resp, err = client.GetChunkRef(ctx, in, opts...)
			return err
		})
		return resp, err
	}
	return s.grpcClient.GetChunkRef(ctx, in, opts...)
}

func (s *GatewayClient) GetSeries(ctx context.Context, in *logproto.GetSeriesRequest, opts ...grpc.CallOption) (*logproto.GetSeriesResponse, error) {
	if s.cfg.Mode == indexgateway.RingMode {
		var (
			resp *logproto.GetSeriesResponse
			err  error
		)
		err = s.ringModeDo(ctx, func(client logproto.IndexGatewayClient) error {
			resp, err = client.GetSeries(ctx, in, opts...)
			return err
		})
		return resp, err
	}
	return s.grpcClient.GetSeries(ctx, in, opts...)
}

func (s *GatewayClient) LabelNamesForMetricName(ctx context.Context, in *logproto.LabelNamesForMetricNameRequest, opts ...grpc.CallOption) (*logproto.LabelResponse, error) {
	if s.cfg.Mode == indexgateway.RingMode {
		var (
			resp *logproto.LabelResponse
			err  error
		)
		err = s.ringModeDo(ctx, func(client logproto.IndexGatewayClient) error {
			resp, err = client.LabelNamesForMetricName(ctx, in, opts...)
			return err
		})
		return resp, err
	}
	return s.grpcClient.LabelNamesForMetricName(ctx, in, opts...)
}

func (s *GatewayClient) LabelValuesForMetricName(ctx context.Context, in *logproto.LabelValuesForMetricNameRequest, opts ...grpc.CallOption) (*logproto.LabelResponse, error) {
	if s.cfg.Mode == indexgateway.RingMode {
		var (
			resp *logproto.LabelResponse
			err  error
		)
		err = s.ringModeDo(ctx, func(client logproto.IndexGatewayClient) error {
			resp, err = client.LabelValuesForMetricName(ctx, in, opts...)
			return err
		})
		return resp, err
	}
	return s.grpcClient.LabelValuesForMetricName(ctx, in, opts...)
}

func (s *GatewayClient) GetStats(ctx context.Context, in *logproto.IndexStatsRequest, opts ...grpc.CallOption) (*logproto.IndexStatsResponse, error) {
	if s.cfg.Mode == indexgateway.RingMode {
		var (
			resp *logproto.IndexStatsResponse
			err  error
		)
		err = s.ringModeDo(ctx, func(client logproto.IndexGatewayClient) error {
			resp, err = client.GetStats(ctx, in, opts...)
			return err
		})
		return resp, err
	}
	return s.grpcClient.GetStats(ctx, in, opts...)
}

func (s *GatewayClient) GetSeriesVolume(ctx context.Context, in *logproto.VolumeRequest, opts ...grpc.CallOption) (*logproto.VolumeResponse, error) {
	if s.cfg.Mode == indexgateway.RingMode {
		var (
			resp *logproto.VolumeResponse
			err  error
		)
		err = s.ringModeDo(ctx, func(client logproto.IndexGatewayClient) error {
			resp, err = client.GetSeriesVolume(ctx, in, opts...)
			return err
		})
		return resp, err
	}
	return s.grpcClient.GetSeriesVolume(ctx, in, opts...)
}

func (s *GatewayClient) doQueries(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	queryKeyQueryMap := make(map[string]index.Query, len(queries))
	gatewayQueries := make([]*logproto.IndexQuery, 0, len(queries))

	for _, query := range queries {
		queryKeyQueryMap[shipper_util.QueryKey(query)] = query
		gatewayQueries = append(gatewayQueries, &logproto.IndexQuery{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		})
	}

	if s.cfg.Mode == indexgateway.RingMode {
		return s.ringModeDo(ctx, func(client logproto.IndexGatewayClient) error {
			return s.clientDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback, client)
		})
	}

	return s.clientDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback, s.grpcClient)
}

// clientDoQueries send a query request to an Index Gateway instance using the given gRPC client.
//
// It is used by both, simple and ring mode.
func (s *GatewayClient) clientDoQueries(ctx context.Context, gatewayQueries []*logproto.IndexQuery,
	queryKeyQueryMap map[string]index.Query, callback index.QueryPagesCallback, client logproto.IndexGatewayClient,
) error {
	streamer, err := client.QueryIndex(ctx, &logproto.QueryIndexRequest{Queries: gatewayQueries})
	if err != nil {
		return errors.Wrap(err, "query index")
	}

	for {
		resp, err := streamer.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.WithStack(err)
		}
		query, ok := queryKeyQueryMap[resp.QueryKey]
		if !ok {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("unexpected %s QueryKey received, expected queries %s", resp.QueryKey, fmt.Sprint(queryKeyQueryMap)))
			return fmt.Errorf("unexpected %s QueryKey received", resp.QueryKey)
		}
		if !callback(query, &readBatch{resp}) {
			return nil
		}
	}

	return nil
}

// ringModeDo executes the given function for each Index Gateway instance in the ring mapping to the correct tenant in the index.
// In case of callback failure, we'll try another member of the ring for that tenant ID.
func (s *GatewayClient) ringModeDo(ctx context.Context, callback func(client logproto.IndexGatewayClient) error) error {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return errors.Wrap(err, "index gateway client get tenant ID")
	}

	bufDescs := s.instanceBufPool.Get().([]ring.InstanceDesc)
	defer s.instanceBufPool.Put(bufDescs) //nolint:staticcheck
	bufHosts := s.stringBufPool.Get().([]string)
	defer s.stringBufPool.Put(bufHosts) //nolint:staticcheck
	bufZones := s.stringBufPool.Get().([]string)
	defer s.stringBufPool.Put(bufZones) //nolint:staticcheck

	key := util.TokenFor(userID, "" /* labels */)
	rs, err := s.ring.Get(key, ring.WriteNoExtend, bufDescs[:0], bufHosts[:0], bufZones[:0])
	if err != nil {
		return errors.Wrap(err, "index gateway get ring")
	}

	addrs := rs.GetAddresses()
	// shuffle addresses to make sure we don't always access the same Index Gateway instances in sequence for same tenant.
	rand.Shuffle(len(addrs), func(i, j int) {
		addrs[i], addrs[j] = addrs[j], addrs[i]
	})
	var lastErr error
	for _, addr := range addrs {
		if s.cfg.LogGatewayRequests {
			level.Debug(util_log.Logger).Log("msg", "sending request to gateway", "gateway", addr, "tenant", userID)
		}

		genericClient, err := s.pool.GetClientFor(addr)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to get client for instance %s", addr), "err", err)
			continue
		}

		client := (genericClient.(logproto.IndexGatewayClient))
		if err := callback(client); err != nil {
			lastErr = err
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("client do failed for instance %s", addr), "err", err)
			continue
		}

		return nil
	}

	return lastErr
}

func (s *GatewayClient) NewWriteBatch() index.WriteBatch {
	panic("unsupported")
}

func (s *GatewayClient) BatchWrite(_ context.Context, _ index.WriteBatch) error {
	panic("unsupported")
}

type readBatch struct {
	*logproto.QueryIndexResponse
}

func (r *readBatch) Iterator() index.ReadBatchIterator {
	return &grpcIter{
		i:                  -1,
		QueryIndexResponse: r.QueryIndexResponse,
	}
}

type grpcIter struct {
	i int
	*logproto.QueryIndexResponse
}

func (b *grpcIter) Next() bool {
	b.i++
	return b.i < len(b.Rows)
}

func (b *grpcIter) RangeValue() []byte {
	return b.Rows[b.i].RangeValue
}

func (b *grpcIter) Value() []byte {
	return b.Rows[b.i].Value
}
