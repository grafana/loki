package uploads

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type IndexSet interface {
	Add(idx index.Index)
	Upload(ctx context.Context) error
	Cleanup(indexRetainPeriod time.Duration) error
	ForEach(callback index.ForEachIndexCallback) error
	Close()
}

// indexSet is a collection of multiple files created for a same table by various ingesters.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type indexSet struct {
	storageIndexSet   storage.IndexSet
	tableName, userID string
	logger            log.Logger

	index    map[string]index.Index
	indexMtx sync.RWMutex

	indexUploadTime    map[string]time.Time
	indexUploadTimeMtx sync.RWMutex
}

func NewIndexSet(tableName, userID string, baseIndexSet storage.IndexSet, logger log.Logger) (IndexSet, error) {
	if baseIndexSet.IsUserBasedIndexSet() && userID == "" {
		return nil, fmt.Errorf("userID must not be empty")
	} else if !baseIndexSet.IsUserBasedIndexSet() && userID != "" {
		return nil, fmt.Errorf("userID must be empty")
	}

	is := indexSet{
		storageIndexSet: baseIndexSet,
		tableName:       tableName,
		index:           map[string]index.Index{},
		indexUploadTime: map[string]time.Time{},
		userID:          userID,
		logger:          logger,
	}

	return &is, nil
}

func (t *indexSet) Add(idx index.Index) {
	t.indexMtx.Lock()
	defer t.indexMtx.Unlock()

	t.index[idx.Name()] = idx
}

func (t *indexSet) ForEach(callback index.ForEachIndexCallback) error {
	t.indexMtx.RLock()
	defer t.indexMtx.RUnlock()

	for _, idx := range t.index {
		if err := callback(t.userID == "", idx); err != nil {
			return err
		}
	}

	return nil
}

// Upload uploads all the dbs which are never uploaded or have been modified since the last batch was uploaded.
func (t *indexSet) Upload(ctx context.Context) error {
	t.indexMtx.RLock()
	defer t.indexMtx.RUnlock()

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("uploading table %s", t.tableName))

	for name, idx := range t.index {
		// if the file is uploaded already do not upload it again.
		t.indexUploadTimeMtx.RLock()
		_, ok := t.indexUploadTime[name]
		t.indexUploadTimeMtx.RUnlock()

		if ok {
			continue
		}

		if err := t.uploadIndex(ctx, idx); err != nil {
			return err
		}

		t.indexUploadTimeMtx.Lock()
		t.indexUploadTime[name] = time.Now()
		t.indexUploadTimeMtx.Unlock()
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("finished uploading table %s", t.tableName))

	return nil
}

// Close Closes references to all the indexes.
func (t *indexSet) Close() {
	t.indexMtx.Lock()
	defer t.indexMtx.Unlock()

	for name, idx := range t.index {
		if err := idx.Close(); err != nil {
			level.Error(t.logger).Log("msg", fmt.Sprintf("failed to close index %s", name), "err", err)
		}
	}

	t.index = map[string]index.Index{}
}

func (t *indexSet) uploadIndex(ctx context.Context, idx index.Index) error {
	fileName := idx.Name()
	level.Debug(t.logger).Log("msg", fmt.Sprintf("uploading index %s", fileName))

	idxPath := idx.Path()

	filePath := fmt.Sprintf("%s%s", idxPath, tempFileSuffix)
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close temp file", "path", filePath, "err", err)
		}

		if err := os.Remove(filePath); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to remove temp file", "path", filePath, "err", err)
		}
	}()

	gzipPool := compression.GetWriterPool(compression.GZIP)
	compressedWriter := gzipPool.GetWriter(f)
	defer gzipPool.PutWriter(compressedWriter)

	idxReader, err := idx.Reader()
	if err != nil {
		return err
	}

	_, err = idxReader.Seek(0, 0)
	if err != nil {
		return err
	}

	_, err = io.Copy(compressedWriter, idxReader)
	if err != nil {
		return err
	}

	err = compressedWriter.Close()
	if err != nil {
		return err
	}

	// flush the file to disk and seek the file to the beginning.
	if err := f.Sync(); err != nil {
		return err
	}

	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	return t.storageIndexSet.PutFile(ctx, t.tableName, t.userID, t.buildFileName(fileName), f)
}

// Cleanup removes indexes which are already uploaded and have been retained for period longer than indexRetainPeriod since they were uploaded.
func (t *indexSet) Cleanup(indexRetainPeriod time.Duration) error {
	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("cleaning up unwanted indexes from table %s", t.tableName))

	var filesToCleanup []string
	cutoffTime := time.Now().Add(-indexRetainPeriod)

	t.indexMtx.RLock()

	for name := range t.index {
		t.indexUploadTimeMtx.RLock()
		indexUploadTime, ok := t.indexUploadTime[name]
		t.indexUploadTimeMtx.RUnlock()

		if ok && indexUploadTime.Before(cutoffTime) {
			filesToCleanup = append(filesToCleanup, name)
		}
	}

	t.indexMtx.RUnlock()

	for i := range filesToCleanup {
		level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("dropping uploaded index %s from table %s", filesToCleanup[i], t.tableName))

		if err := t.removeIndex(filesToCleanup[i]); err != nil {
			return err
		}
	}

	return nil
}

// removeIndex closes the index and removes the file locally.
func (t *indexSet) removeIndex(name string) error {
	t.indexMtx.Lock()
	defer t.indexMtx.Unlock()

	idx, ok := t.index[name]
	if !ok {
		return nil
	}

	err := idx.Close()
	if err != nil {
		return err
	}

	delete(t.index, name)

	t.indexUploadTimeMtx.Lock()
	delete(t.indexUploadTime, name)
	t.indexUploadTimeMtx.Unlock()

	return os.Remove(idx.Path())
}

func (t *indexSet) buildFileName(indexName string) string {
	return fmt.Sprintf("%s.gz", indexName)
}
