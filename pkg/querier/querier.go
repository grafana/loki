package querier

import (
	"context"
	"flag"
	"time"

	cortex_client "github.com/weaveworks/cortex/pkg/ingester/client"
	"github.com/weaveworks/cortex/pkg/ring"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/logish/pkg/ingester/client"
	"github.com/grafana/logish/pkg/logproto"
)

type Config struct {
	PoolConfig cortex_client.PoolConfig

	RemoteTimeout time.Duration
	ClientConfig  client.Config
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ClientConfig.RegisterFlags(f)
	cfg.PoolConfig.RegisterFlags(f)

	f.DurationVar(&cfg.RemoteTimeout, "querier.remote-timeout", 10*time.Second, "")
}

type Querier struct {
	cfg  Config
	ring ring.ReadRing
	pool *cortex_client.Pool
}

func New(cfg Config, ring ring.ReadRing) (*Querier, error) {
	factory := func(addr string) (grpc_health_v1.HealthClient, error) {
		return client.New(cfg.ClientConfig, addr)
	}

	return &Querier{
		cfg:  cfg,
		ring: ring,
		pool: cortex_client.NewPool(cfg.PoolConfig, ring, factory),
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
		go func(ingester *ring.IngesterDesc) {
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

func (q *Querier) Query(ctx context.Context, req *logproto.QueryRequest) (*logproto.QueryResponse, error) {
	clients, err := q.forAllIngesters(func(client logproto.QuerierClient) (interface{}, error) {
		return client.Query(ctx, req)
	})
	if err != nil {
		return nil, err
	}

	iterators := make([]EntryIterator, len(clients))
	for i := range clients {
		iterators[i] = newQueryClientIterator(clients[i].(logproto.Querier_QueryClient), req.Direction)
	}
	iterator := NewHeapIterator(iterators, req.Direction)
	defer iterator.Close()

	resp, _, err := ReadBatch(iterator, req.Limit)
	return resp, err
}

func ReadBatch(i EntryIterator, size uint32) (*logproto.QueryResponse, uint32, error) {
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
func (*Querier) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

type iteratorBatcher struct {
	iterator    EntryIterator
	queryServer logproto.Querier_QueryServer
}
