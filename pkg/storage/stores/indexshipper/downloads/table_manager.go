package downloads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/storage"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

const (
	cacheCleanupInterval = time.Hour
	daySeconds           = int64(24 * time.Hour / time.Second)
)

// regexp for finding the trailing index bucket number at the end of table name
var extractTableNumberRegex = regexp.MustCompile(`[0-9]+$`)

type Limits interface {
	AllByUserID() map[string]*validation.Limits
	DefaultLimits() *validation.Limits
}

// IndexGatewayOwnsTenant is invoked by an IndexGateway instance and answers whether if the given tenant is assigned to this instance or not.
//
// It is only relevant by an IndexGateway in the ring mode and if it returns false for a given tenant, that tenant will be ignored by this IndexGateway during query readiness.
type IndexGatewayOwnsTenant func(tenant string) bool

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
	cfg                 Config
	openIndexFileFunc   index.OpenIndexFileFunc
	indexStorageClient  storage.Client
	tableRangesToHandle config.TableRanges

	tables    map[string]Table
	tablesMtx sync.RWMutex
	metrics   *metrics

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	ownsTenant IndexGatewayOwnsTenant
}

func NewTableManager(cfg Config, openIndexFileFunc index.OpenIndexFileFunc, indexStorageClient storage.Client,
	ownsTenantFn IndexGatewayOwnsTenant, tableRangesToHandle config.TableRanges, reg prometheus.Registerer) (TableManager, error) {
	if err := util.EnsureDirectory(cfg.CacheDir); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	tm := &tableManager{
		cfg:                 cfg,
		openIndexFileFunc:   openIndexFileFunc,
		indexStorageClient:  indexStorageClient,
		tableRangesToHandle: tableRangesToHandle,
		ownsTenant:          ownsTenantFn,
		tables:              make(map[string]Table),
		metrics:             newMetrics(reg),
		ctx:                 ctx,
		cancel:              cancel,
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

	go tm.loop()
	return tm, nil
}

func (tm *tableManager) loop() {
	tm.wg.Add(1)
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
				level.Error(util_log.Logger).Log("msg", "error syncing local boltdb files with storage", "err", err)
			}

			// we need to keep ensuring query readiness to download every days new table which would otherwise be downloaded only during queries.
			err = tm.ensureQueryReadiness(tm.ctx)
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "error ensuring query readiness of tables", "err", err)
			}
		case <-cacheCleanupTicker.C:
			err := tm.cleanupCache()
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "error cleaning up expired tables", "err", err)
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
	tm.tablesMtx.RLock()
	table, ok := tm.tables[tableName]
	tm.tablesMtx.RUnlock()

	if !ok {
		tm.tablesMtx.Lock()
		defer tm.tablesMtx.Unlock()

		// check if some other competing goroutine got the lock before us and created the table, use it if so.
		table, ok = tm.tables[tableName]
		if !ok {
			// table not found, creating one.
			level.Info(util_log.Logger).Log("msg", fmt.Sprintf("downloading all files for table %s", tableName))

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

	level.Info(util_log.Logger).Log("msg", "syncing tables")

	for _, table := range tm.tables {
		err := table.Sync(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

func (tm *tableManager) cleanupCache() error {
	tm.tablesMtx.Lock()
	defer tm.tablesMtx.Unlock()

	level.Info(util_log.Logger).Log("msg", "cleaning tables cache")

	for name, table := range tm.tables {
		level.Info(util_log.Logger).Log("msg", fmt.Sprintf("cleaning up expired table %s", name))
		isEmpty, err := table.DropUnusedIndex(tm.cfg.CacheTTL, time.Now())
		if err != nil {
			return err
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
		level.Info(util_log.Logger).Log("msg", "query readiness setup completed", "duration", time.Since(start), "distinct_users_len", len(distinctUsers))
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

	tables, err := tm.indexStorageClient.ListTables(ctx)
	if err != nil {
		return err
	}

	for _, tableName := range tables {
		tableNumber, err := extractTableNumberFromName(tableName)
		if err != nil {
			return err
		}

		if tableNumber == -1 || !tm.tableRangesToHandle.TableInRange(tableNumber, tableName) {
			continue
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
		usersToBeQueryReadyFor := tm.findUsersInTableForQueryReadiness(tableNumber, usersWithIndex, queryReadinessNumByUserID)

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

		level.Info(util_log.Logger).Log(
			"msg", "index pre-download for query readiness completed",
			"users_len", len(usersToBeQueryReadyFor),
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
func (tm *tableManager) findUsersInTableForQueryReadiness(tableNumber int64, usersWithIndexInTable []string,
	queryReadinessNumByUserID map[string]int) []string {
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

		if tm.ownsTenant != nil && !tm.ownsTenant(userID) {
			continue
		}

		if activeTableNumber-tableNumber <= int64(queryReadyNumDays) {
			usersToBeQueryReadyFor = append(usersToBeQueryReadyFor, userID)
		}
	}

	return usersToBeQueryReadyFor
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

		tableNumber, err := extractTableNumberFromName(entry.Name())
		if err != nil {
			return err
		}
		if tableNumber == -1 || !tm.tableRangesToHandle.TableInRange(tableNumber, entry.Name()) {
			continue
		}

		level.Info(util_log.Logger).Log("msg", fmt.Sprintf("loading local table %s", entry.Name()))

		table, err := LoadTable(entry.Name(), filepath.Join(tm.cfg.CacheDir, entry.Name()),
			tm.indexStorageClient, tm.openIndexFileFunc, tm.metrics)
		if err != nil {
			return err
		}

		tm.tables[entry.Name()] = table
	}

	return nil
}

// extractTableNumberFromName extract the table number from a given tableName.
// if the tableName doesn't match the regex, it would return -1 as table number.
func extractTableNumberFromName(tableName string) (int64, error) {
	match := extractTableNumberRegex.Find([]byte(tableName))
	if match == nil {
		return -1, nil
	}

	tableNumber, err := strconv.ParseInt(string(match), 10, 64)
	if err != nil {
		return -1, err
	}

	return tableNumber, nil
}
func getActiveTableNumber() int64 {
	return getTableNumberForTime(model.Now())
}

func getTableNumberForTime(t model.Time) int64 {
	return t.Unix() / daySeconds
}
