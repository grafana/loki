package cassandra

import (
	"context"
	"io"
	"os"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

// GOCQL doesn't provide nice mocks, so we use a real Cassandra instance.
// To enable these tests:
// $ docker run -d --name cassandra --rm -p 9042:9042 cassandra:3.11
// $ CASSANDRA_TEST_ADDRESSES=localhost:9042 go test ./pkg/chunk/storage

type fixture struct {
	name      string
	addresses string
}

func (f *fixture) Name() string {
	return f.name
}

func (f *fixture) Clients() (index.Client, client.Client, index.TableClient, config.SchemaConfig, io.Closer, error) {
	var cfg Config
	flagext.DefaultValues(&cfg)
	cfg.Addresses = f.addresses
	cfg.Keyspace = "test"
	cfg.Consistency = "QUORUM"
	cfg.ReplicationFactor = 1

	// Get a SchemaConfig with the defaults.
	schemaConfig := testutils.DefaultSchemaConfig("cassandra")

	storageClient, err := NewStorageClient(cfg, schemaConfig, nil)
	if err != nil {
		return nil, nil, nil, schemaConfig, nil, err
	}

	objectClient, err := NewObjectClient(cfg, schemaConfig, nil, 150)
	if err != nil {
		return nil, nil, nil, schemaConfig, nil, err
	}

	tableClient, err := NewTableClient(context.Background(), cfg, nil)
	if err != nil {
		return nil, nil, nil, schemaConfig, nil, err
	}

	closer := testutils.CloserFunc(func() error {
		storageClient.Stop()
		objectClient.Stop()
		tableClient.Stop()
		return nil
	})

	return storageClient, objectClient, tableClient, schemaConfig, closer, nil
}

// Fixtures for unit testing Cassandra integration.
func Fixtures() []testutils.Fixture {
	addresses := os.Getenv("CASSANDRA_TEST_ADDRESSES")
	if addresses == "" {
		return nil
	}

	return []testutils.Fixture{
		&fixture{
			name:      "Cassandra",
			addresses: addresses,
		},
	}
}
