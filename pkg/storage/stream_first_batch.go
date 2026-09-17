package storage

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util"
)

// newStreamFirstSampleBatchIterator returns a sample iterator over chunks in stream-first order.
//
// Chunks are fetched ahead of the consumer by a bounded, ordered preloader. The consumer decodes
// one stream at a time and releases that stream's compressed data once it is exhausted. So the
// compressed working set stays bounded to the preload window plus the stream being decoded.
//
// Stream identity is the TSDB index fingerprint from each chunk's ref. That fingerprint is
// available before the chunk is fetched; a chunk's Metric labels are not. This equals the
// ingester's labels.StableHash of the raw stream labels in the normal case: both are the xxhash
// of the same labels. It diverges only under the fingerprint-mapper's 64-bit collision
// remapping. Closing that gap needs the raw labels plumbed from the index before the fetch,
// which this reader does not do.
func newStreamFirstSampleBatchIterator(
	ctx context.Context,
	schemas config.SchemaConfig,
	metrics *ChunkMetrics,
	chunks []*LazyChunk,
	batchSize int,
	matchers []*labels.Matcher,
	start, end time.Time,
	chunkFilterer chunk.Filterer,
	maxConcurrentBatches int,
	fetch chunkFetchFunc,
	extractor syntax.SampleExtractor,
) (iter.SampleIterator, error) {
	byFingerprint := partitionBySeriesChunks(chunks)

	type streamFirstChunks struct {
		hash   uint64
		chunks []*LazyChunk
	}

	// partitionBySeriesChunks partitions each stream's chunks into non-overlapping runs, not into a
	// single From-ascending list, so flattening them here does not give a From-ascending order.
	// That is fine: the per-stream decode below re-sorts and re-dedups this stream's chunks anyway,
	// the same way the timestamp-first path does.
	groups := make([]streamFirstChunks, 0, len(byFingerprint))
	for fp, seriesChunks := range byFingerprint {
		var flat []*LazyChunk
		for _, cs := range seriesChunks {
			flat = append(flat, cs...)
		}
		if len(flat) == 0 {
			continue
		}
		groups = append(groups, streamFirstChunks{hash: uint64(fp), chunks: flat})
	}

	// Order streams by the fingerprint each will expose as StreamHash, so the querier's
	// stream-first merge can align this source's streams against the others.
	sort.Slice(groups, func(i, j int) bool { return groups[i].hash < groups[j].hash })

	// Flatten the chunks in stream order and record each stream's end offset in that flat list.
	// The preloader fetches the flat list in this order. A batch boundary may fall mid-stream, so
	// streamEndIndexes tells the consumer exactly when a stream's chunks are all fetched.
	var (
		streamChunkLists = make([][]*LazyChunk, len(groups))
		streamEndIndexes = make([]int, len(groups))
		streamHashes     = make([]uint64, len(groups))
		flatChunks       = make([]*LazyChunk, 0, len(chunks))
	)
	for i := range groups {
		streamChunkLists[i] = groups[i].chunks
		streamHashes[i] = groups[i].hash
		flatChunks = append(flatChunks, groups[i].chunks...)
		streamEndIndexes[i] = len(flatChunks)
	}

	batcher := newStreamFirstChunkBatcher(flatChunks, batchSize)
	loader := newStreamFirstBatchLoader(schemas, metrics, fetch)
	preloader := newStreamFirstChunkPreloader(ctx, batcher, loader, maxConcurrentBatches)

	return &lazyStreamFirstSampleIterator{
		ctx:              ctx,
		schemas:          schemas,
		metrics:          metrics,
		batchSize:        batchSize,
		matchers:         matchers,
		start:            start,
		end:              end,
		chunkFilterer:    chunkFilterer,
		extractor:        extractor,
		streamChunks:     streamChunkLists,
		streamEndIndexes: streamEndIndexes,
		streamHashes:     streamHashes,
		preloader:        preloader,
		idx:              -1,
	}, nil
}

// lazyStreamFirstSampleIterator concatenates per-stream sample iterators, built lazily via
// newTimestampFirstSampleBatchIterator, so the overall output is stream-first. The preloader
// fetches chunks ahead of the consumer. The consumer builds a stream's iterator only once that
// stream's chunks are preloaded, so decoding never triggers a foreground fetch.
type lazyStreamFirstSampleIterator struct {
	ctx           context.Context
	schemas       config.SchemaConfig
	metrics       *ChunkMetrics
	batchSize     int
	matchers      []*labels.Matcher
	start, end    time.Time
	chunkFilterer chunk.Filterer
	extractor     syntax.SampleExtractor

	// streamChunks, streamEndIndexes and streamHashes are parallel, indexed by stream in stream-first
	// (streamHash ascending) order. streamChunks[i] is stream i's chunks.
	streamChunks [][]*LazyChunk

	// streamEndIndexes[i] is the exclusive end offset of stream i's chunks in the flattened chunk
	// list the preloader fetches.
	streamEndIndexes []int

	// streamHashes[i] is stream i's fingerprint, exposed as StreamHash while it is decoded.
	streamHashes []uint64

	preloader   *streamFirstChunkPreloader
	fetchedUpTo int
	idx         int
	cur         iter.SampleIterator
	err         error
}

func (it *lazyStreamFirstSampleIterator) Next() bool {
	// A repeat call after an error must stay false and must not overwrite it.err with a later
	// stream's outcome, including a nil on success.
	if it.err != nil {
		return false
	}

	for {
		if it.cur != nil {
			if it.cur.Next() {
				return true
			}

			// Err first: it also reports a canceled context, which
			// timestampFirstSampleBatchIterator.Close does not. Fall back to Close's own return
			// only when Err saw nothing, since Close can still fail on a sub-iterator Next never
			// reached.
			it.err = it.cur.Err()
			closeErr := it.cur.Close()
			if it.err == nil {
				it.err = closeErr
			} else if closeErr != nil {
				// it.err already reports the real cause. Log this one instead of dropping it, so
				// a genuine close-time failure is not lost just because it lost the race to be
				// the error Err() reports.
				util.LogErrorWithContext(it.ctx, "closing stream-first per-stream sample iterator", func() error { return closeErr })
			}
			it.releaseStream(it.idx)
			it.cur = nil
			if it.err != nil {
				return false
			}
		}

		it.idx++
		if it.idx >= len(it.streamChunks) {
			return false
		}

		// Wait for this stream's chunks to be fetched before building its iterator, so the
		// per-stream fetch path below sees Data already populated and does no foreground I/O.
		if !it.waitUntilFetched(it.streamEndIndexes[it.idx]) {
			return false
		}

		cur, err := newTimestampFirstSampleBatchIterator(
			it.ctx, it.schemas, it.metrics, it.streamChunks[it.idx], it.batchSize,
			it.matchers, it.start, it.end, it.chunkFilterer, it.extractor)
		if err != nil {
			it.err = err
			return false
		}
		it.cur = cur
	}
}

// waitUntilFetched blocks, pulling preloaded batches, until at least upTo chunks (in stream order)
// are fetched. It returns immediately if that many are already fetched, and false on an error or
// cancellation.
func (it *lazyStreamFirstSampleIterator) waitUntilFetched(upTo int) bool {
	for it.fetchedUpTo < upTo {
		start := time.Now()
		ok := it.preloader.Next()
		if !ok {
			// The preloader is seeded with every chunk, and upTo never exceeds that count.
			// Exhaustion alone would already have satisfied the loop above. Reaching here means a
			// fetch error or a canceled context. The default case guards against neither: a bug
			// where the preloader under-delivered without reporting either.
			switch {
			case it.preloader.Err() != nil:
				it.err = it.preloader.Err()
			case it.ctx.Err() != nil:
				it.err = it.ctx.Err()
			default:
				it.err = fmt.Errorf("stream-first reader: preloader stopped after fetching %d chunks, short of the %d required, without an error or cancellation", it.fetchedUpTo, upTo)
			}
			return false
		}
		// Observe only a real wait for a batch, not a canceled or failed one above: those
		// durations aren't a consumer waiting on prefetch to keep up, and would skew the metric.
		it.metrics.streamFirstConsumerWait.Observe(time.Since(start).Seconds())
		it.fetchedUpTo += len(it.preloader.At())
	}

	return true
}

// releaseStream frees the compressed Data of a fully-consumed stream. The compressed working set
// then stays bounded to the in-flight preload batches plus the stream being decoded. It does
// not grow with every chunk fetched so far. Chunks are never shared across streams, so this is
// safe once the stream's iterator has closed.
//
// It does not clear LazyChunk.overlappingSampleBlocks, the decoded-sample cache for blocks that
// straddle a chunk boundary. Those caches live on the caller's chunks and accumulate across
// streams for the whole query, the same as on the default timestamp-first path.
func (it *lazyStreamFirstSampleIterator) releaseStream(idx int) {
	if idx < 0 || idx >= len(it.streamChunks) {
		return
	}
	for _, c := range it.streamChunks[idx] {
		c.Chunk.Data = nil
	}
	it.streamChunks[idx] = nil
}

func (it *lazyStreamFirstSampleIterator) At() logproto.Sample {
	if it.cur == nil {
		return logproto.Sample{}
	}
	return it.cur.At()
}

func (it *lazyStreamFirstSampleIterator) Labels() string {
	if it.cur == nil {
		return ""
	}
	return it.cur.Labels()
}

// StreamHash returns the current stream's fingerprint, the identity streams are ordered by.
func (it *lazyStreamFirstSampleIterator) StreamHash() uint64 {
	if it.idx < 0 || it.idx >= len(it.streamHashes) {
		return 0
	}
	return it.streamHashes[it.idx]
}

func (it *lazyStreamFirstSampleIterator) Err() error {
	if it.err != nil {
		return it.err
	}
	return it.preloader.Err()
}

// Close stops the preloader's background goroutines and closes the current stream's
// sub-iterator. It is safe to call more than once.
//
// Close must not run concurrently with an active Next call. To interrupt a Next call blocked
// on the preloader from another goroutine, cancel the iterator context instead.
//
// Close returns the current stream's own close error, not Err. A caller that stops reading
// early because of a real error already has it from Err.
func (it *lazyStreamFirstSampleIterator) Close() error {
	it.preloader.Close()

	var err error
	if it.cur != nil {
		// Close the current iterator. It waits for its own sub-iterator's goroutine to stop,
		// but that is normally quick: waitUntilFetched already guaranteed this stream's chunks are
		// fetched before cur ever existed, so cur's own fetch calls have nothing real left to do.
		err = it.cur.Close()
		it.cur = nil
	}
	return err
}
