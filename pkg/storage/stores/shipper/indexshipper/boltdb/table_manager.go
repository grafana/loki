package boltdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	chunk_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
)

type tableManagerMetrics struct {
	openExistingFileFailuresTotal prometheus.Counter
}

func newTableManagerMetrics(r prometheus.Registerer) *tableManagerMetrics {
	return &tableManagerMetrics{
		openExistingFileFailuresTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "open_existing_file_failures_total",
			Help:      "Total number of failures in opening of existing files while loading active index tables during startup",
		}),
	}
}

type Config struct {
	Uploader             string
	IndexDir             string
	DBRetainPeriod       time.Duration
	MakePerTenantBuckets bool
}

type TableManager struct {
	cfg          Config
	indexShipper Shipper

	metrics    *tableManagerMetrics
	logger     log.Logger
	tables     map[string]*Table
	tableRange config.TableRange
	tablesMtx  sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type Shipper interface {
	AddIndex(tableName, userID string, index shipperindex.Index) error
	ForEach(ctx context.Context, tableName, userID string, callback shipperindex.ForEachIndexCallback) error
}

func NewTableManager(cfg Config, indexShipper Shipper, tableRange config.TableRange, registerer prometheus.Registerer, logger log.Logger) (*TableManager, error) {
	err := chunk_util.EnsureDirectory(cfg.IndexDir)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	tm := TableManager{
		cfg:          cfg,
		indexShipper: indexShipper,
		metrics:      newTableManagerMetrics(registerer),
		tableRange:   tableRange,
		ctx:          ctx,
		cancel:       cancel,
		logger:       logger,
	}

	tables, err := tm.loadTables()
	if err != nil {
		return nil, err
	}

	tm.tables = tables
	// Increment the WaitGroup counter here before starting the goroutine
	tm.wg.Add(1)
	go tm.loop()
	return &tm, nil
}

func (tm *TableManager) loop() {
	defer tm.wg.Done()

	tm.handoverIndexesToShipper(false)

	syncTicker := time.NewTicker(indexshipper.UploadInterval)
	defer syncTicker.Stop()

	for {
		select {
		case <-syncTicker.C:
			tm.handoverIndexesToShipper(false)
		case <-tm.ctx.Done():
			return
		}
	}
}

func (tm *TableManager) Stop() {
	level.Info(tm.logger).Log("msg", "stopping table manager")

	tm.cancel()
	tm.wg.Wait()

	tm.handoverIndexesToShipper(true)
}

func (tm *TableManager) ForEach(ctx context.Context, tableName string, callback func(boltdb *bbolt.DB) error) error {
	table, ok := tm.getTable(tableName)
	if !ok {
		return nil
	}

	return table.ForEach(ctx, callback)
}

func (tm *TableManager) getTable(tableName string) (*Table, bool) {
	tm.tablesMtx.RLock()
	defer tm.tablesMtx.RUnlock()
	table, ok := tm.tables[tableName]
	return table, ok
}

func (tm *TableManager) BatchWrite(ctx context.Context, batch index.WriteBatch) error {
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
	table, ok := tm.getTable(tableName)

	if !ok {
		tm.tablesMtx.Lock()
		defer tm.tablesMtx.Unlock()

		table, ok = tm.tables[tableName]
		if !ok {
			var err error
			table, err = NewTable(filepath.Join(tm.cfg.IndexDir, tableName), tm.cfg.Uploader, tm.indexShipper, tm.cfg.MakePerTenantBuckets)
			if err != nil {
				return nil, err
			}

			tm.tables[tableName] = table
		}
	}

	return table, nil
}

func (tm *TableManager) handoverIndexesToShipper(force bool) {
	tm.tablesMtx.RLock()
	defer tm.tablesMtx.RUnlock()

	level.Info(tm.logger).Log("msg", "handing over indexes to shipper")

	for _, table := range tm.tables {
		err := table.HandoverIndexesToShipper(force)
		if err != nil {
			// continue handing over other tables while skipping cleanup for a failed one.
			level.Error(tm.logger).Log("msg", "failed to handover index", "table", table.name, "err", err)
			continue
		}

		err = table.Snapshot()
		if err != nil {
			// we do not want to stop handing over of index due to failures in snapshotting them so logging just the error here.
			level.Error(tm.logger).Log("msg", "failed to snapshot table for reads", "table", table.name, "err", err)
		}
	}
}

func (tm *TableManager) loadTables() (map[string]*Table, error) {
	localTables := make(map[string]*Table)
	dirEntries, err := os.ReadDir(tm.cfg.IndexDir)
	if err != nil {
		return nil, err
	}

	// regex matching table name patters, i.e prefix+period_number
	re, err := regexp.Compile(`.+[0-9]+$`)
	if err != nil {
		return nil, err
	}

	for _, entry := range dirEntries {
		if !re.MatchString(entry.Name()) {
			continue
		}

		if ok, err := tm.tableRange.TableInRange(entry.Name()); !ok {
			if err != nil {
				level.Error(tm.logger).Log("msg", "failed to load table", "table-name", entry.Name(), "err", err)
			} else {
				level.Debug(tm.logger).Log("msg", "skip loading table as it is not in range", "table-name", entry.Name())
			}

			continue
		}

		// since we are moving to keeping files for same table in a folder, if current element is a file we need to move it inside a directory with the same name
		// i.e file index_123 would be moved to path index_123/index_123.
		if !entry.IsDir() {
			level.Info(tm.logger).Log("msg", fmt.Sprintf("found a legacy file %s, moving it to folder with same name", entry.Name()))
			filePath := filepath.Join(tm.cfg.IndexDir, entry.Name())

			// create a folder with .temp suffix since we can't create a directory with same name as file.
			tempDirPath := filePath + ".temp"
			if err := chunk_util.EnsureDirectory(tempDirPath); err != nil {
				return nil, err
			}

			// move the file to temp dir.
			if err := os.Rename(filePath, filepath.Join(tempDirPath, entry.Name())); err != nil {
				return nil, err
			}

			// rename the directory to name of the file
			if err := os.Rename(tempDirPath, filePath); err != nil {
				return nil, err
			}
		}

		level.Info(tm.logger).Log("msg", fmt.Sprintf("loading table %s", entry.Name()))
		table, err := LoadTable(filepath.Join(tm.cfg.IndexDir, entry.Name()), tm.cfg.Uploader, tm.indexShipper, tm.cfg.MakePerTenantBuckets, tm.metrics)
		if err != nil {
			return nil, err
		}

		if table == nil {
			// if table is nil it means it has no files in it so remove the folder for that table.
			err := os.Remove(filepath.Join(tm.cfg.IndexDir, entry.Name()))
			if err != nil {
				level.Error(tm.logger).Log("msg", "failed to remove empty table folder", "table", entry.Name(), "err", err)
			}
			continue
		}

		// handover indexes to shipper since we won't modify them anymore.
		err = table.HandoverIndexesToShipper(true)
		if err != nil {
			return nil, err
		}

		// Queries are only done against table snapshots so it's important we snapshot as soon as the table is loaded.
		err = table.Snapshot()
		if err != nil {
			return nil, err
		}

		localTables[entry.Name()] = table
	}

	return localTables, nil
}
