package querier

import (
	"context"
	"math"
	"sort"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
)

// BlockQueryable is a storage.Queryable implementation for blocks storage
type BlockQueryable struct {
	services.Service

	us *BucketStoresService
}

// NewBlockQueryable returns a client to query a block store
func NewBlockQueryable(cfg tsdb.Config, logLevel logging.Level, registerer prometheus.Registerer) (*BlockQueryable, error) {
	util.WarnExperimentalUse("Blocks storage engine")
	bucketClient, err := tsdb.NewBucketClient(context.Background(), cfg, "cortex-bucket-stores", util.Logger)
	if err != nil {
		return nil, err
	}

	if registerer != nil {
		bucketClient = objstore.BucketWithMetrics( /* bucket label value */ "", bucketClient, prometheus.WrapRegistererWithPrefix("cortex_querier_", registerer))
	}

	us, err := NewBucketStoresService(cfg, bucketClient, logLevel, util.Logger, registerer)
	if err != nil {
		return nil, err
	}

	b := &BlockQueryable{us: us}
	b.Service = services.NewIdleService(b.starting, b.stopping)

	return b, nil
}

func (b *BlockQueryable) starting(ctx context.Context) error {
	return errors.Wrap(services.StartAndAwaitRunning(ctx, b.us), "failed to start BucketStoresService")
}

func (b *BlockQueryable) stopping(_ error) error {
	return errors.Wrap(services.StopAndAwaitTerminated(context.Background(), b.us), "stopping BucketStoresService")
}

// Querier returns a new Querier on the storage.
func (b *BlockQueryable) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	if s := b.State(); s != services.Running {
		return nil, promql.ErrStorage{Err: errors.Errorf("BlockQueryable is not running: %v", s)}
	}

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, promql.ErrStorage{Err: err}
	}

	return &blocksQuerier{
		ctx:        ctx,
		mint:       mint,
		maxt:       maxt,
		userID:     userID,
		userStores: b.us,
	}, nil
}

type blocksQuerier struct {
	ctx        context.Context
	mint, maxt int64
	userID     string
	userStores *BucketStoresService
}

func (b *blocksQuerier) Select(sp *storage.SelectParams, matchers ...*labels.Matcher) (storage.SeriesSet, storage.Warnings, error) {
	return b.SelectSorted(sp, matchers...)
}

func (b *blocksQuerier) SelectSorted(sp *storage.SelectParams, matchers ...*labels.Matcher) (storage.SeriesSet, storage.Warnings, error) {
	log, ctx := spanlogger.New(b.ctx, "blocksQuerier.Select")
	defer log.Span.Finish()

	mint, maxt := b.mint, b.maxt
	if sp != nil {
		mint, maxt = sp.Start, sp.End
	}
	converted := convertMatchersToLabelMatcher(matchers)

	// Returned series are sorted.
	// No processing of responses is done here. Dealing with multiple responses
	// for the same series and overlapping chunks is done in blockQuerierSeriesSet.
	series, warnings, err := b.userStores.Series(ctx, b.userID, &storepb.SeriesRequest{
		MinTime:                 mint,
		MaxTime:                 maxt,
		Matchers:                converted,
		PartialResponseStrategy: storepb.PartialResponseStrategy_ABORT,
	})
	if err != nil {
		return nil, nil, promql.ErrStorage{Err: err}
	}

	return &blockQuerierSeriesSet{
		series: series,
	}, warnings, nil
}

func convertMatchersToLabelMatcher(matchers []*labels.Matcher) []storepb.LabelMatcher {
	var converted []storepb.LabelMatcher
	for _, m := range matchers {
		var t storepb.LabelMatcher_Type
		switch m.Type {
		case labels.MatchEqual:
			t = storepb.LabelMatcher_EQ
		case labels.MatchNotEqual:
			t = storepb.LabelMatcher_NEQ
		case labels.MatchRegexp:
			t = storepb.LabelMatcher_RE
		case labels.MatchNotRegexp:
			t = storepb.LabelMatcher_NRE
		}

		converted = append(converted, storepb.LabelMatcher{
			Type:  t,
			Name:  m.Name,
			Value: m.Value,
		})
	}
	return converted
}

func (b *blocksQuerier) LabelValues(name string) ([]string, storage.Warnings, error) {
	// Cortex doesn't use this. It will ask ingesters for metadata.
	return nil, nil, errors.New("not implemented")
}

func (b *blocksQuerier) LabelNames() ([]string, storage.Warnings, error) {
	// Cortex doesn't use this. It will ask ingesters for metadata.
	return nil, nil, errors.New("not implemented")
}

func (b *blocksQuerier) Close() error {
	// nothing to do here.
	return nil
}

// Implementation of storage.SeriesSet, based on individual responses from store client.
type blockQuerierSeriesSet struct {
	series []*storepb.Series

	// next response to process
	next int

	currLabels []storepb.Label
	currChunks []storepb.AggrChunk
}

func (bqss *blockQuerierSeriesSet) Next() bool {
	bqss.currChunks = nil
	bqss.currLabels = nil

	if bqss.next >= len(bqss.series) {
		return false
	}

	bqss.currLabels = bqss.series[bqss.next].Labels
	bqss.currChunks = bqss.series[bqss.next].Chunks

	bqss.next++

	// Merge chunks for current series. Chunks may come in multiple responses, but as soon
	// as the response has chunks for a new series, we can stop searching. Series are sorted.
	// See documentation for StoreClient.Series call for details.
	for bqss.next < len(bqss.series) && storepb.CompareLabels(bqss.currLabels, bqss.series[bqss.next].Labels) == 0 {
		bqss.currChunks = append(bqss.currChunks, bqss.series[bqss.next].Chunks...)
		bqss.next++
	}

	return true
}

func (bqss *blockQuerierSeriesSet) At() storage.Series {
	if bqss.currLabels == nil {
		return nil
	}

	return newBlockQuerierSeries(bqss.currLabels, bqss.currChunks)
}

func (bqss *blockQuerierSeriesSet) Err() error {
	return nil
}

func newBlockQuerierSeries(lbls []storepb.Label, chunks []storepb.AggrChunk) *blockQuerierSeries {
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].MinTime < chunks[j].MinTime
	})

	b := labels.NewBuilder(nil)
	for _, l := range lbls {
		// Ignore external label set by the shipper
		if l.Name != tsdb.TenantIDExternalLabel {
			b.Set(l.Name, l.Value)
		}
	}

	return &blockQuerierSeries{labels: b.Labels(), chunks: chunks}
}

type blockQuerierSeries struct {
	labels labels.Labels
	chunks []storepb.AggrChunk
}

func (bqs *blockQuerierSeries) Labels() labels.Labels {
	return bqs.labels
}

func (bqs *blockQuerierSeries) Iterator() storage.SeriesIterator {
	if len(bqs.chunks) == 0 {
		// should not happen in practice, but we have a unit test for it
		return series.NewErrIterator(errors.New("no chunks"))
	}

	its := make([]chunkenc.Iterator, 0, len(bqs.chunks))

	for _, c := range bqs.chunks {
		ch, err := chunkenc.FromData(chunkenc.EncXOR, c.Raw.Data)
		if err != nil {
			return series.NewErrIterator(errors.Wrapf(err, "failed to initialize chunk from XOR encoded raw data (series: %v min time: %d max time: %d)", bqs.Labels(), c.MinTime, c.MaxTime))
		}

		it := ch.Iterator(nil)
		its = append(its, it)
	}

	return newBlockQuerierSeriesIterator(bqs.Labels(), its)
}

func newBlockQuerierSeriesIterator(labels labels.Labels, its []chunkenc.Iterator) *blockQuerierSeriesIterator {
	return &blockQuerierSeriesIterator{labels: labels, iterators: its, lastT: math.MinInt64}
}

// blockQuerierSeriesIterator implements a series iterator on top
// of a list of time-sorted, non-overlapping chunks.
type blockQuerierSeriesIterator struct {
	// only used for error reporting
	labels labels.Labels

	iterators []chunkenc.Iterator
	i         int
	lastT     int64
}

func (it *blockQuerierSeriesIterator) Seek(t int64) bool {
	// We generally expect the chunks already to be cut down
	// to the range we are interested in. There's not much to be gained from
	// hopping across chunks so we just call next until we reach t.
	for {
		ct, _ := it.At()
		if ct >= t {
			return true
		}
		if !it.Next() {
			return false
		}
	}
}

func (it *blockQuerierSeriesIterator) At() (int64, float64) {
	if it.i >= len(it.iterators) {
		return 0, 0
	}

	t, v := it.iterators[it.i].At()
	it.lastT = t
	return t, v
}

func (it *blockQuerierSeriesIterator) Next() bool {
	if it.i >= len(it.iterators) {
		return false
	}

	if it.iterators[it.i].Next() {
		return true
	}
	if it.iterators[it.i].Err() != nil {
		return false
	}

	for {
		it.i++

		if it.i >= len(it.iterators) {
			return false
		}

		// we must advance iterator first, to see if it has any samples.
		// Seek will call At() as its first operation.
		if !it.iterators[it.i].Next() {
			if it.iterators[it.i].Err() != nil {
				return false
			}

			// Found empty iterator without error, skip it.
			continue
		}

		// Chunks are guaranteed to be ordered but not generally guaranteed to not overlap.
		// We must ensure to skip any overlapping range between adjacent chunks.
		return it.Seek(it.lastT + 1)
	}
}

func (it *blockQuerierSeriesIterator) Err() error {
	if it.i >= len(it.iterators) {
		return nil
	}

	err := it.iterators[it.i].Err()
	if err != nil {
		return promql.ErrStorage{Err: errors.Wrapf(err, "cannot iterate chunk for series: %v", it.labels)}
	}
	return nil
}
