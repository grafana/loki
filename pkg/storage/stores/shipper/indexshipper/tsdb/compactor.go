package tsdb

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"time"
	"unsafe"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

const readDBsConcurrency = 50

func NewIndexCompactor() compactor.IndexCompactor {
	return NewIndexCompactorWithConfig(IndexCompactorConfig{})
}

type IndexCompactorConfig struct {
	UseSectionRefTable         bool
	MaxSourceFilesPerCompaction int
	Logger                     log.Logger
}

func NewIndexCompactorWithConfig(cfg IndexCompactorConfig) compactor.IndexCompactor {
	logger := cfg.Logger
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return indexProcessor{
		mode:           newCompactionMode(cfg.UseSectionRefTable, logger),
		maxSourceFiles: cfg.MaxSourceFilesPerCompaction,
	}
}

type indexProcessor struct {
	mode           compactionMode
	maxSourceFiles int
}

func (i indexProcessor) NewTableCompactor(ctx context.Context, commonIndexSet compactor.IndexSet, existingUserIndexSet map[string]compactor.IndexSet, userIndexSetFactoryFunc compactor.MakeEmptyUserIndexSetFunc, periodConfig config.PeriodConfig) compactor.TableCompactor {
	return newTableCompactor(ctx, commonIndexSet, existingUserIndexSet, userIndexSetFactoryFunc, periodConfig, i.mode, i.maxSourceFiles)
}

func (i indexProcessor) OpenCompactedIndexFile(ctx context.Context, path, tableName, userID, workingDir string, periodConfig config.PeriodConfig, logger log.Logger) (compactor.CompactedIndex, error) {
	indexFile, err := OpenShippableTSDB(path)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err := indexFile.Close(); err != nil {
			level.Error(logger).Log("msg", "failed to close index file", "err", err)
		}
	}()

	indexFormat, err := periodConfig.TSDBFormat()
	if err != nil {
		return nil, err
	}

	builder := NewBuilder(indexFormat)

	if i.mode == nil {
		err = indexFile.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, "", nil, 0, math.MaxInt64, func(lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) (stop bool) {
			builder.AddSeries(lbls.Copy(), fp, chks)
			return false
		}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
		if err != nil {
			return nil, err
		}
	} else {
		source, err := i.mode.registerPath(path)
		if err != nil {
			return nil, err
		}
		defer i.mode.releaseSource(source)
		var addErr error
		err = indexFile.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, "", nil, 0, math.MaxInt64, func(lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) (stop bool) {
			addErr = i.mode.addSeries(builder, source, lbls.Copy(), fp, chks)
			return addErr != nil
		}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
		if err != nil {
			return nil, err
		}
		if addErr != nil {
			return nil, addErr
		}
	}

	builder.chunksFinalized = true

	return newCompactedIndex(ctx, tableName, userID, workingDir, periodConfig, builder, i.mode), nil
}

type tableCompactor struct {
	commonIndexSet          compactor.IndexSet
	existingUserIndexSet    map[string]compactor.IndexSet
	userIndexSetFactoryFunc compactor.MakeEmptyUserIndexSetFunc
	ctx                     context.Context
	periodConfig            config.PeriodConfig
	compactedIndexes        map[string]compactor.CompactedIndex
	mode                    compactionMode
	maxSourceFiles          int
}

func newTableCompactor(
	ctx context.Context,
	commonIndexSet compactor.IndexSet,
	existingUserIndexSet map[string]compactor.IndexSet,
	userIndexSetFactoryFunc compactor.MakeEmptyUserIndexSetFunc,
	periodConfig config.PeriodConfig,
	mode compactionMode,
	maxSourceFiles int,
) *tableCompactor {
	return &tableCompactor{
		ctx:                     ctx,
		commonIndexSet:          commonIndexSet,
		existingUserIndexSet:    existingUserIndexSet,
		userIndexSetFactoryFunc: userIndexSetFactoryFunc,
		periodConfig:            periodConfig,
		mode:                    mode,
		maxSourceFiles:          maxSourceFiles,
	}
}

func (t *tableCompactor) CompactTable() error {
	if t.mode != nil {
		t.mode.reset()
	}
	allMultiTenantIndexes := t.commonIndexSet.ListSourceFiles()
	multiTenantIndexes := make([]storage.IndexFile, 0, len(allMultiTenantIndexes))
	for _, source := range allMultiTenantIndexes {
		if t.mode != nil && t.mode.isSidecarFile(source.Name) {
			continue
		}
		multiTenantIndexes = append(multiTenantIndexes, source)
	}

	// index reference and download paths would be stored at the same slice index
	multiTenantIndices := make([]Index, len(multiTenantIndexes))
	downloadPaths := make([]string, len(multiTenantIndexes))
	sectionsPaths := make([]string, len(multiTenantIndexes))
	multiTenantSources := make([]modeSourceHandle, len(multiTenantIndexes))

	if t.maxSourceFiles > 0 && len(multiTenantIndexes) > t.maxSourceFiles {
		level.Info(t.commonIndexSet.GetLogger()).Log("msg", "compacting more multi-tenant indexes than limit, truncating", "count", len(multiTenantIndexes), "limit", t.maxSourceFiles)
		multiTenantIndexes = multiTenantIndexes[:t.maxSourceFiles]
		multiTenantIndices = multiTenantIndices[:t.maxSourceFiles]
		downloadPaths = downloadPaths[:t.maxSourceFiles]
		sectionsPaths = sectionsPaths[:t.maxSourceFiles]
		multiTenantSources = multiTenantSources[:t.maxSourceFiles]
	}

	// concurrently download and open all the multi-tenant indexes
	err := concurrency.ForEachJob(t.ctx, len(multiTenantIndexes), readDBsConcurrency, func(_ context.Context, job int) error {
		downloadedAt, err := t.commonIndexSet.GetSourceFile(multiTenantIndexes[job])
		if err != nil {
			return err
		}

		downloadPaths[job] = downloadedAt
		idx, err := OpenShippableTSDB(downloadedAt)
		if err != nil {
			return err
		}

		if t.mode != nil {
			source, sectionsPath, err := t.mode.registerSource(t.commonIndexSet, multiTenantIndexes[job])
			if err != nil {
				if errors.Is(err, errSectionsSidecarMissing) {
					level.Warn(t.commonIndexSet.GetLogger()).Log("msg", "skipping source tsdb because sections sidecar is missing", "file", multiTenantIndexes[job].Name, "err", err)
					if closeErr := idx.Close(); closeErr != nil {
						level.Error(t.commonIndexSet.GetLogger()).Log("msg", "failed to close skipped multi-tenant source index file", "path", downloadPaths[job], "err", closeErr)
					}
					return nil
				}
				return err
			}
			sectionsPaths[job] = sectionsPath
			multiTenantSources[job] = source
		}
		multiTenantIndices[job] = idx.(Index)

		return nil
	})
	if err != nil {
		return err
	}

	defer func() {
		for i, idx := range multiTenantIndices {
			if idx == nil {
				if downloadPaths[i] != "" {
					if err := os.Remove(downloadPaths[i]); err != nil {
						level.Error(t.commonIndexSet.GetLogger()).Log("msg", "failed to remove skipped downloaded index file", "path", downloadPaths[i], "err", err)
					}
				}
				if sectionsPaths[i] != "" {
					if err := os.Remove(sectionsPaths[i]); err != nil {
						level.Error(t.commonIndexSet.GetLogger()).Log("msg", "failed to remove skipped downloaded sections table file", "path", sectionsPaths[i], "err", err)
					}
				}
				if t.mode != nil {
					t.mode.releaseSource(multiTenantSources[i])
				}
				continue
			}

			if err := idx.Close(); err != nil {
				level.Error(t.commonIndexSet.GetLogger()).Log("msg", "failed to close multi-tenant source index file", "path", downloadPaths[i], "err", err)
			}

			if err := os.Remove(downloadPaths[i]); err != nil {
				level.Error(t.commonIndexSet.GetLogger()).Log("msg", "failed to remove downloaded index file", "path", downloadPaths[i], "err", err)
			}
			if sectionsPaths[i] != "" {
				if err := os.Remove(sectionsPaths[i]); err != nil {
					level.Error(t.commonIndexSet.GetLogger()).Log("msg", "failed to remove downloaded sections table file", "path", sectionsPaths[i], "err", err)
				}
			}
			if t.mode != nil {
				t.mode.releaseSource(multiTenantSources[i])
			}
		}
	}()

	var multiTenantIndex Index = NoopIndex{}
	activeMultiTenantIndices := make([]Index, 0, len(multiTenantIndices))
	activeMultiTenantSources := make([]modeSourceHandle, 0, len(multiTenantSources))
	for i := range multiTenantIndices {
		if multiTenantIndices[i] == nil {
			continue
		}
		activeMultiTenantIndices = append(activeMultiTenantIndices, multiTenantIndices[i])
		if t.mode != nil {
			activeMultiTenantSources = append(activeMultiTenantSources, multiTenantSources[i])
		}
	}
	if len(activeMultiTenantIndices) > 0 {
		multiTenantIndex = NewMultiIndex(IndexSlice(activeMultiTenantIndices))
	}

	// find all the user ids from the multi-tenant indexes using TenantLabel.
	userIDs, err := multiTenantIndex.LabelValues(t.ctx, "", 0, math.MaxInt64, TenantLabel)
	if err != nil {
		return err
	}

	// go through all the users having index in the multi-tenant indexes and setup builder for each user
	// builder would combine users index from multi-tenant indexes and the existing compacted index(es)
	t.compactedIndexes = make(map[string]compactor.CompactedIndex, len(userIDs))
	for _, userID := range userIDs {
		existingUserIndexSet, ok := t.existingUserIndexSet[userID]
		if !ok {
			var err error
			existingUserIndexSet, err = t.userIndexSetFactoryFunc(userID)
			if err != nil {
				return err
			}
		}

		indexType, err := t.periodConfig.TSDBFormat()
		if err != nil {
			return err
		}

		builder, err := setupBuilder(t.ctx, indexType, userID, existingUserIndexSet, activeMultiTenantIndices, activeMultiTenantSources, t.mode)
		if err != nil {
			return err
		}
		if err := builder.FlushSectionRefTable(existingUserIndexSet.GetWorkingDir()); err != nil {
			return err
		}

		compactedIndex := newCompactedIndex(t.ctx, existingUserIndexSet.GetTableName(), userID, existingUserIndexSet.GetWorkingDir(), t.periodConfig, builder, t.mode)
		t.compactedIndexes[userID] = compactedIndex

		if err := existingUserIndexSet.SetCompactedIndex(compactedIndex, true); err != nil {
			return err
		}
	}

	// go through existingUserIndexSet and find the ones that were not initialized now due to no updates and
	// have multiple index files in the storage to merge them into a single index file.
	for userID, srcIdxSet := range t.existingUserIndexSet {
		if _, ok := t.compactedIndexes[userID]; ok || len(srcIdxSet.ListSourceFiles()) <= 1 {
			continue
		}

		indexType, err := t.periodConfig.TSDBFormat()
		if err != nil {
			return err
		}

		builder, err := setupBuilder(t.ctx, indexType, userID, srcIdxSet, []Index{}, nil, t.mode)
		if err != nil {
			return err
		}
		if err := builder.FlushSectionRefTable(srcIdxSet.GetWorkingDir()); err != nil {
			return err
		}

		compactedIndex := newCompactedIndex(t.ctx, srcIdxSet.GetTableName(), userID, srcIdxSet.GetWorkingDir(), t.periodConfig, builder, t.mode)
		t.compactedIndexes[userID] = compactedIndex
		if err := srcIdxSet.SetCompactedIndex(compactedIndex, true); err != nil {
			return err
		}
	}

	if len(activeMultiTenantIndices) > 0 {
		if err := t.commonIndexSet.SetCompactedIndex(nil, true); err != nil {
			return err
		}
	}
	return nil
}

// processSourceIndex processes a single source index file by downloading it,
// reading its series, and adding them to the builder. The file is closed and removed
// using defer statements to ensure cleanup happens even if errors occur.
func processSourceIndex(ctx context.Context, sourceIndex storage.IndexFile, sourceIndexSet compactor.IndexSet, builder *Builder, mode compactionMode) error {
	path, err := sourceIndexSet.GetSourceFile(sourceIndex)
	if err != nil {
		return err
	}

	// Ensure the downloaded file is removed when this function returns
	defer func() {
		if removeErr := os.Remove(path); removeErr != nil {
			level.Error(sourceIndexSet.GetLogger()).Log("msg", "error removing source index file", "err", removeErr)
		}
	}()

	var source modeSourceHandle
	var sectionsPath string
	if mode != nil {
		source, sectionsPath, err = mode.registerSource(sourceIndexSet, sourceIndex)
		if err != nil {
			if errors.Is(err, errSectionsSidecarMissing) {
				level.Warn(sourceIndexSet.GetLogger()).Log("msg", "skipping source tsdb because sections sidecar is missing", "file", sourceIndex.Name, "err", err)
				return nil
			}
			return err
		}
		defer func() {
			if removeErr := os.Remove(sectionsPath); removeErr != nil {
				level.Error(sourceIndexSet.GetLogger()).Log("msg", "error removing source sections table file", "err", removeErr)
			}
		}()
		defer mode.releaseSource(source)
	}

	indexFile, err := OpenShippableTSDB(path)
	if err != nil {
		return err
	}

	// Ensure file is closed when this function returns
	defer func() {
		if closeErr := indexFile.Close(); closeErr != nil {
			level.Error(sourceIndexSet.GetLogger()).Log("msg", "failed to close index file", "err", closeErr)
		}
	}()

	var addErr error
	err = indexFile.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, "", nil, 0, math.MaxInt64, func(lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) (stop bool) {
		if mode == nil {
			builder.AddSeries(withoutTenantLabel(lbls.Copy()), fp, chks)
			return false
		}

		addErr = mode.addSeries(builder, source, withoutTenantLabel(lbls.Copy()), fp, chks)
		return addErr != nil
	}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
	if err != nil {
		return err
	}
	return addErr
}

// setupBuilder creates a Builder for a single user.
// It combines the users index from multiTenantIndexes and its existing compacted index(es)
func setupBuilder(
	ctx context.Context,
	indexType int,
	userID string,
	sourceIndexSet compactor.IndexSet,
	multiTenantIndexes []Index,
	multiTenantSources []modeSourceHandle,
	mode compactionMode,
) (*Builder, error) {
	sourceIndexes := sourceIndexSet.ListSourceFiles()
	builder := NewBuilder(indexType)

	// add users index from multi-tenant indexes to the builder
	for i, idx := range multiTenantIndexes {
		var addErr error
		err := idx.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, "", nil, 0, math.MaxInt64, func(lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) (stop bool) {
			if mode == nil {
				builder.AddSeries(withoutTenantLabel(lbls.Copy()), fp, chks)
				return false
			}

			if i >= len(multiTenantSources) {
				addErr = fmt.Errorf("missing source handle for multi-tenant index at position %d", i)
				return true
			}

			addErr = mode.addSeries(builder, multiTenantSources[i], withoutTenantLabel(lbls.Copy()), fp, chks)
			return addErr != nil
		}, withTenantLabelMatcher(userID, []*labels.Matcher{})...)
		if err != nil {
			return nil, err
		}
		if addErr != nil {
			return nil, addErr
		}
	}

	// download all the existing compacted indexes and add them to the builder
	for _, sourceIndex := range sourceIndexes {
		if mode != nil && mode.isSidecarFile(sourceIndex.Name) {
			continue
		}
		if err := processSourceIndex(ctx, sourceIndex, sourceIndexSet, builder, mode); err != nil {
			return nil, err
		}
	}

	// finalize the chunks to remove the duplicates and sort them
	builder.FinalizeChunks()

	return builder, nil
}

type compactedIndex struct {
	ctx           context.Context
	userID        string
	builder       *Builder
	workingDir    string
	tableInterval model.Interval
	periodConfig  config.PeriodConfig
	mode          compactionMode

	indexChunks     map[string][]tsdbindex.ChunkMeta
	deleteChunks    map[string][]tsdbindex.ChunkMeta
	seriesToCleanup map[string]struct{}
}

func newCompactedIndex(ctx context.Context, tableName, userID, workingDir string, periodConfig config.PeriodConfig, builder *Builder, mode compactionMode) *compactedIndex {
	return &compactedIndex{
		ctx:             ctx,
		userID:          userID,
		builder:         builder,
		workingDir:      workingDir,
		periodConfig:    periodConfig,
		mode:            mode,
		tableInterval:   retention.ExtractIntervalFromTableName(tableName),
		indexChunks:     map[string][]tsdbindex.ChunkMeta{},
		deleteChunks:    map[string][]tsdbindex.ChunkMeta{},
		seriesToCleanup: map[string]struct{}{},
	}
}

// ForEachSeries iterates over all the chunks in the builder and calls the callback function.
func (c *compactedIndex) ForEachSeries(ctx context.Context, callback retention.SeriesCallback) error {
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{c.periodConfig},
	}

	logprotoChunkRef := logproto.ChunkRef{
		UserID: c.userID,
	}
	series := retention.NewSeries()
	for seriesID, stream := range c.builder.streams {
		series.Reset(
			getUnsafeBytes(seriesID),
			getUnsafeBytes(c.userID),
			withoutTenantLabel(stream.labels),
		)
		logprotoChunkRef.Fingerprint = uint64(stream.fp)

		for i := 0; i < len(stream.chunks) && ctx.Err() == nil; i++ {
			chk := stream.chunks[i]
			logprotoChunkRef.From = chk.From()
			logprotoChunkRef.Through = chk.Through()
			logprotoChunkRef.Checksum = chk.Checksum

			series.AppendChunks(retention.Chunk{
				ChunkID: schemaCfg.ExternalKey(logprotoChunkRef),
				From:    logprotoChunkRef.From,
				Through: logprotoChunkRef.Through,
			})
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := callback(series)
		if err != nil {
			return err
		}
	}

	return ctx.Err()
}

// IndexChunk adds the chunk to the list of chunks to index.
// Before accepting the chunk it checks if it falls within the tableInterval and rejects it if not.
func (c *compactedIndex) IndexChunk(chunkRef logproto.ChunkRef, lbls labels.Labels, sizeInKB uint32, logEntriesCount uint32) (bool, error) {
	if chunkRef.From > c.tableInterval.End || c.tableInterval.Start > chunkRef.Through {
		return false, nil
	}

	// TSDB doesnt need the __name__="log" convention the old chunk store index used.
	b := labels.NewBuilder(lbls)
	b.Del(model.MetricNameLabel)
	ls := b.Labels().String()

	c.indexChunks[ls] = append(c.indexChunks[ls], tsdbindex.ChunkMeta{
		Checksum: chunkRef.Checksum,
		MinTime:  int64(chunkRef.From),
		MaxTime:  int64(chunkRef.Through),
		KB:       sizeInKB,
		Entries:  logEntriesCount,
	})

	return true, nil
}

// CleanupSeries removes the series from the builder(including its chunks) and deletes the list of chunks lined up for deletion.
func (c *compactedIndex) CleanupSeries(_ []byte, lbls labels.Labels) error {
	seriesID := lbls.String()
	if _, ok := c.builder.streams[seriesID]; !ok {
		return fmt.Errorf("series cleanup not allowed on non-existing series %s", seriesID)
	}
	delete(c.builder.streams, seriesID)
	delete(c.deleteChunks, seriesID)
	return nil
}

// RemoveChunk notes details of the chunk to remove. Returns true/false for existence of the chunk.
func (c *compactedIndex) RemoveChunk(from, through model.Time, userID []byte, labels labels.Labels, chunkID string) (bool, error) {
	chk, err := chunk.ParseExternalKey(string(userID), chunkID)
	if err != nil {
		return false, err
	}

	seriesID := labels.String()
	chunkMeta := tsdbindex.ChunkMeta{
		Checksum: chk.Checksum,
		MinTime:  int64(from),
		MaxTime:  int64(through),
	}
	hasChunk, err := c.builder.HasChunk(seriesID, chunkMeta)
	if err != nil {
		return false, err
	}
	if !hasChunk {
		return false, nil
	}

	c.deleteChunks[seriesID] = append(c.deleteChunks[seriesID], tsdbindex.ChunkMeta{
		Checksum: chk.Checksum,
		MinTime:  int64(from),
		MaxTime:  int64(through),
	})

	return true, nil
}

func (c *compactedIndex) ChunkExists(_ []byte, lbls labels.Labels, chunkRef logproto.ChunkRef) (bool, error) {
	seriesID := lbls.String()
	chunkMeta := tsdbindex.ChunkMeta{
		Checksum: chunkRef.Checksum,
		MinTime:  int64(chunkRef.From),
		MaxTime:  int64(chunkRef.Through),
	}
	hasChunk, err := c.builder.HasChunk(seriesID, chunkMeta)
	if err != nil {
		return false, err
	}

	return hasChunk, nil
}

func (c *compactedIndex) Cleanup() {}

// ToIndexFile creates an indexFile from the chunksmetas stored in the builder.
// Before building the index, it takes care of the lined up updates i.e deletes and adding of new chunks.
func (c *compactedIndex) ToIndexFile() (shipperindex.Index, error) {
	for seriesID, chks := range c.deleteChunks {
		for _, chk := range chks {
			chunkFound, err := c.builder.DropChunk(seriesID, chk)
			if err != nil {
				return nil, err
			}
			if !chunkFound {
				return nil, fmt.Errorf("could not drop non-existent chunk %x from series %s", chk, seriesID)
			}
		}
	}
	c.deleteChunks = nil

	for ls, metas := range c.indexChunks {
		for i := range metas {
			err := c.builder.InsertChunk(ls, metas[i])
			if err != nil {
				return nil, err
			}
		}
	}
	c.indexChunks = nil

	// cleanup any empty streams due to chunk removals above
	for seriesID, stream := range c.builder.streams {
		if len(stream.chunks) == 0 {
			delete(c.indexChunks, seriesID)
		}
	}

	id, err := c.builder.Build(c.ctx, c.workingDir, func(from, through model.Time, checksum uint32) Identifier {
		id := SingleTenantTSDBIdentifier{
			TS:       time.Now(),
			From:     from,
			Through:  through,
			Checksum: checksum,
		}
		return NewPrefixedIdentifier(id, c.workingDir, "")
	})
	if err != nil {
		return nil, err
	}

	if c.mode != nil {
		if err := c.mode.writeCompactedSidecar(c.builder, id.Path()); err != nil {
			return nil, err
		}
	}

	return NewShippableTSDBFile(id)
}

func getUnsafeBytes(s string) []byte {
	return *((*[]byte)(unsafe.Pointer(&s))) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
}
