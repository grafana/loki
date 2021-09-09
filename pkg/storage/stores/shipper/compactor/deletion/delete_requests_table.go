package deletion

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type deleteRequestsTable struct {
	indexStorageClient storage.Client
	dbPath             string

	boltdbIndexClient *local.BoltIndexClient
	db                *bbolt.DB
	done              chan struct{}
	wg                sync.WaitGroup
}

const deleteRequestsIndexFileName = DeleteRequestsTableName + ".gz"

func newDeleteRequestsTable(workingDirectory string, indexStorageClient storage.Client) (chunk.IndexClient, error) {
	dbPath := filepath.Join(workingDirectory, DeleteRequestsTableName, DeleteRequestsTableName)
	boltdbIndexClient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: filepath.Dir(dbPath)})
	if err != nil {
		return nil, err
	}

	table := &deleteRequestsTable{
		indexStorageClient: indexStorageClient,
		dbPath:             dbPath,
		boltdbIndexClient:  boltdbIndexClient,
		done:               make(chan struct{}),
	}

	err = table.init()
	if err != nil {
		return nil, err
	}

	go table.loop()
	return table, nil
}

func (t *deleteRequestsTable) init() error {
	tempFilePath := fmt.Sprintf("%s%s", t.dbPath, tempFileSuffix)

	if err := os.Remove(tempFilePath); err != nil && !os.IsNotExist(err) {
		level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to remove temp file %s", tempFilePath), "err", err)
	}

	_, err := os.Stat(t.dbPath)
	if err != nil {
		err = shipper_util.GetFileFromStorage(context.Background(), t.indexStorageClient, DeleteRequestsTableName, deleteRequestsIndexFileName, t.dbPath, true)
		if err != nil && !t.indexStorageClient.IsFileNotFoundErr(err) {
			return err
		}
	}

	t.db, err = shipper_util.SafeOpenBoltdbFile(t.dbPath)
	return err
}

func (t *deleteRequestsTable) loop() {
	uploadTicker := time.NewTicker(5 * time.Minute)
	defer uploadTicker.Stop()

	t.wg.Add(1)
	defer t.wg.Done()

	for {
		select {
		case <-uploadTicker.C:
			if err := t.uploadFile(); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to upload delete requests file", "err", err)
			}
		case <-t.done:
			return
		}
	}
}

func (t *deleteRequestsTable) uploadFile() error {
	level.Debug(util_log.Logger).Log("msg", "uploading delete requests db")

	tempFilePath := fmt.Sprintf("%s.%s", t.dbPath, tempFileSuffix)
	f, err := os.Create(tempFilePath)
	if err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close temp file", "path", tempFilePath, "err", err)
		}

		if err := os.Remove(tempFilePath); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to remove temp file", "path", tempFilePath, "err", err)
		}
	}()

	err = t.db.View(func(tx *bbolt.Tx) (err error) {
		compressedWriter := chunkenc.Gzip.GetWriter(f)
		defer chunkenc.Gzip.PutWriter(compressedWriter)

		defer func() {
			cerr := compressedWriter.Close()
			if err == nil {
				err = cerr
			}
		}()

		_, err = tx.WriteTo(compressedWriter)
		return
	})
	if err != nil {
		return err
	}

	// flush the file to disk and seek the file to the beginning.
	if err := f.Sync(); err != nil {
		return err
	}

	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	return t.indexStorageClient.PutFile(context.Background(), DeleteRequestsTableName, deleteRequestsIndexFileName, f)
}

func (t *deleteRequestsTable) Stop() {
	close(t.done)
	t.wg.Wait()

	if err := t.uploadFile(); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to upload delete requests file during shutdown", "err", err)
	}

	if err := t.db.Close(); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to close delete requests db", "err", err)
	}

	t.boltdbIndexClient.Stop()
}

func (t *deleteRequestsTable) NewWriteBatch() chunk.WriteBatch {
	return t.boltdbIndexClient.NewWriteBatch()
}

func (t *deleteRequestsTable) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	boltWriteBatch, ok := batch.(*local.BoltWriteBatch)
	if !ok {
		return errors.New("invalid write batch")
	}

	for _, tableWrites := range boltWriteBatch.Writes {
		if err := t.boltdbIndexClient.WriteToDB(ctx, t.db, tableWrites); err != nil {
			return err
		}
	}

	return nil
}

func (t *deleteRequestsTable) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	for _, query := range queries {
		if err := t.boltdbIndexClient.QueryDB(ctx, t.db, query, callback); err != nil {
			return err
		}
	}

	return nil
}
