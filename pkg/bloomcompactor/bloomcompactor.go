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
	"path/filepath"
	"sort"
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

	"github.com/grafana/loki/pkg/compactor/retention"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/storage/chunk"
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

const (
	fpRate        = 0.01
	bloomFileName = "bloom"
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

	metrics *metrics
	reg     prometheus.Registerer
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
			prometheus.WrapRegistererWithPrefix("loki_tsdb_shipper_", r),
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

func (c *Compactor) compactTenant(ctx context.Context, sc storeClient, tableName string, tenant string) error {
	level.Info(c.logger).Log("msg", "starting compaction of tenant")

	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return err
	}

	// Tokenizer is not thread-safe so we need one per goroutine.
	bt, _ := v1.NewBloomTokenizer(c.reg)

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
				job := NewJob(tenant, tableName, idx.Path(), fingerprint, labels, chksMetas)
				jobLogger := log.With(c.logger, "job", job.String())

				ownsJob, err := c.sharding.OwnsJob(job)
				if err != nil {
					c.metrics.compactionRunUnownedJobs.Inc()
					level.Error(jobLogger).Log("msg", "failed to check if compactor owns job", "err", err)
					errs.Add(err)
					return
				}
				if !ownsJob {
					c.metrics.compactionRunUnownedJobs.Inc()
					level.Debug(jobLogger).Log("msg", "skipping job because it is not owned by this shard")
					return
				}

				if err := c.runCompact(ctx, jobLogger, job, c.bloomShipperClient, bt, sc); err != nil {
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

func (c *Compactor) compactTenantWithRetries(ctx context.Context, logger log.Logger, sc storeClient, tableName string, tenant string) error {
	return runWithRetries(
		ctx,
		c.cfg.RetryMinBackoff,
		c.cfg.RetryMaxBackoff,
		c.cfg.CompactionRetries,
		func(ctx context.Context) error {
			return c.compactTenant(ctx, sc, tableName, tenant)
		},
	)
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
func buildBloomBlock(ctx context.Context, logger log.Logger, bloomForChks v1.SeriesWithBloom, job Job, workingDir string) (bloomshipper.Block, error) {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return bloomshipper.Block{}, err
	}

	localDst := createLocalDirName(workingDir, job)

	// write bloom to a local dir
	builder, err := v1.NewBlockBuilder(v1.NewBlockOptions(), v1.NewDirectoryBlockWriter(localDst))
	if err != nil {
		level.Error(logger).Log("creating builder", err)
		return bloomshipper.Block{}, err
	}

	checksum, err := builder.BuildFrom(v1.NewSliceIter([]v1.SeriesWithBloom{bloomForChks}))
	if err != nil {
		level.Error(logger).Log("writing bloom", err)
		return bloomshipper.Block{}, err
	}

	blockFile, err := os.Open(filepath.Join(localDst, bloomFileName))
	if err != nil {
		level.Error(logger).Log("reading bloomBlock", err)
	}

	blocks := bloomshipper.Block{
		BlockRef: bloomshipper.BlockRef{
			Ref: bloomshipper.Ref{
				TenantID:       job.Tenant(),
				TableName:      job.TableName(),
				MinFingerprint: uint64(job.Fingerprint()), // TODO will change once we compact multiple blooms into a block
				MaxFingerprint: uint64(job.Fingerprint()),
				StartTimestamp: job.From().Unix(),
				EndTimestamp:   job.Through().Unix(),
				Checksum:       checksum,
			},
			IndexPath: job.IndexPath(),
		},
		Data: blockFile,
	}

	return blocks, nil
}

func createLocalDirName(workingDir string, job Job) string {
	dir := fmt.Sprintf("bloomBlock-%s-%s-%s-%s-%s-%s", job.TableName(), job.Tenant(), job.Fingerprint(), job.Fingerprint(), job.From(), job.Through())
	return filepath.Join(workingDir, dir)
}

// Compacts given list of chunks, uploads them to storage and returns a list of bloomBlocks
func CompactNewChunks(ctx context.Context, logger log.Logger, job Job,
	chunks []chunk.Chunk, bt *v1.BloomTokenizer,
	bloomShipperClient bloomshipper.Client, dst string) ([]bloomshipper.Block, error) {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create a bloom for this series
	bloomForChks := v1.SeriesWithBloom{
		Series: &v1.Series{
			Fingerprint: job.Fingerprint(),
		},
		Bloom: &v1.Bloom{
			ScalableBloomFilter: *filter.NewDefaultScalableBloomFilter(fpRate),
		},
	}

	// Tokenize data into n-grams
	bt.PopulateSeriesWithBloom(&bloomForChks, chunks)

	// Build and upload bloomBlock to storage
	blocks, err := buildBloomBlock(ctx, logger, bloomForChks, job, dst)
	if err != nil {
		level.Error(logger).Log("building bloomBlocks", err)
		return nil, err
	}
	storedBlocks, err := bloomShipperClient.PutBlocks(ctx, []bloomshipper.Block{blocks})
	if err != nil {
		level.Error(logger).Log("putting blocks to storage", err)
		return nil, err
	}
	return storedBlocks, nil
}

func (c *Compactor) runCompact(ctx context.Context, logger log.Logger, job Job, bloomShipperClient bloomshipper.Client, bt *v1.BloomTokenizer, storeClient storeClient) error {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return err
	}

	metaSearchParams := bloomshipper.MetaSearchParams{
		TenantID:       job.tenantID,
		MinFingerprint: uint64(job.seriesFP),
		MaxFingerprint: uint64(job.seriesFP),
		StartTimestamp: int64(job.from),
		EndTimestamp:   int64(job.through),
	}
	var metas []bloomshipper.Meta
	//TODO  Configure pool for these to avoid allocations
	var bloomBlocksRefs []bloomshipper.BlockRef
	var tombstonedBlockRefs []bloomshipper.BlockRef

	metas, err := bloomShipperClient.GetMetas(ctx, metaSearchParams)
	if err != nil {
		return err
	}

	if len(metas) == 0 {
		// Get chunks data from list of chunkRefs
		chks, err := storeClient.chunk.GetChunks(ctx, makeChunkRefs(job.Chunks(), job.Tenant(), job.Fingerprint()))
		if err != nil {
			return err
		}

		storedBlocks, err := CompactNewChunks(ctx, logger, job, chks, bt, bloomShipperClient, c.cfg.WorkingDirectory)
		if err != nil {
			return level.Error(logger).Log("compacting new chunks", err)
		}

		storedBlockRefs := make([]bloomshipper.BlockRef, len(storedBlocks))

		for i, block := range storedBlocks {
			storedBlockRefs[i] = block.BlockRef
		}

		// all blocks are new and active blocks
		bloomBlocksRefs = storedBlockRefs
	} else {
		// TODO complete part 2 - periodic compaction for delta from previous period
		// When already compacted metas exists
		// Deduplicate index paths
		uniqueIndexPaths := make(map[string]struct{})

		for _, meta := range metas {
			for _, blockRef := range meta.Blocks {
				uniqueIndexPaths[blockRef.IndexPath] = struct{}{}
				// ...

				// the result should return a list of active
				// blocks and tombstoned bloom blocks.
			}
		}

	}

	// After all is done, create one meta file and upload to storage
	meta := bloomshipper.Meta{
		Tombstones: tombstonedBlockRefs,
		Blocks:     bloomBlocksRefs,
	}
	err = bloomShipperClient.PutMeta(ctx, meta)
	if err != nil {
		level.Error(logger).Log("putting meta.json to storage", err)
		return err
	}
	return nil
}

func getIntervalsForTables(tables []string) map[string]model.Interval {
	tablesIntervals := make(map[string]model.Interval, len(tables))
	for _, table := range tables {
		tablesIntervals[table] = retention.ExtractIntervalFromTableName(table)
	}

	return tablesIntervals
}

func sortTablesByRange(tables []string, intervals map[string]model.Interval) {
	sort.Slice(tables, func(i, j int) bool {
		// less than if start time is after produces a most recent first sort order
		return intervals[tables[i]].Start.After(intervals[tables[j]].Start)
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
