package local

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"
)

// timeout for downloading initial files for a table to avoid leaking resources by allowing it to take all the time.
const downloadTimeout = 5 * time.Minute

type downloadedFile struct {
	mtime  time.Time
	boltdb *bbolt.DB
}

// FilesCollection holds info about shipped boltdb index files by other uploaders(ingesters).
// It is used to hold boltdb files created by all the ingesters for same period i.e with same name.
// In the object store files are uploaded as <boltdb-filename>/<uploader-id> to manage files with same name from different ingesters.
// Note: FilesCollection takes care of locking with all the exported/public methods.
// Note2: It has an err variable which is set when FilesCollection is in invalid state due to some issue.
// All operations which try to access/update the files except cleanup returns an error if set.
type FilesCollection struct {
	mtx sync.RWMutex

	period        string
	cacheLocation string
	metrics       *downloaderMetrics
	storageClient chunk.ObjectClient

	lastUsedAt time.Time
	files      map[string]*downloadedFile
	err        error
	ready      chan struct{}
}

func NewFilesCollection(period, cacheLocation string, metrics *downloaderMetrics, storageClient chunk.ObjectClient) *FilesCollection {
	fc := FilesCollection{
		period:        period,
		cacheLocation: cacheLocation,
		metrics:       metrics,
		storageClient: storageClient,
		files:         map[string]*downloadedFile{},
		ready:         make(chan struct{}),
	}

	// keep the files collection locked until all the files are downloaded.
	fc.mtx.Lock()
	go func() {
		defer fc.mtx.Unlock()
		defer close(fc.ready)

		ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
		defer cancel()

		// Using background context to avoid cancellation of download when request times out.
		// We would anyways need the files for serving next requests.
		if err := fc.downloadAllFilesForPeriod(ctx); err != nil {
			level.Error(util.Logger).Log("msg", "failed to download files", "period", fc.period)
		}
	}()

	return &fc
}

func (fc *FilesCollection) cleanupFile(fileName string) error {
	df, ok := fc.files[fileName]
	if !ok {
		return fmt.Errorf("file %s not found in files collection for cleaning up", fileName)
	}

	filePath := df.boltdb.Path()

	if err := df.boltdb.Close(); err != nil {
		return err
	}

	delete(fc.files, fileName)

	return os.Remove(filePath)
}

func (fc *FilesCollection) ForEach(callback func(fileName string, df *downloadedFile) error) error {
	if fc.err != nil {
		return fc.err
	}

	fc.mtx.RLock()
	defer fc.mtx.RUnlock()

	for fileName, df := range fc.files {
		if err := callback(fileName, df); err != nil {
			return err
		}
	}

	return nil
}

func (fc *FilesCollection) CleanupAllFiles() error {
	fc.mtx.Lock()
	defer fc.mtx.Unlock()

	for fileName := range fc.files {
		if err := fc.cleanupFile(fileName); err != nil {
			return err
		}
	}
	return nil
}

func (fc *FilesCollection) UpdateLastUsedAt() {
	fc.lastUsedAt = time.Now()
}

func (fc *FilesCollection) LastUsedAt() time.Time {
	return fc.lastUsedAt
}

func (fc *FilesCollection) setErr(err error) {
	fc.err = err
}

func (fc *FilesCollection) Err() error {
	return fc.err
}

func (fc *FilesCollection) IsReady() chan struct{} {
	return fc.ready
}
