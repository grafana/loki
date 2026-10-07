package logqlbench

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
	ingesterclient "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	objectclient "github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	chunkTargetSize = 1024 * 1024
	chunkBlockSize  = 256 * 1024
)

// chunkStore encodes log entries into real Loki chunks, served through storage.LokiStore.
type chunkStore struct {
	store  *storage.LokiStore
	chunks map[string]*chunkenc.MemChunk
	tenant string
}

// newChunkStore opens a filesystem-backed chunk store rooted at dir. decorator wraps every
// object-client call, e.g. to inject latency or count requests; nil leaves it undecorated.
func newChunkStore(dir, tenant string, decorator func(objectclient.ObjectClient) objectclient.ObjectClient) (*chunkStore, error) {
	storageDir := filepath.Join(dir, storageDirName)
	workingDir := filepath.Join(dir, "workingdir")

	tsdbShipperDir := filepath.Join(workingDir, "tsdb-shipper-active")
	if err := os.MkdirAll(tsdbShipperDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating index directory %s: %w", tsdbShipperDir, err)
	}
	cacheDir := filepath.Join(workingDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache directory %s: %w", cacheDir, err)
	}

	var storeConfig storage.Config
	flagext.DefaultValues(&storeConfig)
	// The benchmark uses the legacy filesystem client, not the default thanos-objstore path.
	storeConfig.UseThanosObjstore = false
	storeConfig.MaxChunkBatchSize = 50
	storeConfig.MaxParallelGetChunk = storeParallelism
	storeConfig.TSDBShipperConfig.ActiveIndexDirectory = tsdbShipperDir
	storeConfig.TSDBShipperConfig.Mode = indexshipper.ModeReadWrite
	storeConfig.TSDBShipperConfig.IngesterName = "logqlbench"
	storeConfig.TSDBShipperConfig.CacheLocation = cacheDir
	storeConfig.TSDBShipperConfig.ResyncInterval = 5 * time.Minute
	storeConfig.TSDBShipperConfig.CacheTTL = 24 * time.Hour
	storeConfig.TSDBShipperConfig.DownloadTimeout = time.Minute
	storeConfig.TSDBShipperConfig.IndexReaderMode = indexshipper.DefaultIndexReaderMode
	storeConfig.FSConfig.Directory = storageDir
	if decorator != nil {
		storeConfig.ObjectClientDecorator = decorator
	}

	// PeriodConfig describes a schema epoch, not a runtime default: every field must be set
	// explicitly, so flagext.DefaultValues would add nothing here.
	period := config.PeriodConfig{
		From:       config.DayTime{Time: model.Earliest},
		IndexType:  "tsdb",
		ObjectType: "filesystem",
		Schema:     "v13",
		IndexTables: config.IndexPeriodicTableConfig{
			PathPrefix:          "index/",
			PeriodicTableConfig: config.PeriodicTableConfig{Prefix: "index_", Period: 24 * time.Hour},
		},
	}
	var schemaCfg config.SchemaConfig
	flagext.DefaultValues(&schemaCfg)
	schemaCfg.Configs = []config.PeriodConfig{period}

	// Pre-warm the memoized schema version single-threaded, to avoid a data race when the TSDB
	// index shipper reads it concurrently during a query.
	for i := range schemaCfg.Configs {
		if _, err := schemaCfg.Configs[i].VersionAsInt(); err != nil {
			return nil, err
		}
	}

	var limits validation.Limits
	flagext.DefaultValues(&limits)
	overrides, err := validation.NewOverrides(limits, nil)
	if err != nil {
		return nil, err
	}

	var chunkStoreConfig config.ChunkStoreConfig
	flagext.DefaultValues(&chunkStoreConfig)

	// A zero ClientMetrics avoids a duplicate-registration panic if more than one store is built.
	store, err := storage.NewStore(storeConfig, chunkStoreConfig, schemaCfg, overrides, storage.ClientMetrics{}, prometheus.NewRegistry(), util_log.Logger, "cortex")
	if err != nil {
		return nil, fmt.Errorf("creating store: %w", err)
	}
	return &chunkStore{store: store, chunks: map[string]*chunkenc.MemChunk{}, tenant: tenant}, nil
}

func (s *chunkStore) Write(ctx context.Context, streams []logproto.Stream) error {
	for _, stream := range streams {
		enc, ok := s.chunks[stream.Labels]
		if !ok {
			enc = newChunkEncoder()
			s.chunks[stream.Labels] = enc
		}
		for _, entry := range stream.Entries {
			if !enc.SpaceFor(&entry) {
				if err := s.flushChunk(ctx, enc, stream.Labels); err != nil {
					return err
				}
				enc = newChunkEncoder()
				s.chunks[stream.Labels] = enc
			}
			if _, err := enc.Append(&entry); err != nil {
				return err
			}
		}
	}
	return nil
}

func newChunkEncoder() *chunkenc.MemChunk {
	return chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.Snappy, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, chunkBlockSize, chunkTargetSize)
}

func (s *chunkStore) flushChunk(ctx context.Context, mc *chunkenc.MemChunk, labelsString string) error {
	if err := mc.Close(); err != nil {
		return err
	}
	lbs, err := syntax.ParseLabels(labelsString)
	if err != nil {
		return err
	}
	metric := labels.NewBuilder(lbs).Set(model.MetricNameLabel, "logs").Labels()
	fp := ingesterclient.Fingerprint(lbs)

	firstTime, lastTime := util.RoundToMilliseconds(mc.Bounds())
	c := chunk.NewChunk(s.tenant, fp, metric, chunkenc.NewFacade(mc, 0, 0), firstTime, lastTime)
	if err := c.Encode(); err != nil {
		return err
	}
	return s.store.Put(ctx, []chunk.Chunk{c})
}

func (s *chunkStore) Close() error {
	for labelsString, mc := range s.chunks {
		if err := s.flushChunk(context.Background(), mc, labelsString); err != nil {
			return err
		}
	}
	clear(s.chunks)
	s.store.Stop()
	return nil
}
