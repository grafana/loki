package cassandra

import (
	"context"
	"os"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/testutils"
)

// GOCQL doesn't provide nice mocks, so we use a real Cassandra instance.
// To enable these tests:
// $ docker run --name cassandra --rm -p 9042:9042 cassandra:3.11
// $ CASSANDRA_TEST_ADDRESSES=localhost:9042 go test ./pkg/chunk/storage

type fixture struct {
	name         string
	indexClient  chunk.IndexClient
	objectClient chunk.ObjectClient
	tableClient  chunk.TableClient
	schemaConfig chunk.SchemaConfig
}

func (f fixture) Name() string {
	return f.name
}

func (f fixture) Clients() (chunk.IndexClient, chunk.ObjectClient, chunk.TableClient, chunk.SchemaConfig, error) {
	return f.indexClient, f.objectClient, f.tableClient, f.schemaConfig, nil
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
		Addresses:         addresses,
		Keyspace:          "test",
		Consistency:       "QUORUM",
		ReplicationFactor: 1,
	}

	// Get a SchemaConfig with the defaults.
	schemaConfig := testutils.DefaultSchemaConfig("cassandra")

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
			name:         "Cassandra",
			indexClient:  storageClient,
			objectClient: storageClient,
			tableClient:  tableClient,
			schemaConfig: schemaConfig,
		},
	}, nil
}
