package compactor

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/config"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type TablesManager interface {
	CompactTable(ctx context.Context, tableName string, applyRetention bool) error
	ApplyStorageUpdates(ctx context.Context, iterator deletion.StorageUpdatesIterator) error
	IterateTables(ctx context.Context, callback func(string, deletion.Table) error) (err error)
}

type tablesManager struct {
	cfg               Config
	expirationChecker retention.ExpirationChecker
	storeContainers   map[config.DayTime]storeContainer
	indexCompactors   map[string]IndexCompactor
	schemaConfig      config.SchemaConfig
	metrics           *metrics

	tableLocker *tableLocker
	wg          sync.WaitGroup
}

func newTablesManager(
	cfg Config,
	storeContainers map[config.DayTime]storeContainer,
	indexCompactors map[string]IndexCompactor,
	schemaConfig config.SchemaConfig,
	expirationChecker retention.ExpirationChecker,
	metrics *metrics,
) *tablesManager {
	t := &tablesManager{
		cfg:               cfg,
		storeContainers:   storeContainers,
		indexCompactors:   indexCompactors,
		schemaConfig:      schemaConfig,
		expirationChecker: expirationChecker,
		metrics:           metrics,

		tableLocker: newTableLocker(),
	}

	return t
}

func (c *tablesManager) start(ctx context.Context) {
	wg := sync.WaitGroup{}

	// To avoid races, wait 1 compaction interval before actually starting the compactor
	// this allows the ring to settle if there are a lot of ring changes and gives
	// time for existing compactors to shutdown before this starts to avoid
	// multiple compactors running at the same time.
	stopped := false
	func() {
		t := time.NewTimer(c.cfg.CompactionInterval)
		defer t.Stop()
		level.Info(util_log.Logger).Log("msg", fmt.Sprintf("waiting %v for ring to stay stable and previous compactions to finish before starting compactor", c.cfg.CompactionInterval))
		select {
		case <-ctx.Done():
			stopped = true
			return
		case <-t.C:
			level.Info(util_log.Logger).Log("msg", "compactor startup delay completed")
			return
		}
	}()

	if stopped {
		return
	}

	// do the initial compaction
	if err := c.runCompaction(ctx, false); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to run compaction", "err", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		ticker := time.NewTicker(c.cfg.CompactionInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := c.runCompaction(ctx, false); err != nil {
					level.Error(util_log.Logger).Log("msg", "failed to run compaction", "err", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if c.cfg.RetentionEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.runCompaction(ctx, true); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to apply retention", "err", err)
			}

			ticker := time.NewTicker(c.cfg.ApplyRetentionInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if err := c.runCompaction(ctx, true); err != nil {
						level.Error(util_log.Logger).Log("msg", "failed to apply retention", "err", err)
					}
				case <-ctx.Done():
					return
				}
			}
		}()

		for _, container := range c.storeContainers {
			wg.Add(1)
			go func(sc storeContainer) {
				// starts the chunk sweeper
				defer func() {
					sc.sweeper.Stop()
					wg.Done()
				}()
				sc.sweeper.Start()
				<-ctx.Done()
			}(container)
		}
	}
	level.Info(util_log.Logger).Log("msg", "compactor started")

	wg.Wait()
}

func (c *tablesManager) listTableNames(ctx context.Context) ([]string, error) {
	var (
		tables []string
		// it possible for two periods to use the same storage bucket and path prefix (different indexType or schema version)
		// so more than one index storage client may end up listing the same set of buckets
		// avoid including the same table twice in the compact tables list.
		seen = make(map[string]struct{})
	)
	for _, sc := range c.storeContainers {
		// refresh index list cache since previous compaction would have changed the index files in the object store
		sc.indexStorageClient.RefreshIndexTableNamesCache(ctx)
		tbls, err := sc.indexStorageClient.ListTables(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list tables: %w", err)
		}

		for _, table := range tbls {
			if table == deletion.DeleteRequestsTableName {
				// we do not want to perform any operations on delete requests table
				continue
			}
			if _, ok := seen[table]; ok {
				continue
			}

			tables = append(tables, table)
			seen[table] = struct{}{}
		}
	}

	// process most recent tables first
	SortTablesByRange(tables)

	// apply passed in compaction limits
	if c.cfg.SkipLatestNTables <= len(tables) {
		tables = tables[c.cfg.SkipLatestNTables:]
	}
	if c.cfg.TablesToCompact > 0 && c.cfg.TablesToCompact < len(tables) {
		tables = tables[:c.cfg.TablesToCompact]
	}

	return tables, nil
}

func (c *tablesManager) initTable(ctx context.Context, tableName string) (*table, error) {
	schemaCfg, ok := SchemaPeriodForTable(c.schemaConfig, tableName)
	if !ok {
		return nil, errSchemaForTableNotFound
	}

	indexCompactor, ok := c.indexCompactors[schemaCfg.IndexType]
	if !ok {
		return nil, fmt.Errorf("index processor not found for index type %s", schemaCfg.IndexType)
	}

	sc, ok := c.storeContainers[schemaCfg.From]
	if !ok {
		return nil, fmt.Errorf("index store client not found for period starting at %s", schemaCfg.From.String())
	}

	table, err := newTable(ctx, filepath.Join(c.cfg.WorkingDirectory, tableName), sc.indexStorageClient, indexCompactor,
		schemaCfg, sc.tableMarker, c.expirationChecker, c.cfg.UploadParallelism)
	if err != nil {
		return nil, err
	}

	return table, nil
}

func (c *tablesManager) runCompaction(ctx context.Context, applyRetention bool) (err error) {
	status := statusSuccess
	start := time.Now()

	if applyRetention {
		c.expirationChecker.MarkPhaseStarted()
	}

	defer func() {
		if err != nil {
			status = statusFailure
		}
		if applyRetention {
			c.metrics.applyRetentionOperationTotal.WithLabelValues(status).Inc()
		} else {
			c.metrics.compactTablesOperationTotal.WithLabelValues(status).Inc()
		}
		runtime := time.Since(start)
		if status == statusSuccess {
			if applyRetention {
				c.metrics.applyRetentionOperationDurationSeconds.Set(runtime.Seconds())
				c.metrics.applyRetentionLastSuccess.SetToCurrentTime()
			} else {
				c.metrics.compactTablesOperationDurationSeconds.Set(runtime.Seconds())
				c.metrics.compactTablesOperationLastSuccess.SetToCurrentTime()
			}
		}

		if applyRetention {
			if status == statusSuccess {
				c.expirationChecker.MarkPhaseFinished()
			} else {
				c.expirationChecker.MarkPhaseFailed()
			}
		}
		if !applyRetention && runtime > c.cfg.CompactionInterval {
			level.Warn(util_log.Logger).Log("msg", fmt.Sprintf("last compaction took %s which is longer than the compaction interval of %s, this can lead to duplicate compactors running if not running a standalone compactor instance.", runtime, c.cfg.CompactionInterval))
		}
	}()

	tables, err := c.listTableNames(ctx)
	if err != nil {
		return err
	}

	compactTablesChan := make(chan string)
	errChan := make(chan error)

	for i := 0; i < c.cfg.MaxCompactionParallelism; i++ {
		go func() {
			var err error
			defer func() {
				errChan <- err
			}()

			for {
				select {
				case tableName, ok := <-compactTablesChan:
					if !ok {
						return
					}

					level.Info(util_log.Logger).Log("msg", "compacting table", "table-name", tableName)
					err = c.CompactTable(ctx, tableName, applyRetention)
					if err != nil {
						return
					}
					level.Info(util_log.Logger).Log("msg", "finished compacting table", "table-name", tableName)
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		for _, tableName := range tables {
			select {
			case compactTablesChan <- tableName:
			case <-ctx.Done():
				return
			}
		}

		close(compactTablesChan)
	}()

	var firstErr error
	// read all the errors
	for i := 0; i < c.cfg.MaxCompactionParallelism; i++ {
		err := <-errChan
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return firstErr
	}

	return ctx.Err()
}

func (c *tablesManager) CompactTable(ctx context.Context, tableName string, applyRetention bool) error {
	schemaCfg, ok := SchemaPeriodForTable(c.schemaConfig, tableName)
	if !ok {
		level.Error(util_log.Logger).Log("msg", "skipping compaction since we can't find schema for table", "table", tableName)
		return nil
	}

	sc, ok := c.storeContainers[schemaCfg.From]
	if !ok {
		return fmt.Errorf("index store client not found for period starting at %s", schemaCfg.From.String())
	}

	for {
		locked, lockWaiterChan := c.tableLocker.lockTable(tableName)
		if locked {
			break
		}
		// do not wait for lock to be released if we are only compacting the table since
		// compaction should happen more frequently than retention and retention anyway compacts un-compacted files as well.
		if !applyRetention {
			hasUncompactedIndex, err := tableHasUncompactedIndex(ctx, tableName, sc.indexStorageClient)
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to check if table has uncompacted index", "table_name", tableName)
				hasUncompactedIndex = true
			}

			if hasUncompactedIndex {
				c.metrics.skippedCompactingLockedTables.WithLabelValues(tableName).Inc()
				level.Warn(util_log.Logger).Log("msg", "skipped compacting table which likely has uncompacted index since it is locked by retention", "table_name", tableName)
			}
			return nil
		}

		// we are applying retention and processing delete requests so,
		// wait for lock to be released since we can't mark delete requests as processed without checking all the tables
		select {
		case <-lockWaiterChan:
		case <-ctx.Done():
			return nil
		}
	}
	defer c.tableLocker.unlockTable(tableName)

	table, err := c.initTable(ctx, tableName)
	if err != nil {
		if errors.Is(err, errSchemaForTableNotFound) {
			level.Error(util_log.Logger).Log("msg", "skipping compaction since we can't find schema for table", "table", tableName)
			return nil
		}
		level.Error(util_log.Logger).Log("msg", "failed to initialize table for compaction", "table", tableName, "err", err)
		return err
	}

	defer table.cleanup()

	interval := retention.ExtractIntervalFromTableName(tableName)
	intervalMayHaveExpiredChunks := false
	if applyRetention {
		intervalMayHaveExpiredChunks = c.expirationChecker.IntervalMayHaveExpiredChunks(interval, "")
	}

	err = table.compact()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to compact files", "table", tableName, "err", err)
		return err
	}

	if intervalMayHaveExpiredChunks {
		err = table.applyRetention()
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to apply retention", "table", tableName, "err", err)
			return err
		}
	}

	err = table.done()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to finish the processing of table", "table", tableName, "err", err)
		return err
	}

	if !applyRetention {
		c.metrics.skippedCompactingLockedTables.WithLabelValues(tableName).Set(0)
	}
	return nil
}

func (c *tablesManager) ApplyStorageUpdates(ctx context.Context, iterator deletion.StorageUpdatesIterator) error {
	var table *table

	defer func() {
		if table != nil {
			table.cleanup()
			c.tableLocker.unlockTable(table.name)
		}
	}()

	for iterator.Next() {
		userID := iterator.UserID()
		tableName := iterator.TableName()
		if table != nil && tableName != table.name {
			if err := table.done(); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to finish table operations", "table_name", table.name, "err", err)
				return err
			}
			table.cleanup()
			c.tableLocker.unlockTable(table.name)
			table = nil
		}
		if table == nil {
			for {
				locked, lockWaiterChan := c.tableLocker.lockTable(tableName)
				if locked {
					break
				}

				select {
				case <-lockWaiterChan:
				case <-ctx.Done():
					return nil
				}
			}

			var err error
			table, err = c.initTable(ctx, tableName)
			if err != nil {
				return err
			}

			if err := table.compact(); err != nil {
				return err
			}
		}

		if err := iterator.ForEachSeries(func(labels string, chunksToDelete []string, chunksToDeIndex []string, chunksToIndex []deletion.Chunk) error {
			return table.applyStorageUpdates(userID, labels, chunksToDelete, chunksToDeIndex, chunksToIndex)
		}); err != nil {
			return err
		}
	}
	if err := iterator.Err(); err != nil {
		return err
	}

	if table != nil {
		if err := table.done(); err != nil {
			return err
		}

		table.cleanup()
		c.tableLocker.unlockTable(table.name)
		table = nil
	}

	return nil
}

func (c *tablesManager) IterateTables(ctx context.Context, callback func(string, deletion.Table) error) (err error) {
	tables, err := c.listTableNames(ctx)
	if err != nil {
		return err
	}

	for _, tableName := range tables {
		err := func() error {
			for {
				locked, lockWaiterChan := c.tableLocker.lockTable(tableName)
				if locked {
					break
				}

				select {
				case <-lockWaiterChan:
				case <-ctx.Done():
					return nil
				}
			}
			defer c.tableLocker.unlockTable(tableName)

			table, err := c.initTable(ctx, tableName)
			if err != nil {
				if errors.Is(err, errSchemaForTableNotFound) {
					return nil
				}
				return err
			}

			defer table.cleanup()

			if err := table.compact(); err != nil {
				return err
			}

			if err := callback(tableName, table); err != nil {
				return err
			}

			return table.done()
		}()
		if err != nil {
			return err
		}

	}
	return nil
}
