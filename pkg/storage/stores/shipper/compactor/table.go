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
	util_math "github.com/grafana/loki/pkg/util/math"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"go.etcd.io/bbolt"

	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

const (
	uploaderName = "compactor"

	readDBsParallelism = 50
	batchSize          = 1000

	// we want to recreate compactedDB when the chances of it changing due to compaction or deletion of data are low.
	// this is to avoid recreation of the DB too often which would be too costly in a large cluster.
	recreateCompactedDBOlderThan = 12 * time.Hour
	dropFreePagesTxMaxSize       = 100 * 1024 * 1024 // 100MB
	recreatedCompactedDBSuffix   = ".r.gz"
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

	sourceFiles          []storage.IndexFile
	compactedDB          *bbolt.DB
	compactedDBRecreated bool
	uploadCompactedDB    bool
	removeSourceFiles    bool
	logger               log.Logger

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

	if len(indexFiles) == 0 {
		level.Info(t.logger).Log("msg", "no index files found")
		return nil
	}

	t.sourceFiles = indexFiles
	level.Info(t.logger).Log("msg", "listed files", "count", len(indexFiles))

	defer func() {
		err := t.cleanup()
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to cleanup table")
		}
	}()

	applyRetention := t.applyRetention && tableHasExpiredStreams

	if len(indexFiles) > 1 {
		if err := t.compactFiles(indexFiles); err != nil {
			return err
		}

		// we have compacted the files to a single file so let use upload the compacted db and remove the source files.
		t.uploadCompactedDB = true
		t.removeSourceFiles = true
	} else if !applyRetention && !t.mustRecreateCompactedDB() {
		return nil
	} else {
		// download the db for applying retention or recreating the compacted db
		downloadAt := filepath.Join(t.workingDirectory, indexFiles[0].Name)
		err = shipper_util.GetFileFromStorage(t.ctx, t.indexStorageClient, t.name, indexFiles[0].Name, downloadAt, false)
		if err != nil {
			return err
		}
		t.compactedDB, err = openBoltdbFileWithNoSync(downloadAt)
		if err != nil {
			return err
		}
	}

	if applyRetention {
		empty, modified, err := t.tableMarker.MarkForDelete(t.ctx, t.name, t.compactedDB)
		if err != nil {
			return err
		}

		if empty {
			// we have deleted all the data so we can remove the source files without uploading the compacted db
			t.removeSourceFiles = true
			t.uploadCompactedDB = false
		} else if modified {
			// we have modified the compacted db so we need to upload the compacted db and remove the source file(s)
			t.uploadCompactedDB = true
			t.removeSourceFiles = true
		}
	}

	// file was not modified so see if we must recreate the compacted db to optimize storage usage
	if !t.uploadCompactedDB && !t.removeSourceFiles && t.mustRecreateCompactedDB() {
		err := t.recreateCompactedDB()
		if err != nil {
			return err
		}

		// we have recreated the compacted db so we need to upload the compacted db and remove the source file
		t.uploadCompactedDB = true
		t.removeSourceFiles = true
		t.compactedDBRecreated = true
	}

	return t.done()
}

// done takes care of uploading the files and cleaning up the working directory based on the value in uploadCompactedDB and removeSourceFiles
func (t *table) done() error {
	if t.uploadCompactedDB {
		err := t.upload()
		if err != nil {
			return err
		}
	}

	if t.removeSourceFiles {
		err := t.removeSourceFilesFromStorage()
		if err != nil {
			return err
		}
	}

	return nil
}

// mustRecreateCompactedDB returns true if the compacted db should be recreated
func (t *table) mustRecreateCompactedDB() bool {
	if len(t.sourceFiles) != 1 {
		// do not recreate if there are multiple source files
		return false
	} else if time.Since(t.sourceFiles[0].ModifiedAt) < recreateCompactedDBOlderThan {
		// do not recreate if the source file is younger than the threshold
		return false
	}

	// recreate the compacted db only if we have not recreated it before
	return !strings.HasSuffix(t.sourceFiles[0].Name, recreatedCompactedDBSuffix)
}

// recreateCompactedDB just copies the old db to the new one using bbolt.Compact for following reasons:
// 1. When files are deleted, boltdb leaves free pages in the file. The only way to drop those free pages is to re-create the file.
//    See https://github.com/boltdb/bolt/issues/308 for more details.
// 2. boltdb by default fills only about 50% of the page in the file. See https://github.com/etcd-io/bbolt/blob/master/bucket.go#L26.
//    This setting is optimal for unordered writes.
//    bbolt.Compact fills the whole page by setting FillPercent to 1 which works well here since while copying the data, it receives the index entries in order.
//    The storage space goes down from anywhere between 25% to 50% as per my(Sandeep) tests.
func (t *table) recreateCompactedDB() error {
	destDB, err := openBoltdbFileWithNoSync(filepath.Join(t.workingDirectory, fmt.Sprint(time.Now().Unix())))
	if err != nil {
		return err
	}

	level.Info(t.logger).Log("msg", "recreating compacted db")

	err = bbolt.Compact(destDB, t.compactedDB, dropFreePagesTxMaxSize)
	if err != nil {
		return err
	}

	sourceSize := int64(0)
	destSize := int64(0)

	if err := t.compactedDB.View(func(tx *bbolt.Tx) error {
		sourceSize = tx.Size()
		return nil
	}); err != nil {
		return err
	}

	if err := destDB.View(func(tx *bbolt.Tx) error {
		destSize = tx.Size()
		return nil
	}); err != nil {
		return err
	}

	level.Info(t.logger).Log("msg", "recreated compacted db", "src_size_bytes", sourceSize, "dest_size_bytes", destSize)

	err = t.compactedDB.Close()
	if err != nil {
		return err
	}

	t.compactedDB = destDB
	return nil
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

	fileNameFormat := "%s.gz"
	if t.compactedDBRecreated {
		fileNameFormat = "%s" + recreatedCompactedDBSuffix
	}
	fileName := fmt.Sprintf(fileNameFormat, shipper_util.BuildIndexFileName(t.name, uploaderName, fmt.Sprint(time.Now().Unix())))
	level.Info(t.logger).Log("msg", "uploading the compacted file", "fileName", fileName)

	return t.indexStorageClient.PutFile(t.ctx, t.name, fileName, compressedDB)
}

// removeSourceFilesFromStorage deletes source db files from storage.
func (t *table) removeSourceFilesFromStorage() error {
	level.Info(t.logger).Log("msg", "removing source db files from storage", "count", len(t.sourceFiles))

	for _, file := range t.sourceFiles {
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
