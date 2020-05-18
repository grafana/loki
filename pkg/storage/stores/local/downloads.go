package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
)

// checkStorageForUpdates compares files from cache with storage and builds the list of files to be downloaded from storage and to be deleted from cache
func (s *Shipper) checkStorageForUpdates(ctx context.Context, period string, fc *filesCollection) (toDownload []chunk.StorageObject, toDelete []string, err error) {
	if s.cfg.Mode == ShipperModeWriteOnly {
		return
	}

	// listing tables from store
	var objects []chunk.StorageObject
	objects, _, err = s.storageClient.List(ctx, period+"/")
	if err != nil {
		return
	}

	listedUploaders := make(map[string]struct{}, len(objects))

	for _, object := range objects {
		uploader := strings.Split(object.Key, "/")[1]
		// don't include the file which was uploaded by same ingester
		if uploader == s.uploader {
			continue
		}
		listedUploaders[uploader] = struct{}{}

		// Checking whether file was updated in the store after we downloaded it, if not, no need to include it in updates
		downloadedFileDetails, ok := fc.files[uploader]
		if !ok || downloadedFileDetails.mtime != object.ModifiedAt {
			toDownload = append(toDownload, object)
		}
	}

	for uploader := range fc.files {
		if _, isOK := listedUploaders[uploader]; !isOK {
			toDelete = append(toDelete, uploader)
		}
	}

	return
}

// syncFilesForPeriod downloads updated and new files from for given period from all the uploaders and removes deleted ones
func (s *Shipper) syncFilesForPeriod(ctx context.Context, period string, fc *filesCollection) error {
	level.Debug(util.Logger).Log("msg", fmt.Sprintf("syncing files for period %s", period))

	fc.RLock()
	toDownload, toDelete, err := s.checkStorageForUpdates(ctx, period, fc)
	fc.RUnlock()

	if err != nil {
		return err
	}

	for _, storageObject := range toDownload {
		err = s.downloadFile(ctx, period, storageObject, fc)
		if err != nil {
			return err
		}
	}

	for _, uploader := range toDelete {
		err := s.deleteFileFromCache(period, uploader, fc)
		if err != nil {
			return err
		}
	}

	return nil
}

// It first downloads file to a temp location so that we close the existing file(if already exists), replace it with new one and then reopen it.
func (s *Shipper) downloadFile(ctx context.Context, period string, storageObject chunk.StorageObject, fc *filesCollection) error {
	uploader := strings.Split(storageObject.Key, "/")[1]
	folderPath, _ := s.getFolderPathForPeriod(period, false)
	filePath := path.Join(folderPath, uploader)

	// download the file temporarily with some other name to allow boltdb client to close the existing file first if it exists
	tempFilePath := path.Join(folderPath, fmt.Sprintf("%s.%s", uploader, "temp"))

	err := s.getFileFromStorage(ctx, storageObject.Key, tempFilePath)
	if err != nil {
		return err
	}

	fc.Lock()
	defer fc.Unlock()

	df, ok := fc.files[uploader]
	if ok {
		if err := df.boltdb.Close(); err != nil {
			return err
		}
	} else {
		df = downloadedFiles{}
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
func (s *Shipper) getFileFromStorage(ctx context.Context, objectKey, destination string) error {
	readCloser, err := s.storageClient.GetObject(ctx, objectKey)
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

// downloadFilesForPeriod should be called when files for a period does not exist i.e they were never downloaded or got cleaned up later on by TTL
// While files are being downloaded it will block all reads/writes on filesCollection by taking an exclusive lock
func (s *Shipper) downloadFilesForPeriod(ctx context.Context, period string, fc *filesCollection) (err error) {
	fc.Lock()
	defer fc.Unlock()

	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
		}
		s.metrics.filesDownloadOperationTotal.WithLabelValues(status).Inc()
	}()

	startTime := time.Now()
	totalFilesSize := int64(0)

	objects, _, err := s.storageClient.List(ctx, period+"/")
	if err != nil {
		return
	}

	level.Debug(util.Logger).Log("msg", fmt.Sprintf("list of files to download for period %s: %s", period, objects))

	folderPath, err := s.getFolderPathForPeriod(period, true)
	if err != nil {
		return
	}

	for _, object := range objects {
		uploader := getUploaderFromObjectKey(object.Key)
		if uploader == s.uploader {
			continue
		}

		filePath := path.Join(folderPath, uploader)
		df := downloadedFiles{}

		err = s.getFileFromStorage(ctx, object.Key, filePath)
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

		fc.files[uploader] = df
	}

	duration := time.Since(startTime).Seconds()
	s.metrics.filesDownloadDurationSeconds.add(period, duration)
	s.metrics.filesDownloadSizeBytes.add(period, totalFilesSize)

	return
}

func (s *Shipper) getFolderPathForPeriod(period string, ensureExists bool) (string, error) {
	folderPath := path.Join(s.cfg.CacheLocation, period)

	if ensureExists {
		err := chunk_util.EnsureDirectory(folderPath)
		if err != nil {
			return "", err
		}
	}

	return folderPath, nil
}

func getUploaderFromObjectKey(objectKey string) string {
	return strings.Split(objectKey, "/")[1]
}
