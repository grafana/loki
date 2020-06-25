package local

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"
)

// uploadFiles uploads all new and updated files to storage.
// It uploads the files from configured boltdb dir where ingester writes the index.
func (s *Shipper) uploadFiles(ctx context.Context) (err error) {
	if s.cfg.Mode == ShipperModeReadOnly {
		return
	}

	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
		}
		s.metrics.filesUploadOperationTotal.WithLabelValues(status).Inc()
	}()

	filesInfo, err := ioutil.ReadDir(s.cfg.ActiveIndexDirectory)
	if err != nil {
		return
	}

	for _, fileInfo := range filesInfo {
		if fileInfo.IsDir() {
			continue
		}

		s.uploadedFilesMtimeMtx.RLock()
		// Checking whether file is updated after last push, if not skipping it
		uploadedFileMtime, ok := s.uploadedFilesMtime[fileInfo.Name()]
		s.uploadedFilesMtimeMtx.RUnlock()

		if ok && uploadedFileMtime.Equal(fileInfo.ModTime()) {
			continue
		}

		err = s.uploadFile(ctx, fileInfo.Name())
		if err != nil {
			return
		}

		s.uploadedFilesMtimeMtx.Lock()
		s.uploadedFilesMtime[fileInfo.Name()] = fileInfo.ModTime()
		s.uploadedFilesMtimeMtx.Unlock()
	}

	return
}

// uploadFile uploads one of the files locally written by ingesters to storage.
func (s *Shipper) uploadFile(ctx context.Context, period string) error {
	if s.cfg.Mode == ShipperModeReadOnly {
		return nil
	}

	level.Debug(util.Logger).Log("msg", fmt.Sprintf("uploading file for period %s", period))

	snapshotPath := path.Join(s.cfg.CacheLocation, period)
	err := chunk_util.EnsureDirectory(snapshotPath)
	if err != nil {
		return err
	}

	filePath := path.Join(snapshotPath, fmt.Sprintf("%s.%s", s.uploader, "temp"))
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer func() {
		if err := os.Remove(filePath); err != nil {
			level.Error(util.Logger)
		}
	}()

	db, err := s.boltDBGetter.GetDB(period, local.DBOperationRead)
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
	objectKey := fmt.Sprintf("%s/%s", period, s.uploader)
	return s.storageClient.PutObject(ctx, objectKey, f)
}
