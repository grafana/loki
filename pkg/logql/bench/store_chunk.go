package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	targetChunkSize = 1572864
	blockSize       = 256 * 1024
)

type ChunkStore struct {
	store     *storage.LokiStore
	periodCfg config.PeriodConfig
	chunks    map[string]*chunkenc.MemChunk
	tenantID  string
}

func NewChunkStore(dir, tenantID string) (*ChunkStore, error) {
	// Create store-specific directory
	storeDir := filepath.Join(dir, "chunks")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", storeDir, err)
	}
	indexDir := filepath.Join(storeDir, "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create index directory %s: %w", indexDir, err)
	}
	storeConfig := storage.Config{
		MaxChunkBatchSize: 50,
		TSDBShipperConfig: indexshipper.Config{
			ActiveIndexDirectory: indexDir,
			Mode:                 indexshipper.ModeReadWrite,
			IngesterName:         "test",
			CacheLocation:        filepath.Join(storeDir, "index_cache"),
			ResyncInterval:       5 * time.Minute,
			CacheTTL:             24 * time.Hour,
		},
		FSConfig: local.FSConfig{Directory: storeDir},
	}
	period := config.PeriodConfig{
		From:       config.DayTime{Time: model.Earliest},
		IndexType:  "tsdb",
		ObjectType: "filesystem",
		Schema:     "v13",
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: "index_",
				Period: time.Hour * 24,
			},
		},
		RowShards: 16,
	}
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			period,
		},
	}
	cm := storage.NewClientMetrics()
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	overrides, _ := validation.NewOverrides(limits, nil)

	store, err := storage.NewStore(storeConfig, config.ChunkStoreConfig{}, schemaCfg, overrides, cm, prometheus.DefaultRegisterer, util_log.Logger, "cortex")
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}
	return &ChunkStore{
		store:     store,
		periodCfg: period,
		tenantID:  tenantID,
		chunks:    make(map[string]*chunkenc.MemChunk),
	}, nil
}

func (s *ChunkStore) Write(ctx context.Context, streams []logproto.Stream) error {
	for _, stream := range streams {
		chunkEnc, ok := s.chunks[stream.Labels]
		if !ok {
			chunkEnc = newMemChunk()
			s.chunks[stream.Labels] = chunkEnc
		}

		for _, entry := range stream.Entries {
			if !chunkEnc.SpaceFor(&entry) {
				if err := s.flushChunk(ctx, chunkEnc, stream.Labels); err != nil {
					return err
				}
				chunkEnc = newMemChunk()
				s.chunks[stream.Labels] = chunkEnc
			}
			if _, err := chunkEnc.Append(&entry); err != nil {
				return err
			}
		}
	}
	return nil
}

func newMemChunk() *chunkenc.MemChunk {
	return chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.Snappy, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, blockSize, targetChunkSize)
}

func (s *ChunkStore) flushChunk(ctx context.Context, memChunk *chunkenc.MemChunk, labelsString string) error {
	lbs, err := syntax.ParseLabels(labelsString)
	if err != nil {
		return err
	}
	labelsBuilder := labels.NewBuilder(lbs)
	labelsBuilder.Set(labels.MetricName, "logs")
	metric := labelsBuilder.Labels()
	fp := client.Fingerprint(lbs)

	from, to := memChunk.Bounds()
	c := chunk.NewChunk(s.tenantID, fp, metric, chunkenc.NewFacade(memChunk, 0, 0), model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(to.UnixNano()))
	if err := c.Encode(); err != nil {
		return err
	}
	return s.store.Put(ctx, []chunk.Chunk{c})
}

// Querier implements Store
func (s *ChunkStore) Querier() (logql.Querier, error) {
	return s.store, nil
}

// Name implements Store
func (s *ChunkStore) Name() string {
	return "chunk"
}

// Close flushes any remaining data and closes resources
func (s *ChunkStore) Close() error {
	for labelsString, chunk := range s.chunks {
		if err := s.flushChunk(context.Background(), chunk, labelsString); err != nil {
			return err
		}
	}
	clear(s.chunks)
	s.store.Stop()
	return nil
}
