package uploads

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	segment "github.com/blugelabs/bluge_segment_api"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bluge_db"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
)

const (
	// create a new db sharded by time based on when write request is received
	shardDBsByDuration = 15 * time.Minute

	// retain dbs for specified duration after they are modified to avoid keeping them locally forever.
	// this period should be big enough than shardDBsByDuration to avoid any conflicts
	dbRetainPeriod = time.Hour

	// a snapshot file is created during uploads with name of the db + snapshotFileSuffix
	snapshotFileSuffix = ".temp"
)

type BlugeDBIndexClient interface {
	QueryDB(ctx context.Context, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error
	WriteToDB(ctx context.Context, writes bluge_db.TableWrites) error
}

type StorageClient interface {
	PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error
}

// Table is a collection of multiple dbs created for a same table by the ingester.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type Table struct {
	name          string
	path          string
	uploader      string
	storageClient StorageClient

	dbs    map[string]*bluge_db.BlugeDB
	dbsMtx sync.RWMutex

	uploadedDBsMtime    map[string]time.Time
	uploadedDBsMtimeMtx sync.RWMutex
	modifyShardsSince   int64
}

// NewTable create a new Table without looking for any existing local dbs belonging to the table.
func NewTable(path, uploader string, storageClient StorageClient) (*Table, error) {
	err := chunk_util.EnsureDirectory(path)
	if err != nil {
		return nil, err
	}

	return newTableWithDBs(map[string]*bluge_db.BlugeDB{}, path, uploader, storageClient)
}

// LoadTable loads local dbs belonging to the table and creates a new Table with references to dbs if there are any otherwise it doesn't create a table
func LoadTable(path, uploader string, storageClient StorageClient) (*Table, error) {
	dbs, err := loadBlugeDBsFromDir(path)
	if err != nil {
		return nil, err
	}

	if len(dbs) == 0 {
		return nil, nil
	}

	return newTableWithDBs(dbs, path, uploader, storageClient)
}

func newTableWithDBs(dbs map[string]*bluge_db.BlugeDB, path, uploader string, storageClient StorageClient) (*Table, error) {
	return &Table{
		name:              filepath.Base(path),
		path:              path,
		uploader:          uploader,
		storageClient:     storageClient,
		dbs:               dbs,
		uploadedDBsMtime:  map[string]time.Time{},
		modifyShardsSince: time.Now().Unix(),
	}, nil
}

// MultiQueries runs multiple queries without having to take lock multiple times for each query.
func (lt *Table) MultiQueries(ctx context.Context, queries []bluge_db.IndexQuery, callback segment.StoredFieldVisitor) error {
	lt.dbsMtx.RLock()
	defer lt.dbsMtx.RUnlock()

	for _, db := range lt.dbs {
		for _, query := range queries {
			if err := db.QueryDB(ctx, query, callback); err != nil {
				return err
			}
		}
	}

	return nil
}

func (lt *Table) getOrAddDB(name string) (*bluge_db.BlugeDB, error) {
	lt.dbsMtx.Lock()
	defer lt.dbsMtx.Unlock()

	var (
		db  *bluge_db.BlugeDB
		err error
		ok  bool
	)

	db, ok = lt.dbs[name]
	if !ok {
		db = bluge_db.NewDB(name, lt.path)

		// create index
		if err != nil {
			return nil, err
		}

		lt.dbs[name] = db
		return db, nil
	}

	return db, nil
}

// Write writes to a db locally with write time set to now.
func (lt *Table) Write(ctx context.Context, writes bluge_db.TableWrites) error {
	return lt.write(ctx, time.Now(), writes)
}

// write writes to a db locally. It shards the db files by truncating the passed time by shardDBsByDuration using https://golang.org/pkg/time/#Time.Truncate
// db files are named after the time shard i.e epoch of the truncated time.
// If a db file does not exist for a shard it gets created.
func (lt *Table) write(ctx context.Context, tm time.Time, writes bluge_db.TableWrites) error {
	// do not write to files older than init time otherwise we might endup modifying file which was already created and uploaded before last shutdown.
	shard := tm.Truncate(shardDBsByDuration).Unix()
	if shard < lt.modifyShardsSince {
		shard = lt.modifyShardsSince
	}

	db, err := lt.getOrAddDB(fmt.Sprint(shard))
	if err != nil {
		return err
	}

	return db.WriteToDB(ctx, writes)
}

// Stop closes all the open dbs.
func (lt *Table) Stop() {
	lt.dbsMtx.Lock()
	defer lt.dbsMtx.Unlock()

	for name, db := range lt.dbs {
		if err := db.Close(); err != nil {
			level.Error(util.Logger).Log("msg", fmt.Errorf("failed to close file %s for table %s", name, lt.name))
		}
	}

	lt.dbs = map[string]*bluge_db.BlugeDB{}
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

	return os.Remove(filepath.Join(lt.path, name))
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

	level.Info(util.Logger).Log("msg", fmt.Sprintf("uploading table %s", lt.name))

	for name, db := range lt.dbs {
		// doing string comparison between unix timestamps in string form since they are anyways of same length
		if !force && filenameWithEpochRe.MatchString(name) && name >= uploadShardsBefore {
			continue
		}
		stat, err := os.Stat(db.Path())
		if err != nil {
			return err
		}

		lt.uploadedDBsMtimeMtx.RLock()
		uploadedDBMtime, ok := lt.uploadedDBsMtime[name]
		lt.uploadedDBsMtimeMtx.RUnlock()

		if ok && !uploadedDBMtime.Before(stat.ModTime()) {
			continue
		}

		err = lt.uploadDB(ctx, name, db)
		if err != nil {
			return err
		}

		lt.uploadedDBsMtimeMtx.Lock()
		lt.uploadedDBsMtime[name] = stat.ModTime()
		lt.uploadedDBsMtimeMtx.Unlock()
	}

	level.Info(util.Logger).Log("msg", fmt.Sprintf("finished uploading table %s", lt.name))

	return nil
}

// 将数据压缩打包 发往objectstore服务器。
func (lt *Table) uploadDB(ctx context.Context, name string, db *bluge_db.BlugeDB) error {
	level.Debug(util.Logger).Log("msg", fmt.Sprintf("uploading db %s from table %s", name, lt.name))
	var buf bytes.Buffer
	err := lt.compress(db.Path(), &buf)
	// write the .tar.gzip
	fullPath := db.Folder + "/" + db.Name + ".tar.gzip"
	fileToWrite, err := os.OpenFile(fullPath, os.O_CREATE|os.O_RDWR, os.FileMode(600))
	defer func() {
		if err := fileToWrite.Close(); err != nil {
			level.Error(util.Logger).Log("msg", "failed to close temp file", "path", fullPath, "err", err)
		}

		if err := os.Remove(fullPath); err != nil {
			level.Error(util.Logger).Log("msg", "failed to remove temp file", "path", fullPath, "err", err)
		}
	}()
	if err != nil {
		panic(err)
	}
	if _, err := io.Copy(fileToWrite, &buf); err != nil {
		panic(err)
	}

	// flush the file to disk and seek the file to the beginning.
	if err := fileToWrite.Sync(); err != nil {
		return err
	}
	if _, err := fileToWrite.Seek(0, 0); err != nil {
		return err
	}

	objectKey := lt.buildObjectKey(name)
	return lt.storageClient.PutObject(ctx, objectKey, fileToWrite)
}

func (lt *Table) compress(src string, buf io.Writer) error {
	// tar > gzip > buf
	zr := gzip.NewWriter(buf)
	tw := tar.NewWriter(zr)

	// is file a folder?
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	mode := fi.Mode()
	if mode.IsRegular() {
		// get header
		header, err := tar.FileInfoHeader(fi, src)
		if err != nil {
			return err
		}
		// write header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		// get content
		data, err := os.Open(src)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, data); err != nil {
			return err
		}
	} else if mode.IsDir() { // folder

		// walk through every file in the folder
		filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
			// generate tar header
			header, err := tar.FileInfoHeader(fi, file)
			if err != nil {
				return err
			}

			// must provide real name
			// (see https://golang.org/src/archive/tar/common.go?#L626)
			header.Name = filepath.ToSlash(file)

			// write header
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			// if not a dir, write file content
			if !fi.IsDir() {
				data, err := os.Open(file)
				if err != nil {
					return err
				}
				if _, err := io.Copy(tw, data); err != nil {
					return err
				}
			}
			return nil
		})
	} else {
		return fmt.Errorf("error: file type not supported")
	}

	// produce tar
	if err := tw.Close(); err != nil {
		return err
	}
	// produce gzip
	if err := zr.Close(); err != nil {
		return err
	}
	//
	return nil
}

// Cleanup removes dbs which are already uploaded and have not been modified for period longer than dbRetainPeriod.
// This is to avoid keeping all the files forever in the ingesters.
func (lt *Table) Cleanup() error {
	level.Info(util.Logger).Log("msg", fmt.Sprintf("cleaning up unwanted dbs from table %s", lt.name))

	var filesToCleanup []string
	cutoffTime := time.Now().Add(-dbRetainPeriod)

	lt.dbsMtx.RLock()

	for name, db := range lt.dbs {
		stat, err := os.Stat(db.Path())
		if err != nil {
			return err
		}

		lt.uploadedDBsMtimeMtx.RLock()
		uploadedDBMtime, ok := lt.uploadedDBsMtime[name]
		lt.uploadedDBsMtimeMtx.RUnlock()

		// consider files which are already uploaded and have mod time before cutoff time to retain files.
		if ok && !uploadedDBMtime.Before(stat.ModTime()) && stat.ModTime().Before(cutoffTime) {
			filesToCleanup = append(filesToCleanup, name)
		}
	}

	lt.dbsMtx.RUnlock()

	for i := range filesToCleanup {
		level.Debug(util.Logger).Log("msg", fmt.Sprintf("removing db %s from table %s", filesToCleanup[i], lt.name))

		if err := lt.RemoveDB(filesToCleanup[i]); err != nil {
			return err
		}
	}

	return nil
}

func (lt *Table) buildObjectKey(dbName string) string {
	// Files are stored with <table-name>/<uploader>-<db-name>
	objectKey := fmt.Sprintf("%s/%s-%s", lt.name, lt.uploader, dbName)

	// if the file is a migrated one then don't add its name to the object key otherwise we would re-upload them again here with a different name.
	if lt.name == dbName {
		objectKey = fmt.Sprintf("%s/%s", lt.name, lt.uploader)
	}

	return fmt.Sprintf("%s.gz", objectKey)
}

func loadBlugeDBsFromDir(dir string) (map[string]*bluge_db.BlugeDB, error) {
	dbs := map[string]*bluge_db.BlugeDB{}
	filesInfo, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, fileInfo := range filesInfo {
		if !fileInfo.IsDir() || fileInfo.Name() == ".DS_Store" {
			continue
		}

		if strings.HasSuffix(fileInfo.Name(), snapshotFileSuffix) {
			// If an ingester is killed abruptly in the middle of an upload operation it could leave out a temp file which holds the snapshot of db for uploading.
			// Cleaning up those temp files to avoid problems.
			if err := os.Remove(filepath.Join(dir, fileInfo.Name())); err != nil {
				level.Error(util.Logger).Log("msg", "failed to remove temp file", "name", fileInfo.Name(), "err", err)
			}
			continue
		}

		db := &bluge_db.BlugeDB{Name: fileInfo.Name(), Folder: dir} //local.OpenBlugeDBFile(filepath.Join(dir, fileInfo.Name()))
		if err != nil {
			return nil, err
		}

		dbs[fileInfo.Name()] = db
	}

	return dbs, nil
}

// getOldestActiveShardTime returns the time of oldest active shard with a buffer of 1 minute.
func getOldestActiveShardTime() time.Time {
	// upload files excluding active shard. It could so happen that we just started a new shard but the file for last shard is still being updated due to pending writes or pending flush to disk.
	// To avoid uploading it, excluding previous active shard as well if it has been not more than a minute since it became inactive.
	return time.Now().Add(-time.Minute).Truncate(shardDBsByDuration)
}
