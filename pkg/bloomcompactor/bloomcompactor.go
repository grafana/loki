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
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	chunk_client "github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/downloads"
	shipperindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/index"
	index_storage "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/storage"
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

	// Client used to run operations on the bucket storing bloom blocks.
	storeClients map[config.DayTime]storeClient

	// temporary workaround until store has implemented read/write shipper interface
	bloomShipperClient bloomshipper.Client
}

type storeClient struct {
	object       chunk_client.ObjectClient
	index        index_storage.Client
	chunk        chunk_client.Client
	indexShipper indexshipper.IndexShipper
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

	//Configure BloomClient for meta.json management
	bloomClient, err := bloomshipper.NewBloomClient(schemaConfig.Configs, storageCfg, clientMetrics)
	if err != nil {
		return nil, err
	}

	c.storeClients = make(map[config.DayTime]storeClient)

	for i, periodicConfig := range schemaConfig.Configs {
		var indexStorageCfg indexshipper.Config
		switch periodicConfig.IndexType {
		case config.TSDBType:
			indexStorageCfg = storageCfg.TSDBShipperConfig.Config
		case config.BoltDBShipperType:
			indexStorageCfg = storageCfg.BoltDBShipperConfig.Config
		default:
			level.Warn(util_log.Logger).Log("msg", "skipping period because index type is unsupported")
			continue
		}

		//Configure ObjectClient and IndexShipper for series and chunk management
		objectClient, err := storage.NewObjectClient(periodicConfig.ObjectType, storageCfg, clientMetrics)
		if err != nil {
			return nil, fmt.Errorf("error creating object client '%s': %w", periodicConfig.ObjectType, err)
		}

		periodEndTime := config.DayTime{Time: math.MaxInt64}
		if i < len(schemaConfig.Configs)-1 {
			periodEndTime = config.DayTime{Time: schemaConfig.Configs[i+1].From.Time.Add(-time.Millisecond)}
		}

		indexShipper, err := indexshipper.NewIndexShipper(
			periodicConfig.IndexTables.PathPrefix,
			indexStorageCfg,
			objectClient,
			limits,
			nil,
			func(p string) (shipperindex.Index, error) {
				return tsdb.OpenShippableTSDB(p, tsdb.IndexOpts{})
			},
			periodicConfig.GetIndexTableNumberRange(periodEndTime),
			prometheus.WrapRegistererWithPrefix("loki_tsdb_shipper_", prometheus.DefaultRegisterer),
			logger,
		)

		if err != nil {
			return nil, errors.Wrap(err, "create index shipper")
		}

		c.storeClients[periodicConfig.From] = storeClient{
			object:       objectClient,
			index:        index_storage.NewIndexStorageClient(objectClient, periodicConfig.IndexTables.PathPrefix),
			chunk:        chunk_client.NewClient(objectClient, nil, schemaConfig),
			indexShipper: indexShipper,
		}

	}

	// temporary workaround until store has implemented read/write shipper interface
	c.bloomShipperClient = bloomClient

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
	tableName, tenant string
	labels            labels.Labels
	fingerPrint       model.Fingerprint
	chunks            []chunk.Chunk
	from, through     model.Time
	indexPath         string
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

// TODO Revisit this step once v1/bloom lib updated to combine blooms in the same series
func buildBloomBlock(bloomForChks v1.SeriesWithBloom, series Series) (bloomshipper.Block, error) {
	localDst := createLocalFileName(series)

	//write bloom to a local dir
	builder, err := v1.NewBlockBuilder(v1.NewBlockOptions(), v1.NewDirectoryBlockWriter(localDst))
	if err != nil {
		level.Info(util_log.Logger).Log("creating builder", err)
		return bloomshipper.Block{}, err
	}

	err = builder.BuildFrom(v1.NewSliceIter([]v1.SeriesWithBloom{bloomForChks}))
	if err != nil {
		level.Info(util_log.Logger).Log("writing bloom", err)
		return bloomshipper.Block{}, err
	}

	blockFile, err := os.Open(filepath.Join(localDst, "bloom"))
	if err != nil {
		level.Info(util_log.Logger).Log("reading bloomBlock", err)
	}

	// read the checksum
	if _, err := blockFile.Seek(-4, 2); err != nil {
		return bloomshipper.Block{}, errors.Wrap(err, "seeking to bloom checksum")
	}
	checksum := make([]byte, 4)
	if _, err := blockFile.Read(checksum); err != nil {
		return bloomshipper.Block{}, errors.Wrap(err, "reading bloom checksum")
	}

	// Reset back to beginning
	if _, err := blockFile.Seek(0, 0); err != nil {
		return bloomshipper.Block{}, errors.Wrap(err, "seeking to back to beginning of the file")
	}

	objectStoreDst := createObjStorageFileName(series, fmt.Sprint(binary.BigEndian.Uint32(checksum)))

	blocks := bloomshipper.Block{
		BlockRef: bloomshipper.BlockRef{
			Ref: bloomshipper.Ref{
				TenantID:       series.tenant,
				TableName:      series.tableName,
				MinFingerprint: uint64(series.fingerPrint), //TODO will change once we compact multiple blooms into a block
				MaxFingerprint: uint64(series.fingerPrint),
				StartTimestamp: series.from.Unix(),
				EndTimestamp:   series.through.Unix(),
				Checksum:       binary.BigEndian.Uint32(checksum),
			},
			IndexPath: series.indexPath, // For reviewer, this is the same filepath with the original TSDBindex for this series?
			BlockPath: objectStoreDst,
		},
		Data: blockFile,
	}

	return blocks, nil
}

func listSeries(ctx context.Context, objectClient storeClient) ([]Series, error) {
	// Returns all the TSDB files, including subdirectories
	prefix := "index/"
	indices, _, err := objectClient.object.List(ctx, prefix, "")

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

			result = append(result, Series{tableName: tableName, tenant: userID, indexPath: index.Key})
		}
	}
	return result, nil
}

func createLocalFileName(series Series) string {
	return fmt.Sprintf("bloomBlock-%s-%s-%s-%s-%s-%s", series.tableName, series.tenant, series.fingerPrint, series.fingerPrint, series.from, series.through)
}
func createObjStorageFileName(series Series, checksum string) string {
	// TODO fix series_fp repeating when creating multi-series bloom blocks
	// From design doc, naming should be something like
	//`bloom/<period>/<tenant>/blooms/<start_fp>-<end_fp>/<start_ts>-<end_ts>-<checksum>`
	fileName := fmt.Sprintf("blooms/%s-%s/%s-%s-%s", series.fingerPrint, series.fingerPrint, series.from, series.through, checksum)
	return strings.Join([]string{"bloom", series.tableName, series.tenant, fileName}, "/")
}

func CompactNewChunks(ctx context.Context, series Series, bloomShipperClient bloomshipper.Client) (err error) {
	// Create a bloom for this series
	bloomForChks := v1.SeriesWithBloom{
		Series: &v1.Series{
			Fingerprint: series.fingerPrint,
		},
		Bloom: &v1.Bloom{
			ScalableBloomFilter: *filter.NewDefaultScalableBloomFilter(0.01),
		},
	}

	// create a tokenizer
	bt, _ := v1.NewBloomTokenizer(prometheus.DefaultRegisterer)
	bt.PopulateSeriesWithBloom(&bloomForChks, series.chunks)

	// Build and upload bloomBlock to storage
	blocks, err := buildBloomBlock(bloomForChks, series)
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

	return nil
}

func (c *Compactor) runCompact(ctx context.Context, bloomShipperClient bloomshipper.Client, storeClient storeClient) error {

	series, err := listSeries(ctx, storeClient)

	if err != nil {
		return err
	}

	for _, s := range series {
		err := storeClient.indexShipper.ForEach(ctx, s.tableName, s.tenant, func(isMultiTenantIndex bool, idx shipperindex.Index) error {
			if isMultiTenantIndex {
				return nil
			}

			_ = idx.(*tsdb.TSDBFile).Index.(*tsdb.TSDBIndex).ForSeries(
				ctx,
				nil,              // TODO no shards for now, revisit with ring work
				0, math.MaxInt64, // Replace with MaxLookBackPeriod

				// Get chunks for a series label and a fp
				func(ls labels.Labels, fp model.Fingerprint, chksMetas []tsdbindex.ChunkMeta) {

					// TODO call bloomShipperClient.GetMetas to get existing meta.json
					var metas []bloomshipper.Meta

					if len(metas) == 0 {
						// Get chunks data from list of chunkRefs
						chks, err := storeClient.chunk.GetChunks(
							ctx,
							makeChunkRefs(chksMetas, s.tenant, fp),
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
							tableName:   s.tableName,
							tenant:      s.tenant,
							labels:      ls,
							fingerPrint: fp,
							chunks:      chks,
							from:        minFrom,
							through:     maxThrough,
							indexPath:   s.indexPath,
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
		if err != nil {
			return errors.Wrap(err, "getting each series")
		}
	}
	return nil
}
