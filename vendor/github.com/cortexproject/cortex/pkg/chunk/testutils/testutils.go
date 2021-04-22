package testutils

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	promchunk "github.com/cortexproject/cortex/pkg/chunk/encoding"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

const (
	userID = "userID"
)

// Fixture type for per-backend testing.
type Fixture interface {
	Name() string
	Clients() (chunk.IndexClient, chunk.Client, chunk.TableClient, chunk.SchemaConfig, io.Closer, error)
}

// CloserFunc is to io.Closer as http.HandlerFunc is to http.Handler.
type CloserFunc func() error

// Close implements io.Closer.
func (f CloserFunc) Close() error {
	return f()
}

// DefaultSchemaConfig returns default schema for use in test fixtures
func DefaultSchemaConfig(kind string) chunk.SchemaConfig {
	schemaConfig := chunk.DefaultSchemaConfig(kind, "v1", model.Now().Add(-time.Hour*2))
	return schemaConfig
}

// Setup a fixture with initial tables
func Setup(fixture Fixture, tableName string) (chunk.IndexClient, chunk.Client, io.Closer, error) {
	var tbmConfig chunk.TableManagerConfig
	flagext.DefaultValues(&tbmConfig)
	indexClient, chunkClient, tableClient, schemaConfig, closer, err := fixture.Clients()
	if err != nil {
		return nil, nil, nil, err
	}

	tableManager, err := chunk.NewTableManager(tbmConfig, schemaConfig, 12*time.Hour, tableClient, nil, nil, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	err = tableManager.SyncTables(context.Background())
	if err != nil {
		return nil, nil, nil, err
	}

	err = tableClient.CreateTable(context.Background(), chunk.TableDesc{
		Name: tableName,
	})

	return indexClient, chunkClient, closer, err
}

// CreateChunks creates some chunks for testing
func CreateChunks(startIndex, batchSize int, from model.Time, through model.Time) ([]string, []chunk.Chunk, error) {
	keys := []string{}
	chunks := []chunk.Chunk{}
	for j := 0; j < batchSize; j++ {
		chunk := dummyChunkFor(from, through, labels.Labels{
			{Name: model.MetricNameLabel, Value: "foo"},
			{Name: "index", Value: strconv.Itoa(startIndex*batchSize + j)},
		})
		chunks = append(chunks, chunk)
		keys = append(keys, chunk.ExternalKey())
	}
	return keys, chunks, nil
}

func dummyChunkFor(from, through model.Time, metric labels.Labels) chunk.Chunk {
	cs := promchunk.New()

	for ts := from; ts <= through; ts = ts.Add(15 * time.Second) {
		_, err := cs.Add(model.SamplePair{Timestamp: ts, Value: 0})
		if err != nil {
			panic(err)
		}
	}

	chunk := chunk.NewChunk(
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

func SetupTestChunkStoreWithClients(indexClient chunk.IndexClient, chunksClient chunk.Client, tableClient chunk.TableClient) (chunk.Store, error) {
	var (
		tbmConfig chunk.TableManagerConfig
		schemaCfg = chunk.DefaultSchemaConfig("", "v10", 0)
	)
	flagext.DefaultValues(&tbmConfig)
	tableManager, err := chunk.NewTableManager(tbmConfig, schemaCfg, 12*time.Hour, tableClient, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	err = tableManager.SyncTables(context.Background())
	if err != nil {
		return nil, err
	}

	var limits validation.Limits
	flagext.DefaultValues(&limits)
	limits.MaxQueryLength = model.Duration(30 * 24 * time.Hour)
	overrides, err := validation.NewOverrides(limits, nil)
	if err != nil {
		return nil, err
	}

	var storeCfg chunk.StoreConfig
	flagext.DefaultValues(&storeCfg)

	store := chunk.NewCompositeStore(nil)
	err = store.AddPeriod(storeCfg, schemaCfg.Configs[0], indexClient, chunksClient, overrides, cache.NewNoopCache(), cache.NewNoopCache())
	if err != nil {
		return nil, err
	}

	return store, nil
}

func SetupTestChunkStore() (chunk.Store, error) {
	storage := chunk.NewMockStorage()
	return SetupTestChunkStoreWithClients(storage, storage, storage)
}

func SetupTestObjectStore() (chunk.ObjectClient, error) {
	return chunk.NewMockStorage(), nil
}
