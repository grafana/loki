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
)

// newStreamFirstSampleBatchIterator returns a sample iterator over the given chunks in stream-first
// order: samples grouped by stream and ordered by (streamHash ASC, timestamp ASC).
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
	groups := make([]streamFirstChunks, 0, len(byFingerprint))
	for fp, seriesChunks := range byFingerprint {
		var flat []*LazyChunk
		for _, cs := range seriesChunks {
			flat = append(flat, cs...)
		}
		if len(flat) == 0 {
			continue
		}
		// Identify each stream by its index fingerprint, which is available from the chunk ref
		// before the chunk is loaded (the chunk's Metric labels are not populated until then). This
		// equals labels.StableHash of the raw stream labels — the identity the ingester exposes and
		// the cross-source merge aligns on — because both are the xxhash of the same labels. They
		// can diverge only under the fp mapper's collision remapping (rare); fully closing that gap
		// would require the raw labels from the index, which are not available at this point.
		groups = append(groups, streamFirstChunks{hash: uint64(fp), chunks: flat})
	}

	// Order streams by the exposed streamHash so the querier's stream-first merge can align this
	// tier's streams with the others.
	sort.Slice(groups, func(i, j int) bool { return groups[i].hash < groups[j].hash })

	// Flatten the chunks in stream order and record each stream's end offset. The preloader fetches
	// this flat list in the order the consumer needs it; streamEndIndexes lets the consumer know when a
	// stream's chunks are all fetched (a batch boundary may fall mid-stream).
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
		streams:          streamChunkLists,
		streamEndIndexes: streamEndIndexes,
		streamHashes:     streamHashes,
		preloader:        preloader,
		idx:              -1,
	}, nil
}

// lazyStreamFirstSampleIterator concatenates per-stream sample iterators (built lazily via
// newTimestampFirstSampleBatchIterator) so the overall output is stream-first. Chunks are fetched ahead of the
// consumer by preloader; the consumer only builds a stream's iterator once that stream's chunks
// have been preloaded, so no fetch happens in the foreground.
type lazyStreamFirstSampleIterator struct {
	ctx           context.Context
	schemas       config.SchemaConfig
	metrics       *ChunkMetrics
	batchSize     int
	matchers      []*labels.Matcher
	start, end    time.Time
	chunkFilterer chunk.Filterer
	extractor     syntax.SampleExtractor

	// streams, streamEndIndexes and streamHashes are parallel, indexed by stream in stream-first
	// (streamHash ASC) order. streams[i] is stream i's chunks.
	streams [][]*LazyChunk

	// streamEndIndexes[i] is the exclusive end offset of stream i's chunks in the flattened chunk
	// list the preloader fetches.
	streamEndIndexes []int

	// streamHashes[i] is stream i's stable stream hash, exposed as StreamHash while it is decoded.
	// Deduplication is based on this hash. We use the stream hash — rather than the extractor's
	// reduced hash, which can collapse many streams into one — to keep a source's output monotonic
	// in (streamHash, timestamp) under label-reducing pushdown.
	streamHashes []uint64

	preloader   *streamFirstChunkPreloader
	fetchedUpTo int
	idx         int
	cur         iter.SampleIterator
	err         error
}

func (it *lazyStreamFirstSampleIterator) Next() bool {
	for {
		if it.cur != nil {
			if it.cur.Next() {
				return true
			}
			if err := it.cur.Err(); err != nil {
				it.err = err
			}
			_ = it.cur.Close()
			it.releaseStream(it.idx)
			it.cur = nil
			if it.err != nil {
				return false
			}
		}

		// Advance to the next stream.
		it.idx++
		if it.idx >= len(it.streams) {
			return false
		}

		// Wait for this stream's chunks to be fetched by the preloader before building its
		// iterator, so the per-stream fetch path sees Data != nil and does no foreground I/O.
		if !it.waitUntilFetched(it.streamEndIndexes[it.idx]) {
			return false
		}

		// Iterate the stream's logs in timestamp order.
		cur, err := newTimestampFirstSampleBatchIterator(
			it.ctx, it.schemas, it.metrics, it.streams[it.idx], it.batchSize,
			it.matchers, it.start, it.end, it.chunkFilterer, it.extractor)
		if err != nil {
			it.err = err
			return false
		}
		it.cur = cur
	}
}

// waitUntilFetched blocks, pulling preloaded batches, until at least upTo chunks (in stream order)
// have been fetched; it returns immediately if that many are already fetched. It returns false if
// an error occurred.
func (it *lazyStreamFirstSampleIterator) waitUntilFetched(upTo int) bool {
	for it.fetchedUpTo < upTo {
		// Wait until the next batch has been fetched (or an error occurred).
		start := time.Now()
		ok := it.preloader.Next()
		it.metrics.streamOrderedConsumerWait.Observe(time.Since(start).Seconds())

		if !ok {
			// The preloader stopped before delivering upTo chunks. It is seeded with every chunk and
			// upTo never exceeds that count, so a normal exhaustion would already have exited the
			// loop; reaching here means a fetch error, a cancelled context, or — if neither — a bug
			// where the preloader under-delivered silently.
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
		it.fetchedUpTo += len(it.preloader.At())
	}

	return true
}

// releaseStream frees the compressed Data of a fully-consumed stream so peak memory stays bounded
// to the in-flight batches plus the stream being decoded, rather than growing to every fetched
// chunk. Chunks are never shared across streams, so this is safe once the stream's iterator closed.
func (it *lazyStreamFirstSampleIterator) releaseStream(idx int) {
	if idx < 0 || idx >= len(it.streams) {
		return
	}
	for _, c := range it.streams[idx] {
		if c != nil {
			c.Chunk.Data = nil
		}
	}
	it.streams[idx] = nil
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

// StreamHash returns the current stream's raw stream hash (StableHash of its unreduced labels),
// which is what streams were sorted by.
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

func (it *lazyStreamFirstSampleIterator) Close() error {
	var err error
	if it.cur != nil {
		err = it.cur.Close()
		it.cur = nil
	}
	it.preloader.Close()
	return err
}
