package uploads

import (
	"context"
	"errors"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	pkg_util "github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// UploadInterval defines interval for uploading active boltdb files from local which are being written to by ingesters.
	UploadInterval = 15 * time.Minute
)

type TableManager struct {
	uploader        string
	indexDir        string
	boltIndexClient BoltDBIndexClient
	storageClient   chunk.ObjectClient

	metrics        *metrics
	tables         map[string]*Table
	localTablesMtx sync.RWMutex

	done chan struct{}
	wg   sync.WaitGroup
}

func NewTableManager(boltIndexClient BoltDBIndexClient, storageClient chunk.ObjectClient, uploader, indexDir string, registerer prometheus.Registerer) (*TableManager, error) {
	tm := TableManager{
		boltIndexClient: boltIndexClient,
		uploader:        uploader,
		indexDir:        indexDir,
		storageClient:   storageClient,
		metrics:         newMetrics(registerer),
		done:            make(chan struct{}),
	}

	tables, err := tm.loadTables()
	if err != nil {
		return nil, err
	}

	tm.tables = tables
	return &tm, nil
}

func (tm *TableManager) loop() {
	defer tm.wg.Done()

	syncTicker := time.NewTicker(UploadInterval)
	defer syncTicker.Stop()

	for {
		select {
		case <-syncTicker.C:
			err := tm.uploadTables(context.Background())
			if err != nil {
				level.Error(pkg_util.Logger).Log("msg", "error uploading local boltdb files to the storage", "err", err)
			}
		case <-tm.done:
			return
		}
	}
}

func (tm *TableManager) Stop() {
	close(tm.done)
	tm.wg.Wait()

	err := tm.uploadTables(context.Background())
	if err != nil {
		level.Error(pkg_util.Logger).Log("msg", "error uploading local boltdb files to the storage before stopping", "err", err)
	}
}

func (tm *TableManager) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	return chunk_util.DoParallelQueries(ctx, tm.query, queries, callback)
}

func (tm *TableManager) query(ctx context.Context, query chunk.IndexQuery, callback chunk_util.Callback) error {
	table, ok := tm.tables[query.TableName]
	if !ok {
		return nil
	}

	return table.Query(ctx, query, callback)
}

func (tm *TableManager) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	boltWriteBatch, ok := batch.(*local.BoltWriteBatch)
	if !ok {
		return errors.New("invalid write batch")
	}

	for tableName, tableWrites := range boltWriteBatch.Writes {
		table, err := tm.getOrCreateLocalTable(tableName)
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

func (tm *TableManager) getOrCreateLocalTable(tableName string) (*Table, error) {
	tm.localTablesMtx.RLock()
	table, ok := tm.tables[tableName]
	tm.localTablesMtx.RUnlock()

	if !ok {
		tm.localTablesMtx.Lock()
		defer tm.localTablesMtx.Unlock()

		table, ok = tm.tables[tableName]
		if !ok {
			var err error
			table, err = NewTable(filepath.Join(tm.indexDir, tableName), tm.uploader, tm.storageClient, tm.boltIndexClient)
			if err != nil {
				return nil, err
			}

			tm.tables[tableName] = table
		}
	}

	return table, nil
}

func (tm *TableManager) uploadTables(ctx context.Context) (err error) {
	tm.localTablesMtx.RLock()
	defer tm.localTablesMtx.RUnlock()

	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
		}
		tm.metrics.tablesUploadOperationTotal.WithLabelValues(status).Inc()
	}()

	for _, table := range tm.tables {
		err := table.Upload(ctx)
		if err != nil {
			return err
		}
	}

	return
}

func (tm *TableManager) loadTables() (map[string]*Table, error) {
	localTables := make(map[string]*Table)
	filesInfo, err := ioutil.ReadDir(tm.indexDir)
	if err != nil {
		return nil, err
	}

	re, err := regexp.Compile(`.+[0-9]+$`)
	if err != nil {
		return nil, err
	}

	for _, fileInfo := range filesInfo {
		if !fileInfo.IsDir() || !re.MatchString(fileInfo.Name()) {
			continue
		}

		table, err := LoadTable(filepath.Join(tm.indexDir, fileInfo.Name()), tm.uploader, tm.storageClient, tm.boltIndexClient)
		if err != nil {
			return nil, err
		}

		if table == nil {
			continue
		}

		localTables[fileInfo.Name()] = table
	}

	return localTables, nil
}
