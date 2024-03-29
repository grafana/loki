package pattern

import (
	"context"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/client_golang/prometheus"
)

type IngesterQuerier struct {
	cfg    Config
	logger log.Logger

	ringClient *RingClient

	registerer prometheus.Registerer
}

func NewIngesterQuerier(
	cfg Config,
	ringClient *RingClient,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*IngesterQuerier, error) {
	return &IngesterQuerier{
		logger:     log.With(logger, "component", "pattern-ingester-querier"),
		ringClient: ringClient,
		cfg:        cfg,
		registerer: prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer),
	}, nil
}

// ForAllIngesters runs f, in parallel, for all ingesters
func (q *IngesterQuerier) ForAllIngesters(ctx context.Context, f func(context.Context, logproto.QuerierClient) (interface{}, error)) ([]ResponseFromIngesters, error) {
	replicationSet, err := q.ringClient.ring.GetReplicationSetForOperation(ring.Read)
	if err != nil {
		return nil, err
	}

	return q.forGivenIngesters(ctx, replicationSet, f)
}

type ResponseFromIngesters struct {
	addr     string
	response interface{}
}

// forGivenIngesters runs f, in parallel, for given ingesters
func (q *IngesterQuerier) forGivenIngesters(ctx context.Context, replicationSet ring.ReplicationSet, f func(context.Context, logproto.QuerierClient) (interface{}, error)) ([]ResponseFromIngesters, error) {
	cfg := ring.DoUntilQuorumConfig{
		// Nothing here
	}
	results, err := ring.DoUntilQuorum(ctx, replicationSet, cfg, func(ctx context.Context, ingester *ring.InstanceDesc) (ResponseFromIngesters, error) {
		client, err := q.ringClient.pool.GetClientFor(ingester.Addr)
		if err != nil {
			return ResponseFromIngesters{addr: ingester.Addr}, err
		}

		resp, err := f(ctx, client.(logproto.QuerierClient))
		if err != nil {
			return ResponseFromIngesters{addr: ingester.Addr}, err
		}

		return ResponseFromIngesters{ingester.Addr, resp}, nil
	}, func(ResponseFromIngesters) {
		// Nothing to do
	})
	if err != nil {
		return nil, err
	}

	responses := make([]ResponseFromIngesters, 0, len(results))
	responses = append(responses, results...)

	return responses, err
}
