package querier

import (
	"context"
	"flag"

	"github.com/cortexproject/cortex/pkg/chunk"
	cortex_client "github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

// Config for a querier.
type Config struct {
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
}

// Querier handlers queries.
type Querier struct {
	cfg   Config
	ring  ring.ReadRing
	pool  *cortex_client.Pool
	store chunk.Store
}

// New makes a new Querier.
func New(cfg Config, clientCfg client.Config, ring ring.ReadRing, store chunk.Store) (*Querier, error) {
	factory := func(addr string) (grpc_health_v1.HealthClient, error) {
		return client.New(clientCfg, addr)
	}

	return &Querier{
		cfg:   cfg,
		ring:  ring,
		pool:  cortex_client.NewPool(clientCfg.PoolConfig, ring, factory, util.Logger),
		store: store,
	}, nil
}

// forAllIngesters runs f, in parallel, for all ingesters
// TODO taken from Cortex, see if we can refactor out an usable interface.
func (q *Querier) forAllIngesters(f func(logproto.QuerierClient) (interface{}, error)) ([]interface{}, error) {
	replicationSet, err := q.ring.GetAll()
	if err != nil {
		return nil, err
	}

	resps, errs := make(chan interface{}), make(chan error)
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
				resps <- resp
			}
		}(ingester)
	}

	var lastErr error
	result, numErrs := []interface{}{}, 0
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
	ingesterIterators, err := q.queryIngesters(ctx, req)
	if err != nil {
		return nil, err
	}

	chunkStoreIterators, err := q.queryStore(ctx, req)
	if err != nil {
		return nil, err
	}

	iterators := append(chunkStoreIterators, ingesterIterators...)
	iterator := iter.NewHeapIterator(iterators, req.Direction)
	defer helpers.LogError("closing iterator", iterator.Close)

	resp, _, err := ReadBatch(iterator, req.Limit)
	return resp, err
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
		iterators[i] = iter.NewQueryClientIterator(clients[i].(logproto.Querier_QueryClient), req.Direction)
	}
	return iterators, nil
}

// Label does the heavy lifting for a Label query.
func (q *Querier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	resps, err := q.forAllIngesters(func(client logproto.QuerierClient) (interface{}, error) {
		return client.Label(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	results := make([][]string, 0, len(resps))
	for _, resp := range resps {
		results = append(results, resp.(*logproto.LabelResponse).Values)
	}

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
