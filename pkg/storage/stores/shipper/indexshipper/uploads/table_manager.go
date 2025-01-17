package uploads

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

type Config struct {
	UploadInterval time.Duration
	DBRetainPeriod time.Duration
}

type TableManager interface {
	Stop()
	AddIndex(tableName, userID string, index index.Index) error
	ForEach(tableName, userID string, callback index.ForEachIndexCallback) error
}

type tableManager struct {
	cfg           Config
	storageClient storage.Client

	tables    map[string]Table
	tablesMtx sync.RWMutex
	metrics   *metrics
	logger    log.Logger

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewTableManager(cfg Config, storageClient storage.Client, reg prometheus.Registerer, logger log.Logger) (TableManager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	tm := tableManager{
		cfg:           cfg,
		storageClient: storageClient,
		tables:        map[string]Table{},
		metrics:       newMetrics(reg),
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
	}

	go tm.loop()
	return &tm, nil
}

func (tm *tableManager) loop() {
	tm.wg.Add(1)
	defer tm.wg.Done()

	tm.uploadTables(context.Background())

	syncTicker := time.NewTicker(tm.cfg.UploadInterval)
	defer syncTicker.Stop()

	for {
		select {
		case <-syncTicker.C:
			tm.uploadTables(context.Background())
		case <-tm.ctx.Done():
			return
		}
	}
}

func (tm *tableManager) Stop() {
	level.Info(tm.logger).Log("msg", "stopping table manager")

	tm.cancel()
	tm.wg.Wait()

	tm.uploadTables(context.Background())

	tm.tablesMtx.Lock()
	defer tm.tablesMtx.Unlock()
	for _, table := range tm.tables {
		table.Stop()
	}

	tm.tables = map[string]Table{}
}

func (tm *tableManager) AddIndex(tableName, userID string, index index.Index) error {
	return tm.getOrCreateTable(tableName).AddIndex(userID, index)
}

func (tm *tableManager) getTable(tableName string) (Table, bool) {
	tm.tablesMtx.RLock()
	defer tm.tablesMtx.RUnlock()

	table, ok := tm.tables[tableName]
	return table, ok
}

func (tm *tableManager) getOrCreateTable(tableName string) Table {
	table, ok := tm.getTable(tableName)
	if ok {
		return table
	}

	tm.tablesMtx.Lock()
	defer tm.tablesMtx.Unlock()

	table, ok = tm.tables[tableName]
	if !ok {
		table = NewTable(tableName, tm.storageClient)
		tm.tables[tableName] = table
	}

	return table
}

func (tm *tableManager) ForEach(tableName, userID string, callback index.ForEachIndexCallback) error {
	table, ok := tm.getTable(tableName)
	if !ok {
		return nil
	}

	return table.ForEach(userID, callback)
}

func (tm *tableManager) uploadTables(ctx context.Context) {
	tm.tablesMtx.RLock()
	defer tm.tablesMtx.RUnlock()

	level.Info(tm.logger).Log("msg", "uploading tables")

	status := statusSuccess
	for _, table := range tm.tables {
		err := table.Upload(ctx)
		if err != nil {
			status = statusFailure
			level.Error(tm.logger).Log("msg", "failed to upload table", "table", table.Name(), "err", err)
			continue
		}

		// cleanup uploaded dbs from local disk after retain period
		err = table.Cleanup(tm.cfg.DBRetainPeriod)
		if err != nil {
			// we do not want to stop uploading of dbs due to failures in cleaning them up so logging just the error here.
			level.Error(tm.logger).Log("msg", "failed to cleanup uploaded index past their retention period", "table", table.Name(), "err", err)
		}
	}

	tm.metrics.tablesUploadOperationTotal.WithLabelValues(status).Inc()
}
