package testutils

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
	Clients() (chunk.IndexClient, chunk.Client, chunk.TableClient, chunk.SchemaConfig, error)
	Teardown() error
}

// DefaultSchemaConfig returns default schema for use in test fixtures
func DefaultSchemaConfig(kind string) chunk.SchemaConfig {
	schemaConfig := chunk.DefaultSchemaConfig(kind, "v1", model.Now().Add(-time.Hour*2))
	return schemaConfig
}

// Setup a fixture with initial tables
func Setup(fixture Fixture, tableName string) (chunk.IndexClient, chunk.Client, error) {
	var tbmConfig chunk.TableManagerConfig
	flagext.DefaultValues(&tbmConfig)
	indexClient, chunkClient, tableClient, schemaConfig, err := fixture.Clients()
	if err != nil {
		return nil, nil, err
	}

	tableManager, err := chunk.NewTableManager(tbmConfig, schemaConfig, 12*time.Hour, tableClient, nil, nil)
	if err != nil {
		return nil, nil, err
	}

	err = tableManager.SyncTables(context.Background())
	if err != nil {
		return nil, nil, err
	}

	err = tableClient.CreateTable(context.Background(), chunk.TableDesc{
		Name: tableName,
	})

	return indexClient, chunkClient, err
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

func TeardownFixture(t *testing.T, fixture Fixture) {
	require.NoError(t, fixture.Teardown())
}

func SetupTestChunkStore() (chunk.Store, error) {
	var (
		tbmConfig chunk.TableManagerConfig
		schemaCfg = chunk.DefaultSchemaConfig("", "v10", 0)
	)
	flagext.DefaultValues(&tbmConfig)
	storage := chunk.NewMockStorage()
	tableManager, err := chunk.NewTableManager(tbmConfig, schemaCfg, 12*time.Hour, storage, nil, nil)
	if err != nil {
		return nil, err
	}

	err = tableManager.SyncTables(context.Background())
	if err != nil {
		return nil, err
	}

	var limits validation.Limits
	flagext.DefaultValues(&limits)
	limits.MaxQueryLength = 30 * 24 * time.Hour
	overrides, err := validation.NewOverrides(limits, nil)
	if err != nil {
		return nil, err
	}

	var storeCfg chunk.StoreConfig
	flagext.DefaultValues(&storeCfg)

	store := chunk.NewCompositeStore()
	err = store.AddPeriod(storeCfg, schemaCfg.Configs[0], storage, storage, overrides, cache.NewNoopCache(), cache.NewNoopCache())
	if err != nil {
		return nil, err
	}

	return store, nil
}

func SetupTestObjectStore() (chunk.ObjectClient, error) {
	return chunk.NewMockStorage(), nil
}
