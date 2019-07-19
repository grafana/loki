package querier

import (
	"context"
	"flag"
	"time"

	cortex_client "github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	token_util "github.com/grafana/loki/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage"
)

// Config for a querier.
type Config struct {
	TailMaxDuration time.Duration `yaml:"tail_max_duration"`
	QueryTimeout    time.Duration `yaml:"query_timeout"`
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.TailMaxDuration, "querier.tail-max-duration", 1*time.Hour, "Limit the duration for which live tailing request would be served")
	f.DurationVar(&cfg.QueryTimeout, "querier.query_timeout", 1*time.Minute, "Timeout when querying backends (ingesters or storage) during the execution of a query request")
}

// Querier handlers queries.
type Querier struct {
	cfg   Config
	ring  ring.ReadRing
	pool  *cortex_client.Pool
	store storage.Store
}

// New makes a new Querier.
func New(cfg Config, clientCfg client.Config, ring ring.ReadRing, store storage.Store) (*Querier, error) {
	factory := func(addr string) (grpc_health_v1.HealthClient, error) {
		return client.New(clientCfg, addr)
	}

	return newQuerier(cfg, clientCfg, factory, ring, store)
}

// newQuerier creates a new Querier and allows to pass a custom ingester client factory
// used for testing purposes
func newQuerier(cfg Config, clientCfg client.Config, clientFactory cortex_client.Factory, ring ring.ReadRing, store storage.Store) (*Querier, error) {
	return &Querier{
		cfg:   cfg,
		ring:  ring,
		pool:  cortex_client.NewPool(clientCfg.PoolConfig, ring, clientFactory, util.Logger),
		store: store,
	}, nil
}

type responseFromIngesters struct {
	addr     string
	response interface{}
}

// forAllIngesters runs f, in parallel, for all ingesters
// TODO taken from Cortex, see if we can refactor out an usable interface.
func (q *Querier) forAllIngesters(f func(logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	replicationSet, err := q.ring.GetAll()
	if err != nil {
		return nil, err
	}

	return q.forGivenIngesters(replicationSet, f)
}

// forGivenIngesters runs f, in parallel, for given ingesters
// TODO taken from Cortex, see if we can refactor out an usable interface.
func (q *Querier) forGivenIngesters(replicationSet ring.ReplicationSet, f func(logproto.QuerierClient) (interface{}, error)) ([]responseFromIngesters, error) {
	resps, errs := make(chan responseFromIngesters), make(chan error)
	for _, ingester := range replicationSet.Ingesters {
		go func(ingester ring.IngesterDesc) {
			client, err := q.pool.GetClientFor(ingester.Addr)
			if err != nil {
				errs <- err
				return
			}

			resp, err := f(client.(logproto.QuerierClient))
			if err != nil {
				errs <- err
			} else {
				resps <- responseFromIngesters{ingester.Addr, resp}
			}
		}(ingester)
	}

	var lastErr error
	result, numErrs := []responseFromIngesters{}, 0
	for range replicationSet.Ingesters {
		select {
		case resp := <-resps:
			result = append(result, resp)
		case lastErr = <-errs:
			numErrs++
		}
	}

	if numErrs > replicationSet.MaxErrors {
		return nil, lastErr
	}

	return result, nil
}

// Query does the heavy lifting for an actual query.
func (q *Querier) Query(ctx context.Context, req *logproto.QueryRequest) (*logproto.QueryResponse, error) {
	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	iterators, err := q.getQueryIterators(ctx, req)
	if err != nil {
		return nil, err
	}

	iterator := iter.NewHeapIterator(iterators, req.Direction)
	defer helpers.LogError("closing iterator", iterator.Close)

	resp, _, err := ReadBatch(iterator, req.Limit)
	return resp, err
}

func (q *Querier) getQueryIterators(ctx context.Context, req *logproto.QueryRequest) ([]iter.EntryIterator, error) {
	ingesterIterators, err := q.queryIngesters(ctx, req)
	if err != nil {
		return nil, err
	}

	chunkStoreIterators, err := q.store.LazyQuery(ctx, req)
	if err != nil {
		return nil, err
	}

	iterators := append(ingesterIterators, chunkStoreIterators)
	return iterators, nil
}

func (q *Querier) queryIngesters(ctx context.Context, req *logproto.QueryRequest) ([]iter.EntryIterator, error) {
	clients, err := q.forAllIngesters(func(client logproto.QuerierClient) (interface{}, error) {
		return client.Query(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	iterators := make([]iter.EntryIterator, len(clients))
	for i := range clients {
		iterators[i] = iter.NewQueryClientIterator(clients[i].response.(logproto.Querier_QueryClient), req.Direction)
	}
	return iterators, nil
}

// Label does the heavy lifting for a Label query.
func (q *Querier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	resps, err := q.forAllIngesters(func(client logproto.QuerierClient) (interface{}, error) {
		return client.Label(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	from, through := model.TimeFromUnixNano(req.Start.UnixNano()), model.TimeFromUnixNano(req.End.UnixNano())
	var storeValues []string
	if req.Values {
		storeValues, err = q.store.LabelValuesForMetricName(ctx, from, through, "logs", req.Name)
		if err != nil {
			return nil, err
		}
	} else {
		storeValues, err = q.store.LabelNamesForMetricName(ctx, from, through, "logs")
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
		Values: mergeLists(results...),
	}, nil
}

// ReadBatch reads a set of entries off an iterator.
func ReadBatch(i iter.EntryIterator, size uint32) (*logproto.QueryResponse, uint32, error) {
	streams := map[string]*logproto.Stream{}
	respSize := uint32(0)
	for ; respSize < size && i.Next(); respSize++ {
		labels, entry := i.Labels(), i.Entry()
		stream, ok := streams[labels]
		if !ok {
			stream = &logproto.Stream{
				Labels: labels,
			}
			streams[labels] = stream
		}
		stream.Entries = append(stream.Entries, entry)
	}

	result := logproto.QueryResponse{
		Streams: make([]*logproto.Stream, 0, len(streams)),
	}
	for _, stream := range streams {
		result.Streams = append(result.Streams, stream)
	}
	return &result, respSize, i.Error()
}

// Check implements the grpc healthcheck
func (*Querier) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func mergeLists(ss ...[]string) []string {
	switch len(ss) {
	case 0:
		return nil
	case 1:
		return ss[0]
	case 2:
		return mergePair(ss[0], ss[1])
	default:
		n := len(ss) / 2
		return mergePair(mergeLists(ss[:n]...), mergeLists(ss[n:]...))
	}
}

func mergePair(s1, s2 []string) []string {
	i, j := 0, 0
	result := make([]string, 0, len(s1)+len(s2))
	for i < len(s1) && j < len(s2) {
		if s1[i] < s2[j] {
			result = append(result, s1[i])
			i++
		} else if s1[i] > s2[j] {
			result = append(result, s2[j])
			j++
		} else {
			result = append(result, s1[i])
			i++
			j++
		}
	}
	for ; i < len(s1); i++ {
		result = append(result, s1[i])
	}
	for ; j < len(s2); j++ {
		result = append(result, s2[j])
	}
	return result
}

// Tail keeps getting matching logs from all ingesters for given query
func (q *Querier) Tail(ctx context.Context, req *logproto.TailRequest) (*Tailer, error) {
	// Enforce the query timeout except when tailing, otherwise the tailing
	// will be terminated once the query timeout is reached
	tailCtx := ctx
	queryCtx, cancelQuery := context.WithDeadline(ctx, time.Now().Add(q.cfg.QueryTimeout))
	defer cancelQuery()

	clients, err := q.forAllIngesters(func(client logproto.QuerierClient) (interface{}, error) {
		return client.Tail(tailCtx, req)
	})
	if err != nil {
		return nil, err
	}

	tailClients := make(map[string]logproto.Querier_TailClient)
	for i := range clients {
		tailClients[clients[i].addr] = clients[i].response.(logproto.Querier_TailClient)
	}

	histReq := logproto.QueryRequest{
		Query:     req.Query,
		Start:     req.Start,
		End:       time.Now(),
		Limit:     req.Limit,
		Direction: logproto.FORWARD,
		Regex:     req.Regex,
	}
	histIterators, err := q.getQueryIterators(queryCtx, &histReq)
	if err != nil {
		return nil, err
	}

	return newTailer(
		time.Duration(req.DelayFor)*time.Second,
		tailClients,
		histIterators,
		func(from, to time.Time, labels string) (iterator iter.EntryIterator, e error) {
			return q.queryDroppedStreams(queryCtx, req, from, to, labels)
		},
		func(connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
			return q.tailDisconnectedIngesters(tailCtx, req, connectedIngestersAddr)
		},
		q.cfg.TailMaxDuration,
	), nil
}

// passed to tailer for querying dropped streams
func (q *Querier) queryDroppedStreams(ctx context.Context, req *logproto.TailRequest, start, end time.Time, labels string) (iter.EntryIterator, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	key := token_util.TokenFor(userID, labels)
	replicationSet, err := q.ring.Get(key, ring.Read)
	if err != nil {
		return nil, err
	}

	query := logproto.QueryRequest{
		Direction: logproto.FORWARD,
		Start:     start,
		End:       end,
		Limit:     10000,
		Query:     req.Query,
		Regex:     req.Regex,
	}

	clients, err := q.forGivenIngesters(replicationSet, func(client logproto.QuerierClient) (interface{}, error) {
		return client.Query(ctx, &query)
	})
	if err != nil {
		return nil, err
	}

	ingesterIterators := make([]iter.EntryIterator, len(clients))
	for i := range clients {
		ingesterIterators[i] = iter.NewQueryClientIterator(clients[i].response.(logproto.Querier_QueryClient), query.Direction)
	}

	chunkStoreIterators, err := q.store.LazyQuery(ctx, &query)
	if err != nil {
		return nil, err
	}

	iterators := append(ingesterIterators, chunkStoreIterators)
	return iter.NewHeapIterator(iterators, query.Direction), nil
}

// passed to tailer for (re)connecting to new or disconnected ingesters
func (q *Querier) tailDisconnectedIngesters(ctx context.Context, req *logproto.TailRequest, connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
	tailClients := make(map[string]logproto.Querier_TailClient)
	for i := range connectedIngestersAddr {
		tailClients[connectedIngestersAddr[i]] = nil
	}

	disconnectedIngesters := []ring.IngesterDesc{}
	replicationSet, err := q.ring.GetAll()
	if err != nil {
		return nil, err
	}

	for _, ingester := range replicationSet.Ingesters {
		if _, isOk := tailClients[ingester.Addr]; isOk {
			delete(tailClients, ingester.Addr)
		} else {
			disconnectedIngesters = append(disconnectedIngesters, ingester)
		}
	}

	clients, err := q.forGivenIngesters(ring.ReplicationSet{Ingesters: disconnectedIngesters}, func(client logproto.QuerierClient) (interface{}, error) {
		return client.Tail(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	for i := range clients {
		tailClients[clients[i].addr] = clients[i].response.(logproto.Querier_TailClient)
	}
	return tailClients, nil
}
