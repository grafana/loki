package boltdb

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	series_index "github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const TempFileSuffix = ".temp"

type IndexFile struct {
	boltDB *bbolt.DB
	name   string

	path             string
	issuedReaders    []*os.File
	issuedReadersMtx sync.Mutex
}

func OpenIndexFile(path string) (index.Index, error) {
	boltdbFile, err := util.SafeOpenBoltdbFile(path)
	if err != nil {
		return nil, err
	}

	return &IndexFile{
		boltDB: boltdbFile,
		name:   filepath.Base(path),
		path:   path,
	}, nil
}

// nolint:revive
func BoltDBToIndexFile(boltdbFile *bbolt.DB, name string) index.Index {
	return &IndexFile{
		boltDB: boltdbFile,
		name:   name,
		path:   boltdbFile.Path(),
	}
}

func (i *IndexFile) GetBoltDB() *bbolt.DB {
	return i.boltDB
}

func (i *IndexFile) Path() string {
	return i.path
}

func (i *IndexFile) Name() string {
	return i.name
}

func (i *IndexFile) Reader() (io.ReadSeeker, error) {
	filePath := path.Join(filepath.Dir(i.Path()), fmt.Sprintf("%d%s", time.Now().UnixNano(), TempFileSuffix))
	f, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	err = i.boltDB.View(func(tx *bbolt.Tx) (err error) {
		_, err = tx.WriteTo(f)
		return
	})
	if err != nil {
		return nil, err
	}

	// flush the file to disk and seek the file to the beginning.
	if err := f.Sync(); err != nil {
		return nil, err
	}

	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}

	i.issuedReadersMtx.Lock()
	defer i.issuedReadersMtx.Unlock()
	i.issuedReaders = append(i.issuedReaders, f)

	return f, nil
}

func (i *IndexFile) Close() error {
	i.issuedReadersMtx.Lock()
	defer i.issuedReadersMtx.Unlock()

	// cleanup all the issued readers
	for _, f := range i.issuedReaders {
		if err := f.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close temp file", "path", f.Name(), "err", err)
		}

		if err := os.Remove(f.Name()); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to remove temp file", "path", f.Name(), "err", err)
		}
	}

	return i.boltDB.Close()
}

func QueryBoltDB(ctx context.Context, db *bbolt.DB, userID []byte, queries []series_index.Query, callback series_index.QueryPagesCallback) error {
	return db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(userID)
		if bucket == nil {
			bucket = tx.Bucket(local.IndexBucketName)
			if bucket == nil {
				return nil
			}
		}

		for _, query := range queries {
			if err := local.QueryWithCursor(ctx, bucket.Cursor(), query, callback); err != nil {
				return err
			}
		}
		return nil
	})
}
