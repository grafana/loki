package shipper

import (
	"context"
	"fmt"
	"io"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/tenant"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	util_math "github.com/grafana/loki/pkg/util/math"
)

const (
	maxQueriesPerGrpc      = 100
	maxConcurrentGrpcCalls = 10
)

type GatewayClient struct {
	cfg IndexGatewayClientConfig

	storeGatewayClientRequestDuration *prometheus.HistogramVec

	conn       *grpc.ClientConn
	grpcClient indexgatewaypb.IndexGatewayClient

	mode indexgateway.Mode

	pool *ring_client.Pool

	ring ring.ReadRing
}

// NewGatewayClient instantiates a new client used to communicate with an Index Gateway instance.
//
// If it is configured to be in ring mode, a pool of GRPC connections to all Index Gateway instances is created.
// Otherwise, it creates a single GRPC connection to an Index Gateway instance running in simple mode.
func NewGatewayClient(cfg IndexGatewayClientConfig, indexGatewayRing ring.ReadRing, r prometheus.Registerer, logger log.Logger) (*GatewayClient, error) {
	sgClient := &GatewayClient{
		cfg: cfg,
		storeGatewayClientRequestDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "store_gateway_request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using boltdb shipper store gateway",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
		mode: cfg.Mode,
		ring: indexGatewayRing,
	}

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(sgClient.storeGatewayClientRequestDuration))
	if err != nil {
		return nil, errors.Wrap(err, "index gateway grpc dial option")
	}

	if sgClient.mode == indexgateway.RingMode {
		factory := func(addr string) (ring_client.PoolClient, error) {
			igPool, err := NewIndexGatewayGRPCPool(addr, dialOpts)
			if err != nil {
				return nil, errors.Wrap(err, "new index gateway grpc pool")
			}

			return igPool, nil
		}

		sgClient.pool = clientpool.NewPool(cfg.PoolConfig, indexGatewayRing, factory, logger)
	} else {
		sgClient.conn, err = grpc.Dial(cfg.Address, dialOpts...)
		if err != nil {
			return nil, errors.Wrap(err, "index gateway grpc dial")
		}

		sgClient.grpcClient = indexgatewaypb.NewIndexGatewayClient(sgClient.conn)
	}

	return sgClient, nil
}

// Stop stops the execution of this gateway client.
//
// If it is in simple mode, the sinlge GRPC connection is closed. Otherwise, nothing happens.
func (s *GatewayClient) Stop() {
	if s.mode == indexgateway.SimpleMode {
		s.conn.Close()
	}
}

func (s *GatewayClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
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

// doQueries queries index pages from the Index Gateway.
func (s *GatewayClient) doQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	queryKeyQueryMap := make(map[string]chunk.IndexQuery, len(queries))
	gatewayQueries := make([]*indexgatewaypb.IndexQuery, 0, len(queries))

	for _, query := range queries {
		queryKeyQueryMap[shipper_util.QueryKey(query)] = query
		gatewayQueries = append(gatewayQueries, &indexgatewaypb.IndexQuery{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		})
	}

	if s.mode == indexgateway.RingMode {
		return s.ringModeDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback)
	}

	return s.clientDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback, s.grpcClient)
}

// clientDoQueries send a query request to an Index Gateway instance using the given gRPC client.
//
// It is used by both, simple and ring mode.
func (s *GatewayClient) clientDoQueries(ctx context.Context, gatewayQueries []*indexgatewaypb.IndexQuery,
	queryKeyQueryMap map[string]chunk.IndexQuery, callback chunk.QueryPagesCallback, client indexgatewaypb.IndexGatewayClient) error {
	streamer, err := client.QueryIndex(ctx, &indexgatewaypb.QueryIndexRequest{Queries: gatewayQueries})
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

// ringModeDoQueries prepares an index query to be sent to the Index Gateway, and then sends it
// using the clientDoQueries implementation.
//
// The preparation and sending phase includes:
// 1. Extracting the tenant name from the query.
// 2. Fetching different Index Gateway instances assigned to the extracted tenant.
// 3. Iterating in parallel over all fetched Index Gateway instances, getting their gRPC connections
//  from the pool and invoking clientDoQueries using their client.
func (s *GatewayClient) ringModeDoQueries(ctx context.Context, gatewayQueries []*indexgatewaypb.IndexQuery, queryKeyQueryMap map[string]chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return errors.Wrap(err, "index gateway client get tenant ID")
	}

	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()

	key := util.TokenFor(userID, "" /* labels */)
	rs, err := s.ring.Get(key, ring.WriteNoExtend, bufDescs, bufHosts, bufZones)
	if err != nil {
		return errors.Wrap(err, "index gateway get ring")
	}

	addrs := rs.GetAddresses()
	for _, addr := range addrs {
		genericClient, err := s.pool.GetClientFor(addr)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to get client for instance %s", addr), "err", err)
			continue
		}

		client := (genericClient.(indexgatewaypb.IndexGatewayClient))
		if err := s.clientDoQueries(ctx, gatewayQueries, queryKeyQueryMap, callback, client); err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("client do queries failed for instance %s", addr), "err", err)
			continue
		}

		return nil
	}

	return fmt.Errorf("index gateway replicationSet clientDoQueries")
}

func (s *GatewayClient) NewWriteBatch() chunk.WriteBatch {
	panic("unsupported")
}

func (s *GatewayClient) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	panic("unsupported")
}

type readBatch struct {
	*indexgatewaypb.QueryIndexResponse
}

func (r *readBatch) Iterator() chunk.ReadBatchIterator {
	return &grpcIter{
		i:                  -1,
		QueryIndexResponse: r.QueryIndexResponse,
	}
}

type grpcIter struct {
	i int
	*indexgatewaypb.QueryIndexResponse
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
