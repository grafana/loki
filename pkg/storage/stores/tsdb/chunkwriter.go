package tsdb

import (
	"context"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/series"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

type IndexWriter interface {
	Append(userID string, ls labels.Labels, chks index.ChunkMetas) error
}

type ChunkWriter struct {
	schemaCfg   config.SchemaConfig
	fetcher     *fetcher.Fetcher
	indexWriter IndexWriter
}

func NewChunkWriter(
	fetcher *fetcher.Fetcher,
	pd config.PeriodConfig,
	indexWriter IndexWriter,
) *ChunkWriter {
	return &ChunkWriter{
		schemaCfg: config.SchemaConfig{
			Configs: []config.PeriodConfig{pd},
		},
		fetcher:     fetcher,
		indexWriter: indexWriter,
	}
}

func (w *ChunkWriter) Put(ctx context.Context, chunks []chunk.Chunk) error {
	for _, chunk := range chunks {
		if err := w.PutOne(ctx, chunk.From, chunk.Through, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (w *ChunkWriter) PutOne(ctx context.Context, from, through model.Time, chk chunk.Chunk) error {
	log, ctx := spanlogger.New(ctx, "TSDBStore.PutOne")
	defer log.Finish()

	// with local TSDB indices, we _always_ write the index entry
	// to avoid data loss if we lose an ingester's disk
	// but we can skip writing the chunk if another replica
	// has already written it to storage.
	writeChunk := true

	// If this chunk is in cache it must already be in the database so we don't need to write it again
	found, _, _, _ := w.fetcher.Cache().Fetch(ctx, []string{w.schemaCfg.ExternalKey(chk.ChunkRef)})

	if len(found) > 0 {
		writeChunk = false
		series.DedupedChunksTotal.Inc()
	}

	chunks := []chunk.Chunk{chk}

	c := w.fetcher.Client()
	if writeChunk {
		if err := c.PutChunks(ctx, chunks); err != nil {
			return errors.Wrap(err, "writing chunk")
		}
	}

	// Always write the index to benefit durability via replication factor.
	metas := index.ChunkMetas{
		{
			Checksum: chk.ChunkRef.Checksum,
			MinTime:  int64(chk.ChunkRef.From),
			MaxTime:  int64(chk.ChunkRef.Through),
			KB:       uint32(chk.Size()) / (1 << 10),
			Entries:  uint32(chk.Data.Entries()),
		},
	}
	if err := w.indexWriter.Append(chk.UserID, chk.Metric, metas); err != nil {
		return errors.Wrap(err, "writing index entry")
	}

	if writeChunk {
		if cacheErr := w.fetcher.WriteBackCache(ctx, chunks); cacheErr != nil {
			level.Warn(log).Log("msg", "could not store chunks in chunk cache", "err", cacheErr)
		}
	}

	return nil
}
