package compactor

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/storage/bucket"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storage/tsdb/bucketindex"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/concurrency"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/services"
)

type BlocksCleanerConfig struct {
	DeletionDelay                      time.Duration
	CleanupInterval                    time.Duration
	CleanupConcurrency                 int
	BlockDeletionMarksMigrationEnabled bool          // TODO Discuss whether we should remove it in Cortex 1.8.0 and document that upgrading to 1.7.0 before 1.8.0 is required.
	TenantCleanupDelay                 time.Duration // Delay before removing tenant deletion mark and "debug".
}

type BlocksCleaner struct {
	services.Service

	cfg          BlocksCleanerConfig
	cfgProvider  ConfigProvider
	logger       log.Logger
	bucketClient objstore.Bucket
	usersScanner *cortex_tsdb.UsersScanner

	// Keep track of the last owned users.
	lastOwnedUsers []string

	// Metrics.
	runsStarted                 prometheus.Counter
	runsCompleted               prometheus.Counter
	runsFailed                  prometheus.Counter
	runsLastSuccess             prometheus.Gauge
	blocksCleanedTotal          prometheus.Counter
	blocksFailedTotal           prometheus.Counter
	blocksMarkedForDeletion     prometheus.Counter
	tenantBlocks                *prometheus.GaugeVec
	tenantMarkedBlocks          *prometheus.GaugeVec
	tenantPartialBlocks         *prometheus.GaugeVec
	tenantBucketIndexLastUpdate *prometheus.GaugeVec
}

func NewBlocksCleaner(cfg BlocksCleanerConfig, bucketClient objstore.Bucket, usersScanner *cortex_tsdb.UsersScanner, cfgProvider ConfigProvider, logger log.Logger, reg prometheus.Registerer) *BlocksCleaner {
	c := &BlocksCleaner{
		cfg:          cfg,
		bucketClient: bucketClient,
		usersScanner: usersScanner,
		cfgProvider:  cfgProvider,
		logger:       log.With(logger, "component", "cleaner"),
		runsStarted: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_block_cleanup_started_total",
			Help: "Total number of blocks cleanup runs started.",
		}),
		runsCompleted: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_block_cleanup_completed_total",
			Help: "Total number of blocks cleanup runs successfully completed.",
		}),
		runsFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_block_cleanup_failed_total",
			Help: "Total number of blocks cleanup runs failed.",
		}),
		runsLastSuccess: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_compactor_block_cleanup_last_successful_run_timestamp_seconds",
			Help: "Unix timestamp of the last successful blocks cleanup run.",
		}),
		blocksCleanedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_blocks_cleaned_total",
			Help: "Total number of blocks deleted.",
		}),
		blocksFailedTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_compactor_block_cleanup_failures_total",
			Help: "Total number of blocks failed to be deleted.",
		}),
		blocksMarkedForDeletion: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        blocksMarkedForDeletionName,
			Help:        blocksMarkedForDeletionHelp,
			ConstLabels: prometheus.Labels{"reason": "retention"},
		}),

		// The following metrics don't have the "cortex_compactor" prefix because not strictly related to
		// the compactor. They're just tracked by the compactor because it's the most logical place where these
		// metrics can be tracked.
		tenantBlocks: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "cortex_bucket_blocks_count",
			Help: "Total number of blocks in the bucket. Includes blocks marked for deletion, but not partial blocks.",
		}, []string{"user"}),
		tenantMarkedBlocks: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "cortex_bucket_blocks_marked_for_deletion_count",
			Help: "Total number of blocks marked for deletion in the bucket.",
		}, []string{"user"}),
		tenantPartialBlocks: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "cortex_bucket_blocks_partials_count",
			Help: "Total number of partial blocks.",
		}, []string{"user"}),
		tenantBucketIndexLastUpdate: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "cortex_bucket_index_last_successful_update_timestamp_seconds",
			Help: "Timestamp of the last successful update of a tenant's bucket index.",
		}, []string{"user"}),
	}

	c.Service = services.NewTimerService(cfg.CleanupInterval, c.starting, c.ticker, nil)

	return c
}

func (c *BlocksCleaner) starting(ctx context.Context) error {
	// Run a cleanup so that any other service depending on this service
	// is guaranteed to start once the initial cleanup has been done.
	c.runCleanup(ctx, true)

	return nil
}

func (c *BlocksCleaner) ticker(ctx context.Context) error {
	c.runCleanup(ctx, false)

	return nil
}

func (c *BlocksCleaner) runCleanup(ctx context.Context, firstRun bool) {
	level.Info(c.logger).Log("msg", "started blocks cleanup and maintenance")
	c.runsStarted.Inc()

	if err := c.cleanUsers(ctx, firstRun); err == nil {
		level.Info(c.logger).Log("msg", "successfully completed blocks cleanup and maintenance")
		c.runsCompleted.Inc()
		c.runsLastSuccess.SetToCurrentTime()
	} else if errors.Is(err, context.Canceled) {
		level.Info(c.logger).Log("msg", "canceled blocks cleanup and maintenance", "err", err)
		return
	} else {
		level.Error(c.logger).Log("msg", "failed to run blocks cleanup and maintenance", "err", err.Error())
		c.runsFailed.Inc()
	}
}

func (c *BlocksCleaner) cleanUsers(ctx context.Context, firstRun bool) error {
	users, deleted, err := c.usersScanner.ScanUsers(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to discover users from bucket")
	}

	isActive := util.StringsMap(users)
	isDeleted := util.StringsMap(deleted)
	allUsers := append(users, deleted...)

	// Delete per-tenant metrics for all tenants not belonging anymore to this shard.
	// Such tenants have been moved to a different shard, so their updated metrics will
	// be exported by the new shard.
	for _, userID := range c.lastOwnedUsers {
		if !isActive[userID] && !isDeleted[userID] {
			c.tenantBlocks.DeleteLabelValues(userID)
			c.tenantMarkedBlocks.DeleteLabelValues(userID)
			c.tenantPartialBlocks.DeleteLabelValues(userID)
			c.tenantBucketIndexLastUpdate.DeleteLabelValues(userID)
		}
	}
	c.lastOwnedUsers = allUsers

	return concurrency.ForEachUser(ctx, allUsers, c.cfg.CleanupConcurrency, func(ctx context.Context, userID string) error {
		if isDeleted[userID] {
			return errors.Wrapf(c.deleteUserMarkedForDeletion(ctx, userID), "failed to delete user marked for deletion: %s", userID)
		}
		return errors.Wrapf(c.cleanUser(ctx, userID, firstRun), "failed to delete blocks for user: %s", userID)
	})
}

// Remove blocks and remaining data for tenant marked for deletion.
func (c *BlocksCleaner) deleteUserMarkedForDeletion(ctx context.Context, userID string) error {
	userLogger := util_log.WithUserID(userID, c.logger)
	userBucket := bucket.NewUserBucketClient(userID, c.bucketClient, c.cfgProvider)

	level.Info(userLogger).Log("msg", "deleting blocks for tenant marked for deletion")

	// We immediately delete the bucket index, to signal to its consumers that
	// the tenant has "no blocks" in the storage.
	if err := bucketindex.DeleteIndex(ctx, c.bucketClient, userID, c.cfgProvider); err != nil {
		return err
	}
	c.tenantBucketIndexLastUpdate.DeleteLabelValues(userID)

	var deletedBlocks, failed int
	err := userBucket.Iter(ctx, "", func(name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		id, ok := block.IsBlockDir(name)
		if !ok {
			return nil
		}

		err := block.Delete(ctx, userLogger, userBucket, id)
		if err != nil {
			failed++
			c.blocksFailedTotal.Inc()
			level.Warn(userLogger).Log("msg", "failed to delete block", "block", id, "err", err)
			return nil // Continue with other blocks.
		}

		deletedBlocks++
		c.blocksCleanedTotal.Inc()
		level.Info(userLogger).Log("msg", "deleted block", "block", id)
		return nil
	})

	if err != nil {
		return err
	}

	if failed > 0 {
		// The number of blocks left in the storage is equal to the number of blocks we failed
		// to delete. We also consider them all marked for deletion given the next run will try
		// to delete them again.
		c.tenantBlocks.WithLabelValues(userID).Set(float64(failed))
		c.tenantMarkedBlocks.WithLabelValues(userID).Set(float64(failed))
		c.tenantPartialBlocks.WithLabelValues(userID).Set(0)

		return errors.Errorf("failed to delete %d blocks", failed)
	}

	// Given all blocks have been deleted, we can also remove the metrics.
	c.tenantBlocks.DeleteLabelValues(userID)
	c.tenantMarkedBlocks.DeleteLabelValues(userID)
	c.tenantPartialBlocks.DeleteLabelValues(userID)

	if deletedBlocks > 0 {
		level.Info(userLogger).Log("msg", "deleted blocks for tenant marked for deletion", "deletedBlocks", deletedBlocks)
	}

	mark, err := cortex_tsdb.ReadTenantDeletionMark(ctx, c.bucketClient, userID)
	if err != nil {
		return errors.Wrap(err, "failed to read tenant deletion mark")
	}
	if mark == nil {
		return errors.Wrap(err, "cannot find tenant deletion mark anymore")
	}

	// If we have just deleted some blocks, update "finished" time. Also update "finished" time if it wasn't set yet, but there are no blocks.
	// Note: this UPDATES the tenant deletion mark. Components that use caching bucket will NOT SEE this update,
	// but that is fine -- they only check whether tenant deletion marker exists or not.
	if deletedBlocks > 0 || mark.FinishedTime == 0 {
		level.Info(userLogger).Log("msg", "updating finished time in tenant deletion mark")
		mark.FinishedTime = time.Now().Unix()
		return errors.Wrap(cortex_tsdb.WriteTenantDeletionMark(ctx, c.bucketClient, userID, c.cfgProvider, mark), "failed to update tenant deletion mark")
	}

	if time.Since(time.Unix(mark.FinishedTime, 0)) < c.cfg.TenantCleanupDelay {
		return nil
	}

	level.Info(userLogger).Log("msg", "cleaning up remaining blocks data for tenant marked for deletion")

	// Let's do final cleanup of tenant.
	if deleted, err := bucket.DeletePrefix(ctx, userBucket, block.DebugMetas, userLogger); err != nil {
		return errors.Wrap(err, "failed to delete "+block.DebugMetas)
	} else if deleted > 0 {
		level.Info(userLogger).Log("msg", "deleted files under "+block.DebugMetas+" for tenant marked for deletion", "count", deleted)
	}

	// Tenant deletion mark file is inside Markers as well.
	if deleted, err := bucket.DeletePrefix(ctx, userBucket, bucketindex.MarkersPathname, userLogger); err != nil {
		return errors.Wrap(err, "failed to delete marker files")
	} else if deleted > 0 {
		level.Info(userLogger).Log("msg", "deleted marker files for tenant marked for deletion", "count", deleted)
	}

	return nil
}

func (c *BlocksCleaner) cleanUser(ctx context.Context, userID string, firstRun bool) (returnErr error) {
	userLogger := util_log.WithUserID(userID, c.logger)
	userBucket := bucket.NewUserBucketClient(userID, c.bucketClient, c.cfgProvider)
	startTime := time.Now()

	level.Info(userLogger).Log("msg", "started blocks cleanup and maintenance")
	defer func() {
		if returnErr != nil {
			level.Warn(userLogger).Log("msg", "failed blocks cleanup and maintenance", "err", returnErr)
		} else {
			level.Info(userLogger).Log("msg", "completed blocks cleanup and maintenance", "duration", time.Since(startTime))
		}
	}()

	// Migrate block deletion marks to the global markers location. This operation is a best-effort.
	if firstRun && c.cfg.BlockDeletionMarksMigrationEnabled {
		if err := bucketindex.MigrateBlockDeletionMarksToGlobalLocation(ctx, c.bucketClient, userID, c.cfgProvider); err != nil {
			level.Warn(userLogger).Log("msg", "failed to migrate block deletion marks to the global markers location", "err", err)
		} else {
			level.Info(userLogger).Log("msg", "migrated block deletion marks to the global markers location")
		}
	}

	// Read the bucket index.
	idx, err := bucketindex.ReadIndex(ctx, c.bucketClient, userID, c.cfgProvider, c.logger)
	if errors.Is(err, bucketindex.ErrIndexCorrupted) {
		level.Warn(userLogger).Log("msg", "found a corrupted bucket index, recreating it")
	} else if err != nil && !errors.Is(err, bucketindex.ErrIndexNotFound) {
		return err
	}

	// Mark blocks for future deletion based on the retention period for the user.
	// Note doing this before UpdateIndex, so it reads in the deletion marks.
	// The trade-off being that retention is not applied if the index has to be
	// built, but this is rare.
	if idx != nil {
		// We do not want to stop the remaining work in the cleaner if an
		// error occurs here. Errors are logged in the function.
		retention := c.cfgProvider.CompactorBlocksRetentionPeriod(userID)
		c.applyUserRetentionPeriod(ctx, idx, retention, userBucket, userLogger)
	}

	// Generate an updated in-memory version of the bucket index.
	w := bucketindex.NewUpdater(c.bucketClient, userID, c.cfgProvider, c.logger)
	idx, partials, err := w.UpdateIndex(ctx, idx)
	if err != nil {
		return err
	}

	// Delete blocks marked for deletion. We iterate over a copy of deletion marks because
	// we'll need to manipulate the index (removing blocks which get deleted).
	for _, mark := range idx.BlockDeletionMarks.Clone() {
		if time.Since(mark.GetDeletionTime()).Seconds() <= c.cfg.DeletionDelay.Seconds() {
			continue
		}

		if err := block.Delete(ctx, userLogger, userBucket, mark.ID); err != nil {
			c.blocksFailedTotal.Inc()
			level.Warn(userLogger).Log("msg", "failed to delete block marked for deletion", "block", mark.ID, "err", err)
			continue
		}

		// Remove the block from the bucket index too.
		idx.RemoveBlock(mark.ID)

		c.blocksCleanedTotal.Inc()
		level.Info(userLogger).Log("msg", "deleted block marked for deletion", "block", mark.ID)
	}

	// Partial blocks with a deletion mark can be cleaned up. This is a best effort, so we don't return
	// error if the cleanup of partial blocks fail.
	if len(partials) > 0 {
		c.cleanUserPartialBlocks(ctx, partials, idx, userBucket, userLogger)
	}

	// Upload the updated index to the storage.
	if err := bucketindex.WriteIndex(ctx, c.bucketClient, userID, c.cfgProvider, idx); err != nil {
		return err
	}

	c.tenantBlocks.WithLabelValues(userID).Set(float64(len(idx.Blocks)))
	c.tenantMarkedBlocks.WithLabelValues(userID).Set(float64(len(idx.BlockDeletionMarks)))
	c.tenantPartialBlocks.WithLabelValues(userID).Set(float64(len(partials)))
	c.tenantBucketIndexLastUpdate.WithLabelValues(userID).SetToCurrentTime()

	return nil
}

// cleanUserPartialBlocks delete partial blocks which are safe to be deleted. The provided partials map
// is updated accordingly.
func (c *BlocksCleaner) cleanUserPartialBlocks(ctx context.Context, partials map[ulid.ULID]error, idx *bucketindex.Index, userBucket objstore.InstrumentedBucket, userLogger log.Logger) {
	for blockID, blockErr := range partials {
		// We can safely delete only blocks which are partial because the meta.json is missing.
		if !errors.Is(blockErr, bucketindex.ErrBlockMetaNotFound) {
			continue
		}

		// We can safely delete only partial blocks with a deletion mark.
		err := metadata.ReadMarker(ctx, userLogger, userBucket, blockID.String(), &metadata.DeletionMark{})
		if errors.Is(err, metadata.ErrorMarkerNotFound) {
			continue
		}
		if err != nil {
			level.Warn(userLogger).Log("msg", "error reading partial block deletion mark", "block", blockID, "err", err)
			continue
		}

		// Hard-delete partial blocks having a deletion mark, even if the deletion threshold has not
		// been reached yet.
		if err := block.Delete(ctx, userLogger, userBucket, blockID); err != nil {
			c.blocksFailedTotal.Inc()
			level.Warn(userLogger).Log("msg", "error deleting partial block marked for deletion", "block", blockID, "err", err)
			continue
		}

		// Remove the block from the bucket index too.
		idx.RemoveBlock(blockID)
		delete(partials, blockID)

		c.blocksCleanedTotal.Inc()
		level.Info(userLogger).Log("msg", "deleted partial block marked for deletion", "block", blockID)
	}
}

// applyUserRetentionPeriod marks blocks for deletion which have aged past the retention period.
func (c *BlocksCleaner) applyUserRetentionPeriod(ctx context.Context, idx *bucketindex.Index, retention time.Duration, userBucket objstore.Bucket, userLogger log.Logger) {
	// The retention period of zero is a special value indicating to never delete.
	if retention <= 0 {
		return
	}

	level.Debug(userLogger).Log("msg", "applying retention", "retention", retention.String())
	blocks := listBlocksOutsideRetentionPeriod(idx, time.Now().Add(-retention))

	// Attempt to mark all blocks. It is not critical if a marking fails, as
	// the cleaner will retry applying the retention in its next cycle.
	for _, b := range blocks {
		level.Info(userLogger).Log("msg", "applied retention: marking block for deletion", "block", b.ID, "maxTime", b.MaxTime)
		if err := block.MarkForDeletion(ctx, userLogger, userBucket, b.ID, fmt.Sprintf("block exceeding retention of %v", retention), c.blocksMarkedForDeletion); err != nil {
			level.Warn(userLogger).Log("msg", "failed to mark block for deletion", "block", b.ID, "err", err)
		}
	}
}

// listBlocksOutsideRetentionPeriod determines the blocks which have aged past
// the specified retention period, and are not already marked for deletion.
func listBlocksOutsideRetentionPeriod(idx *bucketindex.Index, threshold time.Time) (result bucketindex.Blocks) {
	// Whilst re-marking a block is not harmful, it is wasteful and generates
	// a warning log message. Use the block deletion marks already in-memory
	// to prevent marking blocks already marked for deletion.
	marked := make(map[ulid.ULID]struct{}, len(idx.BlockDeletionMarks))
	for _, d := range idx.BlockDeletionMarks {
		marked[d.ID] = struct{}{}
	}

	for _, b := range idx.Blocks {
		maxTime := time.Unix(b.MaxTime/1000, 0)
		if maxTime.Before(threshold) {
			if _, isMarked := marked[b.ID]; !isMarked {
				result = append(result, b)
			}
		}
	}

	return
}
