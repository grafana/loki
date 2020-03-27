package local

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"
)

// uploadFiles uploads all new and updated files to storage.
// It uploads the files from configured boltdb dir where ingester writes the index.
func (a *Shipper) uploadFiles(ctx context.Context) error {
	if a.cfg.Mode == ShipperModeReadOnly {
		return nil
	}

	filesInfo, err := ioutil.ReadDir(a.cfg.ActiveIndexDirectory)
	if err != nil {
		return err
	}

	for _, fileInfo := range filesInfo {
		if fileInfo.IsDir() {
			continue
		}

		a.uploadedFilesMtimeMtx.RLock()
		// Checking whether file is updated after last push, if not skipping it
		uploadedFileMtime, ok := a.uploadedFilesMtime[fileInfo.Name()]
		a.uploadedFilesMtimeMtx.RUnlock()

		if ok && uploadedFileMtime.Equal(fileInfo.ModTime()) {
			continue
		}

		err := a.uploadFile(ctx, fileInfo.Name())
		if err != nil {
			return err
		}

		a.uploadedFilesMtimeMtx.Lock()
		a.uploadedFilesMtime[fileInfo.Name()] = fileInfo.ModTime()
		a.uploadedFilesMtimeMtx.Unlock()
	}

	return nil
}

// uploadFile uploads one of the files locally written by ingesters to storage.
func (a *Shipper) uploadFile(ctx context.Context, period string) error {
	if a.cfg.Mode == ShipperModeReadOnly {
		return nil
	}

	snapshotPath := path.Join(a.cfg.CacheLocation, period)
	err := chunk_util.EnsureDirectory(snapshotPath)
	if err != nil {
		return err
	}

	filePath := path.Join(snapshotPath, fmt.Sprintf("%s.%d", a.uploader, time.Now().Unix()))
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer func() {
		if err := os.Remove(filePath); err != nil {
			level.Error(util.Logger)
		}
	}()

	db, err := a.boltDBGetter.GetDB(period, local.DBOperationRead)
	if err != nil {
		return err
	}

	err = db.View(func(tx *bbolt.Tx) error {
		_, err := tx.WriteTo(f)
		return err
	})
	if err != nil {
		return err
	}

	if err := f.Sync(); err != nil {
		return err
	}

	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			level.Error(util.Logger)
		}
	}()

	// Files are stored with <filename>/<uploader>
	objectKey := fmt.Sprintf("%s/%s", period, a.uploader)
	return a.storageClient.PutObject(ctx, objectKey, f)
}
