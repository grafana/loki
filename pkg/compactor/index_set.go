package compactor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type IndexSet interface {
	GetTableName() string
	ListSourceFiles() []storage.IndexFile
	GetSourceFile(indexFile storage.IndexFile) (string, error)
	GetLogger() log.Logger
	GetWorkingDir() string
	// SetCompactedIndex sets the CompactedIndex for upload/applying retention and making the compactor remove the source files.
	// CompactedIndex can be nil only in case of all the source files in common index set being compacted away to per tenant index.
	// It would return an error if the CompactedIndex is nil and removeSourceFiles is true in case of user index set since
	// compaction should either create new files or can be a noop if there is nothing to compact.
	// There is no need to call SetCompactedIndex if no changes were made to the index for this IndexSet.
	SetCompactedIndex(compactedIndex CompactedIndex, removeSourceFiles bool) error
}

// CompactedIndex is built by TableCompactor for IndexSet after compaction.
// It would be used for:
// 1. applying custom retention, processing delete requests using IndexProcessor
// 2. uploading the compacted index to storage by converting it to index.Index using ToIndexFile
// After all the operations are successfully done or in case of failure, Cleanup would be called to cleanup the state.
type CompactedIndex interface {
	// IndexProcessor is used for applying custom retention and processing delete requests.
	retention.IndexProcessor
	// Cleanup should clean up all the state built during compaction.
	// It is typically called at the end or in case of an error.
	Cleanup()
	// ToIndexFile is used to convert the CompactedIndex to an IndexFile for uploading to the object store.
	// Once the IndexFile is uploaded using Index.Reader, the file is closed using Index.Close and removed from disk using Index.Path.
	ToIndexFile() (index.Index, error)
}

// indexSet helps with doing operations on a set of index files belonging to a single user or common index files shared by users.
type indexSet struct {
	ctx               context.Context
	tableName, userID string
	workingDir        string
	baseIndexSet      storage.IndexSet

	uploadCompactedDB   bool
	removeSourceObjects bool

	compactedIndex CompactedIndex
	sourceObjects  []storage.IndexFile
	logger         log.Logger
}

// newUserIndexSet intializes a new index set for user index.
func newUserIndexSet(ctx context.Context, tableName, userID string, baseUserIndexSet storage.IndexSet, workingDir string, logger log.Logger) (*indexSet, error) {
	if !baseUserIndexSet.IsUserBasedIndexSet() {
		return nil, fmt.Errorf("base index set is not for user index")
	}

	return newIndexSet(ctx, tableName, userID, baseUserIndexSet, workingDir, log.With(logger, "user-id", userID))
}

// newCommonIndexSet intializes a new index set for common index.
func newCommonIndexSet(ctx context.Context, tableName string, baseUserIndexSet storage.IndexSet, workingDir string, logger log.Logger) (*indexSet, error) {
	if baseUserIndexSet.IsUserBasedIndexSet() {
		return nil, fmt.Errorf("base index set is not for common index")
	}

	return newIndexSet(ctx, tableName, "", baseUserIndexSet, workingDir, logger)
}

func newIndexSet(ctx context.Context, tableName, userID string, baseIndexSet storage.IndexSet, workingDir string, logger log.Logger) (*indexSet, error) {
	if err := util.EnsureDirectory(workingDir); err != nil {
		return nil, err
	}

	ui := &indexSet{
		ctx:          ctx,
		tableName:    tableName,
		userID:       userID,
		workingDir:   workingDir,
		baseIndexSet: baseIndexSet,
		logger:       logger,
	}

	if userID != "" {
		ui.logger = log.With(logger, "user-id", userID)
	}

	var err error
	ui.sourceObjects, err = ui.baseIndexSet.ListFiles(ui.ctx, ui.tableName, ui.userID, false)
	if err != nil {
		return nil, err
	}

	return ui, nil
}

func (is *indexSet) GetTableName() string {
	return is.tableName
}

func (is *indexSet) GetWorkingDir() string {
	return is.workingDir
}

func (is *indexSet) ListSourceFiles() []storage.IndexFile {
	return is.sourceObjects
}

func (is *indexSet) GetSourceFile(indexFile storage.IndexFile) (string, error) {
	decompress := storage.IsCompressedFile(indexFile.Name)
	dst := filepath.Join(is.workingDir, indexFile.Name)
	if decompress {
		dst = strings.Trim(dst, gzipExtension)
	}

	err := storage.DownloadFileFromStorage(dst, storage.IsCompressedFile(indexFile.Name),
		false, storage.LoggerWithFilename(is.logger, indexFile.Name),
		func() (io.ReadCloser, error) {
			return is.baseIndexSet.GetFile(is.ctx, is.tableName, is.userID, indexFile.Name)
		})
	if err != nil {
		return "", err
	}

	return dst, nil
}

func (is *indexSet) GetLogger() log.Logger {
	return is.logger
}

func (is *indexSet) SetCompactedIndex(compactedIndex CompactedIndex, removeSourceFiles bool) error {
	if compactedIndex == nil && removeSourceFiles && is.userID != "" {
		return errors.New("compacted index can't be nil when remove source files is true for user index set")
	}

	is.setCompactedIndex(compactedIndex, compactedIndex != nil, removeSourceFiles)
	return nil
}

func (is *indexSet) setCompactedIndex(compactedIndex CompactedIndex, uploadCompactedDB, removeSourceObjects bool) {
	is.compactedIndex = compactedIndex
	is.uploadCompactedDB = uploadCompactedDB
	is.removeSourceObjects = removeSourceObjects
}

// runRetention runs the retention on index set
func (is *indexSet) runRetention(tableMarker retention.TableMarker) error {
	if is.compactedIndex == nil {
		return nil
	}

	empty, modified, err := tableMarker.MarkForDelete(is.ctx, is.tableName, is.userID, is.compactedIndex, is.logger)
	if err != nil {
		return err
	}

	if empty {
		is.uploadCompactedDB = false
		is.removeSourceObjects = true
	} else if modified {
		is.uploadCompactedDB = true
		is.removeSourceObjects = true
	}

	return nil
}

// upload uploads the compacted index in compressed format.
func (is *indexSet) upload() error {
	if is.compactedIndex == nil {
		return errors.New("can't upload nil or empty compacted index")
	}

	// ToDo(Sandeep): move index uploading to a common function and share it with generic index-shipper
	idx, err := is.compactedIndex.ToIndexFile()
	if err != nil {
		return err
	}

	defer func() {
		filePath := idx.Path()

		if err := idx.Close(); err != nil {
			level.Error(is.logger).Log("msg", "failed to close indexFile", "err", err)
			return
		}

		if err := os.Remove(filePath); err != nil {
			level.Error(is.logger).Log("msg", "failed to remove indexFile", "err", err)
			return
		}
	}()

	fileName := idx.Name()
	level.Debug(is.logger).Log("msg", fmt.Sprintf("uploading index %s", fileName))

	idxPath := idx.Path()

	filePath := fmt.Sprintf("%s%s", idxPath, ".temp")
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

	compressedWriter := chunkenc.Gzip.GetWriter(f)
	defer chunkenc.Gzip.PutWriter(compressedWriter)

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

	return is.baseIndexSet.PutFile(is.ctx, is.tableName, is.userID, fmt.Sprintf("%s.gz", fileName), f)
}

// removeFilesFromStorage deletes source objects from storage.
func (is *indexSet) removeFilesFromStorage() error {
	level.Info(is.logger).Log("msg", "removing source db files from storage", "count", len(is.sourceObjects))

	for _, object := range is.sourceObjects {
		err := is.baseIndexSet.DeleteFile(is.ctx, is.tableName, is.userID, object.Name)
		if err != nil {
			return err
		}
	}

	return nil
}

// done takes care of file operations which includes:
// - recreate the compacted db if required.
// - upload the compacted db if required.
// - remove the source objects from storage if required.
func (is *indexSet) done() error {
	if is.uploadCompactedDB {
		if err := is.upload(); err != nil {
			return err
		}
	}

	if is.removeSourceObjects {
		return is.removeFilesFromStorage()
	}

	return nil
}

func (is *indexSet) cleanup() {
	if is.compactedIndex == nil {
		return
	}
	is.compactedIndex.Cleanup()
}
