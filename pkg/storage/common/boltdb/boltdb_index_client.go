package boltdb

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	IndexBucketName         = []byte("index")
	ErrUnexistentBoltDB     = errors.New("boltdb file does not exist")
	ErrEmptyIndexBucketName = errors.New("empty index bucket name")
)

const (
	separator      = "\000"
	dbReloadPeriod = 10 * time.Minute

	DBOperationRead = iota
	DBOperationWrite

	openBoltDBFileTimeout = 5 * time.Second
)

// >>>>> COPIED FROM pkg/storage/stores/series/index/index.go

// QueryPagesCallback from an IndexQuery.
type QueryPagesCallback func(Query, ReadBatchResult) bool

// Client for the read path.
type ReadClient interface {
	QueryPages(ctx context.Context, queries []Query, callback QueryPagesCallback) error
}

// Client for the write path.
type WriteClient interface {
	NewWriteBatch() WriteBatch
	BatchWrite(context.Context, WriteBatch) error
}

// Client is a client for the storage of the index (e.g. DynamoDB).
type Client interface {
	ReadClient
	WriteClient
	Stop()
}

// ReadBatchResult represents the results of a QueryPages.
type ReadBatchResult interface {
	Iterator() ReadBatchIterator
}

// ReadBatchIterator is an iterator over a ReadBatch.
type ReadBatchIterator interface {
	Next() bool
	RangeValue() []byte
	Value() []byte
}

// WriteBatch represents a batch of writes.
type WriteBatch interface {
	Add(tableName, hashValue string, rangeValue []byte, value []byte)
	Delete(tableName, hashValue string, rangeValue []byte)
}

// Query describes a query for entries
type Query struct {
	TableName string
	HashValue string

	// One of RangeValuePrefix or RangeValueStart might be set:
	// - If RangeValuePrefix is not nil, must read all keys with that prefix.
	// - If RangeValueStart is not nil, must read all keys from there onwards.
	// - If neither is set, must read all keys for that row.
	// RangeValueStart should only be used for querying Chunk IDs.
	// If this is going to change then please take care of func isChunksQuery in pkg/chunk/storage/caching_index_client.go which relies on it.
	RangeValuePrefix []byte
	RangeValueStart  []byte

	// Filters for querying
	ValueEqual []byte

	// If the result of this lookup is immutable or not (for caching).
	Immutable bool
}

// <<<<< END

// Config for a BoltDB index client.
type Config struct {
	Directory string `yaml:"directory"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Directory, "boltdb.dir", "", "Location of BoltDB index files.")
}

type BoltIndexClient struct {
	cfg Config

	dbsMtx sync.RWMutex
	dbs    map[string]*bbolt.DB
	done   chan struct{}
	wait   sync.WaitGroup
}

// NewBoltDBIndexClient creates a new IndexClient that used BoltDB.
func NewBoltDBIndexClient(cfg Config) (*BoltIndexClient, error) {
	if err := util.EnsureDirectory(cfg.Directory); err != nil {
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

func (b *BoltIndexClient) NewWriteBatch() WriteBatch {
	return NewWriteBatch()
}

func NewWriteBatch() WriteBatch {
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
	db, err := bbolt.Open(path.Join(b.cfg.Directory, name), 0o666, &bbolt.Options{Timeout: openBoltDBFileTimeout})
	if err != nil {
		return nil, fmt.Errorf("failed to open boltdb index file: %w", err)
	}

	b.dbs[name] = db
	return db, nil
}

func WriteToDB(_ context.Context, db *bbolt.DB, bucketName []byte, writes TableWrites) error {
	return db.Update(func(tx *bbolt.Tx) error {
		var b *bbolt.Bucket
		if len(bucketName) == 0 {
			return ErrEmptyIndexBucketName
		}

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

		for key := range writes.deletes {
			if err := b.Delete([]byte(key)); err != nil {
				return err
			}
		}

		for key, value := range writes.puts {
			if err := b.Put([]byte(key), value); err != nil {
				return err
			}
		}

		return nil
	})
}

func (b *BoltIndexClient) BatchWrite(ctx context.Context, batch WriteBatch) error {
	for table, writes := range batch.(*BoltWriteBatch).Writes {
		db, err := b.GetDB(table, DBOperationWrite)
		if err != nil {
			return err
		}

		err = WriteToDB(ctx, db, IndexBucketName, writes)
		if err != nil {
			return err
		}
	}

	return nil
}

func (b *BoltIndexClient) QueryPages(ctx context.Context, queries []Query, callback QueryPagesCallback) error {
	return DoParallelQueries(ctx, b.query, queries, callback)
}

func (b *BoltIndexClient) query(ctx context.Context, query Query, callback QueryPagesCallback) error {
	db, err := b.GetDB(query.TableName, DBOperationRead)
	if err != nil {
		if err == ErrUnexistentBoltDB {
			return nil
		}

		return err
	}

	return QueryDB(ctx, db, IndexBucketName, query, callback)
}

func QueryDB(ctx context.Context, db *bbolt.DB, bucketName []byte, query Query, callback QueryPagesCallback) error {
	return db.View(func(tx *bbolt.Tx) error {
		if len(bucketName) == 0 {
			return ErrEmptyIndexBucketName
		}
		bucket := tx.Bucket(bucketName)
		if bucket == nil {
			return nil
		}

		return QueryWithCursor(ctx, bucket.Cursor(), query, callback)
	})
}

func QueryWithCursor(_ context.Context, c *bbolt.Cursor, query Query, callback QueryPagesCallback) error {
	batch := batchPool.Get().(*cursorBatch)
	defer batchPool.Put(batch)

	batch.reset(c, &query)
	callback(query, batch)
	return nil
}

var batchPool = sync.Pool{
	New: func() any {
		return &cursorBatch{
			start:     bytes.NewBuffer(make([]byte, 0, 1024)),
			rowPrefix: bytes.NewBuffer(make([]byte, 0, 1024)),
		}
	},
}

type cursorBatch struct {
	cursor    *bbolt.Cursor
	query     *Query
	start     *bytes.Buffer
	rowPrefix *bytes.Buffer
	seeked    bool

	currRangeValue []byte
	currValue      []byte
}

func (c *cursorBatch) Iterator() ReadBatchIterator {
	return c
}

func (c *cursorBatch) nextItem() ([]byte, []byte) {
	if !c.seeked {
		if len(c.query.RangeValuePrefix) > 0 {
			c.start.WriteString(c.query.HashValue)
			c.start.WriteString(separator)
			c.start.Write(c.query.RangeValuePrefix)
		} else if len(c.query.RangeValueStart) > 0 {
			c.start.WriteString(c.query.HashValue)
			c.start.WriteString(separator)
			c.start.Write(c.query.RangeValueStart)
		} else {
			c.start.WriteString(c.query.HashValue)
			c.start.WriteString(separator)
		}
		c.rowPrefix.WriteString(c.query.HashValue)
		c.rowPrefix.WriteString(separator)
		c.seeked = true
		return c.cursor.Seek(c.start.Bytes())
	}
	return c.cursor.Next()
}

func (c *cursorBatch) Next() bool {
	for k, v := c.nextItem(); k != nil; k, v = c.nextItem() {
		if !bytes.HasPrefix(k, c.rowPrefix.Bytes()) {
			break
		}

		if len(c.query.RangeValuePrefix) > 0 && !bytes.HasPrefix(k, c.start.Bytes()) {
			break
		}
		if len(c.query.ValueEqual) > 0 && !bytes.Equal(v, c.query.ValueEqual) {
			continue
		}

		// make a copy since k, v are only valid for the life of the transaction.
		// See: https://godoc.org/github.com/boltdb/bolt#Cursor.Seek
		rangeValue := make([]byte, len(k)-c.rowPrefix.Len())
		copy(rangeValue, k[c.rowPrefix.Len():])

		value := make([]byte, len(v))
		copy(value, v)

		c.currRangeValue = rangeValue
		c.currValue = value
		return true
	}
	return false
}

func (c *cursorBatch) RangeValue() []byte {
	return c.currRangeValue
}

func (c *cursorBatch) Value() []byte {
	return c.currValue
}

func (c *cursorBatch) reset(cur *bbolt.Cursor, q *Query) {
	c.currRangeValue = nil
	c.currValue = nil
	c.seeked = false
	c.cursor = cur
	c.query = q
	c.rowPrefix.Reset()
	c.start.Reset()
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

// Open the database.
// Set Timeout to avoid obtaining file lock wait indefinitely.
func OpenBoltdbFile(path string) (*bbolt.DB, error) {
	return bbolt.Open(path, 0o666, &bbolt.Options{Timeout: 5 * time.Second})
}
