package tests

import (
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/validation"
)

type fixture struct {
	fixture testutils.Fixture
}

func (f fixture) Name() string { return "caching-store" }
func (f fixture) Clients() (index.Client, client.Client, index.TableClient, config.SchemaConfig, io.Closer, error) {
	limits, err := defaultLimits()
	if err != nil {
		return nil, nil, nil, config.SchemaConfig{}, nil, err
	}
	indexClient, chunkClient, tableClient, schemaConfig, closer, err := f.fixture.Clients()
	reg := prometheus.NewRegistry()
	logger := log.NewNopLogger()
	indexClient = index.NewCachingIndexClient(indexClient, cache.NewEmbeddedCache("index-embedded", cache.EmbeddedCacheConfig{
		MaxSizeItems: 500,
		TTL:          5 * time.Minute,
	}, reg, logger, stats.ChunkCache), 5*time.Minute, limits, logger, false)
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
