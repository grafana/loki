package retention

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"go.etcd.io/bbolt"
)

type MarkerStorageWriter interface {
	Put(chunkID []byte) error
	Count() int64
	Close() error
}

type markerStorageWriter struct {
	db     *bbolt.DB
	tx     *bbolt.Tx
	bucket *bbolt.Bucket

	count    int64
	fileName string
}

func NewMarkerStorageWriter(workingDir string) (*markerStorageWriter, error) {
	err := chunk_util.EnsureDirectory(filepath.Join(workingDir, markersFolder))
	if err != nil {
		return nil, err
	}
	fileName := filepath.Join(workingDir, markersFolder, fmt.Sprint(time.Now().UnixNano()))
	db, err := shipper_util.SafeOpenBoltdbFile(fileName)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin(true)
	if err != nil {
		return nil, err
	}
	bucket, err := tx.CreateBucketIfNotExists(chunkBucket)
	if err != nil {
		return nil, err
	}
	return &markerStorageWriter{
		db:       db,
		tx:       tx,
		bucket:   bucket,
		count:    0,
		fileName: fileName,
	}, err
}

func (m *markerStorageWriter) Put(chunkID []byte) error {
	if err := m.bucket.Put(chunkID, empty); err != nil {
		return err
	}
	m.count++
	return nil
}

func (m *markerStorageWriter) Count() int64 {
	return m.count
}

func (m *markerStorageWriter) Close() error {
	if err := m.tx.Commit(); err != nil {
		return err
	}
	if err := m.db.Close(); err != nil {
		return err
	}
	// The marker file is empty we can remove.
	if m.count > 0 {
		return os.Remove(m.fileName)
	}
	return nil
}
