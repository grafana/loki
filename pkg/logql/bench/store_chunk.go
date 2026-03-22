package bench

import (
	"context"
	"fmt"
	"io"
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
	objectclient "github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	targetChunkSize = 1572864
	blockSize       = 256 * 1024
)

type ChunkStore struct {
	store         *storage.LokiStore
	periodCfg     config.PeriodConfig
	chunks        map[string]*chunkenc.MemChunk
	tenantID      string
	clientMetrics storage.ClientMetrics
}

func NewChunkStore(dir, tenantID string) (*ChunkStore, error) {
	return NewChunkStoreWithRegisterer(dir, tenantID, prometheus.DefaultRegisterer)
}

func NewChunkStoreWithRegisterer(dir, tenantID string, reg prometheus.Registerer) (*ChunkStore, error) {
	storageDir := filepath.Join(dir, storageDir)
	workingDir := filepath.Join(dir, workingDir)

	tsdbShipperDir := filepath.Join(workingDir, "tsdb-shipper-active")
	if err := os.MkdirAll(tsdbShipperDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create index directory %s: %w", tsdbShipperDir, err)
	}
	cacheDir := filepath.Join(workingDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create index directory %s: %w", cacheDir, err)
	}

	storeConfig := storage.Config{
		MaxChunkBatchSize: 50,
		TSDBShipperConfig: indexshipper.Config{
			ActiveIndexDirectory: tsdbShipperDir,
			Mode:                 indexshipper.ModeReadWrite,
			IngesterName:         "test",
			CacheLocation:        cacheDir,
			ResyncInterval:       5 * time.Minute,
			CacheTTL:             24 * time.Hour,
		},
		FSConfig: local.FSConfig{Directory: storageDir},
	}
	if *storageLatency > 0 {
		storeConfig.ObjectClientDecorator = func(c objectclient.ObjectClient) objectclient.ObjectClient {
			return newLatencyObjectClient(c, *storageLatency)
		}
	}
	period := config.PeriodConfig{
		From:       config.DayTime{Time: model.Earliest},
		IndexType:  "tsdb",
		ObjectType: "filesystem",
		Schema:     "v13",
		IndexTables: config.IndexPeriodicTableConfig{
			PathPrefix: "index/",
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

	store, err := storage.NewStore(storeConfig, config.ChunkStoreConfig{}, schemaCfg, overrides, cm, reg, util_log.Logger, "cortex")
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}
	return &ChunkStore{
		store:         store,
		periodCfg:     period,
		tenantID:      tenantID,
		chunks:        make(map[string]*chunkenc.MemChunk),
		clientMetrics: cm,
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
	if err := memChunk.Close(); err != nil {
		return err
	}

	lbs, err := syntax.ParseLabels(labelsString)
	if err != nil {
		return err
	}
	labelsBuilder := labels.NewBuilder(lbs)
	labelsBuilder.Set(model.MetricNameLabel, "logs")
	metric := labelsBuilder.Labels()
	fp := client.Fingerprint(lbs)

	firstTime, lastTime := util.RoundToMilliseconds(memChunk.Bounds())
	c := chunk.NewChunk(s.tenantID, fp, metric, chunkenc.NewFacade(memChunk, 0, 0), firstTime, lastTime)
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
	// Unregister metrics to avoid conflicts in tests
	s.clientMetrics.Unregister()
	return nil
}

// latencyObjectClient wraps an ObjectClient and injects a configurable delay
// before each read operation to simulate object storage latency (e.g. S3/GCS).
type latencyObjectClient struct {
	objectclient.ObjectClient
	latency time.Duration
}

func newLatencyObjectClient(c objectclient.ObjectClient, latency time.Duration) *latencyObjectClient {
	return &latencyObjectClient{ObjectClient: c, latency: latency}
}

func (c *latencyObjectClient) delay() {
	time.Sleep(c.latency)
}

func (c *latencyObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	c.delay()
	return c.ObjectClient.GetObject(ctx, objectKey)
}

func (c *latencyObjectClient) GetObjectRange(ctx context.Context, objectKey string, off, length int64) (io.ReadCloser, error) {
	c.delay()
	return c.ObjectClient.GetObjectRange(ctx, objectKey, off, length)
}

func (c *latencyObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	c.delay()
	return c.ObjectClient.ObjectExists(ctx, objectKey)
}

func (c *latencyObjectClient) GetAttributes(ctx context.Context, objectKey string) (objectclient.ObjectAttributes, error) {
	c.delay()
	return c.ObjectClient.GetAttributes(ctx, objectKey)
}

func (c *latencyObjectClient) List(ctx context.Context, prefix string, delimiter string) ([]objectclient.StorageObject, []objectclient.StorageCommonPrefix, error) {
	c.delay()
	return c.ObjectClient.List(ctx, prefix, delimiter)
}
