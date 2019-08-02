package storage

import (
	"context"
	"sort"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// lazyChunks is a slice of lazy chunks that can ordered by chunk boundaries
// in ascending or descending depending on the direction
type lazyChunks struct {
	chunks    []*chunkenc.LazyChunk
	direction logproto.Direction
}

func (l lazyChunks) Len() int                  { return len(l.chunks) }
func (l lazyChunks) Swap(i, j int)             { l.chunks[i], l.chunks[j] = l.chunks[j], l.chunks[i] }
func (l lazyChunks) Peek() *chunkenc.LazyChunk { return l.chunks[0] }
func (l lazyChunks) Less(i, j int) bool {
	if l.direction == logproto.FORWARD {
		t1, t2 := l.chunks[i].Chunk.From, l.chunks[j].Chunk.From
		if !t1.Equal(t2) {
			return t1.Before(t2)
		}
		return l.chunks[i].Chunk.Fingerprint < l.chunks[j].Chunk.Fingerprint
	}
	t1, t2 := l.chunks[i].Chunk.Through, l.chunks[j].Chunk.Through
	if !t1.Equal(t2) {
		return t1.After(t2)
	}
	return l.chunks[i].Chunk.Fingerprint > l.chunks[j].Chunk.Fingerprint
}

// pop returns the top `count` lazychunks, the original slice is splitted an copied
// to avoid retaining chunks in the slice backing array.
func (l *lazyChunks) pop(count int) []*chunkenc.LazyChunk {
	if len(l.chunks) <= count {
		old := l.chunks
		l.chunks = nil
		return old
	}
	// split slices into two new ones and copy parts to each so we don't keep old reference
	res := make([]*chunkenc.LazyChunk, count)
	copy(res, l.chunks[0:count])
	new := make([]*chunkenc.LazyChunk, len(l.chunks)-count)
	copy(new, l.chunks[count:len(l.chunks)])
	l.chunks = new
	return res
}

// batchChunkIterator is an EntryIterator that iterates through chunks by batch of `batchSize`.
// Since chunks can overlap across batches for each iteration the iterator will keep all overlapping
// chunks with the next chunk from the next batch and added it to the next iteration. In this case the boundaries of the batch
// is reduced to non-overlapping chunks boundaries.
type batchChunkIterator struct {
	chunks          lazyChunks
	batchSize       int
	err             error
	curr            iter.EntryIterator
	lastOverlapping []*chunkenc.LazyChunk

	ctx      context.Context
	matchers []*labels.Matcher
	filter   logql.Filter
	req      *logproto.QueryRequest
}

// newBatchChunkIterator creates a new batch iterator with the given batchSize.
func newBatchChunkIterator(ctx context.Context, chunks []*chunkenc.LazyChunk, batchSize int, matchers []*labels.Matcher, filter logql.Filter, req *logproto.QueryRequest) *batchChunkIterator {
	res := &batchChunkIterator{
		batchSize: batchSize,
		matchers:  matchers,
		filter:    filter,
		req:       req,
		ctx:       ctx,
		chunks:    lazyChunks{direction: req.Direction, chunks: chunks},
	}
	sort.Sort(res.chunks)
	return res
}

func (it *batchChunkIterator) Next() bool {
	var err error
	// for loop to avoid recursion
	for {
		if it.curr != nil && it.curr.Next() {
			return true
		}
		if it.chunks.Len() == 0 {
			return false
		}
		// close previous iterator
		if it.curr != nil {
			it.err = it.curr.Close()
		}
		it.curr, err = it.nextBatch()
		if err != nil {
			it.err = err
			return false
		}
	}
}

func (it *batchChunkIterator) nextBatch() (iter.EntryIterator, error) {
	// pop the next batch of chunks and append/preprend previous overlapping chunks
	// so we can merge/de-dupe overlapping entries.
	batch := make([]*chunkenc.LazyChunk, 0, it.batchSize+len(it.lastOverlapping))
	if it.req.Direction == logproto.FORWARD {
		batch = append(batch, it.lastOverlapping...)
	}
	batch = append(batch, it.chunks.pop(it.batchSize)...)
	if it.req.Direction == logproto.BACKWARD {
		batch = append(batch, it.lastOverlapping...)
	}

	from, through := it.req.Start, it.req.End
	if it.chunks.Len() > 0 {
		nextChunk := it.chunks.Peek()
		// we max out our iterator boundaries to the next chunks in the queue
		// so that overlapping chunks are together
		if it.req.Direction == logproto.BACKWARD {
			from = time.Unix(0, nextChunk.Chunk.Through.UnixNano())
		} else {
			through = time.Unix(0, nextChunk.Chunk.From.UnixNano())
		}
		// we save all overlapping chunks as they are also needed in the next batch to properly order entries.
		// If we have chunks like below:
		//      ┌──────────────┐
		//      │     # 47     │
		//      └──────────────┘
		//          ┌──────────────────────────┐
		//          │           # 48           │
		//          └──────────────────────────┘
		//              ┌──────────────┐
		//              │     # 49     │
		//              └──────────────┘
		//                        ┌────────────────────┐
		//                        │        # 50        │
		//                        └────────────────────┘
		//
		//  And nextChunk is # 49, we need to keep references to #47 and #48 as they won't be
		//  iterated over completely (we're clipping through to #49's from)  and then add them to the next batch.
		it.lastOverlapping = it.lastOverlapping[:0]
		for _, c := range batch {
			if it.req.Direction == logproto.BACKWARD {
				if c.Chunk.From.Before(nextChunk.Chunk.Through) || c.Chunk.From == nextChunk.Chunk.Through {
					it.lastOverlapping = append(it.lastOverlapping, c)
				}
			} else {
				if !c.Chunk.Through.Before(nextChunk.Chunk.From) {
					it.lastOverlapping = append(it.lastOverlapping, c)
				}
			}
		}
	} else {
		if len(it.lastOverlapping) > 0 {
			if it.req.Direction == logproto.BACKWARD {
				through = time.Unix(0, it.lastOverlapping[0].Chunk.From.UnixNano())
			} else {
				from = time.Unix(0, it.lastOverlapping[0].Chunk.Through.UnixNano())
			}
		}
	}

	// create the new chunks iterator from the current batch.
	return newChunksIterator(it.ctx, batch, it.matchers, it.filter, it.req.Direction, from, through)
}

func (it *batchChunkIterator) Entry() logproto.Entry {
	return it.curr.Entry()
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
	if it.curr != nil {
		return it.curr.Close()
	}
	return nil
}

// newChunksIterator creates an iterator over a set of lazychunks.
func newChunksIterator(ctx context.Context, chunks []*chunkenc.LazyChunk, matchers []*labels.Matcher, filter logql.Filter, direction logproto.Direction, from, through time.Time) (iter.EntryIterator, error) {
	chksBySeries := partitionBySeriesChunks(chunks)

	// Make sure the initial chunks are loaded. This is not one chunk
	// per series, but rather a chunk per non-overlapping iterator.
	if err := loadFirstChunks(ctx, chksBySeries); err != nil {
		return nil, err
	}

	// Now that we have the first chunk for each series loaded,
	// we can proceed to filter the series that don't match.
	chksBySeries = filterSeriesByMatchers(chksBySeries, matchers)

	var allChunks []*chunkenc.LazyChunk
	for _, series := range chksBySeries {
		for _, chunks := range series {
			allChunks = append(allChunks, chunks...)
		}
	}

	// Finally we load all chunks not already loaded
	if err := fetchLazyChunks(ctx, allChunks); err != nil {
		return nil, err
	}

	iters, err := buildIterators(ctx, chksBySeries, filter, direction, from, through)
	if err != nil {
		return nil, err
	}

	return iter.NewHeapIterator(iters, direction), nil
}

func buildIterators(ctx context.Context, chks map[model.Fingerprint][][]*chunkenc.LazyChunk, filter logql.Filter, direction logproto.Direction, from, through time.Time) ([]iter.EntryIterator, error) {
	result := make([]iter.EntryIterator, 0, len(chks))
	for _, chunks := range chks {
		iterator, err := buildHeapIterator(ctx, chunks, filter, direction, from, through)
		if err != nil {
			return nil, err
		}
		result = append(result, iterator)
	}

	return result, nil
}

func buildHeapIterator(ctx context.Context, chks [][]*chunkenc.LazyChunk, filter logql.Filter, direction logproto.Direction, from, through time.Time) (iter.EntryIterator, error) {
	result := make([]iter.EntryIterator, 0, len(chks))
	if chks[0][0].Chunk.Metric.Has("__name__") {
		labelsBuilder := labels.NewBuilder(chks[0][0].Chunk.Metric)
		labelsBuilder.Del("__name__")
		chks[0][0].Chunk.Metric = labelsBuilder.Labels()
	}
	labels := chks[0][0].Chunk.Metric.String()

	for i := range chks {
		iterators := make([]iter.EntryIterator, 0, len(chks[i]))
		for j := range chks[i] {
			iterator, err := chks[i][j].Iterator(ctx, from, through, direction, filter)
			if err != nil {
				return nil, err
			}
			iterators = append(iterators, iterator)
		}
		if direction == logproto.BACKWARD {
			for i, j := 0, len(iterators)-1; i < j; i, j = i+1, j-1 {
				iterators[i], iterators[j] = iterators[j], iterators[i]
			}
		}
		result = append(result, iter.NewNonOverlappingIterator(iterators, labels))
	}

	return iter.NewHeapIterator(result, direction), nil
}

func filterSeriesByMatchers(chks map[model.Fingerprint][][]*chunkenc.LazyChunk, matchers []*labels.Matcher) map[model.Fingerprint][][]*chunkenc.LazyChunk {
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

func fetchLazyChunks(ctx context.Context, chunks []*chunkenc.LazyChunk) error {
	log, ctx := spanlogger.New(ctx, "LokiStore.fetchLazyChunks")
	defer log.Finish()

	var totalChunks int
	chksByFetcher := map[*chunk.Fetcher][]*chunkenc.LazyChunk{}
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
		go func(fetcher *chunk.Fetcher, chunks []*chunkenc.LazyChunk) {

			keys := make([]string, 0, len(chunks))
			chks := make([]chunk.Chunk, 0, len(chunks))
			index := make(map[string]*chunkenc.LazyChunk, len(chunks))

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

	return lastErr
}

func loadFirstChunks(ctx context.Context, chks map[model.Fingerprint][][]*chunkenc.LazyChunk) error {
	var toLoad []*chunkenc.LazyChunk
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

func partitionBySeriesChunks(chunks []*chunkenc.LazyChunk) map[model.Fingerprint][][]*chunkenc.LazyChunk {
	chunksByFp := map[model.Fingerprint][]*chunkenc.LazyChunk{}
	for _, c := range chunks {
		fp := c.Chunk.Fingerprint
		chunksByFp[fp] = append(chunksByFp[fp], c)
	}
	result := make(map[model.Fingerprint][][]*chunkenc.LazyChunk, len(chunksByFp))

	for fp, chks := range chunksByFp {
		result[fp] = partitionOverlappingChunks(chks)
	}

	return result
}

// partitionOverlappingChunks splits the list of chunks into different non-overlapping lists.
// todo this might reverse the order.
func partitionOverlappingChunks(chunks []*chunkenc.LazyChunk) [][]*chunkenc.LazyChunk {
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].Chunk.From < chunks[j].Chunk.From
	})

	css := [][]*chunkenc.LazyChunk{}
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
		cs := make([]*chunkenc.LazyChunk, 0, len(chunks)/(len(css)+1))
		cs = append(cs, c)
		css = append(css, cs)
	}

	return css
}
