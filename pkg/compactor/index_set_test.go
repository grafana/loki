package compactor

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

type indexUpdatesRecorder struct {
	CompactedIndex
	schemaCfg     config.SchemaConfig
	indexedChunks map[string][]deletion.Chunk
	removedChunks map[string][]string
	missingChunks map[string]struct{}
}

func newIndexUpdatesRecorder(schemaCfg config.SchemaConfig, missingChunks []string) *indexUpdatesRecorder {
	missingChunksMap := map[string]struct{}{}
	for _, chunk := range missingChunks {
		missingChunksMap[chunk] = struct{}{}
	}
	return &indexUpdatesRecorder{
		schemaCfg:     schemaCfg,
		indexedChunks: map[string][]deletion.Chunk{},
		removedChunks: map[string][]string{},
		missingChunks: missingChunksMap,
	}
}

func (i *indexUpdatesRecorder) IndexChunk(chunkRef logproto.ChunkRef, lbls labels.Labels, sizeInKB uint32, logEntriesCount uint32) (bool, error) {
	lblsString := lbls.String()
	indexedChunks, ok := i.indexedChunks[lblsString]
	if !ok {
		i.indexedChunks[lblsString] = []deletion.Chunk{}
		indexedChunks = i.indexedChunks[lblsString]
	}
	indexedChunks = append(indexedChunks, dummyChunk{
		from:        chunkRef.From,
		through:     chunkRef.Through,
		fingerprint: chunkRef.Fingerprint,
		checksum:    chunkRef.Checksum,
		kb:          sizeInKB,
		entries:     logEntriesCount,
	})
	i.indexedChunks[lblsString] = indexedChunks

	return true, nil
}

func (i *indexUpdatesRecorder) RemoveChunk(_, _ model.Time, _ []byte, lbls labels.Labels, chunkID string) (bool, error) {
	if _, ok := i.missingChunks[chunkID]; ok {
		return false, nil
	}
	lblsString := lbls.String()
	removedChunks, ok := i.removedChunks[lblsString]
	if !ok {
		i.removedChunks[lblsString] = []string{}
		removedChunks = i.removedChunks[lblsString]
	}
	removedChunks = append(removedChunks, chunkID)
	i.removedChunks[lblsString] = removedChunks

	return true, nil
}

func (i *indexUpdatesRecorder) ChunkExists(_ []byte, _ labels.Labels, chunkRef logproto.ChunkRef) (bool, error) {
	chunkID := i.schemaCfg.ExternalKey(chunkRef)
	_, ok := i.missingChunks[chunkID]
	return !ok, nil
}

func (i *indexUpdatesRecorder) sortEntries() {
	for lbl := range i.indexedChunks {
		slices.SortFunc(i.indexedChunks[lbl], func(a, b deletion.Chunk) int {
			if a.GetFrom() < b.GetFrom() {
				return -1
			} else if a.GetFrom() > b.GetFrom() {
				return 1
			}
			return 0
		})
	}

	for lbl := range i.removedChunks {
		slices.SortFunc(i.removedChunks[lbl], func(a, b string) int {
			return strings.Compare(a, b)
		})
	}
}

type dummyChunk struct {
	from, through model.Time
	fingerprint   uint64
	checksum      uint32
	kb, entries   uint32
}

func (c dummyChunk) GetFrom() model.Time {
	return c.from
}

func (c dummyChunk) GetThrough() model.Time {
	return c.through
}

func (c dummyChunk) GetFingerprint() uint64 {
	return c.fingerprint
}

func (c dummyChunk) GetChecksum() uint32 {
	return c.checksum
}

func (c dummyChunk) GetSize() uint32 {
	return c.kb
}

func (c dummyChunk) GetEntriesCount() uint32 {
	return c.entries
}

func TestIndexSet_ApplyIndexUpdates(t *testing.T) {
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       dayFromTime(0),
				IndexType:  "tsdb",
				ObjectType: "filesystem",
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 16,
			},
		},
	}

	userID := "u1"
	var chunksToDeIndex []string
	var expectedChunksToRemove []string
	for i := 0; i < 10; i++ {
		chunkID := schemaCfg.ExternalKey(logproto.ChunkRef{
			Fingerprint: uint64(i),
			UserID:      userID,
			From:        model.Time(i),
			Through:     model.Time(i + 1),
			Checksum:    uint32(i),
		})
		chunksToDeIndex = append(chunksToDeIndex, chunkID)
		expectedChunksToRemove = append(expectedChunksToRemove, chunkID)
	}

	// build 10 chunks with only the first 5 having a new chunk built out of them
	rebuiltChunks := make(map[string]deletion.Chunk)
	var sourceChunkIDs []string
	var expectedChunksToIndex []deletion.Chunk
	for i := 10; i < 20; i++ {
		chunkID := schemaCfg.ExternalKey(logproto.ChunkRef{
			Fingerprint: uint64(i),
			UserID:      userID,
			From:        model.Time(i),
			Through:     model.Time(i + 1),
			Checksum:    uint32(i),
		})
		var newChunk deletion.Chunk
		if i >= 15 {
			newChunk = dummyChunk{
				from:        model.Time(i),
				through:     model.Time(i + 1),
				fingerprint: uint64(i),
				checksum:    uint32(i + 1),
				kb:          uint32(i),
				entries:     uint32(i),
			}
			expectedChunksToIndex = append(expectedChunksToIndex, newChunk)
		}
		rebuiltChunks[chunkID] = newChunk
		expectedChunksToRemove = append(expectedChunksToRemove, chunkID)
		sourceChunkIDs = append(sourceChunkIDs, chunkID)
	}

	indexUpdatesRecorder := newIndexUpdatesRecorder(schemaCfg, nil)
	idxSet := &indexSet{
		userID:         userID,
		compactedIndex: indexUpdatesRecorder,
	}

	lblFoo := labels.FromStrings("foo", "bar")
	chunksNotIndexed, err := idxSet.applyUpdates(lblFoo, rebuiltChunks, chunksToDeIndex)
	require.NoError(t, err)
	require.Len(t, chunksNotIndexed, 0)

	// sort the entries and see if index updates recorder got the expected updates
	indexUpdatesRecorder.sortEntries()
	slices.SortFunc(expectedChunksToRemove, func(a, b string) int {
		return strings.Compare(a, b)
	})
	require.Equal(t, map[string][]string{lblFoo.String(): expectedChunksToRemove}, indexUpdatesRecorder.removedChunks)
	require.Equal(t, map[string][]deletion.Chunk{lblFoo.String(): expectedChunksToIndex}, indexUpdatesRecorder.indexedChunks)

	// make index updates recorder say all the chunk entries are missing
	indexUpdatesRecorder = newIndexUpdatesRecorder(schemaCfg, append(sourceChunkIDs, chunksToDeIndex...))
	idxSet = &indexSet{
		userID:         userID,
		compactedIndex: indexUpdatesRecorder,
	}
	chunksNotIndexed, err = idxSet.applyUpdates(lblFoo, rebuiltChunks, chunksToDeIndex)
	require.NoError(t, err)
	require.Len(t, chunksNotIndexed, len(expectedChunksToIndex))

	// it would not remove any chunks because all the chunk entries were marked missing
	require.Len(t, indexUpdatesRecorder.removedChunks, 0)
	// it would not index any new chunks since all the source chunks were marked missing
	require.Len(t, indexUpdatesRecorder.indexedChunks, 0)
}

func TestIndexSet_RemoveFilesFromStorageRetriesBackendError(t *testing.T) {
	restoreBackoffConfig := setDeleteSourceObjectsBackoffConfigForTest(t)
	defer restoreBackoffConfig()

	backendErr := errors.New("googleapi: Error 503: We encountered an internal error. Please try again., backendError")
	sourceObjects := []storage.IndexFile{{Name: "source.gz"}}
	baseIndexSet := newDeleteRetryIndexSet(sourceObjects, map[string][]error{
		"source.gz": {backendErr, backendErr},
	})
	indexSet := &indexSet{
		ctx:           context.Background(),
		tableName:     "test",
		baseIndexSet:  baseIndexSet,
		sourceObjects: sourceObjects,
		logger:        log.NewNopLogger(),
	}

	require.NoError(t, indexSet.removeFilesFromStorage())
	require.Equal(t, 3, baseIndexSet.deleteCalls["source.gz"])
}

func TestIndexSet_RemoveFilesFromStorageContinuesAfterDeleteFailure(t *testing.T) {
	restoreBackoffConfig := setDeleteSourceObjectsBackoffConfigForTest(t)
	defer restoreBackoffConfig()

	backendErr := errors.New("googleapi: Error 503: We encountered an internal error. Please try again., backendError")
	sourceObjects := []storage.IndexFile{{Name: "failed.gz"}, {Name: "deleted.gz"}}
	baseIndexSet := newDeleteRetryIndexSet(sourceObjects, map[string][]error{
		"failed.gz": {backendErr, backendErr, backendErr},
	})
	indexSet := &indexSet{
		ctx:           context.Background(),
		tableName:     "test",
		baseIndexSet:  baseIndexSet,
		sourceObjects: sourceObjects,
		logger:        log.NewNopLogger(),
	}

	err := indexSet.removeFilesFromStorage()
	require.Error(t, err)
	require.ErrorContains(t, err, "failed.gz")
	require.Equal(t, 3, baseIndexSet.deleteCalls["failed.gz"])
	require.Equal(t, 1, baseIndexSet.deleteCalls["deleted.gz"])
}

func setDeleteSourceObjectsBackoffConfigForTest(t *testing.T) func() {
	t.Helper()

	originalConfig := deleteSourceObjectsBackoffConfig
	deleteSourceObjectsBackoffConfig.MinBackoff = time.Nanosecond
	deleteSourceObjectsBackoffConfig.MaxBackoff = time.Nanosecond
	deleteSourceObjectsBackoffConfig.MaxRetries = 3

	return func() {
		deleteSourceObjectsBackoffConfig = originalConfig
	}
}

type deleteRetryIndexSet struct {
	sourceObjects []storage.IndexFile
	deleteErrors  map[string][]error
	deleteCalls   map[string]int
	notFoundErr   error
}

func newDeleteRetryIndexSet(sourceObjects []storage.IndexFile, deleteErrors map[string][]error) *deleteRetryIndexSet {
	return &deleteRetryIndexSet{
		sourceObjects: sourceObjects,
		deleteErrors:  deleteErrors,
		deleteCalls:   map[string]int{},
		notFoundErr:   errors.New("not found"),
	}
}

func (d *deleteRetryIndexSet) RefreshIndexTableCache(_ context.Context, _ string) {}

func (d *deleteRetryIndexSet) ListFiles(_ context.Context, _, _ string, _ bool) ([]storage.IndexFile, error) {
	return d.sourceObjects, nil
}

func (d *deleteRetryIndexSet) GetFile(_ context.Context, _, _, _ string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (d *deleteRetryIndexSet) PutFile(_ context.Context, _, _, _ string, _ io.ReadSeeker) error {
	return errors.New("not implemented")
}

func (d *deleteRetryIndexSet) DeleteFile(_ context.Context, _, _, fileName string) error {
	call := d.deleteCalls[fileName]
	d.deleteCalls[fileName] = call + 1
	if call < len(d.deleteErrors[fileName]) {
		return d.deleteErrors[fileName][call]
	}
	return nil
}

func (d *deleteRetryIndexSet) IsFileNotFoundErr(err error) bool {
	return errors.Is(err, d.notFoundErr)
}

func (d *deleteRetryIndexSet) IsUserBasedIndexSet() bool {
	return false
}
