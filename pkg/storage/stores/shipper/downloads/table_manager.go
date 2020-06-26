package downloads

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	pkg_util "github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
)

const cacheCleanupInterval = 24 * time.Hour

type TableManager struct {
	boltIndexClient BoltDBIndexClient
	indexDir        string
	storageClient   chunk.ObjectClient
	syncInterval    time.Duration
	cacheTTL        time.Duration

	tables    map[string]*Table
	tablesMtx sync.RWMutex
	metrics   *metrics

	done chan struct{}
	wg   sync.WaitGroup
}

func NewTableManager(boltIndexClient BoltDBIndexClient, indexDir string, storageClient chunk.ObjectClient,
	syncInterval, cacheTTL time.Duration, registerer prometheus.Registerer) (*TableManager, error) {
	return &TableManager{
		boltIndexClient: boltIndexClient,
		indexDir:        indexDir,
		storageClient:   storageClient,
		syncInterval:    syncInterval,
		cacheTTL:        cacheTTL,
		tables:          make(map[string]*Table),
		metrics:         newMetrics(registerer),
		done:            make(chan struct{}),
	}, nil
}

func (tm *TableManager) loop() {
	defer tm.wg.Done()

	syncTicker := time.NewTicker(tm.syncInterval)
	defer syncTicker.Stop()

	cacheCleanupTicker := time.NewTicker(cacheCleanupInterval)
	defer cacheCleanupTicker.Stop()

	for {
		select {
		case <-syncTicker.C:
			err := tm.syncTables(context.Background())
			if err != nil {
				level.Error(pkg_util.Logger).Log("msg", "error syncing local boltdb files with storage", "err", err)
			}
		case <-cacheCleanupTicker.C:
			err := tm.cleanupCache()
			if err != nil {
				level.Error(pkg_util.Logger).Log("msg", "error cleaning up expired tables", "err", err)
			}
		case <-tm.done:
			return
		}
	}
}

func (tm *TableManager) Stop() {
	close(tm.done)
	tm.wg.Wait()

	tm.tablesMtx.Lock()
	defer tm.tablesMtx.Unlock()

	for _, table := range tm.tables {
		table.Close()
	}
}

func (tm *TableManager) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	return chunk_util.DoParallelQueries(ctx, tm.query, queries, callback)
}

func (tm *TableManager) query(ctx context.Context, query chunk.IndexQuery, callback chunk_util.Callback) error {
	table := tm.getOrCreateTable(query.TableName)

	// let us check if table is ready for use while also honoring the context timeout
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-table.IsReady():
	}

	if table.Err() != nil {
		tm.tablesMtx.Lock()
		defer tm.tablesMtx.Unlock()

		delete(tm.tables, query.TableName)
		return table.Err()
	}

	return table.Query(ctx, query, callback)
}

func (tm *TableManager) getOrCreateTable(tableName string) *Table {
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
			level.Info(pkg_util.Logger).Log("msg", fmt.Sprintf("downloading all files for table %s", tableName))

			table = NewTable(tableName, tm.indexDir, tm.storageClient, tm.metrics)
			tm.tables[tableName] = table
		}
		tm.tablesMtx.Unlock()
	}

	return table
}

func (tm *TableManager) syncTables(ctx context.Context) error {
	tm.tablesMtx.RLock()
	defer tm.tablesMtx.RUnlock()

	for _, table := range tm.tables {
		err := table.Sync(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

func (tm *TableManager) cleanupCache() error {
	tm.tablesMtx.Lock()
	defer tm.tablesMtx.Unlock()

	for name, table := range tm.tables {
		lastUsedAt := table.LastUsedAt()
		if lastUsedAt.Add(tm.cacheTTL).Before(time.Now()) {
			err := table.CleanupAllFiles()
			if err != nil {
				return err
			}

			delete(tm.tables, name)
		}
	}

	return nil
}
