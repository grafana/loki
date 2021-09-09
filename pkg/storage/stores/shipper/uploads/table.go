package uploads

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	// create a new db sharded by time based on when write request is received
	ShardDBsByDuration = 15 * time.Minute

	// a temp file is created during uploads with name of the db + tempFileSuffix
	tempFileSuffix = ".temp"

	// a snapshot file is created with name of the db + snapshotFileSuffix periodically for read operation.
	snapshotFileSuffix = ".snapshot"
)

var bucketName = []byte("index")

type BoltDBIndexClient interface {
	QueryWithCursor(_ context.Context, c *bbolt.Cursor, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error
	WriteToDB(ctx context.Context, db *bbolt.DB, writes local.TableWrites) error
}

type StorageClient interface {
	PutFile(ctx context.Context, tableName, fileName string, file io.ReadSeeker) error
}

type dbSnapshot struct {
	boltdb      *bbolt.DB
	writesCount int
}

// Table is a collection of multiple dbs created for a same table by the ingester.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type Table struct {
	name              string
	path              string
	uploader          string
	storageClient     StorageClient
	boltdbIndexClient BoltDBIndexClient

	dbs    map[string]*bbolt.DB
	dbsMtx sync.RWMutex

	dbSnapshots    map[string]*dbSnapshot
	dbSnapshotsMtx sync.RWMutex

	modifyShardsSince int64
	dbUploadTime      map[string]time.Time
	dbUploadTimeMtx   sync.RWMutex
}

// NewTable create a new Table without looking for any existing local dbs belonging to the table.
func NewTable(path, uploader string, storageClient StorageClient, boltdbIndexClient BoltDBIndexClient) (*Table, error) {
	err := chunk_util.EnsureDirectory(path)
	if err != nil {
		return nil, err
	}

	return newTableWithDBs(map[string]*bbolt.DB{}, path, uploader, storageClient, boltdbIndexClient)
}

// LoadTable loads local dbs belonging to the table and creates a new Table with references to dbs if there are any otherwise it doesn't create a table
func LoadTable(path, uploader string, storageClient StorageClient, boltdbIndexClient BoltDBIndexClient, metrics *metrics) (*Table, error) {
	dbs, err := loadBoltDBsFromDir(path, metrics)
	if err != nil {
		return nil, err
	}

	if len(dbs) == 0 {
		return nil, nil
	}

	return newTableWithDBs(dbs, path, uploader, storageClient, boltdbIndexClient)
}

func newTableWithDBs(dbs map[string]*bbolt.DB, path, uploader string, storageClient StorageClient, boltdbIndexClient BoltDBIndexClient) (*Table, error) {
	return &Table{
		name:              filepath.Base(path),
		path:              path,
		uploader:          uploader,
		storageClient:     storageClient,
		boltdbIndexClient: boltdbIndexClient,
		dbs:               dbs,
		dbSnapshots:       map[string]*dbSnapshot{},
		dbUploadTime:      map[string]time.Time{},
		modifyShardsSince: time.Now().Unix(),
	}, nil
}

func (lt *Table) Snapshot() error {
	lt.dbsMtx.RLock()
	defer lt.dbsMtx.RUnlock()

	lt.dbSnapshotsMtx.Lock()
	defer lt.dbSnapshotsMtx.Unlock()

	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("snapshotting table %s", lt.name))

	for name, db := range lt.dbs {
		level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("checking db %s for snapshot", name))
		srcWriteCount := 0
		err := db.View(func(tx *bbolt.Tx) error {
			srcWriteCount = db.Stats().TxStats.Write
			return nil
		})
		if err != nil {
			return err
		}

		snapshot, ok := lt.dbSnapshots[name]
		filePath := path.Join(lt.path, fmt.Sprintf("%s%s", name, snapshotFileSuffix))

		if !ok {
			snapshot = &dbSnapshot{}
		} else if snapshot.writesCount == srcWriteCount {
			continue
		} else {
			if err := snapshot.boltdb.Close(); err != nil {
				return err
			}

			if err := os.Remove(filePath); err != nil {
				return err
			}
		}

		f, err := os.Create(filePath)
		if err != nil {
			return err
		}

		err = db.View(func(tx *bbolt.Tx) (err error) {
			_, err = tx.WriteTo(f)
			return
		})
		if err != nil {
			return err
		}

		// flush the file to disk.
		if err := f.Sync(); err != nil {
			return err
		}

		if err := f.Close(); err != nil {
			return err
		}

		snapshot.boltdb, err = shipper_util.SafeOpenBoltdbFile(filePath)
		if err != nil {
			return err
		}

		snapshot.writesCount = srcWriteCount
		lt.dbSnapshots[name] = snapshot

		level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("finished snaphotting db %s", name))
	}

	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("finished snapshotting table %s", lt.name))

	return nil
}

// MultiQueries runs multiple queries without having to take lock multiple times for each query.
func (lt *Table) MultiQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk_util.Callback) error {
	lt.dbSnapshotsMtx.RLock()
	defer lt.dbSnapshotsMtx.RUnlock()

	for _, db := range lt.dbSnapshots {
		err := db.boltdb.View(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket(bucketName)
			if bucket == nil {
				return nil
			}

			for _, query := range queries {
				if err := lt.boltdbIndexClient.QueryWithCursor(ctx, bucket.Cursor(), query, callback); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (lt *Table) getOrAddDB(name string) (*bbolt.DB, error) {
	lt.dbsMtx.Lock()
	defer lt.dbsMtx.Unlock()

	var (
		db  *bbolt.DB
		err error
		ok  bool
	)

	db, ok = lt.dbs[name]
	if !ok {
		db, err = shipper_util.SafeOpenBoltdbFile(filepath.Join(lt.path, name))
		if err != nil {
			return nil, err
		}

		lt.dbs[name] = db
		return db, nil
	}

	return db, nil
}

// Write writes to a db locally with write time set to now.
func (lt *Table) Write(ctx context.Context, writes local.TableWrites) error {
	return lt.write(ctx, time.Now(), writes)
}

// write writes to a db locally. It shards the db files by truncating the passed time by ShardDBsByDuration using https://golang.org/pkg/time/#Time.Truncate
// db files are named after the time shard i.e epoch of the truncated time.
// If a db file does not exist for a shard it gets created.
func (lt *Table) write(ctx context.Context, tm time.Time, writes local.TableWrites) error {
	// do not write to files older than init time otherwise we might endup modifying file which was already created and uploaded before last shutdown.
	shard := tm.Truncate(ShardDBsByDuration).Unix()
	if shard < lt.modifyShardsSince {
		shard = lt.modifyShardsSince
	}

	db, err := lt.getOrAddDB(fmt.Sprint(shard))
	if err != nil {
		return err
	}

	return lt.boltdbIndexClient.WriteToDB(ctx, db, writes)
}

// Stop closes all the open dbs.
func (lt *Table) Stop() {
	lt.dbsMtx.Lock()
	defer lt.dbsMtx.Unlock()

	for name, db := range lt.dbs {
		if err := db.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Errorf("failed to close file %s for table %s", name, lt.name))
		}
	}

	lt.dbs = map[string]*bbolt.DB{}
}

// RemoveDB closes the db and removes the file locally.
func (lt *Table) RemoveDB(name string) error {
	lt.dbsMtx.Lock()
	defer lt.dbsMtx.Unlock()

	db, ok := lt.dbs[name]
	if !ok {
		return nil
	}

	err := db.Close()
	if err != nil {
		return err
	}

	delete(lt.dbs, name)

	lt.dbUploadTimeMtx.Lock()
	delete(lt.dbUploadTime, name)
	lt.dbUploadTimeMtx.Unlock()

	return os.Remove(filepath.Join(lt.path, name))
}

func (lt *Table) RemoveSnapshotDB(name string) error {
	lt.dbSnapshotsMtx.Lock()
	defer lt.dbSnapshotsMtx.Unlock()

	db, ok := lt.dbSnapshots[name]
	if !ok {
		return nil
	}

	err := db.boltdb.Close()
	if err != nil {
		return err
	}

	delete(lt.dbSnapshots, name)

	return os.Remove(filepath.Join(lt.path, fmt.Sprintf("%s%s", name, snapshotFileSuffix)))
}

// Upload uploads all the dbs which are never uploaded or have been modified since the last batch was uploaded.
func (lt *Table) Upload(ctx context.Context, force bool) error {
	lt.dbsMtx.RLock()
	defer lt.dbsMtx.RUnlock()

	uploadShardsBefore := fmt.Sprint(getOldestActiveShardTime().Unix())

	// Adding check for considering only files which are sharded and have just an epoch in their name.
	// Before introducing sharding we had a single file per table which were were moved inside the folder per table as part of migration.
	// The files were named with <table_prefix><period>.
	// Since sharding was introduced we have a new file every 15 mins and their names just include an epoch timestamp, for e.g `1597927538`.
	// We can remove this check after we no longer support upgrading from 1.5.0.
	filenameWithEpochRe, err := regexp.Compile(`^[0-9]{10}$`)
	if err != nil {
		return err
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("uploading table %s", lt.name))

	for name, db := range lt.dbs {
		// doing string comparison between unix timestamps in string form since they are anyways of same length
		if !force && filenameWithEpochRe.MatchString(name) && name >= uploadShardsBefore {
			continue
		}

		// if the file is uploaded already do not upload it again.
		lt.dbUploadTimeMtx.RLock()
		_, ok := lt.dbUploadTime[name]
		lt.dbUploadTimeMtx.RUnlock()

		if ok {
			continue
		}

		err = lt.uploadDB(ctx, name, db)
		if err != nil {
			return err
		}

		lt.dbUploadTimeMtx.Lock()
		lt.dbUploadTime[name] = time.Now()
		lt.dbUploadTimeMtx.Unlock()
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("finished uploading table %s", lt.name))

	return nil
}

func (lt *Table) uploadDB(ctx context.Context, name string, db *bbolt.DB) error {
	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("uploading db %s from table %s", name, lt.name))

	filePath := path.Join(lt.path, fmt.Sprintf("%s%s", name, tempFileSuffix))
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close temp file", "path", filePath, "err", err)
		}

		if err := os.Remove(filePath); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to remove temp file", "path", filePath, "err", err)
		}
	}()

	err = db.View(func(tx *bbolt.Tx) (err error) {
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

	fileName := lt.buildFileName(name)
	return lt.storageClient.PutFile(ctx, lt.name, fileName, f)
}

// Cleanup removes dbs which are already uploaded and have not been modified for period longer than dbRetainPeriod.
// This is to avoid keeping all the files forever in the ingesters.
func (lt *Table) Cleanup(dbRetainPeriod time.Duration) error {
	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("cleaning up unwanted dbs from table %s", lt.name))

	var filesToCleanup []string
	cutoffTime := time.Now().Add(-dbRetainPeriod)

	lt.dbsMtx.RLock()

	for name := range lt.dbs {
		lt.dbUploadTimeMtx.RLock()
		dbUploadTime, ok := lt.dbUploadTime[name]
		lt.dbUploadTimeMtx.RUnlock()

		// consider files which are already uploaded and have mod time before cutoff time to retain files.
		if ok && dbUploadTime.Before(cutoffTime) {
			filesToCleanup = append(filesToCleanup, name)
		}
	}

	lt.dbsMtx.RUnlock()

	for i := range filesToCleanup {
		level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("removing db %s from table %s", filesToCleanup[i], lt.name))

		if err := lt.RemoveDB(filesToCleanup[i]); err != nil {
			return err
		}

		if err := lt.RemoveSnapshotDB(filesToCleanup[i]); err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to remove snapshot db %s", filesToCleanup[i]))
		}
	}

	return nil
}

func (lt *Table) buildFileName(dbName string) string {
	// Files are stored with <uploader>-<db-name>
	fileName := fmt.Sprintf("%s-%s", lt.uploader, dbName)

	// if the file is a migrated one then don't add its name to the object key otherwise we would re-upload them again here with a different name.
	if lt.name == dbName {
		fileName = lt.uploader
	}

	return fmt.Sprintf("%s.gz", fileName)
}

func loadBoltDBsFromDir(dir string, metrics *metrics) (map[string]*bbolt.DB, error) {
	dbs := map[string]*bbolt.DB{}
	filesInfo, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, fileInfo := range filesInfo {
		if fileInfo.IsDir() {
			continue
		}
		fullPath := filepath.Join(dir, fileInfo.Name())

		if strings.HasSuffix(fileInfo.Name(), tempFileSuffix) || strings.HasSuffix(fileInfo.Name(), snapshotFileSuffix) {
			// If an ingester is killed abruptly in the middle of an upload operation it could leave out a temp file which holds the snapshot of db for uploading.
			// Cleaning up those temp files to avoid problems.
			if err := os.Remove(fullPath); err != nil {
				level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to remove temp file %s", fullPath), "err", err)
			}
			continue
		}

		db, err := shipper_util.SafeOpenBoltdbFile(fullPath)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to open file %s. Please fix or remove this file.", fullPath), "err", err)
			metrics.openExistingFileFailuresTotal.Inc()
			continue
		}

		hasBucket := false
		_ = db.View(func(tx *bbolt.Tx) error {
			hasBucket = tx.Bucket(bucketName) != nil
			return nil
		})

		if !hasBucket {
			level.Info(util_log.Logger).Log("msg", fmt.Sprintf("file %s has no bucket named %s, so removing it", fullPath, bucketName))
			_ = db.Close()
			if err := os.Remove(fullPath); err != nil {
				level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to remove file %s without bucket", fullPath), "err", err)
			}
			continue
		}

		dbs[fileInfo.Name()] = db
	}

	return dbs, nil
}

// getOldestActiveShardTime returns the time of oldest active shard with a buffer of 1 minute.
func getOldestActiveShardTime() time.Time {
	// upload files excluding active shard. It could so happen that we just started a new shard but the file for last shard is still being updated due to pending writes or pending flush to disk.
	// To avoid uploading it, excluding previous active shard as well if it has been not more than a minute since it became inactive.
	return time.Now().Add(-time.Minute).Truncate(ShardDBsByDuration)
}
