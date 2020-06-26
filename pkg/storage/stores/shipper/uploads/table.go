package uploads

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"
)

// create a new file sharded by time based on when write request is received
const shardIndexFilesByDuration = 15 * time.Minute

type BoltDBIndexClient interface {
	QueryDB(ctx context.Context, db *bbolt.DB, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error
	WriteToDB(ctx context.Context, db *bbolt.DB, writes local.TableWrites) error
}

type Table struct {
	name              string
	path              string
	uploader          string
	storageClient     chunk.ObjectClient
	boltdbIndexClient BoltDBIndexClient

	dbs    map[string]*bbolt.DB
	dbsMtx sync.RWMutex

	uploadedDBsMtime    map[string]time.Time
	uploadedDBsMtimeMtx sync.RWMutex
}

func NewTable(path, uploader string, storageClient chunk.ObjectClient, boltdbIndexClient BoltDBIndexClient) (*Table, error) {
	err := chunk_util.EnsureDirectory(path)
	if err != nil {
		return nil, err
	}

	return newTableWithDBs(map[string]*bbolt.DB{}, path, uploader, storageClient, boltdbIndexClient)
}

func LoadTable(path, uploader string, storageClient chunk.ObjectClient, boltdbIndexClient BoltDBIndexClient) (*Table, error) {
	dbs, err := loadBoltDBsFromDir(path)
	if err != nil {
		return nil, err
	}

	if len(dbs) == 0 {
		return nil, nil
	}

	return newTableWithDBs(dbs, path, uploader, storageClient, boltdbIndexClient)
}

func newTableWithDBs(dbs map[string]*bbolt.DB, path, uploader string, storageClient chunk.ObjectClient, boltdbIndexClient BoltDBIndexClient) (*Table, error) {
	return &Table{
		name:              filepath.Base(path),
		path:              path,
		uploader:          uploader,
		storageClient:     storageClient,
		boltdbIndexClient: boltdbIndexClient,
		dbs:               dbs,
	}, nil
}

func (lt *Table) Query(ctx context.Context, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	lt.dbsMtx.RLock()
	defer lt.dbsMtx.RUnlock()

	for _, db := range lt.dbs {
		if err := lt.boltdbIndexClient.QueryDB(ctx, db, query, callback); err != nil {
			return err
		}
	}

	return nil
}

func (lt *Table) addFile(filename string) error {
	lt.dbsMtx.Lock()
	defer lt.dbsMtx.Unlock()

	_, ok := lt.dbs[filename]
	if !ok {
		db, err := local.OpenBoltdbFile(filepath.Join(lt.path, filename))
		if err != nil {
			return err
		}

		lt.dbs[filename] = db
	}

	return nil
}

func (lt *Table) Write(ctx context.Context, writes local.TableWrites) error {
	shard := fmt.Sprint(time.Now().Truncate(shardIndexFilesByDuration).Unix())

	lt.dbsMtx.RLock()
	defer lt.dbsMtx.RUnlock()

	db, ok := lt.dbs[shard]
	if !ok {
		lt.dbsMtx.RUnlock()

		err := lt.addFile(shard)
		if err != nil {
			return err
		}

		lt.dbsMtx.RLock()
		db = lt.dbs[shard]
	}

	return lt.boltdbIndexClient.WriteToDB(ctx, db, writes)
}

func (lt *Table) Stop() {
	lt.dbsMtx.Lock()
	defer lt.dbsMtx.RUnlock()

	for name, db := range lt.dbs {
		if err := db.Close(); err != nil {
			level.Error(util.Logger).Log("msg", fmt.Errorf("failed to close file %s for table %s", name, lt.name))
		}
	}

	lt.dbs = map[string]*bbolt.DB{}
}

func (lt *Table) RemoveFile(name string) error {
	lt.dbsMtx.Lock()
	defer lt.dbsMtx.RUnlock()

	db, ok := lt.dbs[name]
	if !ok {
		return nil
	}

	err := db.Close()
	if err != nil {
		return err
	}

	return os.Remove(filepath.Join(lt.path, name))
}

func (lt *Table) Upload(ctx context.Context) error {
	lt.dbsMtx.RLock()
	defer lt.dbsMtx.RUnlock()

	for name, db := range lt.dbs {
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

	return nil
}

func (lt *Table) uploadDB(ctx context.Context, name string, db *bbolt.DB) error {
	filePath := path.Join(lt.path, fmt.Sprintf("%s.%s", lt.uploader, "temp"))
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer func() {
		if err := os.Remove(filePath); err != nil {
			level.Error(util.Logger).Log("msg", "failed to remove temp file", "path", filePath, "err", err)
		}
	}()

	err = db.View(func(tx *bbolt.Tx) error {
		_, err := tx.WriteTo(f)
		return err
	})
	if err != nil {
		return err
	}

	if err := f.Sync(); err != nil {
		return err
	}

	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			level.Error(util.Logger)
		}
	}()

	// Files are stored with <table-name>/<uploader>-<db-name>
	objectKey := fmt.Sprintf("%s/%s-%s", lt.name, lt.uploader, name)
	return lt.storageClient.PutObject(ctx, objectKey, f)
}

func loadBoltDBsFromDir(dir string) (map[string]*bbolt.DB, error) {
	dbs := map[string]*bbolt.DB{}
	filesInfo, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, fileInfo := range filesInfo {
		if fileInfo.IsDir() {
			continue
		}

		db, err := local.OpenBoltdbFile(filepath.Join(dir, fileInfo.Name()))
		if err != nil {
			return nil, err
		}

		dbs[fileInfo.Name()] = db
	}

	return dbs, nil
}
