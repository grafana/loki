package compactor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var deleteFileBackoffConfig = backoff.Config{
	MinBackoff: 100 * time.Millisecond,
	MaxBackoff: time.Second,
	MaxRetries: 5,
}

var errShardedIndexCopy = errors.New("failed to update sharded index copy")

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
// 2. uploading the compacted index to storage by converting it to index.Index using ToIndexFiles
// After all the operations are successfully done or in case of failure, Cleanup would be called to cleanup the state.
type CompactedIndex interface {
	// IndexProcessor is used for applying custom retention and processing delete requests.
	retention.IndexProcessor
	// Cleanup should clean up all the state built during compaction.
	// It is typically called at the end or in case of an error.
	Cleanup()
	// ToIndexFiles converts the compacted index into files for uploading to the object store.
	// Once the IndexFile is uploaded using Index.Reader, the file is closed using Index.Close and removed from disk using Index.Path.
	ToIndexFiles() ([]index.Index, error)
}

// ShardedCompactedIndex is optionally implemented by a CompactedIndex that can
// also be written as fingerprint-range shard files, for the sharded index copy.
type ShardedCompactedIndex interface {
	ToShardedIndexFiles() ([]index.Index, error)
}

// indexSet helps with doing operations on a set of index files belonging to a single user or common index files shared by users.
type indexSet struct {
	ctx               context.Context
	tableName, userID string
	workingDir        string
	baseIndexSet      storage.IndexSet
	shardedIndexSet   storage.IndexSet

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
		dst = strings.TrimSuffix(dst, gzipExtension)
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

	empty, modified, err := tableMarker.FindAndMarkChunksForDeletion(is.ctx, is.tableName, is.userID, is.compactedIndex, is.logger)
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

func (is *indexSet) chunkExists(lbls labels.Labels, chunkRef logproto.ChunkRef) (bool, error) {
	if is.compactedIndex == nil {
		return false, fmt.Errorf("compacted index should be initialized before checking for existence of chunks")
	}

	userIDBytes := unsafeGetBytes(is.userID)
	return is.compactedIndex.ChunkExists(userIDBytes, lbls, chunkRef)
}

// applyUpdates applies the given updates to the compacted index. Returns list of chunks which were not indexed due to their missing source chunks.
func (is *indexSet) applyUpdates(labels labels.Labels, rebuiltChunks map[string]deletion.Chunk, chunksToDeIndex []string) ([]deletion.Chunk, error) {
	if is.compactedIndex == nil {
		return nil, fmt.Errorf("compacted index should be initialized before applying updates")
	}

	userIDBytes := unsafeGetBytes(is.userID)

	chunksNotIndexed := make([]deletion.Chunk, 0, len(rebuiltChunks))
	for chunkID, newChunk := range rebuiltChunks {
		chk, err := chunk.ParseExternalKey(is.userID, chunkID)
		if err != nil {
			return nil, err
		}

		sourceChunkExisted, err := is.compactedIndex.RemoveChunk(chk.From, chk.Through, userIDBytes, labels, chunkID)
		if err != nil {
			return nil, err
		}
		if newChunk == nil {
			// if we ended up removing the whole source chunk without building a new chunk, there is nothing to do further.
			continue
		}
		if !sourceChunkExisted {
			// if the source chunk was already removed from the index, we need not index the new chunk.
			chunksNotIndexed = append(chunksNotIndexed, newChunk)
			continue
		}
		_, err = is.compactedIndex.IndexChunk(logproto.ChunkRef{
			Fingerprint: newChunk.GetFingerprint(),
			UserID:      is.userID,
			From:        newChunk.GetFrom(),
			Through:     newChunk.GetThrough(),
			Checksum:    newChunk.GetChecksum(),
		}, labels, newChunk.GetIngestedAt(), newChunk.GetSize(), newChunk.GetEntriesCount())
		if err != nil {
			return nil, err
		}

	}

	for _, chunkID := range chunksToDeIndex {
		chk, err := chunk.ParseExternalKey(is.userID, chunkID)
		if err != nil {
			return nil, err
		}

		_, err = is.compactedIndex.RemoveChunk(chk.From, chk.Through, userIDBytes, labels, chunkID)
		if err != nil {
			return nil, err
		}
	}

	is.uploadCompactedDB = true
	is.removeSourceObjects = true

	return chunksNotIndexed, nil
}

// upload uploads the compacted index in compressed format, one object per index file.
func (is *indexSet) upload() error {
	if is.compactedIndex == nil {
		return errors.New("can't upload nil or empty compacted index")
	}

	idxs, err := is.compactedIndex.ToIndexFiles()
	if err != nil {
		return err
	}
	if len(idxs) == 0 {
		return errors.New("compacted index produced no index files")
	}

	defer is.closeAndRemove(idxs)

	for _, idx := range idxs {
		if err := is.uploadFile(is.baseIndexSet, idx); err != nil {
			return err
		}
	}
	return nil
}

func (is *indexSet) closeAndRemove(idxs []index.Index) {
	for _, idx := range idxs {
		filePath := idx.Path()

		if err := idx.Close(); err != nil {
			level.Error(is.logger).Log("msg", "failed to close indexFile", "err", err)
			continue
		}

		if err := os.Remove(filePath); err != nil {
			level.Error(is.logger).Log("msg", "failed to remove indexFile", "err", err)
			continue
		}
	}
}

// uploadFile compresses and uploads a single index file to dest. The caller owns cleanup.
func (is *indexSet) uploadFile(dest storage.IndexSet, idx index.Index) error {
	// ToDo(Sandeep): move index uploading to a common function and share it with generic index-shipper

	fileName := idx.Name()
	level.Debug(is.logger).Log("msg", "uploading index", "file_name", fileName)

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

	gzipPool := compression.GetWriterPool(compression.GZIP)
	compressedWriter := gzipPool.GetWriter(f)
	defer gzipPool.PutWriter(compressedWriter)

	idxReader, err := idx.Reader()
	if err != nil {
		return err
	}
	defer func() {
		if err := idxReader.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close index reader", "path", idxPath, "err", err)
		}
	}()

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

	return dest.PutFile(is.ctx, is.tableName, is.userID, fmt.Sprintf("%s.gz", fileName), f)
}

// removeFilesFromStorage deletes source objects from storage.
func (is *indexSet) removeFilesFromStorage() error {
	level.Info(is.logger).Log("msg", "removing source db files from storage", "count", len(is.sourceObjects))

	for _, object := range is.sourceObjects {
		if err := is.removeFileFromStorage(is.baseIndexSet, object); err != nil {
			level.Error(is.logger).Log("msg", "failed to remove source db file from storage", "file", object.Name, "err", err)
			return err
		}
	}

	return nil
}

// removeFileFromStorage deletes the object from dest.
// It retries transient errors and treats FileNotFound errors
// as a success. A missing file should not stall compaction.
func (is *indexSet) removeFileFromStorage(dest storage.IndexSet, object storage.IndexFile) error {
	retry := backoff.New(is.ctx, deleteFileBackoffConfig)
	for retry.Ongoing() {
		err := dest.DeleteFile(is.ctx, is.tableName, is.userID, object.Name)
		if err == nil {
			return nil
		}

		if dest.IsRetryableErr(err) {
			level.Warn(is.logger).Log(
				"msg", "delete from storage: retrying transient error",
				"file", object.Name,
				"err", err,
			)
			retry.Wait()
		} else if dest.IsFileNotFoundErr(err) {
			level.Warn(is.logger).Log(
				"msg", "delete from storage: ignoring file not found error",
				"file", object.Name,
				"err", err,
			)
			return nil
		} else {
			return err
		}
	}

	return retry.Err()
}

// done takes care of file operations which includes:
// - upload the compacted db if required.
// - update the sharded index copy if configured.
// - remove the source objects from storage if required.
func (is *indexSet) done() error {
	if is.uploadCompactedDB {
		if err := is.upload(); err != nil {
			return err
		}
	}

	if is.shardedIndexSet != nil {
		if err := is.updateShardedCopy(); err != nil {
			return fmt.Errorf("%w: %w", errShardedIndexCopy, err)
		}
	}

	if is.removeSourceObjects {
		return is.removeFilesFromStorage()
	}

	return nil
}

// updateShardedCopy replaces the files of this index set in the sharded index
// prefix with the compacted index split into shards, or removes them when the
// compacted index was dropped.
func (is *indexSet) updateShardedCopy() error {
	if !is.uploadCompactedDB && !is.removeSourceObjects {
		return nil
	}

	var idxs []index.Index
	if is.uploadCompactedDB {
		sharded, ok := is.compactedIndex.(ShardedCompactedIndex)
		if !ok {
			return fmt.Errorf("compacted index %T cannot produce sharded index files", is.compactedIndex)
		}
		var err error
		idxs, err = sharded.ToShardedIndexFiles()
		if err != nil {
			return err
		}
		defer is.closeAndRemove(idxs)
	}

	// bypass the listing cache, which can hide files written by a previous run
	// and leave them behind.
	previous, err := is.shardedIndexSet.ListFiles(is.ctx, is.tableName, is.userID, true)
	if err != nil {
		return err
	}

	uploaded := make(map[string]struct{}, len(idxs))
	for _, idx := range idxs {
		if err := is.uploadFile(is.shardedIndexSet, idx); err != nil {
			return err
		}
		uploaded[idx.Name()+".gz"] = struct{}{}
	}

	for _, f := range previous {
		if _, ok := uploaded[f.Name]; ok {
			continue
		}
		if err := is.removeFileFromStorage(is.shardedIndexSet, f); err != nil {
			return err
		}
	}
	return nil
}

func (is *indexSet) cleanup() {
	if is == nil || is.compactedIndex == nil {
		return
	}
	is.compactedIndex.Cleanup()
}
