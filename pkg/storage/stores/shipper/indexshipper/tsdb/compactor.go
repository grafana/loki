package tsdb

import (
	"context"
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
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

const readDBsConcurrency = 50

type indexProcessor struct{}

func NewIndexCompactor() compactor.IndexCompactor {
	return indexProcessor{}
}

func (i indexProcessor) NewTableCompactor(ctx context.Context, commonIndexSet compactor.IndexSet, existingUserIndexSet map[string]compactor.IndexSet, userIndexSetFactoryFunc compactor.MakeEmptyUserIndexSetFunc, periodConfig config.PeriodConfig) compactor.TableCompactor {
	return newTableCompactor(ctx, commonIndexSet, existingUserIndexSet, userIndexSetFactoryFunc, periodConfig)
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
	err = indexFile.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, "", nil, 0, math.MaxInt64, func(lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) (stop bool) {
		builder.AddSeries(lbls.Copy(), fp, chks)
		return false
	}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
	if err != nil {
		return nil, err
	}

	builder.chunksFinalized = true

	return newCompactedIndex(ctx, tableName, userID, workingDir, periodConfig, builder), nil
}

type tableCompactor struct {
	commonIndexSet          compactor.IndexSet
	existingUserIndexSet    map[string]compactor.IndexSet
	userIndexSetFactoryFunc compactor.MakeEmptyUserIndexSetFunc
	ctx                     context.Context
	periodConfig            config.PeriodConfig
	compactedIndexes        map[string]compactor.CompactedIndex
}

func newTableCompactor(
	ctx context.Context,
	commonIndexSet compactor.IndexSet,
	existingUserIndexSet map[string]compactor.IndexSet,
	userIndexSetFactoryFunc compactor.MakeEmptyUserIndexSetFunc,
	periodConfig config.PeriodConfig,
) *tableCompactor {
	return &tableCompactor{
		ctx:                     ctx,
		commonIndexSet:          commonIndexSet,
		existingUserIndexSet:    existingUserIndexSet,
		userIndexSetFactoryFunc: userIndexSetFactoryFunc,
		periodConfig:            periodConfig,
	}
}

func (t *tableCompactor) CompactTable() error {
	multiTenantIndexes := t.commonIndexSet.ListSourceFiles()

	// index reference and download paths would be stored at the same slice index
	multiTenantIndices := make([]Index, len(multiTenantIndexes))
	downloadPaths := make([]string, len(multiTenantIndexes))

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

		multiTenantIndices[job] = idx.(Index)

		return nil
	})
	if err != nil {
		return err
	}

	defer func() {
		for i, idx := range multiTenantIndices {
			if err := idx.Close(); err != nil {
				level.Error(t.commonIndexSet.GetLogger()).Log("msg", "failed to close multi-tenant source index file", "path", downloadPaths[i], "err", err)
			}

			if err := os.Remove(downloadPaths[i]); err != nil {
				level.Error(t.commonIndexSet.GetLogger()).Log("msg", "failed to remove downloaded index file", "path", downloadPaths[i], "err", err)
			}
		}
	}()

	var multiTenantIndex Index = NoopIndex{}
	if len(multiTenantIndices) > 0 {
		multiTenantIndex = NewMultiIndex(IndexSlice(multiTenantIndices))
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

		builder, err := setupBuilder(t.ctx, indexType, userID, existingUserIndexSet, multiTenantIndices)
		if err != nil {
			return err
		}

		compactedIndex := newCompactedIndex(t.ctx, existingUserIndexSet.GetTableName(), userID, existingUserIndexSet.GetWorkingDir(), t.periodConfig, builder)
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

		builder, err := setupBuilder(t.ctx, indexType, userID, srcIdxSet, []Index{})
		if err != nil {
			return err
		}

		compactedIndex := newCompactedIndex(t.ctx, srcIdxSet.GetTableName(), userID, srcIdxSet.GetWorkingDir(), t.periodConfig, builder)
		t.compactedIndexes[userID] = compactedIndex
		if err := srcIdxSet.SetCompactedIndex(compactedIndex, true); err != nil {
			return err
		}
	}

	if len(multiTenantIndices) > 0 {
		if err := t.commonIndexSet.SetCompactedIndex(nil, true); err != nil {
			return err
		}
	}
	return nil
}

// setupBuilder creates a Builder for a single user.
// It combines the users index from multiTenantIndexes and its existing compacted index(es)
func setupBuilder(ctx context.Context, indexType int, userID string, sourceIndexSet compactor.IndexSet, multiTenantIndexes []Index) (*Builder, error) {
	sourceIndexes := sourceIndexSet.ListSourceFiles()
	builder := NewBuilder(indexType)

	// add users index from multi-tenant indexes to the builder
	for _, idx := range multiTenantIndexes {
		err := idx.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, "", nil, 0, math.MaxInt64, func(lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) (stop bool) {
			builder.AddSeries(withoutTenantLabel(lbls.Copy()), fp, chks)
			return false
		}, withTenantLabelMatcher(userID, []*labels.Matcher{})...)
		if err != nil {
			return nil, err
		}
	}

	// download all the existing compacted indexes and add them to the builder
	for _, sourceIndex := range sourceIndexes {
		path, err := sourceIndexSet.GetSourceFile(sourceIndex)
		if err != nil {
			return nil, err
		}

		defer func() {
			if err := os.Remove(path); err != nil {
				level.Error(sourceIndexSet.GetLogger()).Log("msg", "error removing source index file", "err", err)
			}
		}()

		indexFile, err := OpenShippableTSDB(path)
		if err != nil {
			return nil, err
		}

		defer func() {
			if err := indexFile.Close(); err != nil {
				level.Error(sourceIndexSet.GetLogger()).Log("msg", "failed to close index file", "err", err)
			}
		}()

		err = indexFile.(*TSDBFile).Index.(*TSDBIndex).ForSeries(ctx, "", nil, 0, math.MaxInt64, func(lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) (stop bool) {
			builder.AddSeries(lbls.Copy(), fp, chks)
			return false
		}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
		if err != nil {
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

	indexChunks     map[string][]tsdbindex.ChunkMeta
	deleteChunks    map[string][]tsdbindex.ChunkMeta
	seriesToCleanup map[string]struct{}
}

func newCompactedIndex(ctx context.Context, tableName, userID, workingDir string, periodConfig config.PeriodConfig, builder *Builder) *compactedIndex {
	return &compactedIndex{
		ctx:             ctx,
		userID:          userID,
		builder:         builder,
		workingDir:      workingDir,
		periodConfig:    periodConfig,
		tableInterval:   retention.ExtractIntervalFromTableName(tableName),
		indexChunks:     map[string][]tsdbindex.ChunkMeta{},
		deleteChunks:    map[string][]tsdbindex.ChunkMeta{},
		seriesToCleanup: map[string]struct{}{},
	}
}

// ForEachChunk iterates over all the chunks in the builder and calls the callback function.
func (c *compactedIndex) ForEachChunk(ctx context.Context, callback retention.ChunkEntryCallback) error {
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{c.periodConfig},
	}

	chunkEntry := retention.ChunkEntry{
		ChunkRef: retention.ChunkRef{
			UserID: getUnsafeBytes(c.userID),
		},
	}
	logprotoChunkRef := logproto.ChunkRef{
		UserID: c.userID,
	}
	for seriesID, stream := range c.builder.streams {
		logprotoChunkRef.Fingerprint = uint64(stream.fp)
		chunkEntry.SeriesID = getUnsafeBytes(seriesID)
		chunkEntry.Labels = withoutTenantLabel(stream.labels)

		for i := 0; i < len(stream.chunks) && ctx.Err() == nil; i++ {
			chk := stream.chunks[i]
			logprotoChunkRef.From = chk.From()
			logprotoChunkRef.Through = chk.Through()
			logprotoChunkRef.Checksum = chk.Checksum

			chunkEntry.ChunkID = getUnsafeBytes(schemaCfg.ExternalKey(logprotoChunkRef))
			chunkEntry.From = logprotoChunkRef.From
			chunkEntry.Through = logprotoChunkRef.Through

			deleteChunk, err := callback(chunkEntry)
			if err != nil {
				return err
			}

			if deleteChunk {
				// add the chunk to the list of chunks to delete which would be taken care of while building the index.
				c.deleteChunks[seriesID] = append(c.deleteChunks[seriesID], chk)
			}
		}
	}

	return ctx.Err()
}

// IndexChunk adds the chunk to the list of chunks to index.
// Before accepting the chunk it checks if it falls within the tableInterval and rejects it if not.
func (c *compactedIndex) IndexChunk(chk chunk.Chunk) (bool, error) {
	if chk.From > c.tableInterval.End || c.tableInterval.Start > chk.Through {
		return false, nil
	}

	// TSDB doesnt need the __name__="log" convention the old chunk store index used.
	b := labels.NewBuilder(chk.Metric)
	b.Del(labels.MetricName)
	ls := b.Labels().String()

	approxKB := math.Round(float64(chk.Data.UncompressedSize()) / float64(1<<10))

	c.indexChunks[ls] = append(c.indexChunks[ls], tsdbindex.ChunkMeta{
		Checksum: chk.Checksum,
		MinTime:  int64(chk.From),
		MaxTime:  int64(chk.Through),
		KB:       uint32(approxKB),
		Entries:  uint32(chk.Data.Entries()),
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

	return NewShippableTSDBFile(id)
}

func getUnsafeBytes(s string) []byte {
	return *((*[]byte)(unsafe.Pointer(&s))) // #nosec G103 -- we know the string is not mutated
}
