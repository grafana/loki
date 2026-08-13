package logqltest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

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
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

// testingChunkStore encodes log entries into real Loki chunks and serves queries through the
// production storage.LokiStore read path (chunk decode + pipeline), so scripts exercise
// as much production code as practical. It is not meant for large data.
type testingChunkStore struct {
	store     *storage.LokiStore
	chunks    map[string]*chunkenc.MemChunk
	closeOnce sync.Once
}

// newTestingChunkStore builds a filesystem-backed store (TSDB index) rooted in a fresh temp dir.
func newTestingChunkStore(t *testing.T) *testingChunkStore {
	t.Helper()
	dir := t.TempDir()

	storeConfig := storage.Config{
		MaxChunkBatchSize: 50,
		TSDBShipperConfig: indexshipper.Config{
			ActiveIndexDirectory: dir + "/index",
			Mode:                 indexshipper.ModeReadWrite,
			IngesterName:         "test",
			CacheLocation:        dir + "/cache",
			ResyncInterval:       5 * time.Minute,
			CacheTTL:             24 * time.Hour,
		},
		FSConfig: local.FSConfig{Directory: dir + "/storage"},
	}
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
	schemaCfg := config.SchemaConfig{Configs: []config.PeriodConfig{period}}

	// Pre-warm the memoized schema version. PeriodConfig.VersionAsInt lazily populates
	// (and writes) a cached value; that write normally happens during YAML unmarshal, but we
	// build the config as a literal, so populate it single-threaded here to avoid a data race
	// when the TSDB index shipper reads it concurrently during a query.
	for i := range schemaCfg.Configs {
		_, err := schemaCfg.Configs[i].VersionAsInt()
		require.NoError(t, err)
	}

	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	overrides, err := validation.NewOverrides(limits, nil)
	require.NoError(t, err)

	// A zero ClientMetrics avoids registering the object-store metrics on the global default
	// registry, so multiple stores can run in parallel without a duplicate-registration panic.
	// The filesystem client never uses those metrics.
	store, err := storage.NewStore(storeConfig, config.ChunkStoreConfig{}, schemaCfg, overrides, storage.ClientMetrics{}, prometheus.NewRegistry(), util_log.Logger, "cortex")
	require.NoError(t, err)

	return &testingChunkStore{store: store, chunks: map[string]*chunkenc.MemChunk{}}
}

// write appends entries to per-stream memchunks, flushing a chunk when it fills.
func (s *testingChunkStore) write(t *testing.T, streams []logproto.Stream) {
	t.Helper()

	for _, stream := range streams {
		enc, ok := s.chunks[stream.Labels]
		if !ok {
			enc = newMemChunk()
			s.chunks[stream.Labels] = enc
		}
		for _, entry := range stream.Entries {
			if !enc.SpaceFor(&entry) {
				s.flushChunk(t, enc, stream.Labels)
				enc = newMemChunk()
				s.chunks[stream.Labels] = enc
			}
			dup, err := enc.Append(&entry)
			require.NoError(t, err)
			require.Falsef(t, dup, "duplicate entry dropped by dedup (same timestamp+line+metadata) in stream %s at %s: %q — give entries distinct timestamps or use {{.i}}", stream.Labels, entry.Timestamp, entry.Line)
		}
	}
}

// flush writes all buffered chunks to the store so they become queryable.
func (s *testingChunkStore) flush(t *testing.T) {
	t.Helper()

	for lbs, enc := range s.chunks {
		s.flushChunk(t, enc, lbs)
	}
	clear(s.chunks)
}

func (s *testingChunkStore) flushChunk(t *testing.T, memChunk *chunkenc.MemChunk, labelsString string) {
	t.Helper()
	require.NoError(t, memChunk.Close())

	lbs, err := syntax.ParseLabels(labelsString)
	require.NoError(t, err)
	metric := labels.NewBuilder(lbs).Set(model.MetricNameLabel, "logs").Labels()
	fp := client.Fingerprint(lbs)

	firstTime, lastTime := util.RoundToMilliseconds(memChunk.Bounds())
	c := chunk.NewChunk(tenant, fp, metric, chunkenc.NewFacade(memChunk, 0, 0), firstTime, lastTime)
	require.NoError(t, c.Encode())
	require.NoError(t, s.store.Put(context.Background(), []chunk.Chunk{c}))
}

func (s *testingChunkStore) querier() logql.Querier {
	return s.store
}

// close stops the underlying store. It is safe to call more than once: setStreams closes the
// previous store on refresh, and a t.Cleanup closes the final one at test end.
func (s *testingChunkStore) close() {
	s.closeOnce.Do(s.store.Stop)
}

func newMemChunk() *chunkenc.MemChunk {
	const (
		targetChunkSize = 1024 * 1024
		blockSize       = 256 * 1024
	)

	return chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.Snappy, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, blockSize, targetChunkSize)
}
