package storage

import (
	"context"
	"sort"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
)

type genericIterator interface {
	Next() bool
	Labels() string
	Error() error
	Close() error
}

type chunksIteratorFactory func(chunks []*LazyChunk, from, through time.Time, nextChunk *LazyChunk) (genericIterator, error)

// batchChunkIterator is an EntryIterator that iterates through chunks by batch of `batchSize`.
// Since chunks can overlap across batches for each iteration the iterator will keep all overlapping
// chunks with the next chunk from the next batch and added it to the next iteration. In this case the boundaries of the batch
// is reduced to non-overlapping chunks boundaries.
type batchChunkIterator struct {
	chunks          lazyChunks
	batchSize       int
	err             error
	curr            genericIterator
	lastOverlapping []*LazyChunk
	iterFactory     chunksIteratorFactory

	cancel     context.CancelFunc
	start, end time.Time
	direction  logproto.Direction
	next       chan *struct {
		iter genericIterator
		err  error
	}
}

// newBatchChunkIterator creates a new batch iterator with the given batchSize.
func newBatchChunkIterator(
	ctx context.Context,
	chunks []*LazyChunk,
	batchSize int,
	direction logproto.Direction,
	start, end time.Time,
	iterFactory chunksIteratorFactory,
) *batchChunkIterator {

	ctx, cancel := context.WithCancel(ctx)
	res := &batchChunkIterator{
		batchSize: batchSize,

		start:       start,
		end:         end,
		direction:   direction,
		cancel:      cancel,
		iterFactory: iterFactory,
		chunks:      lazyChunks{direction: direction, chunks: chunks},
		next: make(chan *struct {
			iter genericIterator
			err  error
		}),
	}
	sort.Sort(res.chunks)
	go res.loop(ctx)
	return res
}

func (it *batchChunkIterator) loop(ctx context.Context) {
	for {
		if it.chunks.Len() == 0 {
			close(it.next)
			return
		}
		next, err := it.nextBatch()
		select {
		case <-ctx.Done():
			close(it.next)
			// next can be nil if we are waiting to return that the nextBatch was empty and the context is closed
			// or if another error occurred reading nextBatch
			if next == nil {
				return
			}
			err = next.Close()
			if err != nil {
				level.Error(util.WithContext(ctx, util.Logger)).Log("msg", "Failed to close the pre-fetched iterator when pre-fetching was canceled", "err", err)
			}
			return
		case it.next <- &struct {
			iter genericIterator
			err  error
		}{next, err}:
		}
	}
}

func (it *batchChunkIterator) Next() bool {
	var err error
	// for loop to avoid recursion
	for {
		if it.curr != nil && it.curr.Next() {
			return true
		}
		// close previous iterator
		if it.curr != nil {
			it.err = it.curr.Close()
		}
		next := <-it.next
		if next == nil {
			return false
		}
		it.curr = next.iter
		if next.err != nil {
			it.err = err
			return false
		}
	}
}

func (it *batchChunkIterator) nextBatch() (genericIterator, error) {
	// the first chunk of the batch
	headChunk := it.chunks.Peek()
	from, through := it.start, it.end
	batch := make([]*LazyChunk, 0, it.batchSize+len(it.lastOverlapping))
	var nextChunk *LazyChunk

	for it.chunks.Len() > 0 {

		// pop the next batch of chunks and append/prepend previous overlapping chunks
		// so we can merge/de-dupe overlapping entries.
		if it.direction == logproto.FORWARD {
			batch = append(batch, it.lastOverlapping...)
		}
		batch = append(batch, it.chunks.pop(it.batchSize)...)
		if it.direction == logproto.BACKWARD {
			batch = append(batch, it.lastOverlapping...)
		}

		if it.chunks.Len() > 0 {
			nextChunk = it.chunks.Peek()
			// we max out our iterator boundaries to the next chunks in the queue
			// so that overlapping chunks are together
			if it.direction == logproto.BACKWARD {
				from = time.Unix(0, nextChunk.Chunk.Through.UnixNano())

				// we have to reverse the inclusivity of the chunk iterator from
				// [from, through) to (from, through] for backward queries, except when
				// the batch's `from` is equal to the query's Start. This can be achieved
				// by shifting `from` by one nanosecond.
				if !from.Equal(it.start) {
					from = from.Add(time.Nanosecond)
				}
			} else {
				through = time.Unix(0, nextChunk.Chunk.From.UnixNano())
			}
			// we save all overlapping chunks as they are also needed in the next batch to properly order entries.
			// If we have chunks like below:
			//      ┌──────────────┐
			//      │     # 47     │
			//      └──────────────┘
			//          ┌──────────────────────────┐
			//          │           # 48           |
			//          └──────────────────────────┘
			//              ┌──────────────┐
			//              │     # 49     │
			//              └──────────────┘
			//                        ┌────────────────────┐
			//                        │        # 50        │
			//                        └────────────────────┘
			//
			//  And nextChunk is # 49, we need to keep references to #47 and #48 as they won't be
			//  iterated over completely (we're clipping through to #49's from) and then add them to the next batch.

		}

		if it.direction == logproto.BACKWARD {
			through = time.Unix(0, headChunk.Chunk.Through.UnixNano())

			if through.After(it.end) {
				through = it.end
			}

			// we have to reverse the inclusivity of the chunk iterator from
			// [from, through) to (from, through] for backward queries, except when
			// the batch's `through` is equal to the query's End. This can be achieved
			// by shifting `through` by one nanosecond.
			if !through.Equal(it.end) {
				through = through.Add(time.Nanosecond)
			}
		} else {
			from = time.Unix(0, headChunk.Chunk.From.UnixNano())

			// when clipping the from it should never be before the start or equal to the end.
			// Doing so would include entries not requested.
			if from.Before(it.start) || from.Equal(it.end) {
				from = it.start
			}
		}

		// it's possible that the current batch and the next batch are fully overlapping in which case
		// we should keep adding more items until the batch boundaries difference is positive.
		if through.Sub(from) > 0 {
			break
		}
	}

	if it.chunks.Len() > 0 {
		it.lastOverlapping = it.lastOverlapping[:0]
		for _, c := range batch {
			if c.IsOverlapping(nextChunk, it.direction) {
				it.lastOverlapping = append(it.lastOverlapping, c)
			}
		}
	}
	// create the new chunks iterator from the current batch.
	return it.iterFactory(batch, from, through, nextChunk)
}

func (it *batchChunkIterator) Labels() string {
	return it.curr.Labels()
}

func (it *batchChunkIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	if it.curr != nil {
		return it.curr.Error()
	}
	return nil
}

func (it *batchChunkIterator) Close() error {
	it.cancel()
	if it.curr != nil {
		return it.curr.Close()
	}
	return nil
}

type labelCache map[model.Fingerprint]string

// computeLabels compute the labels string representation, uses a map to cache result per fingerprint.
func (l labelCache) computeLabels(c *LazyChunk) string {
	if lbs, ok := l[c.Chunk.Fingerprint]; ok {
		return lbs
	}
	lbs := dropLabels(c.Chunk.Metric, labels.MetricName).String()
	l[c.Chunk.Fingerprint] = lbs
	return lbs
}

type logBatchIterator struct {
	*batchChunkIterator

	ctx      context.Context
	matchers []*labels.Matcher
	filter   logql.LineFilter
	labels   labelCache
}

func newLogBatchIterator(
	ctx context.Context,
	chunks []*LazyChunk,
	batchSize int,
	matchers []*labels.Matcher,
	filter logql.LineFilter,
	direction logproto.Direction,
	start, end time.Time,
) (iter.EntryIterator, error) {
	// __name__ is not something we filter by because it's a constant in loki
	// and only used for upstream compatibility; therefore remove it.
	// The same applies to the sharding label which is injected by the cortex storage code.
	matchers = removeMatchersByName(matchers, labels.MetricName, astmapper.ShardLabel)
	logbatch := &logBatchIterator{
		labels:   map[model.Fingerprint]string{},
		matchers: matchers,
		filter:   filter,
		ctx:      ctx,
	}
	batch := newBatchChunkIterator(ctx, chunks, batchSize, direction, start, end, logbatch.newChunksIterator)
	logbatch.batchChunkIterator = batch
	return logbatch, nil
}

func (it *logBatchIterator) Entry() logproto.Entry {
	return it.curr.(iter.EntryIterator).Entry()
}

// newChunksIterator creates an iterator over a set of lazychunks.
func (it *logBatchIterator) newChunksIterator(chunks []*LazyChunk, from, through time.Time, nextChunk *LazyChunk) (genericIterator, error) {
	chksBySeries, err := fetchChunkBySeries(it.ctx, chunks, it.matchers)
	if err != nil {
		return nil, err
	}

	iters, err := it.buildIterators(chksBySeries, from, through, nextChunk)
	if err != nil {
		return nil, err
	}

	return iter.NewHeapIterator(it.ctx, iters, it.direction), nil
}

func (it *logBatchIterator) buildIterators(chks map[model.Fingerprint][][]*LazyChunk, from, through time.Time, nextChunk *LazyChunk) ([]iter.EntryIterator, error) {
	result := make([]iter.EntryIterator, 0, len(chks))
	for _, chunks := range chks {
		iterator, err := it.buildHeapIterator(chunks, from, through, nextChunk)
		if err != nil {
			return nil, err
		}
		result = append(result, iterator)
	}

	return result, nil
}

func (it *logBatchIterator) buildHeapIterator(chks [][]*LazyChunk, from, through time.Time, nextChunk *LazyChunk) (iter.EntryIterator, error) {
	result := make([]iter.EntryIterator, 0, len(chks))

	// __name__ is only used for upstream compatibility and is hardcoded within loki. Strip it from the return label set.
	labels := it.labels.computeLabels(chks[0][0])
	for i := range chks {
		iterators := make([]iter.EntryIterator, 0, len(chks[i]))
		for j := range chks[i] {
			if !chks[i][j].IsValid {
				continue
			}
			iterator, err := chks[i][j].Iterator(it.ctx, from, through, it.direction, it.filter, nextChunk)
			if err != nil {
				return nil, err
			}
			iterators = append(iterators, iterator)
		}
		if it.direction == logproto.BACKWARD {
			for i, j := 0, len(iterators)-1; i < j; i, j = i+1, j-1 {
				iterators[i], iterators[j] = iterators[j], iterators[i]
			}
		}
		result = append(result, iter.NewNonOverlappingIterator(iterators, labels))
	}

	return iter.NewHeapIterator(it.ctx, result, it.direction), nil
}

type sampleBatchIterator struct {
	*batchChunkIterator

	ctx       context.Context
	matchers  []*labels.Matcher
	filter    logql.LineFilter
	extractor logql.SampleExtractor
	labels    labelCache
}

func newSampleBatchIterator(
	ctx context.Context,
	chunks []*LazyChunk,
	batchSize int,
	matchers []*labels.Matcher,
	filter logql.LineFilter,
	extractor logql.SampleExtractor,
	start, end time.Time,
) (iter.SampleIterator, error) {
	// __name__ is not something we filter by because it's a constant in loki
	// and only used for upstream compatibility; therefore remove it.
	// The same applies to the sharding label which is injected by the cortex storage code.
	matchers = removeMatchersByName(matchers, labels.MetricName, astmapper.ShardLabel)

	samplebatch := &sampleBatchIterator{
		labels:    map[model.Fingerprint]string{},
		matchers:  matchers,
		filter:    filter,
		extractor: extractor,
		ctx:       ctx,
	}
	batch := newBatchChunkIterator(ctx, chunks, batchSize, logproto.FORWARD, start, end, samplebatch.newChunksIterator)
	samplebatch.batchChunkIterator = batch
	return samplebatch, nil
}

func (it *sampleBatchIterator) Sample() logproto.Sample {
	return it.curr.(iter.SampleIterator).Sample()
}

// newChunksIterator creates an iterator over a set of lazychunks.
func (it *sampleBatchIterator) newChunksIterator(chunks []*LazyChunk, from, through time.Time, nextChunk *LazyChunk) (genericIterator, error) {
	chksBySeries, err := fetchChunkBySeries(it.ctx, chunks, it.matchers)
	if err != nil {
		return nil, err
	}
	iters, err := it.buildIterators(chksBySeries, from, through, nextChunk)
	if err != nil {
		return nil, err
	}

	return iter.NewHeapSampleIterator(it.ctx, iters), nil
}

func (it *sampleBatchIterator) buildIterators(chks map[model.Fingerprint][][]*LazyChunk, from, through time.Time, nextChunk *LazyChunk) ([]iter.SampleIterator, error) {
	result := make([]iter.SampleIterator, 0, len(chks))
	for _, chunks := range chks {
		iterator, err := it.buildHeapIterator(chunks, from, through, nextChunk)
		if err != nil {
			return nil, err
		}
		result = append(result, iterator)
	}

	return result, nil
}

func (it *sampleBatchIterator) buildHeapIterator(chks [][]*LazyChunk, from, through time.Time, nextChunk *LazyChunk) (iter.SampleIterator, error) {
	result := make([]iter.SampleIterator, 0, len(chks))

	// __name__ is only used for upstream compatibility and is hardcoded within loki. Strip it from the return label set.
	labels := it.labels.computeLabels(chks[0][0])
	for i := range chks {
		iterators := make([]iter.SampleIterator, 0, len(chks[i]))
		for j := range chks[i] {
			if !chks[i][j].IsValid {
				continue
			}
			iterator, err := chks[i][j].SampleIterator(it.ctx, from, through, it.filter, it.extractor, nextChunk)
			if err != nil {
				return nil, err
			}
			iterators = append(iterators, iterator)
		}

		result = append(result, iter.NewNonOverlappingSampleIterator(iterators, labels))
	}

	return iter.NewHeapSampleIterator(it.ctx, result), nil
}

func removeMatchersByName(matchers []*labels.Matcher, names ...string) []*labels.Matcher {
	for _, omit := range names {
		for i := range matchers {
			if matchers[i].Name == omit {
				matchers = append(matchers[:i], matchers[i+1:]...)
				break
			}
		}
	}
	return matchers
}

func fetchChunkBySeries(ctx context.Context, chunks []*LazyChunk, matchers []*labels.Matcher) (map[model.Fingerprint][][]*LazyChunk, error) {
	chksBySeries := partitionBySeriesChunks(chunks)

	// Make sure the initial chunks are loaded. This is not one chunk
	// per series, but rather a chunk per non-overlapping iterator.
	if err := loadFirstChunks(ctx, chksBySeries); err != nil {
		return nil, err
	}

	// Now that we have the first chunk for each series loaded,
	// we can proceed to filter the series that don't match.
	chksBySeries = filterSeriesByMatchers(chksBySeries, matchers)

	var allChunks []*LazyChunk
	for _, series := range chksBySeries {
		for _, chunks := range series {
			allChunks = append(allChunks, chunks...)
		}
	}

	// Finally we load all chunks not already loaded
	if err := fetchLazyChunks(ctx, allChunks); err != nil {
		return nil, err
	}
	return chksBySeries, nil
}

func filterSeriesByMatchers(chks map[model.Fingerprint][][]*LazyChunk, matchers []*labels.Matcher) map[model.Fingerprint][][]*LazyChunk {
outer:
	for fp, chunks := range chks {
		for _, matcher := range matchers {
			if !matcher.Matches(chunks[0][0].Chunk.Metric.Get(matcher.Name)) {
				delete(chks, fp)
				continue outer
			}
		}
	}
	return chks
}

func fetchLazyChunks(ctx context.Context, chunks []*LazyChunk) error {
	log, ctx := spanlogger.New(ctx, "LokiStore.fetchLazyChunks")
	defer log.Finish()
	start := time.Now()
	storeStats := stats.GetStoreData(ctx)
	var totalChunks int64
	defer func() {
		storeStats.ChunksDownloadTime += time.Since(start)
		storeStats.TotalChunksDownloaded += totalChunks
	}()

	chksByFetcher := map[*chunk.Fetcher][]*LazyChunk{}
	for _, c := range chunks {
		if c.Chunk.Data == nil {
			chksByFetcher[c.Fetcher] = append(chksByFetcher[c.Fetcher], c)
			totalChunks++
		}
	}
	if len(chksByFetcher) == 0 {
		return nil
	}
	level.Debug(log).Log("msg", "loading lazy chunks", "chunks", totalChunks)

	errChan := make(chan error)
	for fetcher, chunks := range chksByFetcher {
		go func(fetcher *chunk.Fetcher, chunks []*LazyChunk) {
			keys := make([]string, 0, len(chunks))
			chks := make([]chunk.Chunk, 0, len(chunks))
			index := make(map[string]*LazyChunk, len(chunks))

			// FetchChunks requires chunks to be ordered by external key.
			sort.Slice(chunks, func(i, j int) bool { return chunks[i].Chunk.ExternalKey() < chunks[j].Chunk.ExternalKey() })
			for _, chk := range chunks {
				key := chk.Chunk.ExternalKey()
				keys = append(keys, key)
				chks = append(chks, chk.Chunk)
				index[key] = chk
			}
			chks, err := fetcher.FetchChunks(ctx, chks, keys)
			if err != nil {
				if isInvalidChunkError(err) {
					level.Error(util.Logger).Log("msg", "checksum of chunks does not match", "err", chunk.ErrInvalidChecksum)
					errChan <- nil
					return
				}
				errChan <- err
				return

			}
			// assign fetched chunk by key as FetchChunks doesn't guarantee the order.
			for _, chk := range chks {
				index[chk.ExternalKey()].Chunk = chk
			}

			errChan <- nil
		}(fetcher, chunks)
	}

	var lastErr error
	for i := 0; i < len(chksByFetcher); i++ {
		if err := <-errChan; err != nil {
			lastErr = err
		}
	}

	if lastErr != nil {
		return lastErr
	}

	for _, c := range chunks {
		if c.Chunk.Data != nil {
			c.IsValid = true
		}
	}
	return nil
}

func isInvalidChunkError(err error) bool {
	err = errors.Cause(err)
	if err, ok := err.(promql.ErrStorage); ok {
		return err.Err == chunk.ErrInvalidChecksum || err.Err == chunkenc.ErrInvalidChecksum
	}
	return false
}

func loadFirstChunks(ctx context.Context, chks map[model.Fingerprint][][]*LazyChunk) error {
	var toLoad []*LazyChunk
	for _, lchks := range chks {
		for _, lchk := range lchks {
			if len(lchk) == 0 {
				continue
			}
			toLoad = append(toLoad, lchk[0])
		}
	}
	return fetchLazyChunks(ctx, toLoad)
}

func partitionBySeriesChunks(chunks []*LazyChunk) map[model.Fingerprint][][]*LazyChunk {
	chunksByFp := map[model.Fingerprint][]*LazyChunk{}
	for _, c := range chunks {
		fp := c.Chunk.Fingerprint
		chunksByFp[fp] = append(chunksByFp[fp], c)
	}
	result := make(map[model.Fingerprint][][]*LazyChunk, len(chunksByFp))

	for fp, chks := range chunksByFp {
		result[fp] = partitionOverlappingChunks(chks)
	}

	return result
}

// partitionOverlappingChunks splits the list of chunks into different non-overlapping lists.
// todo this might reverse the order.
func partitionOverlappingChunks(chunks []*LazyChunk) [][]*LazyChunk {
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].Chunk.From < chunks[j].Chunk.From
	})

	css := [][]*LazyChunk{}
outer:
	for _, c := range chunks {
		for i, cs := range css {
			// If the chunk doesn't overlap with the current list, then add it to it.
			if cs[len(cs)-1].Chunk.Through.Before(c.Chunk.From) {
				css[i] = append(css[i], c)
				continue outer
			}
		}
		// If the chunk overlaps with every existing list, then create a new list.
		cs := make([]*LazyChunk, 0, len(chunks)/(len(css)+1))
		cs = append(cs, c)
		css = append(css, cs)
	}

	return css
}

// dropLabels returns a new label set with certain labels dropped
func dropLabels(ls labels.Labels, removals ...string) (dst labels.Labels) {
	toDel := make(map[string]struct{})
	for _, r := range removals {
		toDel[r] = struct{}{}
	}

	for _, l := range ls {
		_, remove := toDel[l.Name]
		if !remove {
			dst = append(dst, l)
		}
	}

	return dst
}
