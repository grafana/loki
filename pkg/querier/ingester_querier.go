package querier

import (
	"context"
	"net/http"
	"strings"
	"time"

	cortex_distributor "github.com/cortexproject/cortex/pkg/distributor"
	"github.com/cortexproject/cortex/pkg/ring"
	ring_client "github.com/cortexproject/cortex/pkg/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type responseFromIngesters struct {
	addr     string
	response interface{}
}

// IngesterQuerier helps with querying the ingesters.
type IngesterQuerier struct {
	ring            ring.ReadRing
	pool            *ring_client.Pool
	extraQueryDelay time.Duration
}

func NewIngesterQuerier(clientCfg client.Config, ring ring.ReadRing, extraQueryDelay time.Duration) (*IngesterQuerier, error) {
	factory := func(addr string) (ring_client.PoolClient, error) {
		return client.New(clientCfg, addr)
	}

	return newIngesterQuerier(clientCfg, ring, extraQueryDelay, factory)
}

// newIngesterQuerier creates a new IngesterQuerier and allows to pass a custom ingester client factory
// used for testing purposes
func newIngesterQuerier(clientCfg client.Config, ring ring.ReadRing, extraQueryDelay time.Duration, clientFactory ring_client.PoolFactory) (*IngesterQuerier, error) {
	iq := IngesterQuerier{
		ring:            ring,
		pool:            cortex_distributor.NewPool(clientCfg.PoolConfig, ring, clientFactory, util_log.Logger),
		extraQueryDelay: extraQueryDelay,
	}

	err := services.StartAndAwaitRunning(context.Background(), iq.pool)
	if err != nil {
		return nil, errors.Wrap(err, "querier pool")
	}

	return &iq, nil
}

// forAllIngesters runs f, in parallel, for all ingesters
// TODO taken from Cortex, see if we can refactor out an usable interface.
func (q *IngesterQuerier) forAllIngesters(ctx context.Context, f func(logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	replicationSet, err := q.ring.GetReplicationSetForOperation(ring.Read)
	if err != nil {
		return nil, err
	}

	return q.forGivenIngesters(ctx, replicationSet, f)
}

// forGivenIngesters runs f, in parallel, for given ingesters
// TODO taken from Cortex, see if we can refactor out an usable interface.
func (q *IngesterQuerier) forGivenIngesters(ctx context.Context, replicationSet ring.ReplicationSet, f func(logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	results, err := replicationSet.Do(ctx, q.extraQueryDelay, func(ctx context.Context, ingester *ring.InstanceDesc) (interface{}, error) {
		client, err := q.pool.GetClientFor(ingester.Addr)
		if err != nil {
			return nil, err
		}

		resp, err := f(client.(logproto.QuerierClient))
		if err != nil {
			return nil, err
		}

		return responseFromIngesters{ingester.Addr, resp}, nil
	})
	if err != nil {
		return nil, err
	}

	responses := make([]responseFromIngesters, 0, len(results))
	for _, result := range results {
		responses = append(responses, result.(responseFromIngesters))
	}

	return responses, err
}

func (q *IngesterQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) ([]iter.EntryIterator, error) {
	resps, err := q.forAllIngesters(ctx, func(client logproto.QuerierClient) (interface{}, error) {
		return client.Query(ctx, params.QueryRequest, stats.CollectTrailer(ctx))
	})
	if err != nil {
		return nil, err
	}

	iterators := make([]iter.EntryIterator, len(resps))
	for i := range resps {
		iterators[i] = iter.NewQueryClientIterator(resps[i].response.(logproto.Querier_QueryClient), params.Direction)
	}
	return iterators, nil
}

func (q *IngesterQuerier) SelectSample(ctx context.Context, params logql.SelectSampleParams) ([]iter.SampleIterator, error) {
	resps, err := q.forAllIngesters(ctx, func(client logproto.QuerierClient) (interface{}, error) {
		return client.QuerySample(ctx, params.SampleQueryRequest, stats.CollectTrailer(ctx))
	})
	if err != nil {
		return nil, err
	}

	iterators := make([]iter.SampleIterator, len(resps))
	for i := range resps {
		iterators[i] = iter.NewSampleQueryClientIterator(resps[i].response.(logproto.Querier_QuerySampleClient))
	}
	return iterators, nil
}

func (q *IngesterQuerier) Label(ctx context.Context, req *logproto.LabelRequest) ([][]string, error) {
	resps, err := q.forAllIngesters(ctx, func(client logproto.QuerierClient) (interface{}, error) {
		return client.Label(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	results := make([][]string, 0, len(resps))
	for _, resp := range resps {
		results = append(results, resp.response.(*logproto.LabelResponse).Values)
	}

	return results, nil
}

func (q *IngesterQuerier) Tail(ctx context.Context, req *logproto.TailRequest) (map[string]logproto.Querier_TailClient, error) {
	resps, err := q.forAllIngesters(ctx, func(client logproto.QuerierClient) (interface{}, error) {
		return client.Tail(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	tailClients := make(map[string]logproto.Querier_TailClient)
	for i := range resps {
		tailClients[resps[i].addr] = resps[i].response.(logproto.Querier_TailClient)
	}

	return tailClients, nil
}

func (q *IngesterQuerier) TailDisconnectedIngesters(ctx context.Context, req *logproto.TailRequest, connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
	// Build a map to easily check if an ingester address is already connected
	connected := make(map[string]bool)
	for _, addr := range connectedIngestersAddr {
		connected[addr] = true
	}

	// Get the current replication set from the ring
	replicationSet, err := q.ring.GetReplicationSetForOperation(ring.Read)
	if err != nil {
		return nil, err
	}

	// Look for disconnected ingesters or new one we should (re)connect to
	reconnectIngesters := []ring.InstanceDesc{}

	for _, ingester := range replicationSet.Instances {
		if _, ok := connected[ingester.Addr]; ok {
			continue
		}

		// Skip ingesters which are leaving or joining the cluster
		if ingester.State != ring.ACTIVE {
			continue
		}

		reconnectIngesters = append(reconnectIngesters, ingester)
	}

	if len(reconnectIngesters) == 0 {
		return nil, nil
	}

	// Instance a tail client for each ingester to re(connect)
	reconnectClients, err := q.forGivenIngesters(ctx, ring.ReplicationSet{Instances: reconnectIngesters}, func(client logproto.QuerierClient) (interface{}, error) {
		return client.Tail(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	reconnectClientsMap := make(map[string]logproto.Querier_TailClient)
	for _, client := range reconnectClients {
		reconnectClientsMap[client.addr] = client.response.(logproto.Querier_TailClient)
	}

	return reconnectClientsMap, nil
}

func (q *IngesterQuerier) Series(ctx context.Context, req *logproto.SeriesRequest) ([][]logproto.SeriesIdentifier, error) {
	resps, err := q.forAllIngesters(ctx, func(client logproto.QuerierClient) (interface{}, error) {
		return client.Series(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	var acc [][]logproto.SeriesIdentifier
	for _, resp := range resps {
		acc = append(acc, resp.response.(*logproto.SeriesResponse).Series)
	}

	return acc, nil
}

func (q *IngesterQuerier) TailersCount(ctx context.Context) ([]uint32, error) {
	replicationSet, err := q.ring.GetAllHealthy(ring.Read)
	if err != nil {
		return nil, err
	}

	// we want to check count of active tailers with only active ingesters
	ingesters := make([]ring.InstanceDesc, 0, 1)
	for i := range replicationSet.Instances {
		if replicationSet.Instances[i].State == ring.ACTIVE {
			ingesters = append(ingesters, replicationSet.Instances[i])
		}
	}

	if len(ingesters) == 0 {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "no active ingester found")
	}

	responses, err := q.forGivenIngesters(ctx, replicationSet, func(querierClient logproto.QuerierClient) (interface{}, error) {
		resp, err := querierClient.TailersCount(ctx, &logproto.TailersCountRequest{})
		if err != nil {
			return nil, err
		}
		return resp.Count, nil
	})
	// We are only checking active ingesters, and any error returned stops checking other ingesters
	// so return that error here as well.
	if err != nil {
		return nil, err
	}

	counts := make([]uint32, len(responses))

	for _, resp := range responses {
		counts = append(counts, resp.response.(uint32))
	}

	return counts, nil
}

func (q *IngesterQuerier) GetChunkIDs(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	resps, err := q.forAllIngesters(ctx, func(querierClient logproto.QuerierClient) (interface{}, error) {
		return querierClient.GetChunkIDs(ctx, &logproto.GetChunkIDsRequest{
			Matchers: convertMatchersToString(matchers),
			Start:    from.Time(),
			End:      through.Time(),
		})
	})

	if err != nil {
		return nil, err
	}

	var chunkIDs []string
	for i := range resps {
		chunkIDs = append(chunkIDs, resps[i].response.(*logproto.GetChunkIDsResponse).ChunkIDs...)
	}

	return chunkIDs, nil
}

func convertMatchersToString(matchers []*labels.Matcher) string {
	out := strings.Builder{}
	out.WriteRune('{')

	for idx, m := range matchers {
		if idx > 0 {
			out.WriteRune(',')
		}

		out.WriteString(m.String())
	}

	out.WriteRune('}')
	return out.String()
}
