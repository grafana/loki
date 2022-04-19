package series

import (
	"context"
	"fmt"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

var (
	DedupedChunksTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "chunk_store_deduped_chunks_total",
		Help:      "Count of chunks which were not stored because they have already been stored by another replica.",
	})

	IndexEntriesPerChunk = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "chunk_store_index_entries_per_chunk",
		Help:      "Number of entries written to storage per chunk.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 5),
	})
)

type IndexWriter interface {
	NewWriteBatch() index.WriteBatch
	BatchWrite(context.Context, index.WriteBatch) error
}

type SchemaWrites interface {
	GetChunkWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]index.Entry, error)
	GetCacheKeysAndLabelWriteEntries(from, through model.Time, userID string, metricName string, labels labels.Labels, chunkID string) ([]string, [][]index.Entry, error)
}

type Writer struct {
	writeDedupeCache          cache.Cache
	schemaCfg                 config.SchemaConfig
	DisableIndexDeduplication bool

	indexWriter IndexWriter
	schema      SchemaWrites
	fetcher     *fetcher.Fetcher
}

func NewWriter(fetcher *fetcher.Fetcher, schemaCfg config.SchemaConfig, indexWriter IndexWriter, schema SchemaWrites, writeDedupeCache cache.Cache, disableIndexDeduplication bool) *Writer {
	return &Writer{
		writeDedupeCache:          writeDedupeCache,
		schemaCfg:                 schemaCfg,
		DisableIndexDeduplication: disableIndexDeduplication,
		fetcher:                   fetcher,
		indexWriter:               indexWriter,
		schema:                    schema,
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
	log, ctx := spanlogger.New(ctx, "SeriesStore.PutOne")
	defer log.Finish()
	writeChunk := true

	// If this chunk is in cache it must already be in the database so we don't need to write it again
	found, _, _, _ := c.fetcher.Cache().Fetch(ctx, []string{c.schemaCfg.ExternalKey(chk.ChunkRef)})

	if len(found) > 0 {
		writeChunk = false
		DedupedChunksTotal.Inc()
	}

	// If we dont have to write the chunk and DisableIndexDeduplication is false, we do not have to do anything.
	// If we dont have to write the chunk and DisableIndexDeduplication is true, we have to write index and not chunk.
	// Otherwise write both index and chunk.
	if !writeChunk && !c.DisableIndexDeduplication {
		return nil
	}

	chunks := []chunk.Chunk{chk}

	writeReqs, keysToCache, err := c.calculateIndexEntries(ctx, from, through, chk)
	if err != nil {
		return err
	}

	if oic, ok := c.fetcher.Client().(client.ObjectAndIndexClient); ok {
		chunks := chunks
		if !writeChunk {
			chunks = []chunk.Chunk{}
		}
		if err = oic.PutChunksAndIndex(ctx, chunks, writeReqs); err != nil {
			return err
		}
	} else {
		// chunk not found, write it.
		if writeChunk {
			err := c.fetcher.Client().PutChunks(ctx, chunks)
			if err != nil {
				return err
			}
		}
		if err := c.indexWriter.BatchWrite(ctx, writeReqs); err != nil {
			return err
		}
	}

	// we already have the chunk in the cache so don't write it back to the cache.
	if writeChunk {
		if cacheErr := c.fetcher.WriteBackCache(ctx, chunks); cacheErr != nil {
			level.Warn(log).Log("msg", "could not store chunks in chunk cache", "err", cacheErr)
		}
	}

	bufs := make([][]byte, len(keysToCache))
	err = c.writeDedupeCache.Store(ctx, keysToCache, bufs)
	if err != nil {
		level.Warn(log).Log("msg", "could not Store store in write dedupe cache", "err", err)
	}
	return nil
}

// calculateIndexEntries creates a set of batched WriteRequests for all the chunks it is given.
func (c *Writer) calculateIndexEntries(ctx context.Context, from, through model.Time, chunk chunk.Chunk) (index.WriteBatch, []string, error) {
	seenIndexEntries := map[string]struct{}{}
	entries := []index.Entry{}

	metricName := chunk.Metric.Get(labels.MetricName)
	if metricName == "" {
		return nil, nil, fmt.Errorf("no MetricNameLabel for chunk")
	}

	keys, labelEntries, err := c.schema.GetCacheKeysAndLabelWriteEntries(from, through, chunk.UserID, metricName, chunk.Metric, c.schemaCfg.ExternalKey(chunk.ChunkRef))
	if err != nil {
		return nil, nil, err
	}
	_, _, missing, _ := c.writeDedupeCache.Fetch(ctx, keys)
	// keys and labelEntries are matched in order, but Fetch() may
	// return missing keys in any order so check against all of them.
	for _, missingKey := range missing {
		for i, key := range keys {
			if key == missingKey {
				entries = append(entries, labelEntries[i]...)
			}
		}
	}

	chunkEntries, err := c.schema.GetChunkWriteEntries(from, through, chunk.UserID, metricName, chunk.Metric, c.schemaCfg.ExternalKey(chunk.ChunkRef))
	if err != nil {
		return nil, nil, err
	}
	entries = append(entries, chunkEntries...)

	IndexEntriesPerChunk.Observe(float64(len(entries)))

	// Remove duplicate entries based on tableName:hashValue:rangeValue
	result := c.indexWriter.NewWriteBatch()
	for _, entry := range entries {
		key := fmt.Sprintf("%s:%s:%x", entry.TableName, entry.HashValue, entry.RangeValue)
		if _, ok := seenIndexEntries[key]; !ok {
			seenIndexEntries[key] = struct{}{}
			result.Add(entry.TableName, entry.HashValue, entry.RangeValue, entry.Value)
		}
	}

	return result, missing, nil
}
