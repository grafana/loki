package pattern

import (
	"context"
	"math"
	"net/http"
	"sort"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
)

// TODO(kolesnikovae): parametrise QueryPatternsRequest
const (
	minClusterSize = 30
	maxPatterns    = 300
)

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
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}
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
	// TODO(kolesnikovae): Incorporate with pruning
	resp, err := iter.ReadBatch(iter.NewMerge(iterators...), math.MaxInt32)
	if err != nil {
		return nil, err
	}
	return prunePatterns(resp, minClusterSize, q.ingesterQuerierMetrics), nil
}

func prunePatterns(resp *logproto.QueryPatternsResponse, minClusterSize int64, metrics *ingesterQuerierMetrics) *logproto.QueryPatternsResponse {
	patternsBefore := len(resp.Series)
	total := make([]int64, len(resp.Series))

	for i, p := range resp.Series {
		for _, s := range p.Samples {
			total[i] += s.Value
		}
	}

	// Create a slice of structs to keep Series and total together
	type SeriesWithTotal struct {
		Series *logproto.PatternSeries
		Total  int64
	}

	seriesWithTotals := make([]SeriesWithTotal, len(resp.Series))
	for i := range resp.Series {
		seriesWithTotals[i] = SeriesWithTotal{
			Series: resp.Series[i],
			Total:  total[i],
		}
	}

	// Sort the slice of structs by the Total field
	sort.Slice(seriesWithTotals, func(i, j int) bool {
		return seriesWithTotals[i].Total > seriesWithTotals[j].Total
	})

	// Initialize a variable to keep track of the position for valid series
	pos := 0

	// Iterate over the seriesWithTotals
	for i := range seriesWithTotals {
		if seriesWithTotals[i].Total >= minClusterSize {
			// Place the valid series at the current position
			resp.Series[pos] = seriesWithTotals[i].Series
			pos++
		}
	}

	// Slice the resp.Series to include only the valid series
	resp.Series = resp.Series[:pos]

	if len(resp.Series) > maxPatterns {
		resp.Series = resp.Series[:maxPatterns]
	}

	metrics.patternsPrunedTotal.Add(float64(patternsBefore - len(resp.Series)))
	metrics.patternsRetainedTotal.Add(float64(len(resp.Series)))

	return resp
}

// ForAllIngesters runs f, in parallel, for all ingesters
func (q *IngesterQuerier) forAllIngesters(ctx context.Context, f func(context.Context, logproto.PatternClient) (interface{}, error)) ([]ResponseFromIngesters, error) {
	replicationSet, err := q.ringClient.Ring().GetAllHealthy(ring.Read)
	if err != nil {
		return nil, err
	}

	return q.forGivenIngesters(ctx, replicationSet, f)
}

type ResponseFromIngesters struct {
	addr     string
	response interface{}
}

func (q *IngesterQuerier) forGivenIngesters(ctx context.Context, replicationSet ring.ReplicationSet, f func(context.Context, logproto.PatternClient) (interface{}, error)) ([]ResponseFromIngesters, error) {
	g, ctx := errgroup.WithContext(ctx)
	responses := make([]ResponseFromIngesters, len(replicationSet.Instances))

	for i, ingester := range replicationSet.Instances {
		ingester := ingester
		i := i
		g.Go(func() error {
			client, err := q.ringClient.GetClientFor(ingester.Addr)
			if err != nil {
				return err
			}

			resp, err := f(ctx, client.(logproto.PatternClient))
			if err != nil {
				return err
			}
			responses[i] = ResponseFromIngesters{addr: ingester.Addr, response: resp}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return responses, nil
}
