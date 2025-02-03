package boltdb

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	chunk_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	shipper_util "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	// create a new db sharded by time based on when write request is received
	ShardDBsByDuration = 15 * time.Minute

	// a snapshot file is created with name of the db + snapshotFileSuffix periodically for read operation.
	snapshotFileSuffix = ".snapshot"
)

// nolint:revive
type BoltDBIndexClient interface {
	QueryWithCursor(_ context.Context, c *bbolt.Cursor, query index.Query, callback index.QueryPagesCallback) error
	WriteToDB(ctx context.Context, db *bbolt.DB, bucketName []byte, writes local.TableWrites) error
}

type dbSnapshot struct {
	boltdb      *bbolt.DB
	writesCount int64
}

// Table is a collection of multiple index files created for a same table by the ingester.
// It is used on the write path for writing the index.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type Table struct {
	name                 string
	path                 string
	uploader             string
	indexShipper         Shipper
	makePerTenantBuckets bool

	dbs    map[string]*bbolt.DB
	dbsMtx sync.RWMutex

	dbSnapshots    map[string]*dbSnapshot
	dbSnapshotsMtx sync.RWMutex

	modifyShardsSince int64
}

// NewTable create a new Table without looking for any existing local dbs belonging to the table.
func NewTable(path, uploader string, indexShipper Shipper, makePerTenantBuckets bool) (*Table, error) {
	err := chunk_util.EnsureDirectory(path)
	if err != nil {
		return nil, err
	}

	return newTableWithDBs(map[string]*bbolt.DB{}, path, uploader, indexShipper, makePerTenantBuckets)
}

// LoadTable loads local dbs belonging to the table and creates a new Table with references to dbs if there are any otherwise it doesn't create a table
func LoadTable(path, uploader string, indexShipper Shipper, makePerTenantBuckets bool, metrics *tableManagerMetrics) (*Table, error) {
	dbs, err := loadBoltDBsFromDir(path, metrics)
	if err != nil {
		return nil, err
	}

	if len(dbs) == 0 {
		return nil, nil
	}

	return newTableWithDBs(dbs, path, uploader, indexShipper, makePerTenantBuckets)
}

func newTableWithDBs(dbs map[string]*bbolt.DB, path, uploader string, indexShipper Shipper, makePerTenantBuckets bool) (*Table, error) {
	return &Table{
		name:                 filepath.Base(path),
		path:                 path,
		uploader:             uploader,
		indexShipper:         indexShipper,
		dbs:                  dbs,
		dbSnapshots:          map[string]*dbSnapshot{},
		modifyShardsSince:    time.Now().Unix(),
		makePerTenantBuckets: makePerTenantBuckets,
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
		srcWriteCount := int64(0)
		err := db.View(func(_ *bbolt.Tx) error {
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

func (lt *Table) ForEach(_ context.Context, callback func(b *bbolt.DB) error) error {
	lt.dbSnapshotsMtx.RLock()
	defer lt.dbSnapshotsMtx.RUnlock()

	for _, db := range lt.dbSnapshots {
		if err := callback(db.boltdb); err != nil {
			return err
		}
	}

	return nil
}

func (lt *Table) getOrAddDB(name string) (*bbolt.DB, error) {
	lt.dbsMtx.RLock()
	db, ok := lt.dbs[name]
	lt.dbsMtx.RUnlock()

	if ok {
		return db, nil
	}

	lt.dbsMtx.Lock()
	defer lt.dbsMtx.Unlock()

	db, ok = lt.dbs[name]
	if ok {
		return db, nil
	}

	var err error
	db, err = shipper_util.SafeOpenBoltdbFile(filepath.Join(lt.path, name))
	if err != nil {
		return nil, err
	}

	lt.dbs[name] = db

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
	writeToBucket := local.IndexBucketName
	if lt.makePerTenantBuckets {
		userID, err := tenant.TenantID(ctx)
		if err != nil {
			return err
		}

		writeToBucket = []byte(userID)
	}

	// do not write to files older than init time otherwise we might endup modifying file which was already created and uploaded before last shutdown.
	shard := tm.Truncate(ShardDBsByDuration).Unix()
	if shard < lt.modifyShardsSince {
		shard = lt.modifyShardsSince
	}

	db, err := lt.getOrAddDB(fmt.Sprint(shard))
	if err != nil {
		return err
	}

	return local.WriteToDB(ctx, db, writeToBucket, writes)
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

func (lt *Table) removeSnapshotDB(name string) error {
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

// HandoverIndexesToShipper hands over the inactive dbs to shipper for uploading
func (lt *Table) HandoverIndexesToShipper(force bool) error {
	indexesHandedOverToShipper, err := lt.handoverIndexesToShipper(force)
	if err != nil {
		return err
	}

	lt.dbsMtx.Lock()
	defer lt.dbsMtx.Unlock()

	for _, name := range indexesHandedOverToShipper {
		delete(lt.dbs, name)
		if err := lt.removeSnapshotDB(name); err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to remove snapshot db %s", name))
		}
	}

	return nil
}

func (lt *Table) handoverIndexesToShipper(force bool) ([]string, error) {
	lt.dbsMtx.RLock()
	defer lt.dbsMtx.RUnlock()

	handoverShardsBefore := fmt.Sprint(getOldestActiveShardTime().Unix())

	// Adding check for considering only files which are sharded and have just an epoch in their name.
	// Before introducing sharding we had a single file per table which were moved inside the folder per table as part of migration.
	// The files were named with <table_prefix><period>.
	// Since sharding was introduced we have a new file every 15 mins and their names just include an epoch timestamp, for e.g `1597927538`.
	// We can remove this check after we no longer support upgrading from 1.5.0.
	filenameWithEpochRe, err := regexp.Compile(`^[0-9]{10}$`)
	if err != nil {
		return nil, err
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("handing over indexes to shipper %s", lt.name))

	var indexesHandedOverToShipper []string
	for name, db := range lt.dbs {
		// doing string comparison between unix timestamps in string form since they are anyways of same length
		if !force && filenameWithEpochRe.MatchString(name) && name >= handoverShardsBefore {
			continue
		}

		err = lt.indexShipper.AddIndex(lt.name, "", BoltDBToIndexFile(db, lt.buildFileName(name)))
		if err != nil {
			return nil, err
		}
		indexesHandedOverToShipper = append(indexesHandedOverToShipper, name)
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("finished handing over table %s", lt.name))

	return indexesHandedOverToShipper, nil
}

func (lt *Table) buildFileName(dbName string) string {
	// Files are stored with <uploader>-<db-name>
	fileName := fmt.Sprintf("%s-%s", lt.uploader, dbName)

	// if the file is a migrated one then don't add its name to the object key otherwise we would re-upload them again here with a different name.
	if lt.name == dbName {
		fileName = lt.uploader
	}

	return fileName
}

func loadBoltDBsFromDir(dir string, metrics *tableManagerMetrics) (map[string]*bbolt.DB, error) {
	dbs := map[string]*bbolt.DB{}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(dir, entry.Name())

		if strings.HasSuffix(entry.Name(), TempFileSuffix) || strings.HasSuffix(entry.Name(), snapshotFileSuffix) {
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
			return tx.ForEach(func(_ []byte, _ *bbolt.Bucket) error {
				hasBucket = true
				return nil
			})
		})

		if !hasBucket {
			level.Info(util_log.Logger).Log("msg", fmt.Sprintf("file %s has no buckets, so removing it", fullPath))
			_ = db.Close()
			if err := os.Remove(fullPath); err != nil {
				level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to remove file %s without any buckets", fullPath), "err", err)
			}
			continue
		}

		dbs[entry.Name()] = db
	}

	return dbs, nil
}

// getOldestActiveShardTime returns the time of oldest active shard with a buffer of 1 minute.
func getOldestActiveShardTime() time.Time {
	// upload files excluding active shard. It could so happen that we just started a new shard but the file for last shard is still being updated due to pending writes or pending flush to disk.
	// To avoid uploading it, excluding previous active shard as well if it has been not more than a minute since it became inactive.
	return time.Now().Add(-time.Minute).Truncate(ShardDBsByDuration)
}
