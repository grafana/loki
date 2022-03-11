package downloads

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage/chunk"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

const (
	cacheCleanupInterval = time.Hour
	durationDay          = 24 * time.Hour
)

type Limits interface {
	AllByUserID() map[string]*validation.Limits
	DefaultLimits() *validation.Limits
}

type Config struct {
	CacheDir          string
	SyncInterval      time.Duration
	CacheTTL          time.Duration
	QueryReadyNumDays int
	Limits            Limits
}

type TableManager struct {
	cfg                Config
	boltIndexClient    BoltDBIndexClient
	indexStorageClient storage.Client

	tables    map[string]Table
	tablesMtx sync.RWMutex
	metrics   *metrics

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewTableManager(cfg Config, boltIndexClient BoltDBIndexClient, indexStorageClient storage.Client, registerer prometheus.Registerer) (*TableManager, error) {
	if err := chunk_util.EnsureDirectory(cfg.CacheDir); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	tm := &TableManager{
		cfg:                cfg,
		boltIndexClient:    boltIndexClient,
		indexStorageClient: indexStorageClient,
		tables:             make(map[string]Table),
		metrics:            newMetrics(registerer),
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

	go tm.loop()
	return tm, nil
}

func (tm *TableManager) loop() {
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

func (tm *TableManager) Stop() {
	tm.cancel()
	tm.wg.Wait()

	tm.tablesMtx.Lock()
	defer tm.tablesMtx.Unlock()

	for _, table := range tm.tables {
		table.Close()
	}
}

func (tm *TableManager) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	queriesByTable := util.QueriesByTable(queries)
	for tableName, queries := range queriesByTable {
		err := tm.query(ctx, tableName, queries, callback)
		if err != nil {
			return err
		}
	}

	return nil
}

func (tm *TableManager) query(ctx context.Context, tableName string, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	logger := util_log.WithContext(ctx, util_log.Logger)
	level.Debug(logger).Log("table-name", tableName)

	table, err := tm.getOrCreateTable(tableName)
	if err != nil {
		return err
	}

	return util.DoParallelQueries(ctx, table, queries, callback)
}

func (tm *TableManager) getOrCreateTable(tableName string) (Table, error) {
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
			err := chunk_util.EnsureDirectory(tablePath)
			if err != nil {
				return nil, err
			}

			table = NewTable(tableName, filepath.Join(tm.cfg.CacheDir, tableName), tm.indexStorageClient, tm.boltIndexClient, tm.metrics)
			tm.tables[tableName] = table
		}
	}

	return table, nil
}

func (tm *TableManager) syncTables(ctx context.Context) error {
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
		err = table.Sync(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

func (tm *TableManager) cleanupCache() error {
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
func (tm *TableManager) ensureQueryReadiness(ctx context.Context) error {
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

	// regex for finding daily tables which have a 5 digit number at the end.
	re, err := regexp.Compile(`.+[0-9]{5}$`)
	if err != nil {
		return err
	}

	for _, tableName := range tables {
		if !re.MatchString(tableName) {
			continue
		}

		tableNumber, err := strconv.ParseInt(tableName[len(tableName)-5:], 10, 64)
		if err != nil {
			return err
		}

		// continue if the table is not within query readiness
		if activeTableNumber-tableNumber > int64(largestQueryReadinessNum) {
			continue
		}

		// list the users that have dedicated index files for this table
		_, usersWithIndex, err := tm.indexStorageClient.ListFiles(ctx, tableName)
		if err != nil {
			return err
		}

		// find the users whos index we need to keep ready for querying from this table
		usersToBeQueryReadyFor := tm.findUsersInTableForQueryReadiness(tableNumber, usersWithIndex, queryReadinessNumByUserID)

		// continue if both user index and common index is not required to be downloaded for query readiness
		if len(usersToBeQueryReadyFor) == 0 && activeTableNumber-tableNumber > int64(tm.cfg.QueryReadyNumDays) {
			continue
		}

		table, err := tm.getOrCreateTable(tableName)
		if err != nil {
			return err
		}

		if err := table.EnsureQueryReadiness(ctx, usersToBeQueryReadyFor); err != nil {
			return err
		}
	}

	return nil
}

// findUsersInTableForQueryReadiness returns the users that needs their index to be query ready based on the tableNumber and
// query readiness number provided per user
func (tm *TableManager) findUsersInTableForQueryReadiness(tableNumber int64, usersWithIndexInTable []string,
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

		if activeTableNumber-tableNumber <= int64(queryReadyNumDays) {
			usersToBeQueryReadyFor = append(usersToBeQueryReadyFor, userID)
		}
	}

	return usersToBeQueryReadyFor
}

// loadLocalTables loads tables present locally.
func (tm *TableManager) loadLocalTables() error {
	filesInfo, err := ioutil.ReadDir(tm.cfg.CacheDir)
	if err != nil {
		return err
	}

	for _, fileInfo := range filesInfo {
		if !fileInfo.IsDir() {
			continue
		}

		level.Info(util_log.Logger).Log("msg", fmt.Sprintf("loading local table %s", fileInfo.Name()))

		table, err := LoadTable(fileInfo.Name(), filepath.Join(tm.cfg.CacheDir, fileInfo.Name()), tm.indexStorageClient, tm.boltIndexClient, tm.metrics)
		if err != nil {
			return err
		}

		tm.tables[fileInfo.Name()] = table
	}

	return nil
}

func getActiveTableNumber() int64 {
	periodSecs := int64(durationDay / time.Second)

	return time.Now().Unix() / periodSecs
}
