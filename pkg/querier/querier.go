package querier

import (
	"context"
	"flag"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/distributor"
	"github.com/cortexproject/cortex/pkg/ring"
	ring_client "github.com/cortexproject/cortex/pkg/ring/client"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
	cortex_validation "github.com/cortexproject/cortex/pkg/util/validation"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
	"github.com/grafana/loki/pkg/storage"
	listutil "github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/validation"
)

const (
	// How long the Tailer should wait - once there are no entries to read from ingesters -
	// before checking if a new entry is available (to avoid spinning the CPU in a continuous
	// check loop)
	tailerWaitEntryThrottle = time.Second / 2
)

// Config for a querier.
type Config struct {
	QueryTimeout                  time.Duration    `yaml:"query_timeout"`
	TailMaxDuration               time.Duration    `yaml:"tail_max_duration"`
	ExtraQueryDelay               time.Duration    `yaml:"extra_query_delay,omitempty"`
	QueryIngestersWithin          time.Duration    `yaml:"query_ingesters_within,omitempty"`
	IngesterQueryStoreMaxLookback time.Duration    `yaml:"-"`
	Engine                        logql.EngineOpts `yaml:"engine,omitempty"`
	MaxConcurrent                 int              `yaml:"max_concurrent"`
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Engine.RegisterFlagsWithPrefix("querier", f)
	f.DurationVar(&cfg.TailMaxDuration, "querier.tail-max-duration", 1*time.Hour, "Limit the duration for which live tailing request would be served")
	f.DurationVar(&cfg.QueryTimeout, "querier.query-timeout", 1*time.Minute, "Timeout when querying backends (ingesters or storage) during the execution of a query request")
	f.DurationVar(&cfg.ExtraQueryDelay, "querier.extra-query-delay", 0, "Time to wait before sending more than the minimum successful query requests.")
	f.DurationVar(&cfg.QueryIngestersWithin, "querier.query-ingesters-within", 0, "Maximum lookback beyond which queries are not sent to ingester. 0 means all queries are sent to ingester.")
	f.IntVar(&cfg.MaxConcurrent, "querier.max-concurrent", 20, "The maximum number of concurrent queries.")
}

// Querier handlers queries.
type Querier struct {
	cfg    Config
	ring   ring.ReadRing
	pool   *ring_client.Pool
	store  storage.Store
	engine *logql.Engine
	limits *validation.Overrides
}

// New makes a new Querier.
func New(cfg Config, clientCfg client.Config, ring ring.ReadRing, store storage.Store, limits *validation.Overrides) (*Querier, error) {
	factory := func(addr string) (ring_client.PoolClient, error) {
		return client.New(clientCfg, addr)
	}

	return newQuerier(cfg, clientCfg, factory, ring, store, limits)
}

// newQuerier creates a new Querier and allows to pass a custom ingester client factory
// used for testing purposes
func newQuerier(cfg Config, clientCfg client.Config, clientFactory ring_client.PoolFactory, ring ring.ReadRing, store storage.Store, limits *validation.Overrides) (*Querier, error) {
	querier := Querier{
		cfg:    cfg,
		ring:   ring,
		pool:   distributor.NewPool(clientCfg.PoolConfig, ring, clientFactory, util.Logger),
		store:  store,
		limits: limits,
	}

	querier.engine = logql.NewEngine(cfg.Engine, &querier)
	err := services.StartAndAwaitRunning(context.Background(), querier.pool)
	if err != nil {
		return nil, errors.Wrap(err, "querier pool")
	}

	return &querier, nil
}

type responseFromIngesters struct {
	addr     string
	response interface{}
}

// forAllIngesters runs f, in parallel, for all ingesters
// TODO taken from Cortex, see if we can refactor out an usable interface.
func (q *Querier) forAllIngesters(ctx context.Context, f func(logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	replicationSet, err := q.ring.GetAll(ring.Read)
	if err != nil {
		return nil, err
	}

	return q.forGivenIngesters(ctx, replicationSet, f)
}

// forGivenIngesters runs f, in parallel, for given ingesters
// TODO taken from Cortex, see if we can refactor out an usable interface.
func (q *Querier) forGivenIngesters(ctx context.Context, replicationSet ring.ReplicationSet, f func(logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	results, err := replicationSet.Do(ctx, q.cfg.ExtraQueryDelay, func(ingester *ring.IngesterDesc) (interface{}, error) {
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

// Select Implements logql.Querier which select logs via matchers and regex filters.
func (q *Querier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	err := q.validateQueryRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	var chunkStoreIter iter.EntryIterator

	if q.cfg.IngesterQueryStoreMaxLookback == 0 {
		// IngesterQueryStoreMaxLookback is zero, the default state, query the store normally
		chunkStoreIter, err = q.store.SelectLogs(ctx, params)
		if err != nil {
			return nil, err
		}
	} else if q.cfg.IngesterQueryStoreMaxLookback > 0 {
		// IngesterQueryStoreMaxLookback is greater than zero
		// Adjust the store query range to only query for data ingesters are not already querying for
		adjustedEnd := params.End.Add(-q.cfg.IngesterQueryStoreMaxLookback)
		if params.Start.After(adjustedEnd) {
			chunkStoreIter = iter.NoopIterator
		} else {
			// Make a copy of the request before modifying
			// because the initial request is used below to query ingesters
			queryRequestCopy := *params.QueryRequest
			newParams := logql.SelectLogParams{
				QueryRequest: &queryRequestCopy,
			}
			newParams.End = adjustedEnd
			chunkStoreIter, err = q.store.SelectLogs(ctx, newParams)
			if err != nil {
				return nil, err
			}
		}
	} else {
		// IngesterQueryStoreMaxLookback is less than zero
		// ingesters will be querying all the way back in time so there is no reason to query the store
		chunkStoreIter = iter.NoopIterator
	}

	// skip ingester queries only when QueryIngestersWithin is enabled (not the zero value) and
	// the end of the query is earlier than the lookback
	if !shouldQueryIngester(q.cfg, params) {
		return chunkStoreIter, nil
	}

	iters, err := q.queryIngesters(ctx, params)
	if err != nil {
		return nil, err
	}

	return iter.NewHeapIterator(ctx, append(iters, chunkStoreIter), params.Direction), nil
}

func (q *Querier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	err := q.validateQueryRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	var chunkStoreIter iter.SampleIterator

	switch {
	case q.cfg.IngesterQueryStoreMaxLookback == 0:
		// IngesterQueryStoreMaxLookback is zero, the default state, query the store normally
		chunkStoreIter, err = q.store.SelectSamples(ctx, params)
		if err != nil {
			return nil, err
		}
	case q.cfg.IngesterQueryStoreMaxLookback > 0:
		adjustedEnd := params.End.Add(-q.cfg.IngesterQueryStoreMaxLookback)
		if params.Start.After(adjustedEnd) {
			chunkStoreIter = iter.NoopIterator
			break
		}
		// Make a copy of the request before modifying
		// because the initial request is used below to query ingesters
		queryRequestCopy := *params.SampleQueryRequest
		newParams := logql.SelectSampleParams{
			SampleQueryRequest: &queryRequestCopy,
		}
		newParams.End = adjustedEnd
		chunkStoreIter, err = q.store.SelectSamples(ctx, newParams)
		if err != nil {
			return nil, err
		}
	default:
		chunkStoreIter = iter.NoopIterator

	}
	// skip ingester queries only when QueryIngestersWithin is enabled (not the zero value) and
	// the end of the query is earlier than the lookback
	if !shouldQueryIngester(q.cfg, params) {
		return chunkStoreIter, nil
	}

	iters, err := q.queryIngestersForSample(ctx, params)
	if err != nil {
		return nil, err
	}
	return iter.NewHeapSampleIterator(ctx, append(iters, chunkStoreIter)), nil
}

func shouldQueryIngester(cfg Config, params logql.QueryParams) bool {
	lookback := time.Now().Add(-cfg.QueryIngestersWithin)
	return !(cfg.QueryIngestersWithin != 0 && params.GetEnd().Before(lookback))
}

func (q *Querier) queryIngesters(ctx context.Context, params logql.SelectLogParams) ([]iter.EntryIterator, error) {
	clients, err := q.forAllIngesters(ctx, func(client logproto.QuerierClient) (interface{}, error) {
		return client.Query(ctx, params.QueryRequest, stats.CollectTrailer(ctx))
	})
	if err != nil {
		return nil, err
	}

	iterators := make([]iter.EntryIterator, len(clients))
	for i := range clients {
		iterators[i] = iter.NewQueryClientIterator(clients[i].response.(logproto.Querier_QueryClient), params.Direction)
	}
	return iterators, nil
}

func (q *Querier) queryIngestersForSample(ctx context.Context, params logql.SelectSampleParams) ([]iter.SampleIterator, error) {
	clients, err := q.forAllIngesters(ctx, func(client logproto.QuerierClient) (interface{}, error) {
		return client.QuerySample(ctx, params.SampleQueryRequest, stats.CollectTrailer(ctx))
	})
	if err != nil {
		return nil, err
	}

	iterators := make([]iter.SampleIterator, len(clients))
	for i := range clients {
		iterators[i] = iter.NewSampleQueryClientIterator(clients[i].response.(logproto.Querier_QuerySampleClient))
	}
	return iterators, nil
}

// Label does the heavy lifting for a Label query.
func (q *Querier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	if err = q.validateQueryTimeRange(userID, *req.Start, *req.End); err != nil {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	resps, err := q.forAllIngesters(ctx, func(client logproto.QuerierClient) (interface{}, error) {
		return client.Label(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	from, through := model.TimeFromUnixNano(req.Start.UnixNano()), model.TimeFromUnixNano(req.End.UnixNano())
	var storeValues []string
	if req.Values {
		storeValues, err = q.store.LabelValuesForMetricName(ctx, userID, from, through, "logs", req.Name)
		if err != nil {
			return nil, err
		}
	} else {
		storeValues, err = q.store.LabelNamesForMetricName(ctx, userID, from, through, "logs")
		if err != nil {
			return nil, err
		}
	}

	results := make([][]string, 0, len(resps))
	for _, resp := range resps {
		results = append(results, resp.response.(*logproto.LabelResponse).Values)
	}
	results = append(results, storeValues)

	return &logproto.LabelResponse{
		Values: listutil.MergeStringLists(results...),
	}, nil
}

// Check implements the grpc healthcheck
func (*Querier) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

// Tail keeps getting matching logs from all ingesters for given query
func (q *Querier) Tail(ctx context.Context, req *logproto.TailRequest) (*Tailer, error) {
	err := q.checkTailRequestLimit(ctx)
	if err != nil {
		return nil, err
	}

	histReq := logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Selector:  req.Query,
			Start:     req.Start,
			End:       time.Now(),
			Limit:     req.Limit,
			Direction: logproto.BACKWARD,
		},
	}

	err = q.validateQueryRequest(ctx, histReq)
	if err != nil {
		return nil, err
	}

	// Enforce the query timeout except when tailing, otherwise the tailing
	// will be terminated once the query timeout is reached
	tailCtx := ctx
	queryCtx, cancelQuery := context.WithDeadline(ctx, time.Now().Add(q.cfg.QueryTimeout))
	defer cancelQuery()

	clients, err := q.forAllIngesters(ctx, func(client logproto.QuerierClient) (interface{}, error) {
		return client.Tail(tailCtx, req)
	})
	if err != nil {
		return nil, err
	}

	tailClients := make(map[string]logproto.Querier_TailClient)
	for i := range clients {
		tailClients[clients[i].addr] = clients[i].response.(logproto.Querier_TailClient)
	}

	histIterators, err := q.SelectLogs(queryCtx, histReq)
	if err != nil {
		return nil, err
	}

	reversedIterator, err := iter.NewReversedIter(histIterators, req.Limit, true)
	if err != nil {
		return nil, err
	}

	return newTailer(
		time.Duration(req.DelayFor)*time.Second,
		tailClients,
		reversedIterator,
		func(connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
			return q.tailDisconnectedIngesters(tailCtx, req, connectedIngestersAddr)
		},
		q.cfg.TailMaxDuration,
		tailerWaitEntryThrottle,
	), nil
}

// passed to tailer for (re)connecting to new or disconnected ingesters
func (q *Querier) tailDisconnectedIngesters(ctx context.Context, req *logproto.TailRequest, connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
	// Build a map to easily check if an ingester address is already connected
	connected := make(map[string]bool)
	for _, addr := range connectedIngestersAddr {
		connected[addr] = true
	}

	// Get the current replication set from the ring
	replicationSet, err := q.ring.GetAll(ring.Read)
	if err != nil {
		return nil, err
	}

	// Look for disconnected ingesters or new one we should (re)connect to
	reconnectIngesters := []ring.IngesterDesc{}

	for _, ingester := range replicationSet.Ingesters {
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
	reconnectClients, err := q.forGivenIngesters(ctx, ring.ReplicationSet{Ingesters: reconnectIngesters}, func(client logproto.QuerierClient) (interface{}, error) {
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

// Series fetches any matching series for a list of matcher sets
func (q *Querier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	if err = q.validateQueryTimeRange(userID, req.Start, req.End); err != nil {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	return q.awaitSeries(ctx, req)

}

func (q *Querier) awaitSeries(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {

	// buffer the channels to the # of calls they're expecting su
	series := make(chan [][]logproto.SeriesIdentifier, 2)
	errs := make(chan error, 2)

	// fetch series from ingesters and store concurrently

	go func() {
		// fetch series identifiers from ingesters
		resps, err := q.forAllIngesters(ctx, func(client logproto.QuerierClient) (interface{}, error) {
			return client.Series(ctx, req)
		})
		if err != nil {
			errs <- err
			return
		}
		var acc [][]logproto.SeriesIdentifier
		for _, resp := range resps {
			acc = append(acc, resp.response.(*logproto.SeriesResponse).Series)
		}
		series <- acc
	}()

	go func() {
		storeValues, err := q.seriesForMatchers(ctx, req.Start, req.End, req.GetGroups())
		if err != nil {
			errs <- err
			return
		}
		series <- [][]logproto.SeriesIdentifier{storeValues}
	}()

	var sets [][]logproto.SeriesIdentifier
	for i := 0; i < 2; i++ {
		select {
		case err := <-errs:
			return nil, err
		case s := <-series:
			sets = append(sets, s...)
		}
	}

	deduped := make(map[string]logproto.SeriesIdentifier)
	for _, set := range sets {
		for _, s := range set {
			key := loghttp.LabelSet(s.Labels).String()
			if _, exists := deduped[key]; !exists {
				deduped[key] = s
			}
		}
	}

	response := &logproto.SeriesResponse{
		Series: make([]logproto.SeriesIdentifier, 0, len(deduped)),
	}

	for _, s := range deduped {
		response.Series = append(response.Series, s)
	}

	return response, nil
}

// seriesForMatchers fetches series from the store for each matcher set
// TODO: make efficient if/when the index supports labels so we don't have to read chunks
func (q *Querier) seriesForMatchers(
	ctx context.Context,
	from, through time.Time,
	groups []string,
) ([]logproto.SeriesIdentifier, error) {

	var results []logproto.SeriesIdentifier
	// If no matchers were specified for the series query,
	// we send a query with an empty matcher which will match every series.
	if len(groups) == 0 {
		var err error
		results, err = q.seriesForMatcher(ctx, from, through, "")
		if err != nil {
			return nil, err
		}
	} else {
		for _, group := range groups {
			ids, err := q.seriesForMatcher(ctx, from, through, group)
			if err != nil {
				return nil, err
			}
			results = append(results, ids...)
		}
	}
	return results, nil
}

// seriesForMatcher fetches series from the store for a given matcher
func (q *Querier) seriesForMatcher(ctx context.Context, from, through time.Time, matcher string) ([]logproto.SeriesIdentifier, error) {
	ids, err := q.store.GetSeries(ctx, logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Selector:  matcher,
			Limit:     1,
			Start:     from,
			End:       through,
			Direction: logproto.FORWARD,
		},
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (q *Querier) validateQueryRequest(ctx context.Context, req logql.QueryParams) error {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}

	selector, err := req.LogSelector()
	if err != nil {
		return err
	}
	matchers := selector.Matchers()

	maxStreamMatchersPerQuery := q.limits.MaxStreamsMatchersPerQuery(userID)
	if len(matchers) > maxStreamMatchersPerQuery {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"max streams matchers per query exceeded, matchers-count > limit (%d > %d)", len(matchers), maxStreamMatchersPerQuery)
	}

	return q.validateQueryTimeRange(userID, req.GetStart(), req.GetEnd())
}

func (q *Querier) validateQueryTimeRange(userID string, from time.Time, through time.Time) error {
	if (through).Before(from) {
		return httpgrpc.Errorf(http.StatusBadRequest, "invalid query, through < from (%s < %s)", through, from)
	}

	maxQueryLength := q.limits.MaxQueryLength(userID)
	if maxQueryLength > 0 && (through).Sub(from) > maxQueryLength {
		return httpgrpc.Errorf(http.StatusBadRequest, cortex_validation.ErrQueryTooLong, (through).Sub(from), maxQueryLength)
	}

	return nil
}

func (q *Querier) checkTailRequestLimit(ctx context.Context) error {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}

	replicationSet, err := q.ring.GetAll(ring.Read)
	if err != nil {
		return err
	}

	// we want to check count of active tailers with only active ingesters
	ingesters := make([]ring.IngesterDesc, 0, 1)
	for i := range replicationSet.Ingesters {
		if replicationSet.Ingesters[i].State == ring.ACTIVE {
			ingesters = append(ingesters, replicationSet.Ingesters[i])
		}
	}

	if len(ingesters) == 0 {
		return httpgrpc.Errorf(http.StatusInternalServerError, "no active ingester found")
	}

	responses, err := q.forGivenIngesters(ctx, replicationSet, func(querierClient logproto.QuerierClient) (interface{}, error) {
		resp, err := querierClient.TailersCount(ctx, nil)
		if err != nil {
			return nil, err
		}
		return resp.Count, nil
	})
	// We are only checking active ingesters, and any error returned stops checking other ingesters
	// so return that error here as well.
	if err != nil {
		return err
	}

	var maxCnt uint32
	maxCnt = 0
	for _, resp := range responses {
		r := resp.response.(uint32)
		if r > maxCnt {
			maxCnt = r
		}
	}
	l := uint32(q.limits.MaxConcurrentTailRequests(userID))
	if maxCnt >= l {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"max concurrent tail requests limit exceeded, count > limit (%d > %d)", maxCnt+1, l)
	}

	return nil
}
