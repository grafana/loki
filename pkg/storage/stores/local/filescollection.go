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

type downloadedFile struct {
	mtime  time.Time
	boltdb *bbolt.DB
}

// FilesCollection holds info about shipped boltdb index files by other uploaders(ingesters).
// It is used to hold boltdb files created by all the ingesters for same period i.e with same name.
// In the object store files are uploaded as <boltdb-filename>/<uploader-id> to manage files with same name from different ingesters.
// Note: FilesCollection does not manage the locks on mutex to allow users of it to
// do batch operations or single operation or operations requiring intermittent locking.
// Note2: It has an err variable which is set when FilesCollection is in invalid state due to some issue.
// All operations which try to access/update the files except cleanup returns an error if set.
type FilesCollection struct {
	sync.RWMutex

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
	fc.Lock()
	go func() {
		defer fc.Unlock()
		defer close(fc.ready)

		// Using background context to avoid cancellation of download when request times out.
		// We would anyways need the files for serving next requests.
		if err := fc.downloadAllFilesForPeriod(context.Background()); err != nil {
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

	fc.RLock()
	defer fc.RUnlock()

	for fileName, df := range fc.files {
		if err := callback(fileName, df); err != nil {
			return err
		}
	}

	return nil
}

func (fc *FilesCollection) CleanupAllFiles() error {
	fc.Lock()
	defer fc.Unlock()

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
