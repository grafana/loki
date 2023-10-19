/*
Bloom-compactor

This is a standalone service that is responsible for compacting TSDB indexes into bloomfilters.
It creates and merges bloomfilters into an aggregated form, called bloom-blocks.
It maintains a list of references between bloom-blocks and TSDB indexes in files called meta.jsons.

Bloom-compactor regularly runs to check for changes in meta.jsons and runs compaction only upon changes in TSDBs.

bloomCompactor.Compactor

			| // Read/Write path
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
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

type Compactor struct {
	services.Service

	cfg                Config
	logger             log.Logger
	bloomCompactorRing ring.ReadRing
	periodConfigs      []config.PeriodConfig

	// temporary workaround until store has implemented read/write shipper interface
	bloomShipperClient bloomshipper.Client
	bloomStore         bloomshipper.Store
}

func New(cfg Config,
	readRing ring.ReadRing,
	storageCfg storage.Config,
	periodConfigs []config.PeriodConfig,
	logger log.Logger,
	clientMetrics storage.ClientMetrics,
	_ prometheus.Registerer) (*Compactor, error) {
	c := &Compactor{
		cfg:                cfg,
		logger:             logger,
		bloomCompactorRing: readRing,
		periodConfigs:      periodConfigs,
	}

	client, err := bloomshipper.NewBloomClient(periodConfigs, storageCfg, clientMetrics)
	if err != nil {
		return nil, err
	}

	shipper, err := bloomshipper.NewShipper(
		client,
		storageCfg.BloomShipperConfig,
		logger,
	)
	if err != nil {
		return nil, err
	}

	store, err := bloomshipper.NewBloomStore(shipper)
	if err != nil {
		return nil, err
	}

	// temporary workaround until store has implemented read/write shipper interface
	c.bloomShipperClient = client
	c.bloomStore = store
	// TODO use a new service with a loop
	c.Service = services.NewIdleService(c.starting, c.stopping)

	return c, nil
}

func (c *Compactor) starting(_ context.Context) error {
	return nil
}

func (c *Compactor) stopping(_ error) error {
	return nil
}

// TODO Get fpRange owned by the compactor instance
func NoopGetFingerprintRange() (uint64, uint64) { return 0, 0 }

// TODO List Users from TSDB and add logic to owned user via ring
func NoopGetUserID() string { return "" }

// TODO get series from objectClient (TSDB) instead of params
func NoopGetSeries() *v1.Series { return nil }

// TODO Then get chunk data from series
func NoopGetChunks() []byte { return nil }

// part1: Create a compact method that assumes no block/meta files exists (eg first compaction)
// part2: Write logic to check first for existing block/meta files and does above.
func (c *Compactor) compactNewChunks(ctx context.Context, dst string) (err error) {
	// part1
	series := NoopGetSeries()
	data := NoopGetChunks()

	bloom := v1.Bloom{ScalableBloomFilter: *filter.NewDefaultScalableBloomFilter(0.01)}
	// create bloom filters from that.
	bloom.Add([]byte(fmt.Sprint(data)))

	// block and seriesList
	seriesList := []v1.SeriesWithBloom{
		{
			Series: series,
			Bloom:  &bloom,
		},
	}

	writer := v1.NewDirectoryBlockWriter(dst)

	builder, err := v1.NewBlockBuilder(
		v1.BlockOptions{
			SeriesPageSize: 100,
			BloomPageSize:  10 << 10,
		}, writer)
	if err != nil {
		return err
	}
	// BuildFrom closes itself
	err = builder.BuildFrom(v1.NewSliceIter[v1.SeriesWithBloom](seriesList))
	if err != nil {
		return err
	}

	// TODO Ask Owen, shall we expose a method to expose these paths on BlockWriter?
	indexPath := filepath.Join(dst, "series")
	bloomPath := filepath.Join(dst, "bloom")

	blockRef := bloomshipper.BlockRef{
		IndexPath: indexPath,
		BlockPath: bloomPath,
	}

	blocks := []bloomshipper.Block{
		{
			BlockRef: blockRef,

			// TODO point to the data to be read
			Data: nil,
		},
	}

	meta := bloomshipper.Meta{
		// After successful compaction there should be no tombstones
		Tombstones: make([]bloomshipper.BlockRef, 0),
		Blocks:     []bloomshipper.BlockRef{blockRef},
	}

	err = c.bloomShipperClient.PutMeta(ctx, meta)
	if err != nil {
		return err
	}
	_, err = c.bloomShipperClient.PutBlocks(ctx, blocks)
	if err != nil {
		return err
	}
	// TODO may need to change return value of this func
	return nil
}

func (c *Compactor) runCompact(ctx context.Context) error {
	// TODO set MaxLookBackPeriod to Max ingester accepts
	maxLookBackPeriod := c.cfg.MaxLookBackPeriod

	stFp, endFp := NoopGetFingerprintRange()
	tenantID := NoopGetUserID()

	end := time.Now().UTC().UnixMilli()
	start := end - maxLookBackPeriod.Milliseconds()

	metaSearchParams := bloomshipper.MetaSearchParams{
		TenantID:       tenantID,
		MinFingerprint: stFp,
		MaxFingerprint: endFp,
		StartTimestamp: start,
		EndTimestamp:   end,
	}

	metas, err := c.bloomShipperClient.GetMetas(ctx, metaSearchParams)
	if err != nil {
		return err
	}

	if len(metas) == 0 {
		// run compaction from scratch
		tempDst := os.TempDir()
		err = c.compactNewChunks(ctx, tempDst)
		if err != nil {
			return err
		}
	} else {
		// part 2
		// When already compacted metas exists
		// Deduplicate index paths
		uniqueIndexPaths := make(map[string]struct{})

		for _, meta := range metas {
			for _, blockRef := range meta.Blocks {
				uniqueIndexPaths[blockRef.IndexPath] = struct{}{}
			}
		}

		// TODO complete part 2 - discuss with Owen - add part to compare chunks and blocks.
		// 1. for each period at hand, get TSDB table indexes for given fp range
		// 2. Check blocks for given uniqueIndexPaths and TSDBindexes
		//	if bloomBlock refs are a superset (covers TSDBIndexes plus more outside of range)
		//	create a new meta.json file, tombstone unused index/block paths.

		// else if: there are TSDBindexes that are not covered in bloomBlocks (a subset)
		// then call compactNewChunks on them and create a new meta.json

		// else: all good, no compaction
	}
	return nil
}
