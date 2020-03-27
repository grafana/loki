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

// downloadFilesForPeriod downloads all the files from for given period from all the uploaders
func (a *Shipper) downloadFilesForPeriod(ctx context.Context, period string, fc *filesCollection) error {
	toDownload, toDelete, err := a.checkStorageForUpdates(ctx, period, fc)
	if err != nil {
		return err
	}

	for _, afd := range toDownload {
		err := a.downloadFile(ctx, period, afd, fc)
		if err != nil {
			return err
		}
	}

	for _, afd := range toDelete {
		err := a.deleteFileFromCache(period, afd.uploader, fc)
		if err != nil {
			return err
		}
	}

	return nil
}

// checkStorageForUpdates compares files from cache with storage and builds the list of files to be downloaded from storage and to be deleted from cache
func (a *Shipper) checkStorageForUpdates(ctx context.Context, period string, fc *filesCollection) (toDownload []shippedFileDetails, toDelete []shippedFileDetails, err error) {
	if a.cfg.Mode == ShipperModeWriteOnly {
		return
	}

	fc.RLock()
	defer fc.RUnlock()

	// listing tables from store
	var objects []chunk.StorageObject
	objects, err = a.storageClient.List(ctx, period)
	if err != nil {
		return
	}

	listedUploaders := make(map[string]struct{}, len(objects))

	for _, object := range objects {
		uploader := strings.Split(object.Key, "/")[1]
		// don't include the file which was uploaded by same ingester
		if uploader == a.uploader {
			continue
		}
		listedUploaders[uploader] = struct{}{}

		// Checking whether file was updated in the store after we downloaded it, if not, no need to include it in updates
		downloadedFileDetails, ok := fc.files[uploader]
		if !ok || downloadedFileDetails.mtime != object.ModifiedAt {
			toDownload = append(toDownload, shippedFileDetails{uploader: uploader, mtime: object.ModifiedAt})
		}
	}

	for uploader, fileDetails := range fc.files {
		if _, isOK := listedUploaders[uploader]; !isOK {
			toDelete = append(toDelete, shippedFileDetails{uploader: uploader, mtime: fileDetails.mtime})
		}
	}

	return
}

// downloadFile downloads a file from storage to cache.
// It first downloads it to a temp file so that we close the existing file(if already exists), replace it with new one and then reopen it.
func (a *Shipper) downloadFile(ctx context.Context, period string, afd shippedFileDetails, fc *filesCollection) error {
	objectKey := fmt.Sprintf("%s/%s", period, afd.uploader)
	readCloser, err := a.storageClient.GetObject(ctx, objectKey)
	if err != nil {
		return err
	}

	defer func() {
		if err := readCloser.Close(); err != nil {
			level.Error(util.Logger)
		}
	}()

	downloadPath := path.Join(a.cfg.CacheLocation, period)
	err = chunk_util.EnsureDirectory(downloadPath)
	if err != nil {
		return err
	}

	// download the file temporarily with some other name to allow boltdb client to close the existing file first if it exists
	tempFilePath := path.Join(downloadPath, fmt.Sprintf("%s.%d", afd.uploader, time.Now().Unix()))

	f, err := os.Create(tempFilePath)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, readCloser)
	if err != nil {
		return err
	}

	fc.Lock()
	defer fc.Unlock()

	af, ok := fc.files[afd.uploader]
	if ok {
		if err := af.boltdb.Close(); err != nil {
			return err
		}
	} else {
		af = shippedFile{downloadLocation: path.Join(downloadPath, afd.uploader)}
	}

	// move the file from temp location to actual location
	err = os.Rename(tempFilePath, af.downloadLocation)
	if err != nil {
		return err
	}

	af.mtime = afd.mtime
	af.boltdb, err = local.OpenBoltdbFile(af.downloadLocation)
	if err != nil {
		return err
	}

	fc.files[afd.uploader] = af

	return nil
}
