package downloads

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
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
	metrics       *local.downloaderMetrics
	storageClient chunk.ObjectClient

	lastUsedAt time.Time
	files      map[string]*downloadedFile
	err        error
	ready      chan struct{}
}

func NewFilesCollection(period, cacheLocation string, metrics *local.downloaderMetrics, storageClient chunk.ObjectClient) *FilesCollection {
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

// checkStorageForUpdates compares files from cache with storage and builds the list of files to be downloaded from storage and to be deleted from cache
func (fc *FilesCollection) checkStorageForUpdates(ctx context.Context) (toDownload []chunk.StorageObject, toDelete []string, err error) {
	// listing tables from store
	var objects []chunk.StorageObject
	objects, _, err = fc.storageClient.List(ctx, fc.period+"/")
	if err != nil {
		return
	}

	listedUploaders := make(map[string]struct{}, len(objects))

	fc.mtx.RLock()
	for _, object := range objects {
		uploader, err := getUploaderFromObjectKey(object.Key)
		if err != nil {
			return nil, nil, err
		}
		listedUploaders[uploader] = struct{}{}

		// Checking whether file was updated in the store after we downloaded it, if not, no need to include it in updates
		downloadedFileDetails, ok := fc.files[uploader]
		if !ok || downloadedFileDetails.mtime != object.ModifiedAt {
			toDownload = append(toDownload, object)
		}
	}
	fc.mtx.RUnlock()

	err = fc.ForEach(func(uploader string, df *downloadedFile) error {
		if _, isOK := listedUploaders[uploader]; !isOK {
			toDelete = append(toDelete, uploader)
		}
		return nil
	})

	return
}

// Sync downloads updated and new files from for given period from all the uploaders and removes deleted ones
func (fc *FilesCollection) Sync(ctx context.Context) error {
	level.Debug(util.Logger).Log("msg", fmt.Sprintf("syncing files for period %s", fc.period))

	toDownload, toDelete, err := fc.checkStorageForUpdates(ctx)
	if err != nil {
		return err
	}

	for _, storageObject := range toDownload {
		err = fc.downloadFile(ctx, storageObject)
		if err != nil {
			return err
		}
	}

	fc.mtx.Lock()
	defer fc.mtx.Unlock()

	for _, uploader := range toDelete {
		err := fc.cleanupFile(uploader)
		if err != nil {
			return err
		}
	}

	return nil
}

// It first downloads file to a temp location so that we close the existing file(if already exists), replace it with new one and then reopen it.
func (fc *FilesCollection) downloadFile(ctx context.Context, storageObject chunk.StorageObject) error {
	uploader, err := getUploaderFromObjectKey(storageObject.Key)
	if err != nil {
		return err
	}
	folderPath, _ := fc.getFolderPathForPeriod(false)
	filePath := path.Join(folderPath, uploader)

	// download the file temporarily with some other name to allow boltdb client to close the existing file first if it exists
	tempFilePath := path.Join(folderPath, fmt.Sprintf("%s.%s", uploader, "temp"))

	err = fc.getFileFromStorage(ctx, storageObject.Key, tempFilePath)
	if err != nil {
		return err
	}

	fc.mtx.Lock()
	defer fc.mtx.Unlock()

	df, ok := fc.files[uploader]
	if ok {
		if err := df.boltdb.Close(); err != nil {
			return err
		}
	} else {
		df = &downloadedFile{}
	}

	// move the file from temp location to actual location
	err = os.Rename(tempFilePath, filePath)
	if err != nil {
		return err
	}

	df.mtime = storageObject.ModifiedAt
	df.boltdb, err = local.OpenBoltdbFile(filePath)
	if err != nil {
		return err
	}

	fc.files[uploader] = df

	return nil
}

// getFileFromStorage downloads a file from storage to given location.
func (fc *FilesCollection) getFileFromStorage(ctx context.Context, objectKey, destination string) error {
	readCloser, err := fc.storageClient.GetObject(ctx, objectKey)
	if err != nil {
		return err
	}

	defer func() {
		if err := readCloser.Close(); err != nil {
			level.Error(util.Logger)
		}
	}()

	f, err := os.Create(destination)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, readCloser)
	if err != nil {
		return err
	}

	level.Info(util.Logger).Log("msg", fmt.Sprintf("downloaded file %s", objectKey))

	return f.Sync()
}

// downloadAllFilesForPeriod should be called when files for a period does not exist i.e they were never downloaded or got cleaned up later on by TTL
// While files are being downloaded it will block all reads/writes on FilesCollection by taking an exclusive lock
func (fc *FilesCollection) downloadAllFilesForPeriod(ctx context.Context) (err error) {
	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
			fc.setErr(err)

			// cleaning up files due to error to avoid returning invalid results.
			for fileName := range fc.files {
				if err := fc.cleanupFile(fileName); err != nil {
					level.Error(util.Logger).Log("msg", "failed to cleanup partially downloaded file", "filename", fileName, "err", err)
				}
			}
		}
		fc.metrics.filesDownloadOperationTotal.WithLabelValues(status).Inc()
	}()

	startTime := time.Now()
	totalFilesSize := int64(0)

	objects, _, err := fc.storageClient.List(ctx, fc.period+"/")
	if err != nil {
		return
	}

	level.Debug(util.Logger).Log("msg", fmt.Sprintf("list of files to download for period %s: %s", fc.period, objects))

	folderPath, err := fc.getFolderPathForPeriod(true)
	if err != nil {
		return
	}

	for _, object := range objects {
		var uploader string
		uploader, err = getUploaderFromObjectKey(object.Key)
		if err != nil {
			return
		}

		filePath := path.Join(folderPath, uploader)
		df := downloadedFile{}

		err = fc.getFileFromStorage(ctx, object.Key, filePath)
		if err != nil {
			return
		}

		df.mtime = object.ModifiedAt
		df.boltdb, err = local.OpenBoltdbFile(filePath)
		if err != nil {
			return
		}

		var stat os.FileInfo
		stat, err = os.Stat(filePath)
		if err != nil {
			return
		}

		totalFilesSize += stat.Size()

		fc.files[uploader] = &df
	}

	duration := time.Since(startTime).Seconds()
	fc.metrics.filesDownloadDurationSeconds.add(fc.period, duration)
	fc.metrics.filesDownloadSizeBytes.add(fc.period, totalFilesSize)

	return
}

func (fc *FilesCollection) getFolderPathForPeriod(ensureExists bool) (string, error) {
	folderPath := path.Join(fc.cacheLocation, fc.period)

	if ensureExists {
		err := chunk_util.EnsureDirectory(folderPath)
		if err != nil {
			return "", err
		}
	}

	return folderPath, nil
}

func getUploaderFromObjectKey(objectKey string) (string, error) {
	uploaders := strings.Split(objectKey, "/")
	if len(uploaders) != 2 {
		return "", fmt.Errorf("invalid object key: %v", objectKey)
	}
	if uploaders[1] == "" {
		return "", fmt.Errorf("empty uploader, object key: %v", objectKey)
	}
	return uploaders[1], nil
}
