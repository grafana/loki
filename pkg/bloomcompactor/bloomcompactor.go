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
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/downloads"
	shipperindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage"
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

type Series struct { // TODO this can be replaced with Job struct based on Salva's ring work.
	TableName, Tenant string
	Labels            labels.Labels
	FingerPrint       model.Fingerprint
	Chunks            []chunk.Chunk
	from, through     model.Time
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

func listSeries(ctx context.Context, objectClient client.ObjectClient) ([]Series, error) {
	// Returns all the TSDB files, including subdirectories
	prefix := "index/"
	indices, _, err := objectClient.List(ctx, prefix, "")

	if err != nil {
		return nil, err
	}

	var result []Series

	for _, index := range indices {
		s := strings.Split(index.Key, "/")

		if len(s) > 3 {
			tableName := s[1]

			if !strings.HasPrefix(tableName, "loki_") || strings.Contains(tableName, "backup") {
				continue
			}

			userID := s[2]
			_, err := strconv.Atoi(userID)
			if err != nil {
				continue
			}

			result = append(result, Series{TableName: tableName, Tenant: userID})
		}
	}
	return result, nil
}

// part1: Create a compact method that assumes no block/meta files exists (eg first compaction)
// part2: Write logic to check first for existing block/meta files and does above.
func CompactNewChunks(ctx context.Context, series Series, bloomShipperClient bloomshipper.Client, dst string) (err error) {
	fp := series.FingerPrint
	tenant := series.Tenant
	tableName := series.TableName
	from := series.from
	through := series.through

	// Create a bloom for this series
	bloomForChks := v1.SeriesWithBloom{
		Series: &v1.Series{
			Fingerprint: fp,
		},
		Bloom: &v1.Bloom{
			*filter.NewDefaultScalableBloomFilter(0.01),
		},
	}

	// create a tokenizer
	bt, _ := v1.NewBloomTokenizer(prometheus.DefaultRegisterer)
	bt.PopulateSBF(&bloomForChks, series.Chunks)

	// Build and upload bloomBlock to storage
	blocks, err := buildBloomBlock(dst, bloomForChks, tenant, tableName, fp, from, through)
	if err != nil {
		level.Info(util_log.Logger).Log("building bloomBlocks", err)
		return
	}

	//FIXME zip (archive) blocks before uploading to storage
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

	return nil
}

func (c *Compactor) runCompact(ctx context.Context, limits downloads.Limits, bloomShipperClient bloomshipper.Client, chunkClient client.Client, indexShipper indexshipper.IndexShipper, objectClient client.ObjectClient) error {

	series, err := listSeries(ctx, objectClient)

	if err != nil {
		return err
	}

	for _, s := range series {
		indexShipper.ForEach(ctx, s.TableName, s.Tenant, func(isMultiTenantIndex bool, idx shipperindex.Index) error {
			if isMultiTenantIndex {
				return nil
			}

			_ = idx.(*tsdb.TSDBFile).Index.(*tsdb.TSDBIndex).ForSeries(
				ctx,
				nil,              // TODO no shards for now, revisit with ring work
				0, math.MaxInt64, // get all timestamps
				// Get chunks for a series label and a fp
				func(ls labels.Labels, fp model.Fingerprint, chksMetas []tsdbindex.ChunkMeta) {
					// TODO call bloomShipperClient.GetMetas to get existing meta.json
					var metas []bloomshipper.Meta

					if len(metas) == 0 {
						// run compaction from scratch
						tempDst := os.TempDir()

						// Get chunks data from list of chunkRefs
						chks, err := chunkClient.GetChunks(
							ctx,
							makeChunkRefs(chksMetas, s.Tenant, fp),
						)
						if err != nil {
							level.Info(util_log.Logger).Log("getting chunks", err)
							return
						}

						// effectively get min and max of timestamps of the list of chunks in a series
						// There must be a better way to get this, ordering chunkRefs by timestamp doesn't fully solve it
						// chunk files name have this info in ObjectStore, but it's not really exposed
						minFrom := model.Latest
						maxThrough := model.Earliest

						for _, c := range chks {
							if minFrom > c.From {
								minFrom = c.From
							}
							if maxThrough < c.From {
								maxThrough = c.Through
							}
						}

						series := Series{
							TableName:   s.TableName,
							Tenant:      s.Tenant,
							Labels:      ls,
							FingerPrint: fp,
							Chunks:      chks,
							from:        minFrom,
							through:     maxThrough,
						}
						err = CompactNewChunks(ctx, series, bloomShipperClient)
						if err != nil {
							return
						}
					} else {
						// TODO complete part 2 - periodic compaction for delta from previous period
						// When already compacted metas exists
						// Deduplicate index paths
						uniqueIndexPaths := make(map[string]struct{})

						for _, meta := range metas {
							for _, blockRef := range meta.Blocks {
								uniqueIndexPaths[blockRef.IndexPath] = struct{}{}
								//...
							}
						}

					}
				})
			return nil
		})
	}
	return nil
}
