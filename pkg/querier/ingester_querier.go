package querier

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/gogo/status"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/httpgrpc"
	"google.golang.org/grpc/codes"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	index_stats "github.com/grafana/loki/pkg/storage/stores/index/stats"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type IngesterStats struct {
	TotalIngestersReached                 int
	TotalIngestersReachedNonEmptyResponse int
	MaxNonEmptyIngesterLatency            time.Duration
	TotalIngestersLatency                 time.Duration
	QuerySuccess                          bool
}

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
		pool:            clientpool.NewPool(clientCfg.PoolConfig, ring, clientFactory, util_log.Logger),
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
func (q *IngesterQuerier) forAllIngesters(ctx context.Context, f func(context.Context, logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, IngesterStats, error) {
	replicationSet, err := q.ring.GetReplicationSetForOperation(ring.Read)
	if err != nil {
		return nil, IngesterStats{}, err
	}
	level.Info(util_log.Logger).Log("method", "for_all_ingesters", "count", len(replicationSet.Instances))
	return q.forGivenIngesters(ctx, replicationSet, f)
}

// forGivenIngesters runs f, in parallel, for given ingesters
// TODO taken from Cortex, see if we can refactor out an usable interface.
func (q *IngesterQuerier) forGivenIngesters(ctx context.Context, replicationSet ring.ReplicationSet, f func(context.Context, logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, IngesterStats, error) {
	start := time.Now()

	var (
		nonEmptyResponseCount         int
		maxLatencyForNonEmptyResponse time.Duration
	)

	results, err := replicationSet.Do(ctx, q.extraQueryDelay, func(ctx context.Context, ingester *ring.InstanceDesc) (interface{}, error) {
		istart := time.Now()

		client, err := q.pool.GetClientFor(ingester.Addr)
		if err != nil {
			return nil, err
		}

		resp, err := f(ctx, client.(logproto.QuerierClient))
		if err != nil {
			return nil, err
		}

		// TODO(kavi): This may not be the right way to identify if ingester response is empty.
		// This is bit tricky as they return only iterator and we may not know if it's empty response until iterator is consumed.
		// Two ways to solve the problem at the worst case
		// 1. Add an additional flag in the `ingesterRespoinse` that says if response is empty
		// 2. Move the stats calculation part close to where iterator is consumed.
		// There may be a caveat with (1) as flag may say response is empty but during iterator consuption, it may have some data.
		if resp != nil {
			nonEmptyResponseCount++
			elapsed := time.Since(istart)
			if elapsed > maxLatencyForNonEmptyResponse {
				maxLatencyForNonEmptyResponse = elapsed
			}
		}

		return responseFromIngesters{ingester.Addr, resp}, nil
	})
	if err != nil {
		return nil, IngesterStats{}, err
	}

	responses := make([]responseFromIngesters, 0, len(results))
	for _, result := range results {
		r := result.(responseFromIngesters)
		responses = append(responses, r)
		if r.response == nil {
			level.Info(util_log.Logger).Log("component", "ingester_querier", "event", "received nil response")
		}
	}

	// update ingester stats
	stats := IngesterStats{
		TotalIngestersReached:                 len(responses),
		TotalIngestersLatency:                 time.Since(start),
		TotalIngestersReachedNonEmptyResponse: nonEmptyResponseCount,
		MaxNonEmptyIngesterLatency:            maxLatencyForNonEmptyResponse,
	}

	return responses, stats, err
}

func (q *IngesterQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) ([]iter.EntryIterator, error) {
	resps, ingesterStats, err := q.forAllIngesters(ctx, func(_ context.Context, client logproto.QuerierClient) (interface{}, error) {
		stats.FromContext(ctx).AddIngesterReached(1)
		return client.Query(ctx, params.QueryRequest)
	})
	if err != nil {
		return nil, err
	}

	iterators := make([]iter.EntryIterator, len(resps))
	for i := range resps {
		iterators[i] = iter.NewQueryClientIterator(resps[i].response.(logproto.Querier_QueryClient), params.Direction)
	}
	logIngesterStats(ingesterStats, "select_logs")
	return iterators, nil
}

func (q *IngesterQuerier) SelectSample(ctx context.Context, params logql.SelectSampleParams) ([]iter.SampleIterator, error) {
	resps, ingesterStats, err := q.forAllIngesters(ctx, func(_ context.Context, client logproto.QuerierClient) (interface{}, error) {
		stats.FromContext(ctx).AddIngesterReached(1)
		return client.QuerySample(ctx, params.SampleQueryRequest)
	})
	if err != nil {
		return nil, err
	}

	iterators := make([]iter.SampleIterator, len(resps))
	for i := range resps {
		iterators[i] = iter.NewSampleQueryClientIterator(resps[i].response.(logproto.Querier_QuerySampleClient))
	}
	logIngesterStats(ingesterStats, "select_sample")
	return iterators, nil
}

func (q *IngesterQuerier) Label(ctx context.Context, req *logproto.LabelRequest) ([][]string, error) {
	resps, _, err := q.forAllIngesters(ctx, func(ctx context.Context, client logproto.QuerierClient) (interface{}, error) {
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
	resps, _, err := q.forAllIngesters(ctx, func(_ context.Context, client logproto.QuerierClient) (interface{}, error) {
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
	reconnectClients, _, err := q.forGivenIngesters(ctx, ring.ReplicationSet{Instances: reconnectIngesters}, func(_ context.Context, client logproto.QuerierClient) (interface{}, error) {
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
	resps, _, err := q.forAllIngesters(ctx, func(ctx context.Context, client logproto.QuerierClient) (interface{}, error) {
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

	responses, _, err := q.forGivenIngesters(ctx, replicationSet, func(ctx context.Context, querierClient logproto.QuerierClient) (interface{}, error) {
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
	resps, _, err := q.forAllIngesters(ctx, func(ctx context.Context, querierClient logproto.QuerierClient) (interface{}, error) {
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

func (q *IngesterQuerier) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*index_stats.Stats, error) {
	resps, _, err := q.forAllIngesters(ctx, func(ctx context.Context, querierClient logproto.QuerierClient) (interface{}, error) {
		return querierClient.GetStats(ctx, &logproto.IndexStatsRequest{
			From:     from,
			Through:  through,
			Matchers: syntax.MatchersString(matchers),
		})
	})

	if err != nil {
		if isUnimplementedCallError(err) {
			// Handle communication with older ingesters gracefully
			return &index_stats.Stats{}, nil
		}
		return nil, err
	}

	casted := make([]*index_stats.Stats, 0, len(resps))
	for _, resp := range resps {
		casted = append(casted, resp.response.(*index_stats.Stats))
	}

	merged := index_stats.MergeStats(casted...)
	return &merged, nil
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

// isUnimplementedCallError tells if the GRPC error is a gRPC error with code Unimplemented.
func isUnimplementedCallError(err error) bool {
	if err == nil {
		return false
	}

	s, ok := status.FromError(err)
	if !ok {
		return false
	}
	return (s.Code() == codes.Unimplemented)
}

func logIngesterStats(istats IngesterStats, queryType string) {
	level.Info(util_log.Logger).Log(
		"query_type", queryType,
		"total_ingesters_reached", istats.TotalIngestersReached,
		"total_ingesters_reached_non_empty_response", istats.TotalIngestersReachedNonEmptyResponse,
		"total_ingesters_latency", istats.TotalIngestersLatency,
		"max_non_empty_response_ingester_latency", istats.MaxNonEmptyIngesterLatency,
	)
}
