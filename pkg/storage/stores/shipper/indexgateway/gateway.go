package indexgateway

import (
	"context"
	"sync"

	"flag"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	lokiutil "github.com/grafana/loki/pkg/util"
)

const (
	maxIndexEntriesPerResponse     = 1000
	ringAutoForgetUnhealthyPeriods = 10
	ringKey                        = "index-gateway"
	ringNameForServer              = "index-gateway"
	ringNumTokens                  = 1
	ringReplicationFactor          = 3
)

type IndexQuerier interface {
	QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error
	Stop()
}

type Gateway struct {
	services.Service

	indexQuerier IndexQuerier
	cfg          Config
	log          log.Logger

	shipper chunk.IndexClient

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	ringLifecycler *ring.BasicLifecycler
	ring           *ring.Ring
}

type Config struct {
	useIndexGatewayRing bool                `yaml:"use_index_gateway_ring"`       // TODO: maybe just `yaml:"useRing"`?
	IndexGatewayRing    lokiutil.RingConfig `yaml:"index_gateway_ring,omitempty"` // TODO: maybe just `yaml:"ring"`?
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.useIndexGatewayRing, "index-gateway.use-index-gateway-ring", false, "Set to true to enable per-tenant hashing to Index Gateways via a ring. This helps with horizontal scalability by reducing startup time required when provisioning a new Index Gateway")
	cfg.IndexGatewayRing.RegisterFlagsWithPrefix("index-gateway.", "collectors/", f)
}

func NewIndexGateway(cfg Config, log log.Logger, registerer prometheus.Registerer, shipperIndexClient *shipper.Shipper, indexQuerier IndexQuerier) (*Gateway, error) {
	g := &Gateway{
		indexQuerier: indexQuerier,
		shipper:      shipperIndexClient,
		cfg:          cfg,
		log:          log,
	}

	g.Service = services.NewIdleService(nil, func(failureCase error) error {
		g.indexQuerier.Stop()
		return nil
	})

	if cfg.useIndexGatewayRing {
		ringStore, err := kv.NewClient(
			cfg.IndexGatewayRing.KVStore,
			ring.GetCodec(),
			kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), "index-gateway"),
			log,
		)
		if err != nil {
			return nil, errors.Wrap(err, "create KV store client")
		}

		lifecyclerCfg, err := cfg.IndexGatewayRing.ToLifecyclerConfig(ringNumTokens, log)
		if err != nil {
			return nil, errors.Wrap(err, "invalid ring lifecycler config")
		}

		delegate := ring.BasicLifecyclerDelegate(g)
		delegate = ring.NewLeaveOnStoppingDelegate(delegate, log)
		delegate = ring.NewTokensPersistencyDelegate(cfg.IndexGatewayRing.TokensFilePath, ring.JOINING, delegate, log)
		delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*cfg.IndexGatewayRing.HeartbeatTimeout, delegate, log)

		g.ringLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, ringKey, ringStore, delegate, log, registerer)
		if err != nil {
			return nil, errors.Wrap(err, "create ring lifecycler")
		}

		ringCfg := cfg.IndexGatewayRing.ToRingConfig(ringReplicationFactor)
		g.ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, ringNameForServer, ringKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix("loki_", registerer), log)
		if err != nil {
			return nil, errors.Wrap(err, "create ring client")
		}

		svcs := []services.Service{g.ringLifecycler, g.ring}
		g.subservices, err = services.NewManager(svcs...)
		if err != nil {
			return nil, err
		}
		g.subservicesWatcher = services.NewFailureWatcher()
		g.subservicesWatcher.WatchManager(g.subservices)
	}

	return g, nil
}

func (g *Gateway) QueryIndex(request *indexgatewaypb.QueryIndexRequest, server indexgatewaypb.IndexGateway_QueryIndexServer) error {
	var outerErr error
	var innerErr error

	queries := make([]chunk.IndexQuery, 0, len(request.Queries))
	for _, query := range request.Queries {
		queries = append(queries, chunk.IndexQuery{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		})
	}

	sendBatchMtx := sync.Mutex{}
	outerErr = g.indexQuerier.QueryPages(server.Context(), queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		innerErr = buildResponses(query, batch, func(response *indexgatewaypb.QueryIndexResponse) error {
			// do not send grpc responses concurrently. See https://github.com/grpc/grpc-go/blob/master/stream.go#L120-L123.
			sendBatchMtx.Lock()
			defer sendBatchMtx.Unlock()

			return server.Send(response)
		})

		if innerErr != nil {
			return false
		}

		return true
	})

	if innerErr != nil {
		return innerErr
	}

	return outerErr
}

func buildResponses(query chunk.IndexQuery, batch chunk.ReadBatch, callback func(*indexgatewaypb.QueryIndexResponse) error) error {
	itr := batch.Iterator()
	var resp []*indexgatewaypb.Row

	for itr.Next() {
		if len(resp) == maxIndexEntriesPerResponse {
			err := callback(&indexgatewaypb.QueryIndexResponse{
				QueryKey: util.QueryKey(query),
				Rows:     resp,
			})
			if err != nil {
				return err
			}
			resp = []*indexgatewaypb.Row{}
		}

		resp = append(resp, &indexgatewaypb.Row{
			RangeValue: itr.RangeValue(),
			Value:      itr.Value(),
		})
	}

	if len(resp) != 0 {
		err := callback(&indexgatewaypb.QueryIndexResponse{
			QueryKey: util.QueryKey(query),
			Rows:     resp,
		})
		if err != nil {
			return err
		}
	}

	return nil
}
