package storage

import (
	"time"

	"github.com/weaveworks/cortex/pkg/chunk/cache"
	"github.com/weaveworks/cortex/pkg/chunk/gcp"

	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/testutils"
)

type fixture struct {
	fixture testutils.Fixture
}

func (f fixture) Name() string { return "caching-store" }
func (f fixture) Clients() (chunk.StorageClient, chunk.TableClient, chunk.SchemaConfig, error) {
	storageClient, tableClient, schemaConfig, err := f.fixture.Clients()
	client := newCachingStorageClient(storageClient, cache.NewFifoCache("index-fifo", cache.FifoCacheConfig{500, 5 * time.Minute}), 5*time.Minute)
	return client, tableClient, schemaConfig, err
}
func (f fixture) Teardown() error { return f.fixture.Teardown() }

// Fixtures for unit testing the caching storage.
var Fixtures = []testutils.Fixture{
	fixture{gcp.Fixtures[0]},
}
