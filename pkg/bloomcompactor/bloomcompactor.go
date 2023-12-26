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
	"math"
	"os"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"path/filepath"

	"github.com/google/uuid"

	"github.com/grafana/loki/pkg/bloomutils"
	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	chunk_client "github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper"
	shipperindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/index"
	index_storage "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/pkg/util"
)

type Compactor struct {
	services.Service

	cfg       Config
	logger    log.Logger
	schemaCfg config.SchemaConfig
	limits    Limits

	// temporary workaround until store has implemented read/write shipper interface
	bloomShipperClient bloomshipper.Client

	// Client used to run operations on the bucket storing bloom blocks.
	storeClients map[config.DayTime]storeClient

	sharding ShardingStrategy

	metrics   *metrics
	btMetrics *v1.Metrics
	reg       prometheus.Registerer
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
		limits:    limits,
		reg:       r,
	}

	// Configure BloomClient for meta.json management
	bloomClient, err := bloomshipper.NewBloomClient(schemaConfig.Configs, storageCfg, clientMetrics)
	if err != nil {
		return nil, err
	}

	c.storeClients = make(map[config.DayTime]storeClient)

	// initialize metrics
	c.btMetrics = v1.NewMetrics(prometheus.WrapRegistererWithPrefix("loki_bloom_tokenizer", r))

	indexShipperReg := prometheus.WrapRegistererWithPrefix("loki_bloom_compactor_tsdb_shipper_", r)

	for i, periodicConfig := range schemaConfig.Configs {
		if periodicConfig.IndexType != config.TSDBType {
			level.Warn(c.logger).Log("msg", "skipping schema period because index type is not supported", "index_type", periodicConfig.IndexType, "period", periodicConfig.From)
			continue
		}

		// Configure ObjectClient and IndexShipper for series and chunk management
		objectClient, err := storage.NewObjectClient(periodicConfig.ObjectType, storageCfg, clientMetrics)
		if err != nil {
			return nil, fmt.Errorf("error creating object client '%s': %w", periodicConfig.ObjectType, err)
		}

		periodEndTime := config.DayTime{Time: math.MaxInt64}
		if i < len(schemaConfig.Configs)-1 {
			periodEndTime = config.DayTime{Time: schemaConfig.Configs[i+1].From.Time.Add(-time.Millisecond)}
		}

		pReg := prometheus.WrapRegistererWith(
			prometheus.Labels{
				"component": fmt.Sprintf(
					"index-store-%s-%s",
					periodicConfig.IndexType,
					periodicConfig.From.String(),
				),
			}, indexShipperReg)
		pLogger := log.With(logger, "index-store", fmt.Sprintf("%s-%s", periodicConfig.IndexType, periodicConfig.From.String()))

		indexShipper, err := indexshipper.NewIndexShipper(
			periodicConfig.IndexTables.PathPrefix,
			storageCfg.TSDBShipperConfig,
			objectClient,
			limits,
			nil,
			func(p string) (shipperindex.Index, error) {
				return tsdb.OpenShippableTSDB(p)
			},
			periodicConfig.GetIndexTableNumberRange(periodEndTime),
			pReg,
			pLogger,
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
		level.Error(c.logger).Log("msg", "failed to run compaction", "err", err)
	}

	ticker := time.NewTicker(util.DurationWithJitter(c.cfg.CompactionInterval, 0.05))
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.metrics.compactionRunsStarted.Inc()
			if err := c.runCompaction(ctx); err != nil {
				c.metrics.compactionRunsFailed.Inc()
				level.Error(c.logger).Log("msg", "failed to run compaction", "err", err)
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
	var tables []string
	for _, sc := range c.storeClients {
		// refresh index list cache since previous compaction would have changed the index files in the object store
		sc.index.RefreshIndexTableNamesCache(ctx)
		tbls, err := sc.index.ListTables(ctx)
		if err != nil {
			return fmt.Errorf("failed to list tables: %w", err)
		}
		tables = append(tables, tbls...)
	}

	// process most recent tables first
	tablesIntervals := getIntervalsForTables(tables)
	sortTablesByRange(tables, tablesIntervals)

	parallelism := c.cfg.MaxCompactionParallelism
	if parallelism == 0 {
		parallelism = len(tables)
	}

	// TODO(salvacorts): We currently parallelize at the table level. We may want to parallelize at the tenant and job level as well.
	// To do that, we should create a worker pool with c.cfg.MaxCompactionParallelism number of workers.
	errs := multierror.New()
	_ = concurrency.ForEachJob(ctx, len(tables), parallelism, func(ctx context.Context, i int) error {
		tableName := tables[i]
		logger := log.With(c.logger, "table", tableName)
		level.Info(logger).Log("msg", "compacting table")
		err := c.compactTable(ctx, logger, tableName, tablesIntervals[tableName])
		if err != nil {
			errs.Add(err)
			return nil
		}
		level.Info(logger).Log("msg", "finished compacting table")
		return nil
	})

	return errs.Err()
}

func (c *Compactor) compactTable(ctx context.Context, logger log.Logger, tableName string, tableInterval model.Interval) error {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("interrupting compaction of table: %w", err)
	}

	schemaCfg, ok := schemaPeriodForTable(c.schemaCfg, tableName)
	if !ok {
		level.Error(logger).Log("msg", "skipping compaction since we can't find schema for table")
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
	level.Info(logger).Log("msg", "discovered tenants from bucket", "users", len(tenants))
	return c.compactUsers(ctx, logger, sc, tableName, tableInterval, tenants)
}

// See: https://github.com/grafana/mimir/blob/34852137c332d4050e53128481f4f6417daee91e/pkg/compactor/compactor.go#L566-L689
func (c *Compactor) compactUsers(ctx context.Context, logger log.Logger, sc storeClient, tableName string, tableInterval model.Interval, tenants []string) error {
	// Keep track of tenants owned by this shard, so that we can delete the local files for all other users.
	errs := multierror.New()
	ownedTenants := make(map[string]struct{}, len(tenants))
	for _, tenant := range tenants {
		tenantLogger := log.With(logger, "tenant", tenant)

		// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("interrupting compaction of tenants: %w", err)
		}

		// Skip tenant if compaction is not enabled
		if !c.limits.BloomCompactorEnabled(tenant) {
			level.Info(tenantLogger).Log("msg", "compaction disabled for tenant. Skipping.")
			continue
		}

		// Skip this table if it is too new/old for the tenant limits.
		now := model.Now()
		tableMinAge := c.limits.BloomCompactorMinTableAge(tenant)
		tableMaxAge := c.limits.BloomCompactorMaxTableAge(tenant)
		if tableMinAge > 0 && tableInterval.End.After(now.Add(-tableMinAge)) {
			level.Debug(tenantLogger).Log("msg", "skipping tenant because table is too new ", "table-min-age", tableMinAge, "table-end", tableInterval.End, "now", now)
			continue
		}
		if tableMaxAge > 0 && tableInterval.Start.Before(now.Add(-tableMaxAge)) {
			level.Debug(tenantLogger).Log("msg", "skipping tenant because table is too old", "table-max-age", tableMaxAge, "table-start", tableInterval.Start, "now", now)
			continue
		}

		// Ensure the tenant ID belongs to our shard.
		if !c.sharding.OwnsTenant(tenant) {
			c.metrics.compactionRunSkippedTenants.Inc()
			level.Debug(tenantLogger).Log("msg", "skipping tenant because it is not owned by this shard")
			continue
		}

		ownedTenants[tenant] = struct{}{}

		if err := c.compactTenantWithRetries(ctx, tenantLogger, sc, tableName, tenant); err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				// We don't want to count shutdowns as failed compactions because we will pick up with the rest of the compaction after the restart.
				level.Info(tenantLogger).Log("msg", "compaction for tenant was interrupted by a shutdown")
				return nil
			default:
				c.metrics.compactionRunFailedTenants.Inc()
				level.Error(tenantLogger).Log("msg", "failed to compact tenant", "err", err)
				errs.Add(err)
			}
			continue
		}

		c.metrics.compactionRunSucceededTenants.Inc()
		level.Info(tenantLogger).Log("msg", "successfully compacted tenant")
	}

	return errs.Err()

	// TODO: Delete local files for unowned tenants, if there are any.
}

func (c *Compactor) compactTenant(ctx context.Context, logger log.Logger, sc storeClient, tableName string, tenant string) error {
	level.Info(logger).Log("msg", "starting compaction of tenant")

	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return err
	}

	// Tokenizer is not thread-safe so we need one per goroutine.
	NGramLength := c.limits.BloomNGramLength(tenant)
	NGramSkip := c.limits.BloomNGramSkip(tenant)
	bt := v1.NewBloomTokenizer(NGramLength, NGramSkip, c.btMetrics)

	errs := multierror.New()
	rs, err := c.sharding.GetTenantSubRing(tenant).GetAllHealthy(RingOp)
	if err != nil {
		return err
	}
	tokenRanges := bloomutils.GetInstanceWithTokenRange(c.cfg.Ring.InstanceID, rs.Instances)

	_ = sc.indexShipper.ForEach(ctx, tableName, tenant, func(isMultiTenantIndex bool, idx shipperindex.Index) error {
		if isMultiTenantIndex {
			// Skip multi-tenant indexes
			return nil
		}

		tsdbFile, ok := idx.(*tsdb.TSDBFile)
		if !ok {
			errs.Add(fmt.Errorf("failed to cast to TSDBFile"))
			return nil
		}

		tsdbIndex, ok := tsdbFile.Index.(*tsdb.TSDBIndex)
		if !ok {
			errs.Add(fmt.Errorf("failed to cast to TSDBIndex"))
			return nil
		}

		var seriesMetas []seriesMeta

		err := tsdbIndex.ForSeries(
			ctx, nil,
			0, math.MaxInt64, // TODO: Replace with MaxLookBackPeriod
			func(labels labels.Labels, fingerprint model.Fingerprint, chksMetas []tsdbindex.ChunkMeta) {
				if !tokenRanges.Contains(uint32(fingerprint)) {
					return
				}

				temp := make([]tsdbindex.ChunkMeta, len(chksMetas))
				_ = copy(temp, chksMetas)
				//All seriesMetas given a table within fp of this compactor shard
				seriesMetas = append(seriesMetas, seriesMeta{seriesFP: fingerprint, seriesLbs: labels, chunkRefs: temp})
			},
		)

		if err != nil {
			errs.Add(err)
			return nil
		}

		job := NewJob(tenant, tableName, idx.Path(), seriesMetas)
		jobLogger := log.With(logger, "job", job.String())
		c.metrics.compactionRunJobStarted.Inc()

		err = c.runCompact(ctx, jobLogger, job, bt, sc)
		if err != nil {
			c.metrics.compactionRunJobFailed.Inc()
			errs.Add(errors.Wrap(err, "runBloomCompact failed"))
		} else {
			c.metrics.compactionRunJobSuceeded.Inc()
		}
		return nil
	})

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

func (c *Compactor) compactTenantWithRetries(ctx context.Context, logger log.Logger, sc storeClient, tableName string, tenant string) error {
	return runWithRetries(
		ctx,
		c.cfg.RetryMinBackoff,
		c.cfg.RetryMaxBackoff,
		c.cfg.CompactionRetries,
		func(ctx context.Context) error {
			return c.compactTenant(ctx, logger, sc, tableName, tenant)
		},
	)
}

func (c *Compactor) runCompact(ctx context.Context, logger log.Logger, job Job, bt *v1.BloomTokenizer, storeClient storeClient) error {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return err
	}
	metaSearchParams := bloomshipper.MetaSearchParams{
		TenantID:       job.tenantID,
		MinFingerprint: uint64(job.minFp),
		MaxFingerprint: uint64(job.maxFp),
		StartTimestamp: int64(job.from),
		EndTimestamp:   int64(job.through),
	}
	var metas []bloomshipper.Meta
	//TODO  Configure pool for these to avoid allocations
	var activeBloomBlocksRefs []bloomshipper.BlockRef

	metas, err := c.bloomShipperClient.GetMetas(ctx, metaSearchParams)
	if err != nil {
		return err
	}

	// TODO This logic currently is NOT concerned with cutting blocks upon topology changes to bloom-compactors.
	// It may create blocks with series outside of the fp range of the compactor. Cutting blocks will be addressed in a follow-up PR.
	metasMatchingJob, blocksMatchingJob := matchingBlocks(metas, job)

	localDst := createLocalDirName(c.cfg.WorkingDirectory, job)
	blockOptions := v1.NewBlockOptions(bt.GetNGramLength(), bt.GetNGramSkip())

	defer func() {
		//clean up the bloom directory
		if err := os.RemoveAll(localDst); err != nil {
			level.Error(logger).Log("msg", "failed to remove block directory", "dir", localDst, "err", err)
		}
	}()

	var resultingBlock bloomshipper.Block
	defer func() {
		if resultingBlock.Data != nil {
			_ = resultingBlock.Data.Close()
		}
	}()

	if len(blocksMatchingJob) == 0 && len(metasMatchingJob) > 0 {
		// There is no change to any blocks, no compaction needed
		level.Info(logger).Log("msg", "No changes to tsdb, no compaction needed")
		return nil
	} else if len(metasMatchingJob) == 0 {
		// No matching existing blocks for this job, compact all series from scratch

		builder, err := NewPersistentBlockBuilder(localDst, blockOptions)
		if err != nil {
			level.Error(logger).Log("msg", "failed creating block builder", "err", err)
			return err
		}

		fpRate := c.limits.BloomFalsePositiveRate(job.tenantID)
		resultingBlock, err = compactNewChunks(ctx, logger, job, fpRate, bt, storeClient.chunk, builder)
		if err != nil {
			return level.Error(logger).Log("msg", "failed compacting new chunks", "err", err)
		}

	} else if len(blocksMatchingJob) > 0 {
		// When already compacted metas exists, we need to merge all blocks with amending blooms with new series

		var populate = createPopulateFunc(ctx, logger, job, storeClient, bt)

		seriesIter := makeSeriesIterFromSeriesMeta(job)

		blockIters, blockPaths, err := makeBlockIterFromBlocks(ctx, logger, c.bloomShipperClient, blocksMatchingJob, c.cfg.WorkingDirectory)
		defer func() {
			for _, path := range blockPaths {
				if err := os.RemoveAll(path); err != nil {
					level.Error(logger).Log("msg", "failed removing uncompressed bloomDir", "dir", path, "err", err)
				}
			}
		}()

		if err != nil {
			return err
		}

		mergeBlockBuilder, err := NewPersistentBlockBuilder(localDst, blockOptions)
		if err != nil {
			level.Error(logger).Log("msg", "failed creating block builder", "err", err)
			return err
		}

		resultingBlock, err = mergeCompactChunks(logger, populate, mergeBlockBuilder, blockIters, seriesIter, job)
		if err != nil {
			level.Error(logger).Log("msg", "failed merging existing blocks with new chunks", "err", err)
			return err
		}
	}

	archivePath := filepath.Join(c.cfg.WorkingDirectory, uuid.New().String())

	blockToUpload, err := bloomshipper.CompressBloomBlock(resultingBlock.BlockRef, archivePath, localDst, logger)
	if err != nil {
		level.Error(logger).Log("msg", "failed compressing bloom blocks into tar file", "err", err)
		return err
	}
	defer func() {
		err = os.Remove(archivePath)
		if err != nil {
			level.Error(logger).Log("msg", "failed removing archive file", "err", err, "file", archivePath)
		}
	}()

	// Do not change the signature of PutBlocks yet.
	// Once block size is limited potentially, compactNewChunks will return multiple blocks, hence a list is appropriate.
	storedBlocks, err := c.bloomShipperClient.PutBlocks(ctx, []bloomshipper.Block{blockToUpload})
	if err != nil {
		level.Error(logger).Log("msg", "failed uploading blocks to storage", "err", err)
		return err
	}

	// all blocks are new and active blocks
	for _, block := range storedBlocks {
		activeBloomBlocksRefs = append(activeBloomBlocksRefs, block.BlockRef)
	}

	// TODO delete old metas in later compactions
	// After all is done, create one meta file and upload to storage
	meta := bloomshipper.Meta{
		Tombstones: blocksMatchingJob,
		Blocks:     activeBloomBlocksRefs,
	}
	err = c.bloomShipperClient.PutMeta(ctx, meta)
	if err != nil {
		level.Error(logger).Log("msg", "failed uploading meta.json to storage", "err", err)
		return err
	}
	return nil
}
