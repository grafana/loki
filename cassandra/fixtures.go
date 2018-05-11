package cassandra

import (
	"context"
	"flag"
	"os"

	"github.com/prometheus/common/model"
	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/testutils"
	"github.com/weaveworks/cortex/pkg/util"
)

// GOCQL doesn't provide nice mocks, so we use a real Cassandra instance.
// To enable these tests:
// $ docker run --name cassandra --rm -p 9042:9042 cassandra:3.11
// $ CASSANDRA_TEST_ADDRESSES=localhost:9042 go test ./pkg/chunk/storage

type fixture struct {
	name          string
	storageClient chunk.StorageClient
	tableClient   chunk.TableClient
	schemaConfig  chunk.SchemaConfig
}

func (f fixture) Name() string {
	return f.name
}

func (f fixture) Clients() (chunk.StorageClient, chunk.TableClient, chunk.SchemaConfig, error) {
	return f.storageClient, f.tableClient, f.schemaConfig, nil
}

func (f fixture) Teardown() error {
	return nil
}

// Fixtures for unit testing Cassandra integration.
func Fixtures() ([]testutils.Fixture, error) {
	addresses := os.Getenv("CASSANDRA_TEST_ADDRESSES")
	if addresses == "" {
		return nil, nil
	}

	cfg := Config{
		addresses:         addresses,
		keyspace:          "test",
		consistency:       "QUORUM",
		replicationFactor: 1,
	}

	// Get a SchemaConfig with the defaults.
	flagSet := flag.NewFlagSet("flags", flag.PanicOnError)
	schemaConfig := chunk.SchemaConfig{}
	schemaConfig.RegisterFlags(flagSet)
	err := flagSet.Parse([]string{})
	if err != nil {
		return nil, err
	}
	schemaConfig.IndexTables.From = util.NewDayValue(model.Now())
	schemaConfig.ChunkTables.From = util.NewDayValue(model.Now())

	storageClient, err := NewStorageClient(cfg, schemaConfig)
	if err != nil {
		return nil, err
	}

	tableClient, err := NewTableClient(context.Background(), cfg)
	if err != nil {
		return nil, err
	}

	return []testutils.Fixture{
		fixture{
			name:          "Cassandra",
			storageClient: storageClient,
			tableClient:   tableClient,
			schemaConfig:  schemaConfig,
		},
	}, nil
}
