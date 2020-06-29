package querier

import (
	"context"
	"sort"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/prom1/storage/metric"
	"github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/chunkcompat"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
)

// Distributor is the read interface to the distributor, made an interface here
// to reduce package coupling.
type Distributor interface {
	Query(ctx context.Context, from, to model.Time, matchers ...*labels.Matcher) (model.Matrix, error)
	QueryStream(ctx context.Context, from, to model.Time, matchers ...*labels.Matcher) (*client.QueryStreamResponse, error)
	LabelValuesForLabelName(context.Context, model.LabelName) ([]string, error)
	LabelNames(context.Context) ([]string, error)
	MetricsForLabelMatchers(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]metric.Metric, error)
	MetricsMetadata(ctx context.Context) ([]scrape.MetricMetadata, error)
}

func newDistributorQueryable(distributor Distributor, streaming bool, iteratorFn chunkIteratorFunc, queryIngesterWithin time.Duration) QueryableWithFilter {
	return distributorQueryable{
		distributor:         distributor,
		streaming:           streaming,
		iteratorFn:          iteratorFn,
		queryIngesterWithin: queryIngesterWithin,
	}
}

type distributorQueryable struct {
	distributor         Distributor
	streaming           bool
	iteratorFn          chunkIteratorFunc
	queryIngesterWithin time.Duration
}

func (d distributorQueryable) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return &distributorQuerier{
		distributor: d.distributor,
		ctx:         ctx,
		mint:        mint,
		maxt:        maxt,
		streaming:   d.streaming,
		chunkIterFn: d.iteratorFn,
	}, nil
}

func (d distributorQueryable) UseQueryable(now time.Time, _, queryMaxT int64) bool {
	// Include ingester only if maxt is within QueryIngestersWithin w.r.t. current time.
	return d.queryIngesterWithin == 0 || queryMaxT >= util.TimeToMillis(now.Add(-d.queryIngesterWithin))
}

type distributorQuerier struct {
	distributor Distributor
	ctx         context.Context
	mint, maxt  int64
	streaming   bool
	chunkIterFn chunkIteratorFunc
}

// Select implements storage.Querier interface.
// The bool passed is ignored because the series is always sorted.
func (q *distributorQuerier) Select(_ bool, sp *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	log, ctx := spanlogger.New(q.ctx, "distributorQuerier.Select")
	defer log.Span.Finish()

	// Kludge: Prometheus passes nil SelectParams if it is doing a 'series' operation,
	// which needs only metadata.
	if sp == nil {
		ms, err := q.distributor.MetricsForLabelMatchers(ctx, model.Time(q.mint), model.Time(q.maxt), matchers...)
		if err != nil {
			return storage.ErrSeriesSet(err)
		}
		return series.MetricsToSeriesSet(ms)
	}

	mint, maxt := sp.Start, sp.End

	if q.streaming {
		return q.streamingSelect(*sp, matchers)
	}

	matrix, err := q.distributor.Query(ctx, model.Time(mint), model.Time(maxt), matchers...)
	if err != nil {
		return storage.ErrSeriesSet(promql.ErrStorage{Err: err})
	}

	// Using MatrixToSeriesSet (and in turn NewConcreteSeriesSet), sorts the series.
	return series.MatrixToSeriesSet(matrix)
}

func (q *distributorQuerier) streamingSelect(sp storage.SelectHints, matchers []*labels.Matcher) storage.SeriesSet {
	userID, err := user.ExtractOrgID(q.ctx)
	if err != nil {
		return storage.ErrSeriesSet(promql.ErrStorage{Err: err})
	}

	mint, maxt := sp.Start, sp.End

	results, err := q.distributor.QueryStream(q.ctx, model.Time(mint), model.Time(maxt), matchers...)
	if err != nil {
		return storage.ErrSeriesSet(promql.ErrStorage{Err: err})
	}

	if len(results.Timeseries) != 0 {
		return newTimeSeriesSeriesSet(results.Timeseries)
	}

	serieses := make([]storage.Series, 0, len(results.Chunkseries))
	for _, result := range results.Chunkseries {
		// Sometimes the ingester can send series that have no data.
		if len(result.Chunks) == 0 {
			continue
		}

		ls := client.FromLabelAdaptersToLabels(result.Labels)
		sort.Sort(ls)

		chunks, err := chunkcompat.FromChunks(userID, ls, result.Chunks)
		if err != nil {
			return storage.ErrSeriesSet(promql.ErrStorage{Err: err})
		}

		series := &chunkSeries{
			labels:            ls,
			chunks:            chunks,
			chunkIteratorFunc: q.chunkIterFn,
		}
		serieses = append(serieses, series)
	}

	return series.NewConcreteSeriesSet(serieses)
}

func (q *distributorQuerier) LabelValues(name string) ([]string, storage.Warnings, error) {
	lv, err := q.distributor.LabelValuesForLabelName(q.ctx, model.LabelName(name))
	return lv, nil, err
}

func (q *distributorQuerier) LabelNames() ([]string, storage.Warnings, error) {
	ln, err := q.distributor.LabelNames(q.ctx)
	return ln, nil, err
}

func (q *distributorQuerier) Close() error {
	return nil
}
