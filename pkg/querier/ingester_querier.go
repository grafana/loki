package querier

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"

	"github.com/gogo/status"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"google.golang.org/grpc/codes"

	"github.com/grafana/loki/v3/pkg/distributor/clientpool"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	index_stats "github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
	logger          log.Logger
}

func NewIngesterQuerier(clientCfg client.Config, ring ring.ReadRing, extraQueryDelay time.Duration, metricsNamespace string, logger log.Logger) (*IngesterQuerier, error) {
	factory := func(addr string) (ring_client.PoolClient, error) {
		return client.New(clientCfg, addr)
	}

	return newIngesterQuerier(clientCfg, ring, extraQueryDelay, ring_client.PoolAddrFunc(factory), metricsNamespace, logger)
}

// newIngesterQuerier creates a new IngesterQuerier and allows to pass a custom ingester client factory
// used for testing purposes
func newIngesterQuerier(clientCfg client.Config, ring ring.ReadRing, extraQueryDelay time.Duration, clientFactory ring_client.PoolFactory, metricsNamespace string, logger log.Logger) (*IngesterQuerier, error) {
	iq := IngesterQuerier{
		ring:            ring,
		pool:            clientpool.NewPool("ingester", clientCfg.PoolConfig, ring, clientFactory, util_log.Logger, metricsNamespace),
		extraQueryDelay: extraQueryDelay,
		logger:          logger,
	}

	err := services.StartAndAwaitRunning(context.Background(), iq.pool)
	if err != nil {
		return nil, errors.Wrap(err, "querier pool")
	}

	return &iq, nil
}

// forAllIngesters runs f, in parallel, for all ingesters
// TODO taken from Cortex, see if we can refactor out an usable interface.
func (q *IngesterQuerier) forAllIngesters(ctx context.Context, f func(context.Context, logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	replicationSet, err := q.ring.GetReplicationSetForOperation(ring.Read)
	if err != nil {
		return nil, err
	}

	return q.forGivenIngesters(ctx, replicationSet, f)
}

// forGivenIngesters runs f, in parallel, for given ingesters
func (q *IngesterQuerier) forGivenIngesters(ctx context.Context, replicationSet ring.ReplicationSet, f func(context.Context, logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	cfg := ring.DoUntilQuorumConfig{
		// Nothing here
	}
	results, err := ring.DoUntilQuorum(ctx, replicationSet, cfg, func(ctx context.Context, ingester *ring.InstanceDesc) (responseFromIngesters, error) {
		client, err := q.pool.GetClientFor(ingester.Addr)
		if err != nil {
			return responseFromIngesters{addr: ingester.Addr}, err
		}

		resp, err := f(ctx, client.(logproto.QuerierClient))
		if err != nil {
			return responseFromIngesters{addr: ingester.Addr}, err
		}

		return responseFromIngesters{ingester.Addr, resp}, nil
	}, func(responseFromIngesters) {
		// Nothing to do
	})

	if err != nil {
		return nil, err
	}

	responses := make([]responseFromIngesters, 0, len(results))
	responses = append(responses, results...)

	return responses, err
}

func (q *IngesterQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) ([]iter.EntryIterator, error) {
	resps, err := q.forAllIngesters(ctx, func(_ context.Context, client logproto.QuerierClient) (interface{}, error) {
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
	return iterators, nil
}

func (q *IngesterQuerier) SelectSample(ctx context.Context, params logql.SelectSampleParams) ([]iter.SampleIterator, error) {
	resps, err := q.forAllIngesters(ctx, func(_ context.Context, client logproto.QuerierClient) (interface{}, error) {
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
	return iterators, nil
}

func (q *IngesterQuerier) Label(ctx context.Context, req *logproto.LabelRequest) ([][]string, error) {
	resps, err := q.forAllIngesters(ctx, func(ctx context.Context, client logproto.QuerierClient) (interface{}, error) {
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
	resps, err := q.forAllIngesters(ctx, func(_ context.Context, client logproto.QuerierClient) (interface{}, error) {
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
	reconnectClients, err := q.forGivenIngesters(ctx, ring.ReplicationSet{Instances: reconnectIngesters}, func(_ context.Context, client logproto.QuerierClient) (interface{}, error) {
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
	resps, err := q.forAllIngesters(ctx, func(ctx context.Context, client logproto.QuerierClient) (interface{}, error) {
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

	responses, err := q.forGivenIngesters(ctx, replicationSet, func(ctx context.Context, querierClient logproto.QuerierClient) (interface{}, error) {
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

	counts := make([]uint32, 0, len(responses))

	for _, resp := range responses {
		counts = append(counts, resp.response.(uint32))
	}

	return counts, nil
}

func (q *IngesterQuerier) GetChunkIDs(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	resps, err := q.forAllIngesters(ctx, func(ctx context.Context, querierClient logproto.QuerierClient) (interface{}, error) {
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

func (q *IngesterQuerier) Stats(ctx context.Context, _ string, from, through model.Time, matchers ...*labels.Matcher) (*index_stats.Stats, error) {
	resps, err := q.forAllIngesters(ctx, func(ctx context.Context, querierClient logproto.QuerierClient) (interface{}, error) {
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

func (q *IngesterQuerier) Volume(ctx context.Context, _ string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	matcherString := "{}"
	if len(matchers) > 0 {
		matcherString = syntax.MatchersString(matchers)
	}

	resps, err := q.forAllIngesters(ctx, func(ctx context.Context, querierClient logproto.QuerierClient) (interface{}, error) {
		return querierClient.GetVolume(ctx, &logproto.VolumeRequest{
			From:         from,
			Through:      through,
			Matchers:     matcherString,
			Limit:        limit,
			TargetLabels: targetLabels,
			AggregateBy:  aggregateBy,
		})
	})

	if err != nil {
		if isUnimplementedCallError(err) {
			// Handle communication with older ingesters gracefully
			return &logproto.VolumeResponse{}, nil
		}
		return nil, err
	}

	casted := make([]*logproto.VolumeResponse, 0, len(resps))
	for _, resp := range resps {
		casted = append(casted, resp.response.(*logproto.VolumeResponse))
	}

	merged := seriesvolume.Merge(casted, limit)
	return merged, nil
}

func (q *IngesterQuerier) DetectedLabel(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.LabelToValuesResponse, error) {
	ingesterResponses, err := q.forAllIngesters(ctx, func(ctx context.Context, client logproto.QuerierClient) (interface{}, error) {
		return client.GetDetectedLabels(ctx, req)
	})

	if err != nil {
		level.Error(q.logger).Log("msg", "error getting detected labels", "err", err)
		return nil, err
	}

	labelMap := make(map[string][]string)
	for _, resp := range ingesterResponses {
		thisIngester, ok := resp.response.(*logproto.LabelToValuesResponse)
		if !ok {
			level.Warn(q.logger).Log("msg", "Cannot convert response to LabelToValuesResponse in detectedlabels",
				"response", resp)
		}

		if thisIngester == nil {
			continue
		}

		for label, thisIngesterValues := range thisIngester.Labels {
			var combinedValues []string
			allIngesterValues, isLabelPresent := labelMap[label]
			if isLabelPresent {
				combinedValues = append(allIngesterValues, thisIngesterValues.Values...)
			} else {
				combinedValues = thisIngesterValues.Values
			}
			labelMap[label] = combinedValues
		}
	}

	// Dedupe all ingester values
	mergedResult := make(map[string]*logproto.UniqueLabelValues)
	for label, val := range labelMap {
		slices.Sort(val)
		uniqueValues := slices.Compact(val)

		mergedResult[label] = &logproto.UniqueLabelValues{
			Values: uniqueValues,
		}
	}

	return &logproto.LabelToValuesResponse{Labels: mergedResult}, nil
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
