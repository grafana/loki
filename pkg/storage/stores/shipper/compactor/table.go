package compactor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"

	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

const (
	compactMinDBs = 4
	uploaderName  = "compactor"

	readDBsParallelism = 50
	batchSize          = 1000
)

var bucketName = []byte("index")

type indexEntry struct {
	k, v []byte
}

type table struct {
	name             string
	workingDirectory string
	storageClient    chunk.ObjectClient

	compactedDB *bbolt.DB

	ctx  context.Context
	quit chan struct{}
}

func newTable(ctx context.Context, workingDirectory string, objectClient chunk.ObjectClient) (*table, error) {
	err := chunk_util.EnsureDirectory(workingDirectory)
	if err != nil {
		return nil, err
	}

	table := table{
		ctx:              ctx,
		name:             filepath.Base(workingDirectory),
		workingDirectory: workingDirectory,
		storageClient:    objectClient,
		quit:             make(chan struct{}),
	}

	return &table, nil
}

func (t *table) compact() error {
	objects, _, err := t.storageClient.List(t.ctx, t.name+"/")
	if err != nil {
		return err
	}

	level.Info(util.Logger).Log("msg", "listed files", "count", len(objects))

	if len(objects) < compactMinDBs {
		level.Info(util.Logger).Log("msg", fmt.Sprintf("skipping compaction since we have just %d files in storage", len(objects)))
		return nil
	}

	defer func() {
		err := t.cleanup()
		if err != nil {
			level.Error(util.Logger).Log("msg", "failed to cleanup table", "name", t.name)
		}
	}()

	t.compactedDB, err = local.OpenBoltdbFile(filepath.Join(t.workingDirectory, fmt.Sprint(time.Now().Unix())))
	if err != nil {
		return err
	}

	level.Info(util.Logger).Log("msg", "starting compaction of dbs")

	errChan := make(chan error)
	readObjectChan := make(chan string)
	n := util.Min(len(objects), readDBsParallelism)

	// read files parallely
	for i := 0; i < n; i++ {
		go func() {
			var err error
			defer func() {
				errChan <- err
			}()

			for {
				select {
				case objectKey, ok := <-readObjectChan:
					if !ok {
						return
					}

					var dbName string
					dbName, err = shipper_util.GetDBNameFromObjectKey(objectKey)
					if err != nil {
						return
					}

					downloadAt := filepath.Join(t.workingDirectory, dbName)

					err = shipper_util.GetFileFromStorage(t.ctx, t.storageClient, objectKey, downloadAt)
					if err != nil {
						return
					}

					err = t.readFile(downloadAt)
					if err != nil {
						level.Error(util.Logger).Log("msg", "error reading file", "err", err)
						return
					}
				case <-t.quit:
					return
				case <-t.ctx.Done():
					return
				}
			}

		}()
	}

	// send all files to readObjectChan
	go func() {
		for _, object := range objects {
			select {
			case readObjectChan <- object.Key:
			case <-t.quit:
				break
			case <-t.ctx.Done():
				break
			}
		}

		level.Debug(util.Logger).Log("msg", "closing readObjectChan")

		close(readObjectChan)
	}()

	var firstErr error

	// read all the errors
	for i := 0; i < n; i++ {
		select {
		case err := <-errChan:
			if err != nil && firstErr == nil {
				firstErr = err
				close(t.quit)
			}
		}
	}

	if firstErr != nil {
		return firstErr
	}

	// check whether we stopped compaction due to context being cancelled.
	select {
	case <-t.ctx.Done():
		return nil
	default:
	}

	level.Info(util.Logger).Log("msg", "finished compacting the dbs")

	// upload the compacted db
	err = t.upload()
	if err != nil {
		return err
	}

	// remove source files from storage which were compacted
	return t.removeObjectsFromStorage(objects)
}

func (t *table) cleanup() error {
	if t.compactedDB != nil {
		err := t.compactedDB.Close()
		if err != nil {
			return err
		}
	}

	return os.RemoveAll(t.workingDirectory)
}

// writeBatch writes a batch to compactedDB
func (t *table) writeBatch(batch []indexEntry) error {
	return t.compactedDB.Batch(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketName)
		if err != nil {
			return err
		}

		for _, w := range batch {
			err = b.Put(w.k, w.v)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// readFile reads a boltdb file from a path and writes the index in batched mode to compactedDB
func (t *table) readFile(path string) error {
	level.Debug(util.Logger).Log("msg", "reading file for compaction", "path", path)

	db, err := local.OpenBoltdbFile(path)
	if err != nil {
		return err
	}

	defer func() {
		if err := db.Close(); err != nil {
			level.Error(util.Logger).Log("msg", "failed to close db", "path", path, "err", err)
		}

		if err = os.Remove(path); err != nil {
			level.Error(util.Logger).Log("msg", "failed to remove file", "path", path, "err", err)
		}
	}()

	writeBatch := make([]indexEntry, 0, batchSize)

	return db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return errors.New("bucket not found")
		}

		err := b.ForEach(func(k, v []byte) error {
			ie := indexEntry{
				k: make([]byte, len(k)),
				v: make([]byte, len(v)),
			}

			// make a copy since k, v are only valid for the life of the transaction.
			// See: https://godoc.org/github.com/boltdb/bolt#Cursor.Seek
			copy(ie.k, k)
			copy(ie.v, v)

			writeBatch = append(writeBatch, ie)

			if len(writeBatch) == cap(writeBatch) {
				// batch is full, write the batch and create a new one.
				err := t.writeBatch(writeBatch)
				if err != nil {
					return err
				}

				writeBatch = make([]indexEntry, 0, batchSize)
			}

			return nil
		})
		if err != nil {
			return err
		}

		// write the remaining batch which might have been left unwritten due to it not being full yet.
		return t.writeBatch(writeBatch)
	})
}

// upload uploads the compacted db in compressed format.
func (t *table) upload() error {
	compactedDBPath := t.compactedDB.Path()

	// close the compactedDB to make sure all the writes are processed.
	err := t.compactedDB.Close()
	if err != nil {
		return err
	}

	t.compactedDB = nil

	// compress the compactedDB.
	compressedDBPath := fmt.Sprintf("%s.gz", compactedDBPath)
	err = shipper_util.CompressFile(compactedDBPath, compressedDBPath)
	if err != nil {
		return err
	}

	// open the file for reading.
	compressedDB, err := os.Open(compressedDBPath)
	if err != nil {
		return err
	}

	defer func() {
		if err := compressedDB.Close(); err != nil {
			level.Error(util.Logger).Log("msg", "failed to close file", "path", compactedDBPath, "err", err)
		}

		if err := os.Remove(compressedDBPath); err != nil {
			level.Error(util.Logger).Log("msg", "failed to remove file", "path", compressedDBPath, "err", err)
		}
	}()

	objectKey := fmt.Sprintf("%s.gz", shipper_util.BuildObjectKey(t.name, uploaderName, fmt.Sprint(time.Now().Unix())))
	level.Info(util.Logger).Log("msg", "uploading the compacted file", "objectKey", objectKey)

	return t.storageClient.PutObject(t.ctx, objectKey, compressedDB)
}

// removeObjectsFromStorage deletes objects from storage.
func (t *table) removeObjectsFromStorage(objects []chunk.StorageObject) error {
	level.Info(util.Logger).Log("msg", "removing source db files from storage", "count", len(objects))

	for _, object := range objects {
		err := t.storageClient.DeleteObject(t.ctx, object.Key)
		if err != nil {
			return err
		}
	}

	return nil
}
