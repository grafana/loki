package pattern

import (
	"context"
	"errors"
	"math"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/drain"

	loki_iter "github.com/grafana/loki/v3/pkg/iter"
	pattern_iter "github.com/grafana/loki/v3/pkg/pattern/iter"
)

// TODO(kolesnikovae): parametrise QueryPatternsRequest
const minClusterSize = 30

var ErrParseQuery = errors.New("only byte_over_time and count_over_time queries without filters are supported")

type IngesterQuerier struct {
	cfg    Config
	logger log.Logger

	ringClient RingClient

	registerer             prometheus.Registerer
	ingesterQuerierMetrics *ingesterQuerierMetrics
}

func NewIngesterQuerier(
	cfg Config,
	ringClient RingClient,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*IngesterQuerier, error) {
	return &IngesterQuerier{
		logger:                 log.With(logger, "component", "pattern-ingester-querier"),
		ringClient:             ringClient,
		cfg:                    cfg,
		registerer:             prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer),
		ingesterQuerierMetrics: newIngesterQuerierMetrics(registerer, metricsNamespace),
	}, nil
}

func (q *IngesterQuerier) Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	_, err := syntax.ParseMatchers(req.Query, true)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	resps, err := q.forAllIngesters(ctx, func(_ context.Context, client logproto.PatternClient) (interface{}, error) {
		return client.Query(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	iterators := make([]pattern_iter.Iterator, len(resps))
	for i := range resps {
		iterators[i] = pattern_iter.NewQueryClientIterator(resps[i].response.(logproto.Pattern_QueryClient))
	}
	// TODO(kolesnikovae): Incorporate with pruning
	resp, err := pattern_iter.ReadBatch(pattern_iter.NewMerge(iterators...), math.MaxInt32)
	if err != nil {
		return nil, err
	}
	return prunePatterns(resp, minClusterSize, q.ingesterQuerierMetrics), nil
}

func (q *IngesterQuerier) Samples(
	ctx context.Context,
	req *logproto.QuerySamplesRequest,
) (*logproto.QuerySamplesResponse, error) {
	expr, err := syntax.ParseSampleExpr(req.Query)
	if err != nil {
		return nil, err
	}

	var selector syntax.LogSelectorExpr
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		selector, err = e.Selector()
	case *syntax.RangeAggregationExpr:
		selector, err = e.Selector()
	default:
		return nil, ErrParseQuery
	}

	if err != nil {
		return nil, err
	}

	if selector == nil || selector.HasFilter() {
		return nil, ErrParseQuery
	}

	iterators, err := q.querySample(ctx, req)
	if err != nil {
		return nil, err
	}

	// TODO(twhitney): what should batch size be here?
	resp, err := pattern_iter.ReadMetricsBatch(pattern_iter.NewSumMergeSampleIterator(iterators), math.MaxInt32, q.logger)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (q *IngesterQuerier) querySample(ctx context.Context, req *logproto.QuerySamplesRequest) ([]loki_iter.SampleIterator, error) {
	resps, err := q.forAllIngesters(ctx, func(_ context.Context, client logproto.PatternClient) (interface{}, error) {
		return client.QuerySample(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	level.Debug(q.logger).Log("msg", "queried patterns ingesters for metric samples",
		"query", req.Query,
		"num_responses", len(resps))

	iterators := make([]loki_iter.SampleIterator, len(resps))
	for i := range resps {
		iterators[i] = pattern_iter.NewQuerySamplesClientIterator(resps[i].response.(logproto.Pattern_QuerySampleClient), q.logger)
	}
	return iterators, nil
}

func prunePatterns(resp *logproto.QueryPatternsResponse, minClusterSize int, metrics *ingesterQuerierMetrics) *logproto.QueryPatternsResponse {
	pruneConfig := drain.DefaultConfig()
	pruneConfig.SimTh = 1.0 // Merge & de-dup patterns but don't modify them

	patternsBefore := len(resp.Series)
	d := drain.New(pruneConfig, "", nil)
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
			logproto.NewPatternSeries(pattern, cluster.Samples()))
	}
	metrics.patternsPrunedTotal.Add(float64(patternsBefore - len(resp.Series)))
	metrics.patternsRetainedTotal.Add(float64(len(resp.Series)))
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
