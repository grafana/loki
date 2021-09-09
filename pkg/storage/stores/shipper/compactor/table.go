package compactor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	util_math "github.com/cortexproject/cortex/pkg/util/math"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"

	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
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
	name               string
	workingDirectory   string
	indexStorageClient storage.Client
	applyRetention     bool
	tableMarker        retention.TableMarker

	compactedDB *bbolt.DB
	logger      log.Logger

	ctx  context.Context
	quit chan struct{}
}

func newTable(ctx context.Context, workingDirectory string, indexStorageClient storage.Client, applyRetention bool, tableMarker retention.TableMarker) (*table, error) {
	err := chunk_util.EnsureDirectory(workingDirectory)
	if err != nil {
		return nil, err
	}

	table := table{
		ctx:                ctx,
		name:               filepath.Base(workingDirectory),
		workingDirectory:   workingDirectory,
		indexStorageClient: indexStorageClient,
		quit:               make(chan struct{}),
		applyRetention:     applyRetention,
		tableMarker:        tableMarker,
	}
	table.logger = log.With(util_log.Logger, "table-name", table.name)

	return &table, nil
}

func (t *table) compact(tableHasExpiredStreams bool) error {
	indexFiles, err := t.indexStorageClient.ListFiles(t.ctx, t.name)
	if err != nil {
		return err
	}

	level.Info(t.logger).Log("msg", "listed files", "count", len(indexFiles))

	defer func() {
		err := t.cleanup()
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to cleanup table")
		}
	}()

	applyRetention := t.applyRetention && tableHasExpiredStreams

	if !applyRetention {
		if len(indexFiles) < compactMinDBs {
			level.Info(t.logger).Log("msg", fmt.Sprintf("skipping compaction since we have just %d files in storage", len(indexFiles)))
			return nil
		}
		if err := t.compactFiles(indexFiles); err != nil {
			return err
		}
		// upload the compacted db
		err = t.upload()
		if err != nil {
			return err
		}

		// remove source files from storage which were compacted
		err = t.removeFilesFromStorage(indexFiles)
		if err != nil {
			return err
		}
		return nil
	}

	var compacted bool
	if len(indexFiles) > 1 {
		if err := t.compactFiles(indexFiles); err != nil {
			return err
		}
		compacted = true
	}

	if len(indexFiles) == 1 {
		// download the db
		downloadAt := filepath.Join(t.workingDirectory, fmt.Sprint(time.Now().Unix()))
		err = shipper_util.GetFileFromStorage(t.ctx, t.indexStorageClient, t.name, indexFiles[0].Name, downloadAt, false)
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
		return t.removeFilesFromStorage(indexFiles)
	}

	if markCount == 0 && !compacted {
		// we didn't make a modification so let's just return
		return nil
	}

	err = t.upload()
	if err != nil {
		return err
	}

	return t.removeFilesFromStorage(indexFiles)
}

func (t *table) compactFiles(files []storage.IndexFile) error {
	var err error
	level.Info(t.logger).Log("msg", "starting compaction of dbs")

	compactedDBName := filepath.Join(t.workingDirectory, fmt.Sprint(time.Now().Unix()))
	seedFileIdx := findSeedFileIdx(files)

	level.Info(t.logger).Log("msg", fmt.Sprintf("using %s as seed file", files[seedFileIdx].Name))

	err = shipper_util.GetFileFromStorage(t.ctx, t.indexStorageClient, t.name, files[seedFileIdx].Name, compactedDBName, false)
	if err != nil {
		return err
	}

	t.compactedDB, err = openBoltdbFileWithNoSync(compactedDBName)
	if err != nil {
		return err
	}

	errChan := make(chan error)
	readFileChan := make(chan string)
	n := util_math.Min(len(files), readDBsParallelism)

	// read files in parallel
	for i := 0; i < n; i++ {
		go func() {
			var err error
			defer func() {
				errChan <- err
			}()

			for {
				select {
				case fileName, ok := <-readFileChan:
					if !ok {
						return
					}

					downloadAt := filepath.Join(t.workingDirectory, fileName)

					err = shipper_util.GetFileFromStorage(t.ctx, t.indexStorageClient, t.name, fileName, downloadAt, false)
					if err != nil {
						return
					}

					err = t.readFile(downloadAt)
					if err != nil {
						level.Error(t.logger).Log("msg", fmt.Sprintf("error reading file %s", fileName), "err", err)
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

	// send all files to readFileChan
	go func() {
		for i, file := range files {
			// skip seed file
			if i == seedFileIdx {
				continue
			}
			select {
			case readFileChan <- file.Name:
			case <-t.quit:
				break
			case <-t.ctx.Done():
				break
			}
		}

		level.Debug(t.logger).Log("msg", "closing readFileChan")

		close(readFileChan)
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

	fileName := fmt.Sprintf("%s.gz", shipper_util.BuildIndexFileName(t.name, uploaderName, fmt.Sprint(time.Now().Unix())))
	level.Info(t.logger).Log("msg", "uploading the compacted file", "fileName", fileName)

	return t.indexStorageClient.PutFile(t.ctx, t.name, fileName, compressedDB)
}

// removeFilesFromStorage deletes index files from storage.
func (t *table) removeFilesFromStorage(files []storage.IndexFile) error {
	level.Info(t.logger).Log("msg", "removing source db files from storage", "count", len(files))

	for _, file := range files {
		err := t.indexStorageClient.DeleteFile(t.ctx, t.name, file.Name)
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

// findSeedFileIdx returns index of file to use as seed which would then get index from all the files written to.
// It tries to find previously compacted file(which has uploaderName) which would be the biggest file.
// In a large cluster, using previously compacted file as seed would significantly reduce compaction time.
// If it can't find a previously compacted file, it would just use the first file from the list of files.
func findSeedFileIdx(files []storage.IndexFile) int {
	for i, file := range files {
		if strings.HasPrefix(file.Name, uploaderName) {
			return i
		}
	}

	return 0
}
