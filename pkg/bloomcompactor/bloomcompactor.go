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
	"bytes"
	"context"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"path/filepath"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/downloads"
	shipperindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"

	//"github.com/grafana/loki/pkg/validation"
	//"github.com/grafana/loki/tools/tsdb/helpers"
	"math"
	"os"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	logql "github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/storage"

	//"github.com/grafana/loki/pkg/storage/chunk/client"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper"
)

type Compactor struct {
	services.Service

	cfg                Config
	logger             log.Logger
	bloomCompactorRing ring.ReadRing

	// temporary workaround until store has implemented read/write shipper interface
	bloomShipperClient bloomshipper.Client
	indexShipper       indexshipper.IndexShipper
	chunkClient        client.Client
}

func New(cfg Config,
	readRing ring.ReadRing,
	storageCfg storage.Config,
	schemaConfig config.SchemaConfig,
	limits downloads.Limits,
	logger log.Logger,
	clientMetrics storage.ClientMetrics,
	_ prometheus.Registerer) (*Compactor, error) {
	c := &Compactor{
		cfg:                cfg,
		logger:             logger,
		bloomCompactorRing: readRing,
	}

	//Configure ObjectClient and IndexShipper for series and chunk management
	objectClient, err := storage.NewObjectClient(storageCfg.TSDBShipperConfig.SharedStoreType, storageCfg, clientMetrics)
	if err != nil {
		return nil, err
	}

	chunkClient := client.NewClient(objectClient, nil, schemaConfig)

	tableRanges := GetIndexStoreTableRanges(config.TSDBType, schemaConfig.Configs)

	openFn := func(p string) (shipperindex.Index, error) {
		return tsdb.OpenShippableTSDB(p, tsdb.IndexOpts{})
	}
	indexShipper, err := indexshipper.NewIndexShipper(
		storageCfg.TSDBShipperConfig.Config,
		objectClient,
		limits,
		nil, // No need for tenant filter
		openFn,
		tableRanges[len(tableRanges)-1],
		prometheus.WrapRegistererWithPrefix("loki_tsdb_shipper_", prometheus.DefaultRegisterer),
		util_log.Logger,
	)
	//Configure BloomClient for meta.json management
	bloomClient, err := bloomshipper.NewBloomClient(schemaConfig.Configs, storageCfg, clientMetrics)
	if err != nil {
		return nil, err
	}

	// temporary workaround until store has implemented read/write shipper interface
	c.bloomShipperClient = bloomClient
	c.indexShipper = indexShipper
	c.chunkClient = chunkClient

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

// TODO this logic is used in multiple places, better to move to a common place.
// copied from storage/store.go
func GetIndexStoreTableRanges(indexType string, periodicConfigs []config.PeriodConfig) config.TableRanges {
	var ranges config.TableRanges
	for i := range periodicConfigs {
		if periodicConfigs[i].IndexType != indexType {
			continue
		}

		periodEndTime := config.DayTime{Time: math.MaxInt64}
		if i < len(periodicConfigs)-1 {
			periodEndTime = config.DayTime{Time: periodicConfigs[i+1].From.Time.Add(-time.Millisecond)}
		}

		ranges = append(ranges, periodicConfigs[i].GetIndexTableNumberRange(periodEndTime))
	}
	return ranges
}

func NoopGetFingerprintRange() (uint64, uint64) { return 0, 0 }

// TODO List Users from TSDB and add logic to owned user via ring
func NoopGetUserID() string { return "" }

func fillBloom(b v1.SeriesWithBloom, chks []chunk.Chunk) error {
	// TODO remove/replace once https://github.com/grafana/loki/pull/10957 is merged
	tokenizer := func(e logproto.Entry) [][]byte {
		return bytes.Split([]byte(e.Line), []byte(` `))
	}

	for _, c := range chks {
		itr, err := c.Data.(*chunkenc.Facade).LokiChunk().Iterator(
			context.Background(),
			time.Unix(0, 0),
			time.Unix(0, math.MaxInt64),
			logproto.FORWARD,
			logql.NewNoopPipeline().ForStream(c.Metric),
		)
		if err != nil {
			return err
		}
		defer itr.Close()
		for itr.Next() {
			for _, t := range tokenizer(itr.Entry()) {
				b.Bloom.Add(t)
			}
		}
		if err := itr.Error(); err != nil {
			return err
		}
		b.Series.Chunks = append(b.Series.Chunks, v1.ChunkRef{
			Start:    c.From,
			End:      c.Through,
			Checksum: c.Checksum,
		})
	}
	return nil
}

func makeChunkRefs(chksMetas []tsdbindex.ChunkMeta, tenant string, fp model.Fingerprint) []chunk.Chunk {
	chunkRefs := make([]chunk.Chunk, 0, len(chksMetas))
	for _, chk := range chksMetas {
		chunkRefs = append(chunkRefs, chunk.Chunk{
			ChunkRef: logproto.ChunkRef{
				Fingerprint: uint64(fp),
				UserID:      tenant,
				From:        chk.From(),
				Through:     chk.Through(),
				Checksum:    chk.Checksum,
			},
		})
	}
	return chunkRefs
}

func buildBloomBlock(dst string, bloomForChks v1.SeriesWithBloom,
	tenant string, tableName string, fp model.Fingerprint, from, through model.Time) (bloomshipper.Block, error) {
	// TODO Revisit this step once v1/bloom lib updated to combine blooms in the same series
	writer := v1.NewDirectoryBlockWriter(dst)
	builder, err := v1.NewBlockBuilder(v1.NewBlockOptions(), writer)
	if err != nil {
		level.Info(util_log.Logger).Log("creating builder", err)
		return bloomshipper.Block{}, err
	}

	//write bloom to a local dir
	err = builder.BuildFrom(v1.NewSliceIter([]v1.SeriesWithBloom{bloomForChks}))
	if err != nil {
		level.Info(util_log.Logger).Log("writing bloom", err)
		return bloomshipper.Block{}, err
	}

	// meta + upload to storage
	// TODO - Which one is better to read the block via BlockReader or file pointer?
	//blockFile := v1.NewBlock(v1.NewDirectoryBlockReader(dst))
	//err = blockFile.LoadHeaders()

	blockFile, err := os.OpenFile(filepath.Join(dst, "bloom"), os.O_RDONLY, 0700)
	if err != nil {
		level.Info(util_log.Logger).Log("reading bloomBlock", err)
	}

	blocks := bloomshipper.Block{
		BlockRef: bloomshipper.BlockRef{
			Ref: bloomshipper.Ref{
				TenantID:       tenant,
				TableName:      tableName,
				MinFingerprint: uint64(fp),
				MaxFingerprint: uint64(fp),
				StartTimestamp: from.Unix(),
				EndTimestamp:   through.Unix(),
				Checksum:       1, // FIXME get checksum.
			},
			IndexPath: filepath.Join(dst, "series"),
			BlockPath: filepath.Join(dst, "bloom"),
		},
		Data: blockFile,
	}

	return blocks, nil

}

// part1: Create a compact method that assumes no block/meta files exists (eg first compaction)
// part2: Write logic to check first for existing block/meta files and does above.
func CompactNewChunks(ctx context.Context, from model.Time, through model.Time, bloomShipperClient bloomshipper.Client, chunkClient client.Client, indexShipper indexshipper.IndexShipper, objectClient client.ObjectClient, dst string) (err error) {
	tenant := "123"     //FIXME all somehow and loop over
	tableName := "1234" //FIXME get proper tables

	err = indexShipper.ForEach(
		context.Background(),
		tableName,
		tenant,
		func(isMultiTenantIndex bool, idx shipperindex.Index) error {
			if isMultiTenantIndex {
				return nil
			}

			_ = idx.(*tsdb.TSDBFile).Index.(*tsdb.TSDBIndex).ForSeries(
				context.Background(),
				nil,           // TODO no shards for now, revisit with ring work
				from, through, // get all timestamps
				// Get chunks for a series label and a fp
				func(ls labels.Labels, fp model.Fingerprint, chksMetas []tsdbindex.ChunkMeta) {
					// Create a bloom for this series
					bloomForChks := v1.SeriesWithBloom{
						Series: &v1.Series{
							Fingerprint: fp,
						},
						Bloom: &v1.Bloom{
							*filter.NewDefaultScalableBloomFilter(0.01),
						},
					}

					// Get chunks data from list of chunkRefs
					chks, err := chunkClient.GetChunks(
						context.Background(),
						makeChunkRefs(chksMetas, tenant, fp),
					)
					if err != nil {
						level.Info(util_log.Logger).Log("getting chunks", err)
						return
					}

					// Fill the bloom with chunk data
					err = fillBloom(bloomForChks, chks)
					if err != nil {
						level.Info(util_log.Logger).Log("filling blooms", err)
						return
					}

					// Build and upload bloomBlock to storage
					blocks, err := buildBloomBlock(dst, bloomForChks, tenant, tableName, fp, from, through)
					if err != nil {
						level.Info(util_log.Logger).Log("building bloomBlocks", err)
						return
					}

					_, err = bloomShipperClient.PutBlocks(ctx, []bloomshipper.Block{blocks})
					if err != nil {
						level.Info(util_log.Logger).Log("putting blocks to storage", err)
						return
					}

					// Build and upload meta.json to storage
					meta := bloomshipper.Meta{
						// After successful compaction there should be no tombstones
						Tombstones: make([]bloomshipper.BlockRef, 0),
						Blocks:     []bloomshipper.BlockRef{blocks.BlockRef},
					}

					err = bloomShipperClient.PutMeta(ctx, meta)
					if err != nil {
						level.Info(util_log.Logger).Log("putting meta.json to storage", err)
						return
					}
				},
				labels.MustNewMatcher(labels.MatchEqual, "", ""),
			)
			return nil
		},
	)

	if err != nil {
		return err
	}
	return nil
}

func (c *Compactor) runCompact(ctx context.Context, limits downloads.Limits, bloomShipperClient bloomshipper.Client, chunkClient client.Client, indexShipper indexshipper.IndexShipper, objectClient client.ObjectClient) error {
	maxCompactBloomPeriod := time.Duration(limits.DefaultLimits().RejectOldSamplesMaxAge)

	stFp, endFp := NoopGetFingerprintRange()
	tenantID := NoopGetUserID()

	through := time.Now().UnixMilli()
	from := through - maxCompactBloomPeriod.Milliseconds()

	metaSearchParams := bloomshipper.MetaSearchParams{
		TenantID:       tenantID,
		MinFingerprint: stFp,
		MaxFingerprint: endFp,
		StartTimestamp: from,
		EndTimestamp:   through,
	}

	metas, err := bloomShipperClient.GetMetas(ctx, metaSearchParams)
	if err != nil {
		return err
	}

	if len(metas) == 0 {
		// run compaction from scratch
		tempDst := os.TempDir()
		err = CompactNewChunks(ctx, model.Time(from), model.Time(through), bloomShipperClient, chunkClient, indexShipper, objectClient, tempDst)
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

		// TODO complete part 2 - add part to compare chunks and blocks.
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
