package testutils

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/ingester/client"
	chunkclient "github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/encoding"
	promchunk "github.com/grafana/loki/pkg/storage/chunk/encoding"
	"github.com/grafana/loki/pkg/storage/chunk/index"
	"github.com/grafana/loki/pkg/storage/config"
)

const (
	userID = "userID"
)

// Fixture type for per-backend testing.
type Fixture interface {
	Name() string
	Clients() (index.IndexClient, chunkclient.Client, index.TableClient, config.SchemaConfig, io.Closer, error)
}

// CloserFunc is to io.Closer as http.HandlerFunc is to http.Handler.
type CloserFunc func() error

// Close implements io.Closer.
func (f CloserFunc) Close() error {
	return f()
}

// DefaultSchemaConfig returns default schema for use in test fixtures
func DefaultSchemaConfig(kind string) config.SchemaConfig {
	return defaultSchemaConfig(kind, "v9", model.Now().Add(-time.Hour*2))
}

// Setup a fixture with initial tables
func Setup(fixture Fixture, tableName string) (index.IndexClient, chunkclient.Client, io.Closer, error) {
	var tbmConfig index.TableManagerConfig
	flagext.DefaultValues(&tbmConfig)
	indexClient, chunkClient, tableClient, schemaConfig, closer, err := fixture.Clients()
	if err != nil {
		return nil, nil, nil, err
	}

	tableManager, err := index.NewTableManager(tbmConfig, schemaConfig, 12*time.Hour, tableClient, nil, nil, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	err = tableManager.SyncTables(context.Background())
	if err != nil {
		return nil, nil, nil, err
	}

	err = tableClient.CreateTable(context.Background(), config.TableDesc{
		Name: tableName,
	})

	return indexClient, chunkClient, closer, err
}

// CreateChunks creates some chunks for testing
func CreateChunks(scfg config.SchemaConfig, startIndex, batchSize int, from model.Time, through model.Time) ([]string, []encoding.Chunk, error) {
	keys := []string{}
	chunks := []encoding.Chunk{}
	for j := 0; j < batchSize; j++ {
		chunk := dummyChunkFor(from, through, labels.Labels{
			{Name: model.MetricNameLabel, Value: "foo"},
			{Name: "index", Value: strconv.Itoa(startIndex*batchSize + j)},
		})
		chunks = append(chunks, chunk)
		keys = append(keys, scfg.ExternalKey(chunk.ChunkRef))
	}
	return keys, chunks, nil
}

func dummyChunkFor(from, through model.Time, metric labels.Labels) encoding.Chunk {
	cs := promchunk.New()

	for ts := from; ts <= through; ts = ts.Add(15 * time.Second) {
		_, err := cs.Add(model.SamplePair{Timestamp: ts, Value: 0})
		if err != nil {
			panic(err)
		}
	}

	chunk := encoding.NewChunk(
		userID,
		client.Fingerprint(metric),
		metric,
		cs,
		from,
		through,
	)
	// Force checksum calculation.
	err := chunk.Encode()
	if err != nil {
		panic(err)
	}
	return chunk
}

func defaultSchemaConfig(store, schema string, from model.Time) config.SchemaConfig {
	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{{
			IndexType: store,
			Schema:    schema,
			From:      config.DayTime{from},
			ChunkTables: config.PeriodicTableConfig{
				Prefix: "cortex",
				Period: 7 * 24 * time.Hour,
			},
			IndexTables: config.PeriodicTableConfig{
				Prefix: "cortex_chunks",
				Period: 7 * 24 * time.Hour,
			},
		}},
	}
	if err := s.Validate(); err != nil {
		panic(err)
	}
	return s
}

// func SetupTestChunkStoreWithClients(indexClient index.IndexClient, chunksClient chunkclient.Client, tableClient index.TableClient) (chunk.Store, error) {
// 	var (
// 		tbmConfig index.TableManagerConfig
// 		schemaCfg = chunk.DefaultSchemaConfig("", "v10", 0)
// 	)
// 	flagext.DefaultValues(&tbmConfig)
// 	tableManager, err := index.NewTableManager(tbmConfig, schemaCfg, 12*time.Hour, tableClient, nil, nil, nil)
// 	if err != nil {
// 		return nil, err
// 	}

// 	err = tableManager.SyncTables(context.Background())
// 	if err != nil {
// 		return nil, err
// 	}

// 	var limits validation.Limits
// 	flagext.DefaultValues(&limits)
// 	limits.MaxQueryLength = model.Duration(30 * 24 * time.Hour)
// 	overrides, err := validation.NewOverrides(limits, nil)
// 	if err != nil {
// 		return nil, err
// 	}

// 	var storeCfg config.StoreConfig
// 	flagext.DefaultValues(&storeCfg)

// 	store := storage.NewCompositeStore(nil)
// 	err = store.AddPeriod(storeCfg, schemaCfg.Configs[0], indexClient, chunksClient, overrides, cache.NewNoopCache(), cache.NewNoopCache())
// 	if err != nil {
// 		return nil, err
// 	}

// 	return store, nil
// }

// func SetupTestChunkStore() (storage.ChunkStore, error) {
// 	storage := NewMockStorage()
// 	return SetupTestChunkStoreWithClients(storage, storage, storage)
// }

// func SetupTestObjectStore() (chunkclient.ObjectClient, error) {
// 	return NewMockStorage(), nil
// }
