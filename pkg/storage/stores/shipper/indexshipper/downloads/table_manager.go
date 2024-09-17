package downloads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	cacheCleanupInterval = time.Hour
	daySeconds           = int64(24 * time.Hour / time.Second)
)

type Limits interface {
	AllByUserID() map[string]*validation.Limits
	DefaultLimits() *validation.Limits
	VolumeMaxSeries(userID string) int
}

// TenantFilter is invoked by an IndexGateway instance and answers which
// tenants from the given list of tenants are assigned to this instance.
//
// It is only relevant by an IndexGateway in the ring mode and if its result
// does not contain a given tenant, that tenant will be ignored by this
// IndexGateway during query readiness.
//
// It requires the same function signature as indexgateway.(*ShardingStrategy).FilterTenants
type TenantFilter func([]string) ([]string, error)

type TableManager interface {
	Stop()
	ForEach(ctx context.Context, tableName, userID string, callback index.ForEachIndexCallback) error
	ForEachConcurrent(ctx context.Context, tableName, userID string, callback index.ForEachIndexCallback) error
}

type Config struct {
	CacheDir          string
	SyncInterval      time.Duration
	CacheTTL          time.Duration
	QueryReadyNumDays int
	Limits            Limits
}

type tableManager struct {
	cfg                Config
	openIndexFileFunc  index.OpenIndexFileFunc
	indexStorageClient storage.Client
	tableRangeToHandle config.TableRange

	tables    map[string]Table
	tablesMtx sync.RWMutex
	metrics   *metrics
	logger    log.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	tenantFilter TenantFilter
}

func NewTableManager(cfg Config, openIndexFileFunc index.OpenIndexFileFunc, indexStorageClient storage.Client,
	tenantFilter TenantFilter, tableRangeToHandle config.TableRange, reg prometheus.Registerer, logger log.Logger) (TableManager, error) {
	if err := util.EnsureDirectory(cfg.CacheDir); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	tm := &tableManager{
		cfg:                cfg,
		openIndexFileFunc:  openIndexFileFunc,
		indexStorageClient: indexStorageClient,
		tableRangeToHandle: tableRangeToHandle,
		tenantFilter:       tenantFilter,
		tables:             make(map[string]Table),
		metrics:            newMetrics(reg),
		logger:             logger,
		ctx:                ctx,
		cancel:             cancel,
	}

	// load the existing tables first.
	err := tm.loadLocalTables()
	if err != nil {
		// call Stop to close open file references.
		tm.Stop()
		return nil, err
	}

	// download the missing tables.
	err = tm.ensureQueryReadiness(ctx)
	if err != nil {
		// call Stop to close open file references.
		tm.Stop()
		return nil, err
	}

	// Increment the WaitGroup counter here before starting the goroutine
	tm.wg.Add(1)
	go tm.loop()
	return tm, nil
}

func (tm *tableManager) loop() {
	defer tm.wg.Done()

	syncTicker := time.NewTicker(tm.cfg.SyncInterval)
	defer syncTicker.Stop()

	cacheCleanupTicker := time.NewTicker(cacheCleanupInterval)
	defer cacheCleanupTicker.Stop()

	for {
		select {
		case <-syncTicker.C:
			err := tm.syncTables(tm.ctx)
			if err != nil {
				level.Error(tm.logger).Log("msg", "error syncing local index files with storage", "err", err)
			}

			// we need to keep ensuring query readiness to download every days new table which would otherwise be downloaded only during queries.
			err = tm.ensureQueryReadiness(tm.ctx)
			if err != nil {
				level.Error(tm.logger).Log("msg", "error ensuring query readiness of tables", "err", err)
			}
		case <-cacheCleanupTicker.C:
			err := tm.cleanupCache()
			if err != nil {
				level.Error(tm.logger).Log("msg", "error cleaning up expired tables", "err", err)
			}
		case <-tm.ctx.Done():
			return
		}
	}
}

func (tm *tableManager) Stop() {
	tm.cancel()
	tm.wg.Wait()
	tm.tablesMtx.Lock()
	defer tm.tablesMtx.Unlock()

	for _, table := range tm.tables {
		table.Close()
	}
}

// Only used by TSDB. boltdb-shipper manages concurrency elsewhere
func (tm *tableManager) ForEachConcurrent(ctx context.Context, tableName, userID string, callback index.ForEachIndexCallback) error {
	table, err := tm.getOrCreateTable(tableName)
	if err != nil {
		return err
	}
	return table.ForEachConcurrent(ctx, userID, callback)
}

func (tm *tableManager) ForEach(ctx context.Context, tableName, userID string, callback index.ForEachIndexCallback) error {
	table, err := tm.getOrCreateTable(tableName)
	if err != nil {
		return err
	}
	return table.ForEach(ctx, userID, callback)
}

func (tm *tableManager) getOrCreateTable(tableName string) (Table, error) {
	// if table is already there, use it.
	start := time.Now()
	tm.tablesMtx.RLock()
	table, ok := tm.tables[tableName]
	tm.tablesMtx.RUnlock()

	level.Info(tm.logger).Log("msg", "get or create table", "found", ok, "table", tableName, "wait_for_lock", time.Since(start))

	if !ok {
		tm.tablesMtx.Lock()
		defer tm.tablesMtx.Unlock()

		// check if some other competing goroutine got the lock before us and created the table, use it if so.
		table, ok = tm.tables[tableName]
		if !ok {
			// table not found, creating one.
			level.Info(tm.logger).Log("msg", "downloading all files for table", "table", tableName)

			tablePath := filepath.Join(tm.cfg.CacheDir, tableName)
			err := util.EnsureDirectory(tablePath)
			if err != nil {
				return nil, err
			}

			table = NewTable(tableName, filepath.Join(tm.cfg.CacheDir, tableName), tm.indexStorageClient, tm.openIndexFileFunc, tm.metrics)
			tm.tables[tableName] = table
		}
	}

	return table, nil
}

func (tm *tableManager) syncTables(ctx context.Context) error {
	tm.tablesMtx.RLock()
	defer tm.tablesMtx.RUnlock()

	start := time.Now()
	var err error

	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
		}

		tm.metrics.tablesSyncOperationTotal.WithLabelValues(status).Inc()
		tm.metrics.tablesDownloadOperationDurationSeconds.Set(time.Since(start).Seconds())
	}()

	level.Info(tm.logger).Log("msg", "syncing tables")

	for name, table := range tm.tables {
		level.Debug(tm.logger).Log("msg", "syncing table", "table", name)
		start := time.Now()
		err := table.Sync(ctx)
		duration := float64(time.Since(start))
		if err != nil {
			tm.metrics.tableSyncLatency.WithLabelValues(name, statusFailure).Observe(duration)
			return errors.Wrapf(err, "failed to sync table '%s'", name)
		}
		tm.metrics.tableSyncLatency.WithLabelValues(name, statusSuccess).Observe(duration)
	}

	return nil
}

func (tm *tableManager) cleanupCache() error {
	tm.tablesMtx.Lock()
	defer tm.tablesMtx.Unlock()

	level.Info(tm.logger).Log("msg", "cleaning tables cache")

	for name, table := range tm.tables {
		level.Debug(tm.logger).Log("msg", "cleaning up expired table", "table", name)
		isEmpty, err := table.DropUnusedIndex(tm.cfg.CacheTTL, time.Now())
		if err != nil {
			return errors.Wrapf(err, "failed to clean up expired table '%s'", name)
		}

		if isEmpty {
			delete(tm.tables, name)
		}
	}

	return nil
}

// ensureQueryReadiness compares tables required for being query ready with the tables we already have and downloads the missing ones.
func (tm *tableManager) ensureQueryReadiness(ctx context.Context) error {
	start := time.Now()
	distinctUsers := make(map[string]struct{})

	defer func() {
		ids := make([]string, 0, len(distinctUsers))
		for k := range distinctUsers {
			ids = append(ids, k)
		}
		level.Info(tm.logger).Log("msg", "query readiness setup completed", "duration", time.Since(start), "distinct_users_len", len(distinctUsers), "distinct_users", strings.Join(ids, ","))
	}()

	activeTableNumber := getActiveTableNumber()

	// find the largest query readiness number
	largestQueryReadinessNum := tm.cfg.QueryReadyNumDays
	if defaultLimits := tm.cfg.Limits.DefaultLimits(); defaultLimits.QueryReadyIndexNumDays > largestQueryReadinessNum {
		largestQueryReadinessNum = defaultLimits.QueryReadyIndexNumDays
	}

	queryReadinessNumByUserID := make(map[string]int)
	for userID, limits := range tm.cfg.Limits.AllByUserID() {
		if limits.QueryReadyIndexNumDays != 0 {
			queryReadinessNumByUserID[userID] = limits.QueryReadyIndexNumDays
			if limits.QueryReadyIndexNumDays > largestQueryReadinessNum {
				largestQueryReadinessNum = limits.QueryReadyIndexNumDays
			}
		}
	}

	// return early if no table has to be downloaded for query readiness
	if largestQueryReadinessNum == 0 {
		return nil
	}

	tm.indexStorageClient.RefreshIndexTableNamesCache(ctx)
	tables, err := tm.indexStorageClient.ListTables(ctx)
	if err != nil {
		return err
	}

	for _, tableName := range tables {
		if tableName == deletion.DeleteRequestsTableName {
			continue
		}

		if ok, err := tm.tableRangeToHandle.TableInRange(tableName); !ok {
			if err != nil {
				level.Error(tm.logger).Log("msg", "failed to run query readiness for table", "table-name", tableName, "err", err)
			} else {
				level.Debug(tm.logger).Log("msg", "skipping query readiness. table not in range", "table-name", tableName)
			}

			continue
		}

		tableNumber, err := config.ExtractTableNumberFromName(tableName)
		if err != nil {
			return fmt.Errorf("cannot extract table number from %s: %w", tableName, err)
		}

		// continue if the table is not within query readiness
		if activeTableNumber-tableNumber > int64(largestQueryReadinessNum) {
			continue
		}

		// list the users that have dedicated index files for this table
		operationStart := time.Now()
		_, usersWithIndex, err := tm.indexStorageClient.ListFiles(ctx, tableName, false)
		if err != nil {
			return err
		}
		listFilesDuration := time.Since(operationStart)

		// find the users whos index we need to keep ready for querying from this table
		usersToBeQueryReadyFor, err := tm.findUsersInTableForQueryReadiness(tableNumber, usersWithIndex, queryReadinessNumByUserID)
		if err != nil {
			return err
		}

		// continue if both user index and common index is not required to be downloaded for query readiness
		if len(usersToBeQueryReadyFor) == 0 && activeTableNumber-tableNumber > int64(tm.cfg.QueryReadyNumDays) {
			continue
		}

		operationStart = time.Now()
		table, err := tm.getOrCreateTable(tableName)
		if err != nil {
			return err
		}
		createTableDuration := time.Since(operationStart)

		for _, u := range usersToBeQueryReadyFor {
			distinctUsers[u] = struct{}{}
		}

		operationStart = time.Now()
		if err := table.EnsureQueryReadiness(ctx, usersToBeQueryReadyFor); err != nil {
			return err
		}
		ensureQueryReadinessDuration := time.Since(operationStart)

		level.Info(tm.logger).Log(
			"msg", "index pre-download for query readiness completed",
			"users_len", len(usersToBeQueryReadyFor),
			"users", strings.Join(usersToBeQueryReadyFor, ","),
			"query_readiness_duration", ensureQueryReadinessDuration,
			"table", tableName,
			"create_table_duration", createTableDuration,
			"list_files_duration", listFilesDuration,
		)
	}

	return nil
}

// findUsersInTableForQueryReadiness returns the users that needs their index to be query ready based on the tableNumber and
// query readiness number provided per user
func (tm *tableManager) findUsersInTableForQueryReadiness(tableNumber int64, usersWithIndexInTable []string, queryReadinessNumByUserID map[string]int) ([]string, error) {
	activeTableNumber := getActiveTableNumber()
	usersToBeQueryReadyFor := []string{}

	for _, userID := range usersWithIndexInTable {
		// use the query readiness config for the user if it exists or use the default config
		queryReadyNumDays, ok := queryReadinessNumByUserID[userID]
		if !ok {
			queryReadyNumDays = tm.cfg.Limits.DefaultLimits().QueryReadyIndexNumDays
		}

		if queryReadyNumDays == 0 {
			continue
		}

		if activeTableNumber-tableNumber <= int64(queryReadyNumDays) {
			usersToBeQueryReadyFor = append(usersToBeQueryReadyFor, userID)
		}
	}
	if tm.tenantFilter != nil {
		return tm.tenantFilter(usersToBeQueryReadyFor)
	}
	return usersToBeQueryReadyFor, nil
}

// loadLocalTables loads tables present locally.
func (tm *tableManager) loadLocalTables() error {
	dirEntries, err := os.ReadDir(tm.cfg.CacheDir)
	if err != nil {
		return err
	}

	for _, entry := range dirEntries {
		if !entry.IsDir() {
			continue
		}

		if ok, err := tm.tableRangeToHandle.TableInRange(entry.Name()); !ok {
			if err != nil {
				level.Error(tm.logger).Log("msg", "failed to load table", "table-name", entry.Name(), "err", err)
			} else {
				level.Debug(tm.logger).Log("msg", "skip loading table as it is not in range", "table-name", entry.Name())
			}

			continue
		}

		level.Info(tm.logger).Log("msg", fmt.Sprintf("loading local table %s", entry.Name()))

		table, err := LoadTable(entry.Name(), filepath.Join(tm.cfg.CacheDir, entry.Name()),
			tm.indexStorageClient, tm.openIndexFileFunc, tm.metrics)
		if err != nil {
			return err
		}

		tm.tables[entry.Name()] = table
	}

	return nil
}

func getActiveTableNumber() int64 {
	return getTableNumberForTime(model.Now())
}

func getTableNumberForTime(t model.Time) int64 {
	return t.Unix() / daySeconds
}
