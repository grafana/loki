package stores

import (
	"context"

	"github.com/go-kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

var (
	DedupedChunksTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "chunk_store_deduped_chunks_total",
		Help:      "Count of chunks which were not stored because they have already been stored by another replica.",
	})

	DedupedBytesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "chunk_store_deduped_bytes_total",
		Help:      "Count of bytes from chunks which were not stored because they have already been stored by another replica.",
	})

	IndexEntriesPerChunk = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Name:      "chunk_store_index_entries_per_chunk",
		Help:      "Number of entries written to storage per chunk.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 5),
	})
)

type Writer struct {
	schemaCfg                 config.SchemaConfig
	DisableIndexDeduplication bool

	indexWriter index.Writer
	fetcher     *fetcher.Fetcher
}

func NewChunkWriter(fetcher *fetcher.Fetcher, schemaCfg config.SchemaConfig, indexWriter index.Writer, disableIndexDeduplication bool) ChunkWriter {
	return &Writer{
		schemaCfg:                 schemaCfg,
		DisableIndexDeduplication: disableIndexDeduplication,
		fetcher:                   fetcher,
		indexWriter:               indexWriter,
	}
}

// Put implements Store
func (c *Writer) Put(ctx context.Context, chunks []chunk.Chunk) error {
	for _, chunk := range chunks {
		if err := c.PutOne(ctx, chunk.From, chunk.Through, chunk); err != nil {
			return err
		}
	}
	return nil
}

// PutOne implements Store
func (c *Writer) PutOne(ctx context.Context, from, through model.Time, chk chunk.Chunk) error {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "SeriesStore.PutOne")
	defer sp.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Finish()

	var (
		writeChunk = true
		overlap    bool
	)

	// always write the chunk if it spans multiple periods to ensure that it gets added to all the stores
	if chk.From < from || chk.Through > through {
		overlap = true
	}

	// If this chunk is in cache it must already be in the database so we don't need to write it again
	found, _, _, _ := c.fetcher.Cache().Fetch(ctx, []string{c.schemaCfg.ExternalKey(chk.ChunkRef)})

	if len(found) > 0 && !overlap {
		writeChunk = false
		DedupedChunksTotal.Inc()
		encoded, err := chk.Encoded()
		if err != nil {
			level.Error(log).Log("msg", "failed to encode chunk, cannot record compressed de-duped chunk size", "err", err)
		} else {
			DedupedBytesTotal.Add(float64(len(encoded)))
		}

	}

	// If we dont have to write the chunk and DisableIndexDeduplication is false, we do not have to do anything.
	// If we dont have to write the chunk and DisableIndexDeduplication is true, we have to write index and not chunk.
	// Otherwise write both index and chunk.
	if !writeChunk && !c.DisableIndexDeduplication {
		return nil
	}

	chunks := []chunk.Chunk{chk}

	// chunk not found, write it.
	if writeChunk {
		err := c.fetcher.Client().PutChunks(ctx, chunks)
		if err != nil {
			return err
		}
	}

	if err := c.indexWriter.IndexChunk(ctx, from, through, chk); err != nil {
		return err
	}

	// write chunk to the cache if it's not found.
	if len(found) == 0 {
		if cacheErr := c.fetcher.WriteBackCache(ctx, chunks); cacheErr != nil {
			level.Warn(log).Log("msg", "could not store chunks in chunk cache", "err", cacheErr)
		}
	}

	return nil
}
