package indexgateway

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
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
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	maxIndexEntriesPerResponse     = 1000
	ringAutoForgetUnhealthyPeriods = 10
	ringNameForServer              = "index-gateway"
	ringNumTokens                  = 1
	ringCheckPeriod                = 3 * time.Second

	RingIdentifier = "index-gateway"

	RingKey = "index-gateway"

	RingReplicationFactor = 3
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
	UseIndexGatewayRing bool                `yaml:"use_index_gateway_ring,omitempty"` // TODO: maybe just `yaml:"useRing"`?
	IndexGatewayRing    lokiutil.RingConfig `yaml:"index_gateway_ring,omitempty"`     // TODO: maybe just `yaml:"ring"`?
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.UseIndexGatewayRing, "index-gateway.use-index-gateway-ring", false, "Set to true to enable per-tenant hashing to Index Gateways via a ring. This helps with horizontal scalability by reducing startup time required when provisioning a new Index Gateway")
	cfg.IndexGatewayRing.RegisterFlagsWithPrefix("index-gateway.", "collectors/", f)
}

func (g *Gateway) starting(ctx context.Context) (err error) {
	// In case this function will return error we want to unregister the instance
	// from the ring. We do it ensuring dependencies are gracefully stopped if they
	// were already started.
	defer func() {
		if err == nil || g.subservices == nil {
			return
		}

		level.Error(util_log.Logger).Log("msg", "index gateway error:", "err", err)

		if stopErr := services.StopManagerAndAwaitStopped(context.Background(), g.subservices); stopErr != nil {
			level.Error(util_log.Logger).Log("msg", "failed to gracefully stop index gateway dependencies", "err", stopErr)
		}
	}()

	if err := services.StartManagerAndAwaitHealthy(ctx, g.subservices); err != nil {
		return errors.Wrap(err, "unable to start index gateway subservices")
	}

	// The BasicLifecycler does not automatically move state to ACTIVE such that any additional work that
	// someone wants to do can be done before becoming ACTIVE. For the index gateway we don't currently
	// have any additional work so we can become ACTIVE right away.
	// Wait until the ring client detected this instance in the JOINING state to
	// make sure that when we'll run the initial sync we already know the tokens
	// assigned to this instance.
	level.Info(util_log.Logger).Log("msg", "waiting until index gateway is JOINING in the ring")
	if err := ring.WaitInstanceState(ctx, g.ring, g.ringLifecycler.GetInstanceID(), ring.JOINING); err != nil {
		return err
	}
	level.Info(util_log.Logger).Log("msg", "index gateway is JOINING in the ring")

	// Change ring state to ACTIVE
	if err = g.ringLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
		return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
	}

	// Wait until the ring client detected this instance in the ACTIVE state to
	// make sure that when we'll run the loop it won't be detected as a ring
	// topology change.
	level.Info(util_log.Logger).Log("msg", "waiting until index gateway is ACTIVE in the ring")
	if err := ring.WaitInstanceState(ctx, g.ring, g.ringLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
		return err
	}
	level.Info(util_log.Logger).Log("msg", "index gateway is ACTIVE in the ring")

	return nil
}

func (g *Gateway) running(ctx context.Context) error {
	t := time.NewTicker(ringCheckPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-g.subservicesWatcher.Chan():
			return errors.Wrap(err, "running index gateway subservice failed")
		case <-t.C:
			continue
		}
	}
}

func (g *Gateway) stopping(_ error) error {
	level.Debug(util_log.Logger).Log("msg", "stopping index gateway")
	g.shipper.Stop()
	return services.StopManagerAndAwaitStopped(context.Background(), g.subservices)
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

	if cfg.UseIndexGatewayRing {
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

		g.ringLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, RingKey, ringStore, delegate, log, registerer)
		if err != nil {
			return nil, errors.Wrap(err, "index gateway create ring lifecycler")
		}

		ringCfg := cfg.IndexGatewayRing.ToRingConfig(RingReplicationFactor)
		g.ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, ringNameForServer, RingKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix("loki_", registerer), log)
		if err != nil {
			return nil, errors.Wrap(err, "index gateway create ring client")
		}

		svcs := []services.Service{g.ringLifecycler, g.ring}
		g.subservices, err = services.NewManager(svcs...)
		if err != nil {
			return nil, fmt.Errorf("new index gateway services manager: %w", err)
		}

		g.subservicesWatcher = services.NewFailureWatcher()
		g.subservicesWatcher.WatchManager(g.subservices)
		g.Service = services.NewBasicService(g.starting, g.running, g.stopping)
	} else {
		g.Service = services.NewIdleService(nil, func(failureCase error) error {
			g.shipper.Stop()
			return nil
		})
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

func (g *Gateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if g.cfg.UseIndexGatewayRing {
		g.ring.ServeHTTP(w, req)
	} else {
		w.Write([]byte("IndexGateway running with 'useIndexGatewayRing' disabled."))
	}
}
