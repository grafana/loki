package local

import (
	"bytes"
	"context"
	"flag"
	"path"
	"sync"

	"github.com/etcd-io/bbolt"

	"github.com/cortexproject/cortex/pkg/chunk"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
)

var bucketName = []byte("index")

const (
	separator = "\000"
	null      = string('\xff')
)

// BoltDBConfig for a BoltDB index client.
type BoltDBConfig struct {
	Directory string `yaml:"directory"`
}

// RegisterFlags registers flags.
func (cfg *BoltDBConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Directory, "boltdb.dir", "", "Location of BoltDB index files.")
}

type boltIndexClient struct {
	cfg BoltDBConfig

	dbsMtx sync.RWMutex
	dbs    map[string]*bolt.DB
}

// NewBoltDBIndexClient creates a new IndexClient that used BoltDB.
func NewBoltDBIndexClient(cfg BoltDBConfig) (chunk.IndexClient, error) {
	if err := ensureDirectory(cfg.Directory); err != nil {
		return nil, err
	}

	return &boltIndexClient{
		cfg: cfg,
		dbs: map[string]*bolt.DB{},
	}, nil
}

func (b *boltIndexClient) Stop() {
	b.dbsMtx.Lock()
	defer b.dbsMtx.Unlock()
	for _, db := range b.dbs {
		db.Close()
	}
}

func (b *boltIndexClient) NewWriteBatch() chunk.WriteBatch {
	return &boltWriteBatch{
		tables: map[string]map[string][]byte{},
	}
}

func (b *boltIndexClient) getDB(name string) (*bolt.DB, error) {
	b.dbsMtx.RLock()
	db, ok := b.dbs[name]
	b.dbsMtx.RUnlock()
	if ok {
		return db, nil
	}

	b.dbsMtx.Lock()
	defer b.dbsMtx.Unlock()
	db, ok = b.dbs[name]
	if ok {
		return db, nil
	}

	// Open the database.
	db, err := bolt.Open(path.Join(b.cfg.Directory, name), 0666, nil)
	if err != nil {
		return nil, err
	}

	b.dbs[name] = db
	return db, nil
}

func (b *boltIndexClient) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	for table, kvps := range batch.(*boltWriteBatch).tables {
		db, err := b.getDB(table)
		if err != nil {
			return err
		}

		if err := db.Update(func(tx *bolt.Tx) error {
			b, err := tx.CreateBucketIfNotExists(bucketName)
			if err != nil {
				return err
			}

			for key, value := range kvps {
				if err := b.Put([]byte(key), value); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (b *boltIndexClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	return chunk_util.DoParallelQueries(ctx, b.query, queries, callback)
}

func (b *boltIndexClient) query(ctx context.Context, query chunk.IndexQuery, callback func(chunk.ReadBatch) (shouldContinue bool)) error {
	db, err := b.getDB(query.TableName)
	if err != nil {
		return err
	}

	var start []byte
	if len(query.RangeValuePrefix) > 0 {
		start = []byte(query.HashValue + separator + string(query.RangeValuePrefix))
	} else if len(query.RangeValueStart) > 0 {
		start = []byte(query.HashValue + separator + string(query.RangeValueStart))
	} else {
		start = []byte(query.HashValue + separator)
	}

	rowPrefix := []byte(query.HashValue + separator)

	return db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return nil
		}

		var batch boltReadBatch
		c := b.Cursor()
		for k, v := c.Seek(start); k != nil; k, v = c.Next() {
			if len(query.ValueEqual) > 0 && !bytes.Equal(v, query.ValueEqual) {
				continue
			}

			if len(query.RangeValuePrefix) > 0 && !bytes.HasPrefix(k, start) {
				break
			}

			if !bytes.HasPrefix(k, rowPrefix) {
				break
			}

			batch.rangeValue = k[len(rowPrefix):]
			batch.value = v
			if !callback(&batch) {
				break
			}
		}

		return nil
	})
}

type boltWriteBatch struct {
	tables map[string]map[string][]byte
}

func (b *boltWriteBatch) Add(tableName, hashValue string, rangeValue []byte, value []byte) {
	table, ok := b.tables[tableName]
	if !ok {
		table = map[string][]byte{}
		b.tables[tableName] = table
	}

	key := hashValue + separator + string(rangeValue)
	table[key] = value
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
