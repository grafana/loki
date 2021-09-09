package uploads

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type Config struct {
	Uploader       string
	IndexDir       string
	UploadInterval time.Duration
	DBRetainPeriod time.Duration
}

type TableManager struct {
	cfg             Config
	boltIndexClient BoltDBIndexClient
	storageClient   StorageClient

	metrics   *metrics
	tables    map[string]*Table
	tablesMtx sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewTableManager(cfg Config, boltIndexClient BoltDBIndexClient, storageClient StorageClient, registerer prometheus.Registerer) (*TableManager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	tm := TableManager{
		cfg:             cfg,
		boltIndexClient: boltIndexClient,
		storageClient:   storageClient,
		metrics:         newMetrics(registerer),
		ctx:             ctx,
		cancel:          cancel,
	}

	tables, err := tm.loadTables()
	if err != nil {
		return nil, err
	}

	tm.tables = tables
	go tm.loop()
	return &tm, nil
}

func (tm *TableManager) loop() {
	tm.wg.Add(1)
	defer tm.wg.Done()

	tm.uploadTables(context.Background(), false)

	syncTicker := time.NewTicker(tm.cfg.UploadInterval)
	defer syncTicker.Stop()

	for {
		select {
		case <-syncTicker.C:
			tm.uploadTables(context.Background(), false)
		case <-tm.ctx.Done():
			return
		}
	}
}

func (tm *TableManager) Stop() {
	level.Info(util_log.Logger).Log("msg", "stopping table manager")

	tm.cancel()
	tm.wg.Wait()

	tm.uploadTables(context.Background(), true)
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
	tm.tablesMtx.RLock()
	defer tm.tablesMtx.RUnlock()

	log, ctx := spanlogger.New(ctx, "Shipper.Uploads.Query")
	defer log.Span.Finish()

	table, ok := tm.tables[tableName]
	if !ok {
		return nil
	}

	return util.DoParallelQueries(ctx, table, queries, callback)
}

func (tm *TableManager) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	boltWriteBatch, ok := batch.(*local.BoltWriteBatch)
	if !ok {
		return errors.New("invalid write batch")
	}

	for tableName, tableWrites := range boltWriteBatch.Writes {
		table, err := tm.getOrCreateTable(tableName)
		if err != nil {
			return err
		}

		err = table.Write(ctx, tableWrites)
		if err != nil {
			return err
		}
	}

	return nil
}

func (tm *TableManager) getOrCreateTable(tableName string) (*Table, error) {
	tm.tablesMtx.RLock()
	table, ok := tm.tables[tableName]
	tm.tablesMtx.RUnlock()

	if !ok {
		tm.tablesMtx.Lock()
		defer tm.tablesMtx.Unlock()

		table, ok = tm.tables[tableName]
		if !ok {
			var err error
			table, err = NewTable(filepath.Join(tm.cfg.IndexDir, tableName), tm.cfg.Uploader, tm.storageClient, tm.boltIndexClient)
			if err != nil {
				return nil, err
			}

			tm.tables[tableName] = table
		}
	}

	return table, nil
}

func (tm *TableManager) uploadTables(ctx context.Context, force bool) {
	tm.tablesMtx.RLock()
	defer tm.tablesMtx.RUnlock()

	level.Info(util_log.Logger).Log("msg", "uploading tables")

	status := statusSuccess
	for _, table := range tm.tables {
		err := table.Snapshot()
		if err != nil {
			// we do not want to stop uploading of dbs due to failures in snapshotting them so logging just the error here.
			level.Error(util_log.Logger).Log("msg", "failed to snapshot table for reads", "table", table.name, "err", err)
		}

		err = table.Upload(ctx, force)
		if err != nil {
			// continue uploading other tables while skipping cleanup for a failed one.
			status = statusFailure
			level.Error(util_log.Logger).Log("msg", "failed to upload dbs", "table", table.name, "err", err)
			continue
		}

		// cleanup unwanted dbs from the table
		err = table.Cleanup(tm.cfg.DBRetainPeriod)
		if err != nil {
			// we do not want to stop uploading of dbs due to failures in cleaning them up so logging just the error here.
			level.Error(util_log.Logger).Log("msg", "failed to cleanup uploaded dbs past their retention period", "table", table.name, "err", err)
		}
	}

	tm.metrics.tablesUploadOperationTotal.WithLabelValues(status).Inc()
}

func (tm *TableManager) loadTables() (map[string]*Table, error) {
	localTables := make(map[string]*Table)
	filesInfo, err := ioutil.ReadDir(tm.cfg.IndexDir)
	if err != nil {
		return nil, err
	}

	// regex matching table name patters, i.e prefix+period_number
	re, err := regexp.Compile(`.+[0-9]+$`)
	if err != nil {
		return nil, err
	}

	for _, fileInfo := range filesInfo {
		if !re.MatchString(fileInfo.Name()) {
			continue
		}

		// since we are moving to keeping files for same table in a folder, if current element is a file we need to move it inside a directory with the same name
		// i.e file index_123 would be moved to path index_123/index_123.
		if !fileInfo.IsDir() {
			level.Info(util_log.Logger).Log("msg", fmt.Sprintf("found a legacy file %s, moving it to folder with same name", fileInfo.Name()))
			filePath := filepath.Join(tm.cfg.IndexDir, fileInfo.Name())

			// create a folder with .temp suffix since we can't create a directory with same name as file.
			tempDirPath := filePath + ".temp"
			if err := chunk_util.EnsureDirectory(tempDirPath); err != nil {
				return nil, err
			}

			// move the file to temp dir.
			if err := os.Rename(filePath, filepath.Join(tempDirPath, fileInfo.Name())); err != nil {
				return nil, err
			}

			// rename the directory to name of the file
			if err := os.Rename(tempDirPath, filePath); err != nil {
				return nil, err
			}
		}

		level.Info(util_log.Logger).Log("msg", fmt.Sprintf("loading table %s", fileInfo.Name()))
		table, err := LoadTable(filepath.Join(tm.cfg.IndexDir, fileInfo.Name()), tm.cfg.Uploader, tm.storageClient, tm.boltIndexClient, tm.metrics)
		if err != nil {
			return nil, err
		}

		if table == nil {
			// if table is nil it means it has no files in it so remove the folder for that table.
			err := os.Remove(filepath.Join(tm.cfg.IndexDir, fileInfo.Name()))
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to remove empty table folder", "table", fileInfo.Name(), "err", err)
			}
			continue
		}

		// Queries are only done against table snapshots so it's important we snapshot as soon as the table is loaded.
		err = table.Snapshot()
		if err != nil {
			return nil, err
		}

		localTables[fileInfo.Name()] = table
	}

	return localTables, nil
}
