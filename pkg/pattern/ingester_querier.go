package pattern

import (
	"context"
	"errors"
	"math"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
)

// TODO(kolesnikovae): parametrise QueryPatternsRequest
const minClusterSize = 30

var ErrParseQuery = errors.New("only label matcher, byte_over_time, and count_over_time queries without filters are supported")

type IngesterQuerier struct {
	cfg    Config
	logger log.Logger

	ringClient RingClient

	registerer prometheus.Registerer
}

func NewIngesterQuerier(
	cfg Config,
	ringClient RingClient,
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

func (q *IngesterQuerier) Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	_, err := syntax.ParseMatchers(req.Query, true)
	if err != nil {
		// not a pattern query, so either a metric query or an error
		if q.cfg.MetricAggregation.Enabled {
			return q.queryMetricSamples(ctx, req)
		}

		return nil, err
	}

	return q.queryPatternSamples(ctx, req)
}

func (q *IngesterQuerier) queryPatternSamples(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	iterators, err := q.query(ctx, req)
	if err != nil {
		return nil, err
	}

	// TODO(kolesnikovae): Incorporate with pruning
	resp, err := iter.ReadPatternsBatch(iter.NewMerge(iterators...), math.MaxInt32)
	if err != nil {
		return nil, err
	}
	return prunePatterns(resp, minClusterSize), nil
}

func (q *IngesterQuerier) queryMetricSamples(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	expr, err := syntax.ParseSampleExpr(req.Query)
	if err != nil {
		return nil, err
	}

	var selector syntax.LogSelectorExpr
	switch expr.(type) {
	case *syntax.VectorAggregationExpr:
		selector, err = expr.(*syntax.VectorAggregationExpr).Selector()
	case *syntax.RangeAggregationExpr:
		selector, err = expr.(*syntax.RangeAggregationExpr).Selector()
	default:
		return nil, ErrParseQuery
	}

	if err != nil {
		return nil, err
	}

	if selector == nil || selector.HasFilter() {
		return nil, ErrParseQuery
	}

	iterators, err := q.query(ctx, req)
	if err != nil {
		return nil, err
	}

	resp, err := iter.ReadMetricsBatch(iter.NewMerge(iterators...), math.MaxInt32)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (q *IngesterQuerier) query(ctx context.Context, req *logproto.QueryPatternsRequest) ([]iter.Iterator, error) {
	resps, err := q.forAllIngesters(ctx, func(_ context.Context, client logproto.PatternClient) (interface{}, error) {
		return client.Query(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	iterators := make([]iter.Iterator, len(resps))
	for i := range resps {
		iterators[i] = iter.NewQueryClientIterator(resps[i].response.(logproto.Pattern_QueryClient))
	}
	return iterators, nil
}

func prunePatterns(
	resp *logproto.QueryPatternsResponse,
	minClusterSize int,
) *logproto.QueryPatternsResponse {
	d := drain.New(drain.DefaultConfig(), nil)
	for _, p := range resp.Series {
		d.TrainPattern(p.GetPattern(), p.Samples)
	}

	resp.Series = resp.Series[:0]
	for _, cluster := range d.Clusters() {
		if cluster.Size < minClusterSize {
			continue
		}
		pattern := d.PatternString(cluster)
		if pattern == "" {
			continue
		}
		resp.Series = append(resp.Series,
			logproto.NewPatternSeriesWithPattern(pattern, cluster.Samples()))
	}
	return resp
}

// ForAllIngesters runs f, in parallel, for all ingesters
func (q *IngesterQuerier) forAllIngesters(ctx context.Context, f func(context.Context, logproto.PatternClient) (interface{}, error)) ([]ResponseFromIngesters, error) {
	replicationSet, err := q.ringClient.Ring().GetReplicationSetForOperation(ring.Read)
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
func (q *IngesterQuerier) forGivenIngesters(ctx context.Context, replicationSet ring.ReplicationSet, f func(context.Context, logproto.PatternClient) (interface{}, error)) ([]ResponseFromIngesters, error) {
	cfg := ring.DoUntilQuorumConfig{
		// Nothing here
	}
	results, err := ring.DoUntilQuorum(ctx, replicationSet, cfg, func(ctx context.Context, ingester *ring.InstanceDesc) (ResponseFromIngesters, error) {
		client, err := q.ringClient.Pool().GetClientFor(ingester.Addr)
		if err != nil {
			return ResponseFromIngesters{addr: ingester.Addr}, err
		}

		resp, err := f(ctx, client.(logproto.PatternClient))
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
