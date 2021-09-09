package downloads

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage/chunk"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
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
	indexStorageClient StorageClient

	tables    map[string]*Table
	tablesMtx sync.RWMutex
	metrics   *metrics

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewTableManager(cfg Config, boltIndexClient BoltDBIndexClient, indexStorageClient StorageClient, registerer prometheus.Registerer) (*TableManager, error) {
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
	log, ctx := spanlogger.New(ctx, "Shipper.Downloads.Query")
	defer log.Span.Finish()

	level.Debug(log).Log("table-name", tableName)

	table := tm.getOrCreateTable(ctx, tableName)

	err := util.DoParallelQueries(ctx, table, queries, callback)
	if err != nil {
		if table.Err() != nil {
			// table is in invalid state, remove the table so that next queries re-create it.
			tm.tablesMtx.Lock()
			defer tm.tablesMtx.Unlock()

			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("table %s has some problem, cleaning it up", tableName), "err", table.Err())

			delete(tm.tables, tableName)
			return table.Err()
		}
	}

	return err
}

func (tm *TableManager) getOrCreateTable(spanCtx context.Context, tableName string) *Table {
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

			table = NewTable(spanCtx, tableName, tm.cfg.CacheDir, tm.indexStorageClient, tm.boltIndexClient, tm.metrics)
			tm.tables[tableName] = table
		}
		tm.tablesMtx.Unlock()
	}

	return table
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
		lastUsedAt := table.LastUsedAt()
		if lastUsedAt.Add(tm.cfg.CacheTTL).Before(time.Now()) {
			level.Info(util_log.Logger).Log("msg", fmt.Sprintf("cleaning up expired table %s", name))
			err := table.CleanupAllDBs()
			if err != nil {
				return err
			}

			delete(tm.tables, name)

			// remove the directory where files for the table were downloaded.
			err = os.RemoveAll(path.Join(tm.cfg.CacheDir, name))
			if err != nil {
				level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to remove directory for table %s", name), "err", err)
			}
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
			// table already exists, update the last used at time to avoid cache cleanup operation removing it.
			table.UpdateLastUsedAt()
			continue
		}

		level.Info(util_log.Logger).Log("msg", "table required for query readiness does not exist locally, downloading it", "table-name", tableName)
		// table doesn't exist, download it.
		table, err := LoadTable(tm.ctx, tableName, tm.cfg.CacheDir, tm.indexStorageClient, tm.boltIndexClient, tm.metrics)
		if err != nil {
			return err
		}

		tm.tablesMtx.Lock()
		tm.tables[tableName] = table
		tm.tablesMtx.Unlock()
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

		table, err := LoadTable(tm.ctx, fileInfo.Name(), tm.cfg.CacheDir, tm.indexStorageClient, tm.boltIndexClient, tm.metrics)
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
