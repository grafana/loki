package compactor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	util_math "github.com/cortexproject/cortex/pkg/util/math"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
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
	applyRetention   bool
	tableMarker      retention.TableMarker

	compactedDB *bbolt.DB
	logger      log.Logger

	ctx  context.Context
	quit chan struct{}
}

func newTable(ctx context.Context, workingDirectory string, objectClient chunk.ObjectClient, applyRetention bool, tableMarker retention.TableMarker) (*table, error) {
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
		applyRetention:   applyRetention,
		tableMarker:      tableMarker,
	}
	table.logger = log.With(util_log.Logger, "table-name", table.name)

	return &table, nil
}

func (t *table) compact(tableHasExpiredStreams bool) error {
	objects, err := util.ListDirectory(t.ctx, t.name, t.storageClient)
	if err != nil {
		return err
	}

	level.Info(t.logger).Log("msg", "listed files", "count", len(objects))

	defer func() {
		err := t.cleanup()
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to cleanup table")
		}
	}()

	applyRetention := t.applyRetention && tableHasExpiredStreams

	if !applyRetention {
		if len(objects) < compactMinDBs {
			level.Info(t.logger).Log("msg", fmt.Sprintf("skipping compaction since we have just %d files in storage", len(objects)))
			return nil
		}
		if err := t.compactFiles(objects); err != nil {
			return err
		}
		// upload the compacted db
		err = t.upload()
		if err != nil {
			return err
		}

		// remove source files from storage which were compacted
		err = t.removeObjectsFromStorage(objects)
		if err != nil {
			return err
		}
		return nil
	}

	var compacted bool
	if len(objects) > 1 {
		if err := t.compactFiles(objects); err != nil {
			return err
		}
		compacted = true
	}

	if len(objects) == 1 {
		// download the db
		downloadAt := filepath.Join(t.workingDirectory, fmt.Sprint(time.Now().Unix()))
		err = shipper_util.GetFileFromStorage(t.ctx, t.storageClient, objects[0].Key, downloadAt, false)
		if err != nil {
			return err
		}
		t.compactedDB, err = openBoltdbFileWithNoSync(downloadAt)
		if err != nil {
			return err
		}
	}

	if t.compactedDB == nil {
		level.Info(t.logger).Log("msg", "skipping compaction no files found.")
		return nil
	}

	empty, markCount, err := t.tableMarker.MarkForDelete(t.ctx, t.name, t.compactedDB)
	if err != nil {
		return err
	}

	if empty {
		return t.removeObjectsFromStorage(objects)
	}

	if markCount == 0 && !compacted {
		// we didn't make a modification so let's just return
		return nil
	}

	err = t.upload()
	if err != nil {
		return err
	}

	return t.removeObjectsFromStorage(objects)
}

func (t *table) compactFiles(objects []chunk.StorageObject) error {
	var err error
	level.Info(t.logger).Log("msg", "starting compaction of dbs")

	compactedDBName := filepath.Join(t.workingDirectory, fmt.Sprint(time.Now().Unix()))
	seedFileIdx, err := findSeedObjectIdx(objects)
	if err != nil {
		return err
	}

	level.Info(t.logger).Log("msg", fmt.Sprintf("using %s as seed file", objects[seedFileIdx].Key))

	err = shipper_util.GetFileFromStorage(t.ctx, t.storageClient, objects[seedFileIdx].Key, compactedDBName, false)
	if err != nil {
		return err
	}

	t.compactedDB, err = openBoltdbFileWithNoSync(compactedDBName)
	if err != nil {
		return err
	}

	errChan := make(chan error)
	readObjectChan := make(chan string)
	n := util_math.Min(len(objects), readDBsParallelism)

	// read files in parallel
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

					// The s3 client can also return the directory itself in the ListObjects.
					if shipper_util.IsDirectory(objectKey) {
						continue
					}

					var dbName string
					dbName, err = shipper_util.GetDBNameFromObjectKey(objectKey)
					if err != nil {
						return
					}

					downloadAt := filepath.Join(t.workingDirectory, dbName)

					err = shipper_util.GetFileFromStorage(t.ctx, t.storageClient, objectKey, downloadAt, false)
					if err != nil {
						return
					}

					err = t.readFile(downloadAt)
					if err != nil {
						level.Error(t.logger).Log("msg", fmt.Sprintf("error reading file %s", objectKey), "err", err)
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
		for i, object := range objects {
			// skip seed file
			if i == seedFileIdx {
				continue
			}
			select {
			case readObjectChan <- object.Key:
			case <-t.quit:
				break
			case <-t.ctx.Done():
				break
			}
		}

		level.Debug(t.logger).Log("msg", "closing readObjectChan")

		close(readObjectChan)
	}()

	var firstErr error

	// read all the errors
	for i := 0; i < n; i++ {
		err := <-errChan
		if err != nil && firstErr == nil {
			firstErr = err
			close(t.quit)
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

	level.Info(t.logger).Log("msg", "finished compacting the dbs")
	return nil
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
	level.Debug(t.logger).Log("msg", "reading file for compaction", "path", path)

	db, err := openBoltdbFileWithNoSync(path)
	if err != nil {
		return err
	}

	defer func() {
		if err := db.Close(); err != nil {
			level.Error(t.logger).Log("msg", "failed to close db", "path", path, "err", err)
		}

		if err = os.Remove(path); err != nil {
			level.Error(t.logger).Log("msg", "failed to remove file", "path", path, "err", err)
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
				// todo(cyriltovena) we should just re-slice to avoid allocations
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
	err = shipper_util.CompressFile(compactedDBPath, compressedDBPath, false)
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
			level.Error(t.logger).Log("msg", "failed to close file", "path", compactedDBPath, "err", err)
		}

		if err := os.Remove(compressedDBPath); err != nil {
			level.Error(t.logger).Log("msg", "failed to remove file", "path", compressedDBPath, "err", err)
		}
	}()

	objectKey := fmt.Sprintf("%s.gz", shipper_util.BuildObjectKey(t.name, uploaderName, fmt.Sprint(time.Now().Unix())))
	level.Info(t.logger).Log("msg", "uploading the compacted file", "objectKey", objectKey)

	return t.storageClient.PutObject(t.ctx, objectKey, compressedDB)
}

// removeObjectsFromStorage deletes objects from storage.
func (t *table) removeObjectsFromStorage(objects []chunk.StorageObject) error {
	level.Info(t.logger).Log("msg", "removing source db files from storage", "count", len(objects))

	for _, object := range objects {
		err := t.storageClient.DeleteObject(t.ctx, object.Key)
		if err != nil {
			return err
		}
	}

	return nil
}

// openBoltdbFileWithNoSync opens a boltdb file and configures it to not sync the file to disk.
// Compaction process is idempotent and we do not retain the files so there is no need to sync them to disk.
func openBoltdbFileWithNoSync(path string) (*bbolt.DB, error) {
	boltdb, err := shipper_util.SafeOpenBoltdbFile(path)
	if err != nil {
		return nil, err
	}

	// no need to enforce write to disk, we'll upload and delete the file anyway.
	boltdb.NoSync = true

	return boltdb, nil
}

// findSeedObjectIdx returns index of object to use as seed which would then get index from all the files written to.
// It tries to find previously compacted file(which has uploaderName) which would be the biggest file.
// In a large cluster, using previously compacted file as seed would significantly reduce compaction time.
// If it can't find a previously compacted file, it would just use the first file from the list of files.
func findSeedObjectIdx(objects []chunk.StorageObject) (int, error) {
	for i, object := range objects {
		dbName, err := shipper_util.GetDBNameFromObjectKey(object.Key)
		if err != nil {
			return 0, err
		}

		if strings.HasPrefix(dbName, uploaderName) {
			return i, nil
		}
	}

	return 0, nil
}
