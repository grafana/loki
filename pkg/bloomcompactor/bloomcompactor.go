/*
Bloom-compactor

This is a standalone service that is responsible for compacting TSDB indexes into bloomfilters.
It creates and merges bloomfilters into an aggregated form, called bloom-blocks.
It maintains a list of references between bloom-blocks and TSDB indexes in files called meta.jsons.

Bloom-compactor regularly runs to check for changes in meta.jsons and runs compaction only upon changes in TSDBs.

					bloomCompactor.Compactor
						|
			----------------------------------
	// Read path	| 	 				| write path TODO
		bloomshipper.Store**
			|
		bloomshipper.Shipper
			|
		bloomshipper.BloomClient
			|
		ObjectClient
			|
	.....................service boundary
			|
		object storage
*/
package bloomcompactor

import (
	"context"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/bloomshipperconfig"
	"github.com/prometheus/client_golang/prometheus"
)

type Compactor struct {
	services.Service

	cfg    Config
	logger log.Logger

	bloomStore bloomshipper.Store
}

func New(cfg Config, storageCfg storage.Config, logger log.Logger, clientMetrics storage.ClientMetrics, _ prometheus.Registerer) (*Compactor, error) {
	c := &Compactor{
		cfg:    cfg,
		logger: logger,
	}

	client, err := bloomshipper.NewBloomClient(nil, storageCfg, clientMetrics)
	if err != nil {
		return nil, err
	}

	shipper, err := bloomshipper.NewShipper(
		client,
		bloomshipperconfig.Config{WorkingDirectory: cfg.WorkingDirectory},
	)
	if err != nil {
		return nil, err
	}

	store, err := bloomshipper.NewBloomStore(*shipper)
	if err != nil {
		return nil, err
	}
	c.bloomStore = store
	c.Service = services.NewIdleService(c.starting, c.stopping)

	return c, nil
}

func (c *Compactor) starting(_ context.Context) error {
	return nil
}

func (c *Compactor) stopping(_ error) error {
	return nil
}
