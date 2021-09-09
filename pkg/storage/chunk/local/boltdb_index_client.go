package local

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var (
	bucketName          = []byte("index")
	ErrUnexistentBoltDB = errors.New("boltdb file does not exist")
)

const (
	separator      = "\000"
	dbReloadPeriod = 10 * time.Minute

	DBOperationRead = iota
	DBOperationWrite

	openBoltDBFileTimeout = 5 * time.Second
)

// BoltDBConfig for a BoltDB index client.
type BoltDBConfig struct {
	Directory string `yaml:"directory"`
}

// RegisterFlags registers flags.
func (cfg *BoltDBConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Directory, "boltdb.dir", "", "Location of BoltDB index files.")
}

type BoltIndexClient struct {
	cfg BoltDBConfig

	dbsMtx sync.RWMutex
	dbs    map[string]*bbolt.DB
	done   chan struct{}
	wait   sync.WaitGroup
}

// NewBoltDBIndexClient creates a new IndexClient that used BoltDB.
func NewBoltDBIndexClient(cfg BoltDBConfig) (*BoltIndexClient, error) {
	if err := chunk_util.EnsureDirectory(cfg.Directory); err != nil {
		return nil, err
	}

	indexClient := &BoltIndexClient{
		cfg:  cfg,
		dbs:  map[string]*bbolt.DB{},
		done: make(chan struct{}),
	}

	indexClient.wait.Add(1)
	go indexClient.loop()
	return indexClient, nil
}

func (b *BoltIndexClient) loop() {
	defer b.wait.Done()

	ticker := time.NewTicker(dbReloadPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.reload()
		case <-b.done:
			return
		}
	}
}

func (b *BoltIndexClient) reload() {
	b.dbsMtx.RLock()

	removedDBs := []string{}
	for name := range b.dbs {
		if _, err := os.Stat(path.Join(b.cfg.Directory, name)); err != nil && os.IsNotExist(err) {
			removedDBs = append(removedDBs, name)
			level.Debug(util_log.Logger).Log("msg", "boltdb file got removed", "filename", name)
			continue
		}
	}
	b.dbsMtx.RUnlock()

	if len(removedDBs) != 0 {
		b.dbsMtx.Lock()
		defer b.dbsMtx.Unlock()

		for _, name := range removedDBs {
			if err := b.dbs[name].Close(); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to close removed boltdb", "filename", name, "err", err)
				continue
			}
			delete(b.dbs, name)
		}
	}
}

func (b *BoltIndexClient) Stop() {
	close(b.done)

	b.dbsMtx.Lock()
	defer b.dbsMtx.Unlock()
	for _, db := range b.dbs {
		db.Close()
	}

	b.wait.Wait()
}

func (b *BoltIndexClient) NewWriteBatch() chunk.WriteBatch {
	return &BoltWriteBatch{
		Writes: map[string]TableWrites{},
	}
}

// GetDB should always return a db for write operation unless an error occurs while doing so.
// While for read operation it should throw ErrUnexistentBoltDB error if file does not exist for reading
func (b *BoltIndexClient) GetDB(name string, operation int) (*bbolt.DB, error) {
	b.dbsMtx.RLock()
	db, ok := b.dbs[name]
	b.dbsMtx.RUnlock()
	if ok {
		return db, nil
	}

	// we do not want to create a new db for reading if it does not exist
	if operation == DBOperationRead {
		if _, err := os.Stat(path.Join(b.cfg.Directory, name)); err != nil {
			if os.IsNotExist(err) {
				return nil, ErrUnexistentBoltDB
			}
			return nil, err
		}
	}

	b.dbsMtx.Lock()
	defer b.dbsMtx.Unlock()
	db, ok = b.dbs[name]
	if ok {
		return db, nil
	}

	// Open the database.
	// Set Timeout to avoid obtaining file lock wait indefinitely.
	db, err := bbolt.Open(path.Join(b.cfg.Directory, name), 0666, &bbolt.Options{Timeout: openBoltDBFileTimeout})
	if err != nil {
		return nil, err
	}

	b.dbs[name] = db
	return db, nil
}

func (b *BoltIndexClient) WriteToDB(ctx context.Context, db *bbolt.DB, writes TableWrites) error {
	return db.Update(func(tx *bbolt.Tx) error {
		var b *bbolt.Bucket

		// a bucket should already exist for deletes, for other writes we create one otherwise.
		if len(writes.deletes) != 0 {
			b = tx.Bucket(bucketName)
			if b == nil {
				return fmt.Errorf("bucket %s not found in table %s", bucketName, filepath.Base(db.Path()))
			}
		} else {
			var err error
			b, err = tx.CreateBucketIfNotExists(bucketName)
			if err != nil {
				return err
			}
		}

		for key, value := range writes.puts {
			if err := b.Put([]byte(key), value); err != nil {
				return err
			}
		}

		for key := range writes.deletes {
			if err := b.Delete([]byte(key)); err != nil {
				return err
			}
		}

		return nil
	})
}

func (b *BoltIndexClient) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	for table, writes := range batch.(*BoltWriteBatch).Writes {
		db, err := b.GetDB(table, DBOperationWrite)
		if err != nil {
			return err
		}

		err = b.WriteToDB(ctx, db, writes)
		if err != nil {
			return err
		}
	}

	return nil
}

func (b *BoltIndexClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	return chunk_util.DoParallelQueries(ctx, b.query, queries, callback)
}

func (b *BoltIndexClient) query(ctx context.Context, query chunk.IndexQuery, callback chunk_util.Callback) error {
	db, err := b.GetDB(query.TableName, DBOperationRead)
	if err != nil {
		if err == ErrUnexistentBoltDB {
			return nil
		}

		return err
	}

	return b.QueryDB(ctx, db, query, callback)
}

func (b *BoltIndexClient) QueryDB(ctx context.Context, db *bbolt.DB, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	return db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		if bucket == nil {
			return nil
		}

		return b.QueryWithCursor(ctx, bucket.Cursor(), query, callback)
	})
}

func (b *BoltIndexClient) QueryWithCursor(_ context.Context, c *bbolt.Cursor, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	var start []byte
	if len(query.RangeValuePrefix) > 0 {
		start = []byte(query.HashValue + separator + string(query.RangeValuePrefix))
	} else if len(query.RangeValueStart) > 0 {
		start = []byte(query.HashValue + separator + string(query.RangeValueStart))
	} else {
		start = []byte(query.HashValue + separator)
	}

	rowPrefix := []byte(query.HashValue + separator)

	var batch boltReadBatch

	for k, v := c.Seek(start); k != nil; k, v = c.Next() {
		if !bytes.HasPrefix(k, rowPrefix) {
			break
		}

		if len(query.RangeValuePrefix) > 0 && !bytes.HasPrefix(k, start) {
			break
		}
		if len(query.ValueEqual) > 0 && !bytes.Equal(v, query.ValueEqual) {
			continue
		}

		// make a copy since k, v are only valid for the life of the transaction.
		// See: https://godoc.org/github.com/boltdb/bolt#Cursor.Seek
		batch.rangeValue = make([]byte, len(k)-len(rowPrefix))
		copy(batch.rangeValue, k[len(rowPrefix):])

		batch.value = make([]byte, len(v))
		copy(batch.value, v)

		if !callback(query, &batch) {
			break
		}
	}

	return nil
}

type TableWrites struct {
	puts    map[string][]byte
	deletes map[string]struct{}
}

type BoltWriteBatch struct {
	Writes map[string]TableWrites
}

func (b *BoltWriteBatch) getOrCreateTableWrites(tableName string) TableWrites {
	writes, ok := b.Writes[tableName]
	if !ok {
		writes = TableWrites{
			puts:    map[string][]byte{},
			deletes: map[string]struct{}{},
		}
		b.Writes[tableName] = writes
	}

	return writes
}

func (b *BoltWriteBatch) Delete(tableName, hashValue string, rangeValue []byte) {
	writes := b.getOrCreateTableWrites(tableName)

	key := hashValue + separator + string(rangeValue)
	writes.deletes[key] = struct{}{}
}

func (b *BoltWriteBatch) Add(tableName, hashValue string, rangeValue []byte, value []byte) {
	writes := b.getOrCreateTableWrites(tableName)

	key := hashValue + separator + string(rangeValue)
	writes.puts[key] = value
}

type boltReadBatch struct {
	rangeValue []byte
	value      []byte
}

func (b boltReadBatch) Iterator() chunk.ReadBatchIterator {
	return &boltReadBatchIterator{
		boltReadBatch: b,
	}
}

type boltReadBatchIterator struct {
	consumed bool
	boltReadBatch
}

func (b *boltReadBatchIterator) Next() bool {
	if b.consumed {
		return false
	}
	b.consumed = true
	return true
}

func (b *boltReadBatchIterator) RangeValue() []byte {
	return b.rangeValue
}

func (b *boltReadBatchIterator) Value() []byte {
	return b.value
}

// Open the database.
// Set Timeout to avoid obtaining file lock wait indefinitely.
func OpenBoltdbFile(path string) (*bbolt.DB, error) {
	return bbolt.Open(path, 0666, &bbolt.Options{Timeout: 5 * time.Second})
}
