package testutils

import (
	"context"
	"strconv"
	"time"

	promchunk "github.com/cortexproject/cortex/pkg/chunk/encoding"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/prometheus/common/model"

	"github.com/cortexproject/cortex/pkg/chunk"
)

const (
	userID = "userID"
)

// Fixture type for per-backend testing.
type Fixture interface {
	Name() string
	Clients() (chunk.IndexClient, chunk.ObjectClient, chunk.TableClient, chunk.SchemaConfig, error)
	Teardown() error
}

// Setup a fixture with initial tables
func Setup(fixture Fixture, tableName string) (chunk.IndexClient, chunk.ObjectClient, error) {
	var tbmConfig chunk.TableManagerConfig
	flagext.DefaultValues(&tbmConfig)
	indexClient, chunkClient, tableClient, schemaConfig, err := fixture.Clients()
	if err != nil {
		return nil, nil, err
	}

	tableManager, err := chunk.NewTableManager(tbmConfig, schemaConfig, 12*time.Hour, tableClient)
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
func CreateChunks(startIndex, batchSize int) ([]string, []chunk.Chunk, error) {
	keys := []string{}
	chunks := []chunk.Chunk{}
	for j := 0; j < batchSize; j++ {
		chunk := dummyChunkFor(model.Now(), model.Metric{
			model.MetricNameLabel: "foo",
			"index":               model.LabelValue(strconv.Itoa(startIndex*batchSize + j)),
		})
		chunks = append(chunks, chunk)
		_, err := chunk.Encode() // Need to encode it, side effect calculates crc
		if err != nil {
			return nil, nil, err
		}
		keys = append(keys, chunk.ExternalKey())
	}
	return keys, chunks, nil
}

func dummyChunk(now model.Time) chunk.Chunk {
	return dummyChunkFor(now, model.Metric{
		model.MetricNameLabel: "foo",
		"bar":  "baz",
		"toms": "code",
	})
}

func dummyChunkFor(now model.Time, metric model.Metric) chunk.Chunk {
	cs, _ := promchunk.New().Add(model.SamplePair{Timestamp: now, Value: 0})
	chunk := chunk.NewChunk(
		userID,
		metric.Fingerprint(),
		metric,
		cs[0],
		now.Add(-time.Hour),
		now,
	)
	// Force checksum calculation.
	_, err := chunk.Encode()
	if err != nil {
		panic(err)
	}
	return chunk
}
