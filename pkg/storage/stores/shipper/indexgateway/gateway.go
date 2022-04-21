package indexgateway

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	maxIndexEntriesPerResponse     = 1000
	ringAutoForgetUnhealthyPeriods = 10
	ringNameForServer              = "index-gateway"
	ringNumTokens                  = 128
	ringCheckPeriod                = 3 * time.Second

	// RingIdentifier is used as a unique name to register the Index Gateway ring.
	RingIdentifier = "index-gateway"

	// RingKey is the name of the key used to register the different Index Gateway instances in the key-value store.
	RingKey = "index-gateway"
)

type IndexQuerier interface {
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error)
	LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error)
	LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error)
	Stop()
}

type IndexClient interface {
	QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error
	Stop()
}

type Gateway struct {
	services.Service

	indexQuerier IndexQuerier
	indexClient  IndexClient

	cfg Config
	log log.Logger

	shipper IndexQuerier

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	ringLifecycler *ring.BasicLifecycler
	ring           *ring.Ring
}

// NewIndexGateway instantiates a new Index Gateway and start its services.
//
// In case it is configured to be in ring mode, a Basic Service wrapping the ring client is started.
// Otherwise, it starts an Idle Service that doesn't have lifecycle hooks.
func NewIndexGateway(cfg Config, log log.Logger, registerer prometheus.Registerer, indexQuerier IndexQuerier, indexClient IndexClient) (*Gateway, error) {
	g := &Gateway{
		indexQuerier: indexQuerier,
		indexClient:  indexClient,
		cfg:          cfg,
		log:          log,
	}

	if cfg.Mode == RingMode {
		ringStore, err := kv.NewClient(
			cfg.Ring.KVStore,
			ring.GetCodec(),
			kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), "index-gateway"),
			log,
		)
		if err != nil {
			return nil, errors.Wrap(err, "create KV store client")
		}

		lifecyclerCfg, err := cfg.Ring.ToLifecyclerConfig(ringNumTokens, log)
		if err != nil {
			return nil, errors.Wrap(err, "invalid ring lifecycler config")
		}

		delegate := ring.BasicLifecyclerDelegate(g)
		delegate = ring.NewLeaveOnStoppingDelegate(delegate, log)
		delegate = ring.NewTokensPersistencyDelegate(cfg.Ring.TokensFilePath, ring.JOINING, delegate, log)
		delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*cfg.Ring.HeartbeatTimeout, delegate, log)

		g.ringLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, RingKey, ringStore, delegate, log, registerer)
		if err != nil {
			return nil, errors.Wrap(err, "index gateway create ring lifecycler")
		}

		ringCfg := cfg.Ring.ToRingConfig(cfg.Ring.ReplicationFactor)
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
			g.indexQuerier.Stop()
			g.indexClient.Stop()
			return nil
		})
	}

	return g, nil
}

// starting implements the Lifecycler interface and is one of the lifecycle hooks.
//
// Only invoked if the Index Gateway is in ring mode.
func (g *Gateway) starting(ctx context.Context) (err error) {
	// In case this function will return error we want to unregister the instance
	// from the ring. We do it ensuring dependencies are gracefully stopped if they
	// were already started.
	defer func() {
		if err == nil || g.subservices == nil {
			return
		}

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
	// Wait until the ring client detected this instance in the JOINING
	// state to make sure that when we'll run the initial sync we already
	// know the tokens assigned to this instance.
	level.Info(util_log.Logger).Log("msg", "waiting until index gateway is JOINING in the ring")
	if err := ring.WaitInstanceState(ctx, g.ring, g.ringLifecycler.GetInstanceID(), ring.JOINING); err != nil {
		return err
	}
	level.Info(util_log.Logger).Log("msg", "index gateway is JOINING in the ring")

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

// running implements the Lifecycler interface and is one of the lifecycle hooks.
//
// Only invoked if the Index Gateway is in ring mode.
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
			// TODO: should we implement CAS check?
		}
	}
}

// stopping implements the Lifecycler interface and is one of the lifecycle hooks.
//
// Only invoked if the Index Gateway is in ring mode.
func (g *Gateway) stopping(_ error) error {
	level.Debug(util_log.Logger).Log("msg", "stopping index gateway")
	defer func() {
		g.indexQuerier.Stop()
		g.indexClient.Stop()
	}()
	return services.StopManagerAndAwaitStopped(context.Background(), g.subservices)
}

func (g *Gateway) QueryIndex(request *indexgatewaypb.QueryIndexRequest, server indexgatewaypb.IndexGateway_QueryIndexServer) error {
	var outerErr error
	var innerErr error

	queries := make([]index.Query, 0, len(request.Queries))
	for _, query := range request.Queries {
		queries = append(queries, index.Query{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		})
	}

	sendBatchMtx := sync.Mutex{}
	outerErr = g.indexClient.QueryPages(server.Context(), queries, func(query index.Query, batch index.ReadBatchResult) bool {
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

func buildResponses(query index.Query, batch index.ReadBatchResult, callback func(*indexgatewaypb.QueryIndexResponse) error) error {
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

func (g *Gateway) GetChunkRef(ctx context.Context, req *indexgatewaypb.GetChunkRefRequest) (*indexgatewaypb.GetChunkRefResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	matchers, err := syntax.ParseMatchers(req.Matchers)
	if err != nil {
		return nil, err
	}
	chunks, _, err := g.indexQuerier.GetChunkRefs(ctx, instanceID, req.From, req.Through, matchers...)
	if err != nil {
		return nil, err
	}
	result := &indexgatewaypb.GetChunkRefResponse{
		Refs: make([]*logproto.ChunkRef, 0, len(chunks)),
	}
	for _, cs := range chunks {
		for i := range cs {
			result.Refs = append(result.Refs, &cs[i].ChunkRef)
		}
	}
	return result, nil
}

func (g *Gateway) LabelNamesForMetricName(ctx context.Context, req *indexgatewaypb.LabelNamesForMetricNameRequest) (*indexgatewaypb.LabelResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	names, err := g.indexQuerier.LabelNamesForMetricName(ctx, instanceID, req.From, req.Through, req.MetricName)
	if err != nil {
		return nil, err
	}
	return &indexgatewaypb.LabelResponse{
		Values: names,
	}, nil
}

func (g *Gateway) LabelValuesForMetricName(ctx context.Context, req *indexgatewaypb.LabelValuesForMetricNameRequest) (*indexgatewaypb.LabelResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	var matchers []*labels.Matcher
	// An empty matchers string cannot be parsed,
	// therefore we check the string representation of the the matchers.
	if req.Matchers != syntax.EmptyMatchers {
		matchers, err = syntax.ParseMatchers(req.Matchers)
		if err != nil {
			return nil, err
		}
	}
	names, err := g.indexQuerier.LabelValuesForMetricName(ctx, instanceID, req.From, req.Through, req.MetricName, req.LabelName, matchers...)
	if err != nil {
		return nil, err
	}
	return &indexgatewaypb.LabelResponse{
		Values: names,
	}, nil
}

// ServeHTTP serves the HTTP route /indexgateway/ring.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if g.cfg.Mode == RingMode {
		g.ring.ServeHTTP(w, req)
	} else {
		w.Write([]byte("IndexGateway running with 'useIndexGatewayRing' disabled."))
	}
}
