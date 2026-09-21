package querier

import (
	"context"
	"crypto/rand"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/user"

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

var defaultQuorumConfig = ring.DoUntilQuorumConfig{
	// Nothing here
}

type responseFromIngesters struct {
	addr     string
	response interface{}
}

// IngesterQuerier helps with querying the ingesters.
type IngesterQuerier struct {
	querierConfig          Config
	ring                   ring.ReadRing
	partitionRing          *ring.PartitionInstanceRing
	getShardCountForTenant func(string) int
	pool                   *ring_client.Pool
	logger                 log.Logger

	// Zone preferences are evaluated on every query, so only warn once when they
	// cannot be honoured.
	warnZoneAwarenessDisabled sync.Once
	warnTooManyZones          sync.Once
}

func NewIngesterQuerier(querierConfig Config, clientCfg client.Config, ring ring.ReadRing, partitionRing *ring.PartitionInstanceRing, getShardCountForTenant func(string) int, metricsNamespace string, logger log.Logger) (*IngesterQuerier, error) {
	factory := func(addr string) (ring_client.PoolClient, error) {
		return client.New(clientCfg, addr)
	}

	return newIngesterQuerier(querierConfig, clientCfg, ring, partitionRing, getShardCountForTenant, ring_client.PoolAddrFunc(factory), metricsNamespace, logger)
}

// newIngesterQuerier creates a new IngesterQuerier and allows to pass a custom ingester client factory
// used for testing purposes
func newIngesterQuerier(querierConfig Config, clientCfg client.Config, ring ring.ReadRing, partitionRing *ring.PartitionInstanceRing, getShardCountForTenant func(string) int, clientFactory ring_client.PoolFactory, metricsNamespace string, logger log.Logger) (*IngesterQuerier, error) {
	iq := IngesterQuerier{
		querierConfig:          querierConfig,
		ring:                   ring,
		partitionRing:          partitionRing,
		getShardCountForTenant: getShardCountForTenant, // limits?
		pool:                   clientpool.NewPool("ingester", clientCfg.PoolConfig, ring, clientFactory, util_log.Logger, metricsNamespace),
		logger:                 logger,
	}

	err := services.StartAndAwaitRunning(context.Background(), iq.pool)
	if err != nil {
		return nil, errors.Wrap(err, "querier pool")
	}

	return &iq, nil
}

type ctxKeyType string

const (
	partitionCtxKey ctxKeyType = "partitionCtx"
)

type PartitionContext struct {
	isPartitioned bool
	ingestersUsed map[string]PartitionIngesterUsed
	mtx           sync.Mutex
}

type PartitionIngesterUsed struct {
	client logproto.QuerierClient
	addr   string
}

func (p *PartitionContext) AddClient(client logproto.QuerierClient, addr string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	if !p.isPartitioned {
		return
	}
	p.ingestersUsed[addr] = PartitionIngesterUsed{client: client, addr: addr}
}

func (p *PartitionContext) RemoveClient(addr string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	if !p.isPartitioned {
		return
	}
	delete(p.ingestersUsed, addr)
}

func (p *PartitionContext) SetIsPartitioned(isPartitioned bool) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.isPartitioned = isPartitioned
}

func (p *PartitionContext) IsPartitioned() bool {
	return p.isPartitioned
}

func (p *PartitionContext) forQueriedIngesters(ctx context.Context, f func(context.Context, logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	ingestersUsed := make([]PartitionIngesterUsed, 0, len(p.ingestersUsed))
	for _, ingester := range p.ingestersUsed {
		ingestersUsed = append(ingestersUsed, ingester)
	}

	return concurrency.ForEachJobMergeResults(ctx, ingestersUsed, 0, func(ctx context.Context, job PartitionIngesterUsed) ([]responseFromIngesters, error) {
		resp, err := f(ctx, job.client)
		if err != nil {
			return nil, err
		}
		return []responseFromIngesters{{addr: job.addr, response: resp}}, nil
	})
}

// NewPartitionContext creates a new partition context
// This is used to track which ingesters were used in the query and reuse the same ingesters for consecutive queries
func NewPartitionContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, partitionCtxKey, &PartitionContext{
		ingestersUsed: make(map[string]PartitionIngesterUsed),
	})
}

func ExtractPartitionContext(ctx context.Context) *PartitionContext {
	v, ok := ctx.Value(partitionCtxKey).(*PartitionContext)
	if !ok {
		return &PartitionContext{
			ingestersUsed: make(map[string]PartitionIngesterUsed),
		}
	}
	return v
}

// preferredZoneSorter returns a ring.ZoneSorter that moves preferredZones to the
// front of the list, and randomizes the order of the remaining zones. All
// preferred zones are given equal priority. Randomizing the rest spreads load
// evenly across the zones we fall back to.
func preferredZoneSorter(preferredZones []string) ring.ZoneSorter {
	return func(zones []string) []string {
		shuffleZones(zones)

		if len(preferredZones) == 0 {
			return zones
		}

		// Move the preferred zones to the front. They have already been shuffled,
		// so they keep equal priority relative to each other.
		nextPos := 0
		for idx, zone := range zones {
			if slices.Contains(preferredZones, zone) {
				zones[nextPos], zones[idx] = zones[idx], zones[nextPos]
				nextPos++
			}
		}

		return zones
	}
}

// shuffleZones shuffles zones in place, using crypto/rand as the source of
// randomness. If no randomness is available the current order is kept: the order
// zones are queried in must never be a reason to fail a query.
func shuffleZones(zones []string) {
	for i := len(zones) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return
		}

		j := int(n.Int64())
		zones[i], zones[j] = zones[j], zones[i]
	}
}

// countZones returns the number of distinct zones the given instances belong to.
func countZones(instances []ring.InstanceDesc) int {
	zones := make([]string, 0, 3)
	for _, instance := range instances {
		if !slices.Contains(zones, instance.Zone) {
			zones = append(zones, instance.Zone)
		}
	}
	return len(zones)
}

// zoneReadsEnabled returns whether the querier is configured to prefer or restrict
// the zones it reads from.
func (q *IngesterQuerier) zoneReadsEnabled() bool {
	return len(q.querierConfig.PreferAvailabilityZones) > 0 || q.querierConfig.IngesterQueryZones > 0
}

// quorumConfigForZoneReads returns the config to use when querying the ingester
// ring, honouring querier.prefer-availability-zones and
// querier.ingester-query-zones. replicationSet is modified in place when fewer
// zones should be queried than the ring itself requires for quorum.
func (q *IngesterQuerier) quorumConfigForZoneReads(replicationSet *ring.ReplicationSet) ring.DoUntilQuorumConfig {
	if !q.zoneReadsEnabled() {
		return defaultQuorumConfig
	}

	// Without zone awareness the ring gives no guarantee about how replicas are
	// spread across zones, and instances may not report a zone at all, so there is
	// nothing meaningful to prefer or restrict.
	if !replicationSet.ZoneAwarenessEnabled {
		q.warnZoneAwarenessDisabled.Do(func() {
			level.Warn(q.logger).Log("msg", "ignoring querier availability zone preferences because zone awareness is disabled on the ingester ring")
		})
		return defaultQuorumConfig
	}

	if n := q.querierConfig.IngesterQueryZones; n > 0 {
		// The completeness check below must use the number of zones registered in
		// the ring, not the number left in the replication set. Zone-aware rings
		// drop every instance of a zone that has any unhealthy instance, so a ring
		// with more zones than replicas can present a replication set with few
		// enough zones to look like it qualifies, when in fact no single zone holds
		// a complete copy of the data.
		ringZones := q.ring.ZonesCount()

		// The quorum requirement is relative to the zones actually present in the
		// replication set, since that is what dskit counts when deciding how many
		// zones must succeed.
		setZones := countZones(replicationSet.Instances)

		switch {
		case ringZones > q.ring.ReplicationFactor():
			// Querying fewer zones than the ring requires for quorum is only correct
			// if every zone holds a complete copy of the data. Zone-aware replication
			// places at most one replica per zone, so that only holds when there are
			// no more zones than replicas.
			q.warnTooManyZones.Do(func() {
				level.Warn(q.logger).Log(
					"msg", "ignoring querier.ingester-query-zones because the ingester ring has more zones than the replication factor, so a single zone does not hold a complete copy of the data",
					"zones", ringZones,
					"replication_factor", q.ring.ReplicationFactor(),
				)
			})
		case setZones-n > replicationSet.MaxUnavailableZones:
			// Only ever loosen the ring's quorum requirement. Asking for more zones
			// than the ring requires must not make queries more likely to fail.
			replicationSet.MaxUnavailableZones = setZones - n
		}
	}

	return ring.DoUntilQuorumConfig{
		MinimizeRequests: true,
		ZoneSorter:       preferredZoneSorter(q.querierConfig.PreferAvailabilityZones),
	}
}

// forAllIngesters runs f, in parallel, for all ingesters
func (q *IngesterQuerier) forAllIngesters(ctx context.Context, f func(context.Context, logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	if q.querierConfig.QueryPartitionIngesters {
		ExtractPartitionContext(ctx).SetIsPartitioned(true)
		tenantID, err := user.ExtractOrgID(ctx)
		if err != nil {
			return nil, err
		}
		shardSize := q.getShardCountForTenant(tenantID)
		// When the tenant has no configured shard size, share a single subring
		// across all such tenants by using a fixed identifier. The resulting set
		// of partitions is tenant-independent when size == 0, so per-tenant
		// shuffle sharding would just waste CPU and cache memory recomputing the
		// same answer.
		shuffleShardIdentifier := tenantID
		if shardSize == 0 {
			shuffleShardIdentifier = ""
		}
		subring, err := q.partitionRing.ShuffleShardWithLookback(shuffleShardIdentifier, shardSize, q.querierConfig.QueryIngestersWithin, time.Now())
		if err != nil {
			return nil, err
		}
		replicationSets, err := subring.GetReplicationSetsForOperation(ring.Read)
		if err != nil {
			return nil, err
		}
		return q.forGivenIngesterSets(ctx, replicationSets, f)
	}

	replicationSet, err := q.ring.GetReplicationSetForOperation(ring.Read)
	if err != nil {
		return nil, err
	}

	// Must be called before replicationSet is passed by value below, as it may
	// lower the number of zones required to answer the query.
	quorumConfig := q.quorumConfigForZoneReads(&replicationSet)

	return q.forGivenIngesters(ctx, replicationSet, quorumConfig, f)
}

// forGivenIngesterSets runs f, in parallel, for given ingester sets
func (q *IngesterQuerier) forGivenIngesterSets(ctx context.Context, replicationSet []ring.ReplicationSet, f func(context.Context, logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	// Enable minimize requests if we can, so we initially query a single ingester per replication set, as each replication-set is one partition.
	// Ingesters must supply zone information for this to have an effect.
	config := ring.DoUntilQuorumConfig{
		MinimizeRequests: true,
	}
	return concurrency.ForEachJobMergeResults[ring.ReplicationSet, responseFromIngesters](ctx, replicationSet, 0, func(ctx context.Context, set ring.ReplicationSet) ([]responseFromIngesters, error) {
		return q.forGivenIngesters(ctx, set, config, f)
	})
}

// forGivenIngesters runs f, in parallel, for given ingesters until a quorum of responses are received
func (q *IngesterQuerier) forGivenIngesters(ctx context.Context, replicationSet ring.ReplicationSet, quorumConfig ring.DoUntilQuorumConfig, f func(context.Context, logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	results, err := ring.DoUntilQuorum(ctx, replicationSet, quorumConfig, func(ctx context.Context, ingester *ring.InstanceDesc) (responseFromIngesters, error) {
		client, err := q.pool.GetClientFor(ingester.Addr)
		if err != nil {
			return responseFromIngesters{addr: ingester.Addr}, err
		}
		resp, err := f(ctx, client.(logproto.QuerierClient))
		if err != nil {
			return responseFromIngesters{addr: ingester.Addr}, err
		}

		ExtractPartitionContext(ctx).AddClient(client.(logproto.QuerierClient), ingester.Addr)
		return responseFromIngesters{ingester.Addr, resp}, nil
	}, func(cleanup responseFromIngesters) {
		ExtractPartitionContext(ctx).RemoveClient(cleanup.addr)
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
		iterators[i] = iter.NewTimestampFirstSampleQueryClientIterator(resps[i].response.(logproto.Querier_QuerySampleClient))
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

	// When reads are restricted to a subset of zones, only reconnect to ingesters in
	// the zones we would query. Otherwise a long lived tail request would gradually
	// connect to every ingester in the ring, defeating the zone restriction. Zones
	// that already have a connected ingester are also allowed, so a zone we failed
	// over to is not dropped again.
	//
	// Only zones present in the replication set are considered: a preferred zone
	// with no healthy ingester must not keep the tail from reconnecting to the zones
	// that do have one. If that leaves no allowed zone, every zone is considered, so
	// tailing never stops reconnecting.
	var allowedZones []string
	if q.zoneReadsEnabled() && replicationSet.ZoneAwarenessEnabled {
		for _, ingester := range replicationSet.Instances {
			allowed := connected[ingester.Addr] || slices.Contains(q.querierConfig.PreferAvailabilityZones, ingester.Zone)
			if allowed && !slices.Contains(allowedZones, ingester.Zone) {
				allowedZones = append(allowedZones, ingester.Zone)
			}
		}
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

		if len(allowedZones) > 0 && !slices.Contains(allowedZones, ingester.Zone) {
			continue
		}

		reconnectIngesters = append(reconnectIngesters, ingester)
	}

	if len(reconnectIngesters) == 0 {
		return nil, nil
	}

	// Instance a tail client for each ingester to re(connect)
	reconnectClients, err := q.forGivenIngesters(ctx, ring.ReplicationSet{Instances: reconnectIngesters}, defaultQuorumConfig, func(_ context.Context, client logproto.QuerierClient) (interface{}, error) {
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

	responses, err := q.forGivenIngesters(ctx, replicationSet, defaultQuorumConfig, func(ctx context.Context, querierClient logproto.QuerierClient) (interface{}, error) {
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
	ingesterQueryFn := q.forAllIngesters

	partitionCtx := ExtractPartitionContext(ctx)
	if partitionCtx.IsPartitioned() {
		// We need to query the same ingesters as the previous query
		ingesterQueryFn = partitionCtx.forQueriedIngesters
	}

	resps, err := ingesterQueryFn(ctx, func(ctx context.Context, querierClient logproto.QuerierClient) (interface{}, error) {
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
