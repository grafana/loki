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

	util_log "github.com/cortexproject/cortex/pkg/util/log"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage/chunk"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

const (
	cacheCleanupInterval = time.Hour
	durationDay          = 24 * time.Hour
)

type Config struct {
	CacheDir          string
	SyncInterval      time.Duration
	CacheTTL          time.Duration
	QueryReadyNumDays int
}

type TableManager struct {
	cfg                Config
	boltIndexClient    BoltDBIndexClient
	indexStorageClient storage.Client

	tables    map[string]*Table
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
		tables:             make(map[string]*Table),
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
	err = tm.ensureQueryReadiness()
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
			err = tm.ensureQueryReadiness()
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

func (tm *TableManager) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback chunk_util.Callback) error {
	queriesByTable := util.QueriesByTable(queries)
	for tableName, queries := range queriesByTable {
		err := tm.query(ctx, tableName, queries, callback)
		if err != nil {
			return err
		}
	}

	return nil
}

func (tm *TableManager) query(ctx context.Context, tableName string, queries []chunk.IndexQuery, callback chunk_util.Callback) error {
	logger := util_log.WithContext(ctx, util_log.Logger)
	level.Debug(logger).Log("table-name", tableName)

	table, err := tm.getOrCreateTable(tableName)
	if err != nil {
		return err
	}

	return util.DoParallelQueries(ctx, table, queries, callback)
}

func (tm *TableManager) getOrCreateTable(tableName string) (*Table, error) {
	// if table is already there, use it.
	tm.tablesMtx.RLock()
	table, ok := tm.tables[tableName]
	tm.tablesMtx.RUnlock()

	if !ok {
		tm.tablesMtx.Lock()
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
		tm.tablesMtx.Unlock()
	}

	return table, nil
}

func (tm *TableManager) syncTables(ctx context.Context) error {
	tm.tablesMtx.RLock()
	defer tm.tablesMtx.RUnlock()

	var err error

	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
		}

		tm.metrics.tablesSyncOperationTotal.WithLabelValues(status).Inc()
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
func (tm *TableManager) ensureQueryReadiness() error {
	if tm.cfg.QueryReadyNumDays == 0 {
		return nil
	}

	tableNames, err := tm.indexStorageClient.ListTables(context.Background())
	if err != nil {
		return err
	}

	// get the names of tables required for being query ready.
	tableNames, err = tm.tablesRequiredForQueryReadiness(tableNames)
	if err != nil {
		return err
	}

	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("list of tables required for query-readiness %s", tableNames))

	for _, tableName := range tableNames {
		tm.tablesMtx.RLock()
		table, ok := tm.tables[tableName]
		tm.tablesMtx.RUnlock()
		if ok {
			err = table.EnsureQueryReadiness(context.Background())
			if err != nil {
				return err
			}
			continue
		}

		level.Info(util_log.Logger).Log("msg", "table required for query readiness does not exist locally, downloading it", "table-name", tableName)
		// table doesn't exist, download it.
		tablePath := filepath.Join(tm.cfg.CacheDir, tableName)
		err = chunk_util.EnsureDirectory(tablePath)
		if err != nil {
			return err
		}

		table, err = LoadTable(tableName, filepath.Join(tm.cfg.CacheDir, tableName), tm.indexStorageClient, tm.boltIndexClient, tm.metrics)
		if err != nil {
			return err
		}

		tm.tablesMtx.Lock()
		tm.tables[tableName] = table
		tm.tablesMtx.Unlock()

		err = table.EnsureQueryReadiness(context.Background())
		if err != nil {
			return err
		}
	}

	return nil
}

// queryReadyTableNumbersRange returns the table numbers range. Table numbers are added as suffix to table names.
func (tm *TableManager) queryReadyTableNumbersRange() (int64, int64) {
	newestTableNumber := getActiveTableNumber()

	return newestTableNumber - int64(tm.cfg.QueryReadyNumDays), newestTableNumber
}

// tablesRequiredForQueryReadiness returns the names of tables required to be downloaded for being query ready as per configured QueryReadyNumDays.
// It only considers daily tables for simplicity and we anyways have made it mandatory to have daily tables with boltdb-shipper.
func (tm *TableManager) tablesRequiredForQueryReadiness(tablesInStorage []string) ([]string, error) {
	// regex for finding daily tables which have a 5 digit number at the end.
	re, err := regexp.Compile(`.+[0-9]{5}$`)
	if err != nil {
		return nil, err
	}

	minTableNumber, maxTableNumber := tm.queryReadyTableNumbersRange()
	var requiredTableNames []string

	for _, tableName := range tablesInStorage {
		if !re.MatchString(tableName) {
			continue
		}

		tableNumber, err := strconv.ParseInt(tableName[len(tableName)-5:], 10, 64)
		if err != nil {
			return nil, err
		}

		if minTableNumber <= tableNumber && tableNumber <= maxTableNumber {
			requiredTableNames = append(requiredTableNames, tableName)
		}
	}

	return requiredTableNames, nil
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
