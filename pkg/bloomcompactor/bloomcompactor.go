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
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sort"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	chunk_client "github.com/grafana/loki/pkg/storage/chunk/client"
	shipperindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/index"
	index_storage "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/compactor/retention"
	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper"
)

const (
	fpRate        = 0.01
	bloomFileName = "bloom"
)

type Compactor struct {
	services.Service

	cfg       Config
	logger    log.Logger
	schemaCfg config.SchemaConfig

	// temporary workaround until store has implemented read/write shipper interface
	bloomShipperClient bloomshipper.Client

	// Client used to run operations on the bucket storing bloom blocks.
	storeClients map[config.DayTime]storeClient

	sharding ShardingStrategy

	metrics *metrics
}

type storeClient struct {
	object       chunk_client.ObjectClient
	index        index_storage.Client
	chunk        chunk_client.Client
	indexShipper indexshipper.IndexShipper
}

func New(
	cfg Config,
	storageCfg storage.Config,
	schemaConfig config.SchemaConfig,
	limits Limits,
logger log.Logger,
	sharding ShardingStrategy,
	clientMetrics storage.ClientMetrics,
	r prometheus.Registerer,
) (*Compactor, error) {
	c := &Compactor{
		cfg:       cfg,
		logger:    logger,
		schemaCfg: schemaConfig,
		sharding:  sharding,
	}

	// Configure BloomClient for meta.json management
	bloomClient, err := bloomshipper.NewBloomClient(schemaConfig.Configs, storageCfg, clientMetrics)
	if err != nil {
		return nil, err
	}

	c.storeClients = make(map[config.DayTime]storeClient)

	for i, periodicConfig := range schemaConfig.Configs {
		var indexStorageCfg indexshipper.Config
		switch periodicConfig.IndexType {
		case config.TSDBType:
			indexStorageCfg = storageCfg.TSDBShipperConfig
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
				return tsdb.OpenShippableTSDB(p)
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

	c.metrics = newMetrics(r)
	c.metrics.compactionRunInterval.Set(cfg.CompactionInterval.Seconds())

	c.Service = services.NewBasicService(c.starting, c.running, c.stopping)

	return c, nil
}

func (c *Compactor) starting(_ context.Context) (err error) {
	c.metrics.compactorRunning.Set(1)
	return err
}

func (c *Compactor) running(ctx context.Context) error {
	// Run an initial compaction before starting the interval.
	if err := c.runCompaction(ctx); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to run compaction", "err", err)
	}

	ticker := time.NewTicker(util.DurationWithJitter(c.cfg.CompactionInterval, 0.05))
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.metrics.compactionRunsStarted.Inc()
			if err := c.runCompaction(ctx); err != nil {
				c.metrics.compactionRunsErred.Inc()
				level.Error(util_log.Logger).Log("msg", "failed to run compaction", "err", err)
				continue
			}
			c.metrics.compactionRunsCompleted.Inc()
		case <-ctx.Done():
			return nil
		}
	}
}

func (c *Compactor) stopping(_ error) error {
	c.metrics.compactorRunning.Set(0)
	return nil
}

func (c *Compactor) runCompaction(ctx context.Context) error {
	var (
		tables []string
		// it possible for two periods to use the same storage bucket and path prefix (different indexType or schema version)
		// so more than one index storage client may end up listing the same set of buckets
		// avoid including the same table twice in the compact tables list.
		seen = make(map[string]struct{})
	)
	for _, sc := range c.storeClients {
		// refresh index list cache since previous compaction would have changed the index files in the object store
		sc.index.RefreshIndexTableNamesCache(ctx)
		tbls, err := sc.index.ListTables(ctx)
		if err != nil {
			return fmt.Errorf("failed to list tables: %w", err)
		}

		for _, table := range tbls {
			if _, ok := seen[table]; ok {
				continue
			}

			tables = append(tables, table)
			seen[table] = struct{}{}
		}
	}

	// process most recent tables first
	sortTablesByRange(tables)

	// apply passed in compaction limits
	if c.cfg.SkipLatestNTables <= len(tables) {
		tables = tables[c.cfg.SkipLatestNTables:]
	}
	if c.cfg.TablesToCompact > 0 && c.cfg.TablesToCompact < len(tables) {
		tables = tables[:c.cfg.TablesToCompact]
	}

	// Reset discovered tenants metric since we will increase it in compactTable.
	c.metrics.compactionRunDiscoveredTenants.Set(0)

	parallelism := c.cfg.MaxCompactionParallelism
	if parallelism == 0 {
		parallelism = len(tables)
	}

	errs := multierror.New()
	if err := concurrency.ForEachJob(ctx, len(tables), parallelism, func(ctx context.Context, i int) error {
		tableName := tables[i]
		level.Info(util_log.Logger).Log("msg", "compacting table", "table-name", tableName)
		err := c.compactTable(ctx, tableName)
		if err != nil {
			errs.Add(err)
			return nil
		}
		level.Info(util_log.Logger).Log("msg", "finished compacting table", "table-name", tableName)
		return nil
	}); err != nil {
		errs.Add(err)
	}

	return errs.Err()
}

func (c *Compactor) compactTable(ctx context.Context, tableName string) error {
	schemaCfg, ok := schemaPeriodForTable(c.schemaCfg, tableName)
	if !ok {
		level.Error(util_log.Logger).Log("msg", "skipping compaction since we can't find schema for table", "table", tableName)
		return nil
	}

	sc, ok := c.storeClients[schemaCfg.From]
	if !ok {
		return fmt.Errorf("index store client not found for period starting at %s", schemaCfg.From.String())
	}

	_, tenants, err := sc.index.ListFiles(ctx, tableName, false)
	if err != nil {
		return fmt.Errorf("failed to list files for table %s: %w", tableName, err)
	}

	c.metrics.compactionRunDiscoveredTenants.Add(float64(len(tenants)))
	level.Info(c.logger).Log("msg", "discovered tenants from bucket", "users", len(tenants))
	return c.compactUsers(ctx, sc, tableName, tenants)
}

// See: https://github.com/grafana/mimir/blob/34852137c332d4050e53128481f4f6417daee91e/pkg/compactor/compactor.go#L566-L689
func (c *Compactor) compactUsers(ctx context.Context, sc storeClient, tableName string, tenants []string) error {
	// When starting multiple compactor replicas nearly at the same time, running in a cluster with
	// a large number of tenants, we may end up in a situation where the 1st user is compacted by
	// multiple replicas at the same time. Shuffling users helps reduce the likelihood this will happen.
	rand.Shuffle(len(tenants), func(i, j int) {
		tenants[i], tenants[j] = tenants[j], tenants[i]
	})

	// Keep track of tenants owned by this shard, so that we can delete the local files for all other users.
	errs := multierror.New()
	ownedTenants := make(map[string]struct{}, len(tenants))
	for _, tenant := range tenants {
		// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("interrupting compaction of tenants: %w", err)
		}

		// Ensure the tenant ID belongs to our shard.
		owned, err := c.sharding.OwnsTenant(tenant)
		if err != nil {
			c.metrics.compactionRunSkippedTenants.Inc()
			level.Warn(c.logger).Log("msg", "unable to check if tenant is owned by this shard", "tenantID", tenant, "err", err)
			continue
		}
		if !owned {
			c.metrics.compactionRunSkippedTenants.Inc()
			level.Debug(c.logger).Log("msg", "skipping tenant because it is not owned by this shard", "tenantID", tenant)
			continue
		}

		ownedTenants[tenant] = struct{}{}

		if err := c.compactTenantWithRetries(ctx, sc, tableName, tenant); err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				// We don't want to count shutdowns as failed compactions because we will pick up with the rest of the compaction after the restart.
				level.Info(c.logger).Log("msg", "compaction for tenant was interrupted by a shutdown", "tenant", tenant)
				return nil
			default:
				c.metrics.compactionRunFailedTenants.Inc()
				level.Error(c.logger).Log("msg", "failed to compact tenant", "tenant", tenant, "err", err)
				errs.Add(err)
			}
			continue
		}

		c.metrics.compactionRunSucceededTenants.Inc()
		level.Info(c.logger).Log("msg", "successfully compacted tenant", "tenant", tenant)
	}

	return errs.Err()

	// TODO: Delete local files for unowned tenants, if there are any.
}

func (c *Compactor) compactTenant(ctx context.Context, sc storeClient, tableName string, tenant string) error {
	level.Info(c.logger).Log("msg", "starting compaction of tenant", "tenant", tenant)

	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return err
	}

	// TODO: Use ForEachConcurrent?
	errs := multierror.New()
	if err := sc.indexShipper.ForEach(ctx, tableName, tenant, func(isMultiTenantIndex bool, idx shipperindex.Index) error {
		if isMultiTenantIndex {
			return fmt.Errorf("unexpected multi-tenant")
		}

		// TODO: Make these casts safely
		if err := idx.(*tsdb.TSDBFile).Index.(*tsdb.TSDBIndex).ForSeries(
			ctx, nil,
			0, math.MaxInt64, // TODO: Replace with MaxLookBackPeriod
			func(labels labels.Labels, fingerprint model.Fingerprint, chksMetas []tsdbindex.ChunkMeta) {
				job := NewJob(tenant, tableName, fingerprint, chksMetas)

				ownsJob, err := c.sharding.OwnsJob(job)
				if err != nil {
					c.metrics.compactionRunSkippedJobs.Inc()
					level.Error(c.logger).Log("msg", "failed to check if compactor owns job", "job", job, "err", err)
					errs.Add(err)
					return
				}
				if !ownsJob {
					c.metrics.compactionRunSkippedJobs.Inc()
					level.Debug(c.logger).Log("msg", "skipping job because it is not owned by this shard", "job", job)
					return
				}

				if err := c.runCompact(ctx, c.bloomShipperClient, sc, job); err != nil {
					c.metrics.compactionRunFailedJobs.Inc()
					errs.Add(errors.Wrap(err, "runBloomCompact"))
					return
				}

				c.metrics.compactionRunSucceededJobs.Inc()
			},
		); err != nil {
			errs.Add(err)
		}

		return nil
	}); err != nil {
		errs.Add(err)
	}

	return errs.Err()
}

func runWithRetries(
	ctx context.Context,
	minBackoff, maxBackoff time.Duration,
	maxRetries int,
	f func(ctx context.Context) error,
) error {
	var lastErr error

	retries := backoff.New(ctx, backoff.Config{
		MinBackoff: minBackoff,
		MaxBackoff: maxBackoff,
		MaxRetries: maxRetries,
	})

	for retries.Ongoing() {
		lastErr = f(ctx)
		if lastErr == nil {
			return nil
		}

		retries.Wait()
	}

	return lastErr
}

func (c *Compactor) compactTenantWithRetries(ctx context.Context, sc storeClient, tableName string, tenant string) error {
	return runWithRetries(
		ctx,
		c.cfg.retryMinBackoff,
		c.cfg.retryMaxBackoff,
		c.cfg.CompactionRetries,
		func(ctx context.Context) error {
			return c.compactTenant(ctx, sc, tableName, tenant)
		},
	)
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
func buildBloomBlock(bloomForChks v1.SeriesWithBloom, series Series, workingDir string) (bloomshipper.Block, error) {
	localDst := createLocalDirName(workingDir, series)

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

	blockFile, err := os.Open(filepath.Join(localDst, bloomFileName))
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
			IndexPath: series.indexPath,
		},
		Data: blockFile,
	}

	return blocks, nil
}

// TODO Will be replaced with ring implementation in https://github.com/grafana/loki/pull/11154/
func listSeriesForBlooms(ctx context.Context, objectClient storeClient) ([]Series, error) {
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

func createLocalDirName(workingDir string, series Series) string {
	dir := fmt.Sprintf("bloomBlock-%s-%s-%s-%s-%s-%s", series.tableName, series.tenant, series.fingerPrint, series.fingerPrint, series.from, series.through)
	return filepath.Join(workingDir, dir)
}

func CompactNewChunks(ctx context.Context, series Series, bt *v1.BloomTokenizer, bloomShipperClient bloomshipper.Client, dst string) (err error) {
	// Create a bloom for this series
	bloomForChks := v1.SeriesWithBloom{
		Series: &v1.Series{
			Fingerprint: series.fingerPrint,
		},
		Bloom: &v1.Bloom{
			ScalableBloomFilter: *filter.NewDefaultScalableBloomFilter(fpRate),
		},
	}

	// Tokenize data into n-grams
	bt.PopulateSeriesWithBloom(&bloomForChks, series.chunks)

	// Build and upload bloomBlock to storage
	blocks, err := buildBloomBlock(bloomForChks, series, dst)
	if err != nil {
		level.Info(util_log.Logger).Log("building bloomBlocks", err)
		return
	}

	storedBlocks, err := bloomShipperClient.PutBlocks(ctx, []bloomshipper.Block{blocks})
	if err != nil {
		level.Info(util_log.Logger).Log("putting blocks to storage", err)
		return
	}

	storedBlockRefs := make([]bloomshipper.BlockRef, len(storedBlocks))
	// Build and upload meta.json to storage
	meta := bloomshipper.Meta{
		// After successful compaction there should be no tombstones
		Tombstones: make([]bloomshipper.BlockRef, 0),
		Blocks:     storedBlockRefs,
	}

	//TODO move this to an outer layer, otherwise creates a meta per block
	err = bloomShipperClient.PutMeta(ctx, meta)
	if err != nil {
		level.Info(util_log.Logger).Log("putting meta.json to storage", err)
		return
	}

	return nil
}

func (c *Compactor) runCompact(ctx context.Context, bloomShipperClient bloomshipper.Client, storeClient storeClient, _ Job) error {

	series, err := listSeriesForBlooms(ctx, storeClient)

	// TODO tokenizer is not thread-safe
	// consider moving to Job/worker level with https://github.com/grafana/loki/pull/11154/
	// create a tokenizer
	bt, _ := v1.NewBloomTokenizer(prometheus.DefaultRegisterer)

	if err != nil {
		return err
	}

	for _, s := range series {
		err := storeClient.indexShipper.ForEach(ctx, s.tableName, s.tenant, func(isMultiTenantIndex bool, idx shipperindex.Index) error {
			if isMultiTenantIndex {
				return nil
			}

			// TODO make this casting safe
			_ = idx.(*tsdb.TSDBFile).Index.(*tsdb.TSDBIndex).ForSeries(
				ctx,
				nil,              // Process all shards
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

						err = CompactNewChunks(ctx, series, bt, bloomShipperClient, c.cfg.WorkingDirectory)
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

// TODO: comes from pkg/compactor/compactor.go
func sortTablesByRange(tables []string) {
	tableRanges := make(map[string]model.Interval)
	for _, table := range tables {
		tableRanges[table] = retention.ExtractIntervalFromTableName(table)
	}

	sort.Slice(tables, func(i, j int) bool {
		// less than if start time is after produces a most recent first sort order
		return tableRanges[tables[i]].Start.After(tableRanges[tables[j]].Start)
	})

}

// TODO: comes from pkg/compactor/compactor.go
func schemaPeriodForTable(cfg config.SchemaConfig, tableName string) (config.PeriodConfig, bool) {
	tableInterval := retention.ExtractIntervalFromTableName(tableName)
	schemaCfg, err := cfg.SchemaForTime(tableInterval.Start)
	if err != nil || schemaCfg.IndexTables.TableFor(tableInterval.Start) != tableName {
		return config.PeriodConfig{}, false
	}

	return schemaCfg, true
}
