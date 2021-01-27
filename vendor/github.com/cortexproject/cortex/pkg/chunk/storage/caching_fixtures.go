package storage

import (
	"io"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/validation"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/testutils"
)

type fixture struct {
	fixture testutils.Fixture
}

func (f fixture) Name() string { return "caching-store" }
func (f fixture) Clients() (chunk.IndexClient, chunk.Client, chunk.TableClient, chunk.SchemaConfig, io.Closer, error) {
	limits, err := defaultLimits()
	if err != nil {
		return nil, nil, nil, chunk.SchemaConfig{}, nil, err
	}
	indexClient, chunkClient, tableClient, schemaConfig, closer, err := f.fixture.Clients()
	reg := prometheus.NewRegistry()
	logger := log.NewNopLogger()
	indexClient = newCachingIndexClient(indexClient, cache.NewFifoCache("index-fifo", cache.FifoCacheConfig{
		MaxSizeItems: 500,
		Validity:     5 * time.Minute,
	}, reg, logger), 5*time.Minute, limits, logger)
	return indexClient, chunkClient, tableClient, schemaConfig, closer, err
}

// Fixtures for unit testing the caching storage.
var Fixtures = []testutils.Fixture{
	fixture{gcp.Fixtures[0]},
}

func defaultLimits() (*validation.Overrides, error) {
	var defaults validation.Limits
	flagext.DefaultValues(&defaults)
	defaults.CardinalityLimit = 5
	return validation.NewOverrides(defaults, nil)
}
