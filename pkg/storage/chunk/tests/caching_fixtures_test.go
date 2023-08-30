package tests

import (
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/validation"
)

type fixture struct {
	fixture testutils.IndexFixture
}

func (f fixture) Name() string { return "caching-store" }
func (f fixture) Client() (index.Client, io.Closer, error) {
	limits, err := defaultLimits()
	if err != nil {
		return nil, nil, err
	}
	indexClient, closer, err := f.fixture.Client()
	reg := prometheus.NewRegistry()
	logger := log.NewNopLogger()
	indexClient = index.NewCachingIndexClient(indexClient, cache.NewFifoCache("index-fifo", cache.FifoCacheConfig{
		MaxSizeItems: 500,
		TTL:          5 * time.Minute,
	}, reg, logger, stats.ChunkCache), 5*time.Minute, limits, logger, false)
	return indexClient, closer, err
}

// Fixtures for unit testing the caching storage.
var Fixture = fixture{local.IndexFixture}

func defaultLimits() (*validation.Overrides, error) {
	var defaults validation.Limits
	flagext.DefaultValues(&defaults)
	defaults.CardinalityLimit = 5
	return validation.NewOverrides(defaults, nil)
}
