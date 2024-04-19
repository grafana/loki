package compactor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	seriesindex "github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/boltdb"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	shipperutil "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
)

type CompactedIndex struct {
	compactedFile          *bbolt.DB
	compactedFileRecreated bool
	tableName              string
	workingDir             string
	logger                 log.Logger
	periodConfig           config.PeriodConfig

	// used for applying retention and deletion
	boltdbTx      *bbolt.Tx
	chunkIndexer  *chunkIndexer
	seriesCleaner *seriesCleaner
}

func newCompactedIndex(compactedFile *bbolt.DB, tableName, workingDir string, periodConfig config.PeriodConfig, logger log.Logger) *CompactedIndex {
	return &CompactedIndex{compactedFile: compactedFile, tableName: tableName, workingDir: workingDir, periodConfig: periodConfig, logger: logger}
}

func (c *CompactedIndex) isEmpty() (bool, error) {
	empty := false
	err := c.compactedFile.View(func(tx *bbolt.Tx) error {
		empty = tx.Bucket(local.IndexBucketName) == nil
		return nil
	})

	if err != nil {
		return false, err
	}

	return empty, nil
}

// recreateCompactedDB just copies the old db to the new one using bbolt.Compact for following reasons:
//  1. When index entries are deleted, boltdb leaves free pages in the file. The only way to drop those free pages is to re-create the file.
//     See https://github.com/boltdb/bolt/issues/308 for more details.
//  2. boltdb by default fills only about 50% of the page in the file. See https://github.com/etcd-io/bbolt/blob/master/bucket.go#L26.
//     This setting is optimal for unordered writes.
//     bbolt.Compact fills the whole page by setting FillPercent to 1 which works well here since while copying the data, it receives the index entries in order.
//     The storage space goes down from anywhere between 25% to 50% as per my(Sandeep) tests.
func (c *CompactedIndex) recreateCompactedDB() error {
	destDB, err := openBoltdbFileWithNoSync(filepath.Join(c.workingDir, fmt.Sprint(time.Now().UnixNano())))
	if err != nil {
		return err
	}

	level.Info(c.logger).Log("msg", "recreating compacted db")

	err = bbolt.Compact(destDB, c.compactedFile, dropFreePagesTxMaxSize)
	if err != nil {
		return err
	}

	sourceSize := int64(0)
	destSize := int64(0)

	if err := c.compactedFile.View(func(tx *bbolt.Tx) error {
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

	level.Info(c.logger).Log("msg", "recreated compacted db", "src_size_bytes", sourceSize, "dest_size_bytes", destSize)

	err = c.compactedFile.Close()
	if err != nil {
		return err
	}

	c.compactedFile = destDB
	c.compactedFileRecreated = true
	return nil
}

// setupIndexProcessors sets things for processing index for applying retention
func (c *CompactedIndex) setupIndexProcessors() error {
	// if boltdbTx is already set then we would have already setup all the required components
	if c.boltdbTx != nil {
		return nil
	}

	if c.compactedFile == nil {
		level.Error(c.logger).Log("msg", "compactedFile is nil")
		return fmt.Errorf("failed setting up index processors since compactedFile is nil")
	}

	var err error
	c.boltdbTx, err = c.compactedFile.Begin(true)
	if err != nil {
		return err
	}

	bucket := c.boltdbTx.Bucket(local.IndexBucketName)
	if bucket == nil {
		return fmt.Errorf("required bucket not found")
	}

	c.chunkIndexer, err = newChunkIndexer(bucket, c.periodConfig, c.tableName)
	if err != nil {
		return err
	}

	c.seriesCleaner = newSeriesCleaner(bucket, c.periodConfig, c.tableName)

	return nil
}

func (c *CompactedIndex) ForEachChunk(ctx context.Context, callback retention.ChunkEntryCallback) error {
	if err := c.setupIndexProcessors(); err != nil {
		return err
	}

	bucket := c.boltdbTx.Bucket(local.IndexBucketName)
	if bucket == nil {
		return fmt.Errorf("required boltdb bucket not found")
	}

	return ForEachChunk(ctx, bucket, c.periodConfig, callback)
}

func (c *CompactedIndex) IndexChunk(chunk chunk.Chunk) (bool, error) {
	if err := c.setupIndexProcessors(); err != nil {
		return false, err
	}

	return c.chunkIndexer.IndexChunk(chunk)
}

func (c *CompactedIndex) CleanupSeries(userID []byte, lbls labels.Labels) error {
	if err := c.setupIndexProcessors(); err != nil {
		return err
	}

	return c.seriesCleaner.CleanupSeries(userID, lbls)
}

func (c *CompactedIndex) ToIndexFile() (shipperindex.Index, error) {
	if c.boltdbTx != nil {
		err := c.boltdbTx.Commit()
		if err != nil {
			return nil, err
		}
		c.boltdbTx = nil
	}

	fileNameFormat := "%s"
	if c.compactedFileRecreated {
		fileNameFormat = "%s" + recreatedCompactedDBSuffix
	}
	fileName := fmt.Sprintf(fileNameFormat, shipperutil.BuildIndexFileName(c.tableName, uploaderName, fmt.Sprint(time.Now().UnixNano())))

	idxFile := boltdb.BoltDBToIndexFile(c.compactedFile, fileName)
	c.compactedFile = nil
	return idxFile, nil
}

func (c *CompactedIndex) Cleanup() {
	if c.compactedFile == nil {
		return
	}

	if c.boltdbTx != nil {
		if err := c.boltdbTx.Commit(); err != nil {
			level.Error(c.logger).Log("msg", "failed commit boltdb transaction", "err", err)
		}
	}

	compactedFilePath := c.compactedFile.Path()
	if err := c.compactedFile.Close(); err != nil {
		level.Error(c.logger).Log("msg", "failed to close compacted index file", "err", err)
	}

	if err := os.Remove(compactedFilePath); err != nil {
		level.Error(c.logger).Log("msg", "failed to remove compacted index file", "err", err)
	}
}

type chunkIndexer struct {
	bucket    *bbolt.Bucket
	scfg      config.SchemaConfig
	tableName string

	seriesStoreSchema seriesindex.SeriesStoreSchema
}

func newChunkIndexer(bucket *bbolt.Bucket, periodConfig config.PeriodConfig, tableName string) (*chunkIndexer, error) {
	seriesStoreSchema, err := seriesindex.CreateSchema(periodConfig)
	if err != nil {
		return nil, err
	}

	return &chunkIndexer{
		bucket:            bucket,
		scfg:              config.SchemaConfig{Configs: []config.PeriodConfig{periodConfig}},
		tableName:         tableName,
		seriesStoreSchema: seriesStoreSchema,
	}, nil
}

// IndexChunk indexes a chunk if it belongs to the same table by seeing if table name in built index entries match c.tableName.
func (c *chunkIndexer) IndexChunk(newChunk chunk.Chunk) (bool, error) {
	entries, err := c.seriesStoreSchema.GetChunkWriteEntries(newChunk.From, newChunk.Through, newChunk.UserID, "logs", newChunk.Metric, c.scfg.ExternalKey(newChunk.ChunkRef))
	if err != nil {
		return false, err
	}

	chunkIndexed := false

	for _, entry := range entries {
		// write an entry only if it belongs to this table
		if entry.TableName == c.tableName {
			key := entry.HashValue + separator + string(entry.RangeValue)
			if err := c.bucket.Put([]byte(key), nil); err != nil {
				return false, err
			}
			chunkIndexed = true
		}
	}

	return chunkIndexed, nil
}

func writeBatch(indexFile *bbolt.DB, batch []indexEntry) error {
	return indexFile.Batch(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(local.IndexBucketName)
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
