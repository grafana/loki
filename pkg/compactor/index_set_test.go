package compactor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
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
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
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

func (i *indexUpdatesRecorder) IndexChunk(chunkRef logproto.ChunkRef, lbls labels.Labels, ingestedAt model.Time, sizeInKB uint32, logEntriesCount uint32) (bool, error) {
	lblsString := lbls.String()
	indexedChunks, ok := i.indexedChunks[lblsString]
	if !ok {
		i.indexedChunks[lblsString] = []deletion.Chunk{}
		indexedChunks = i.indexedChunks[lblsString]
	}
	indexedChunks = append(indexedChunks, dummyChunk{
		from:        chunkRef.From,
		through:     chunkRef.Through,
		ingestedAt:  ingestedAt,
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
	ingestedAt    model.Time
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

func (c dummyChunk) GetIngestedAt() model.Time {
	return c.ingestedAt
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

func TestIndexSet_RemoveFilesFromStorageRetries(t *testing.T) {
	restoreConfig := setDeleteSourceObjectsBackoffConfigForTest(t)
	defer restoreConfig()

	retryableErr := errors.New("transient error")
	nonRetryableErr := errors.New("non-retryable error")

	tests := []struct {
		name              string
		sourceObjects     []storage.IndexFile
		deleteErrors      map[string][]error
		retryableErr      error
		expectedErr       error
		expectedErrString string
		expectedCalls     map[string]int
	}{
		{
			name:          "retries retryable errors until delete succeeds",
			sourceObjects: []storage.IndexFile{{Name: "source.gz"}},
			deleteErrors: map[string][]error{
				"source.gz": {retryableErr, retryableErr},
			},
			retryableErr: retryableErr,
			expectedCalls: map[string]int{
				"source.gz": 3,
			},
		},
		{
			name:          "fail after max retries",
			sourceObjects: []storage.IndexFile{{Name: "failed.gz"}},
			deleteErrors: map[string][]error{
				"failed.gz": {retryableErr, retryableErr, retryableErr},
			},
			retryableErr:      retryableErr,
			expectedErrString: "terminated after 3 retries",
			expectedCalls: map[string]int{
				"failed.gz": 3,
			},
		},
		{
			name:          "does not retry non-retryable errors",
			sourceObjects: []storage.IndexFile{{Name: "source.gz"}},
			deleteErrors: map[string][]error{
				"source.gz": {nonRetryableErr},
			},
			retryableErr: retryableErr,
			expectedErr:  nonRetryableErr,
			expectedCalls: map[string]int{
				"source.gz": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseIndexSet := newDeleteRetryIndexSet(tt.sourceObjects, tt.deleteErrors, tt.retryableErr)
			indexSet := &indexSet{
				ctx:           context.Background(),
				tableName:     "test",
				baseIndexSet:  baseIndexSet,
				sourceObjects: tt.sourceObjects,
				logger:        log.NewNopLogger(),
			}

			err := indexSet.removeFilesFromStorage()
			if tt.expectedErr != nil {
				require.ErrorIs(t, err, tt.expectedErr)
			} else if tt.expectedErrString != "" {
				require.ErrorContains(t, err, tt.expectedErrString)
			} else {
				require.NoError(t, err)
			}

			for fileName, expectedCalls := range tt.expectedCalls {
				require.Equal(t, expectedCalls, baseIndexSet.deleteCalls[fileName])
			}
		})
	}
}

func setDeleteSourceObjectsBackoffConfigForTest(t *testing.T) func() {
	t.Helper()

	originalConfig := deleteFileBackoffConfig
	deleteFileBackoffConfig.MinBackoff = time.Nanosecond
	deleteFileBackoffConfig.MaxBackoff = time.Nanosecond
	deleteFileBackoffConfig.MaxRetries = 3

	return func() {
		deleteFileBackoffConfig = originalConfig
	}
}

type deleteRetryIndexSet struct {
	sourceObjects []storage.IndexFile
	deleteErrors  map[string][]error
	deleteCalls   map[string]int
	notFoundErr   error
	retryableErr  error
}

func newDeleteRetryIndexSet(sourceObjects []storage.IndexFile, deleteErrors map[string][]error, retryableErr error) *deleteRetryIndexSet {
	return &deleteRetryIndexSet{
		sourceObjects: sourceObjects,
		deleteErrors:  deleteErrors,
		deleteCalls:   map[string]int{},
		notFoundErr:   errors.New("not found"),
		retryableErr:  retryableErr,
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
	calls := d.deleteCalls[fileName]
	d.deleteCalls[fileName] = calls + 1
	if calls < len(d.deleteErrors[fileName]) {
		return d.deleteErrors[fileName][calls]
	}
	return nil
}

func (d *deleteRetryIndexSet) IsFileNotFoundErr(err error) bool {
	return errors.Is(err, d.notFoundErr)
}

func (d *deleteRetryIndexSet) IsRetryableErr(err error) bool {
	return errors.Is(err, d.retryableErr)
}

func (d *deleteRetryIndexSet) IsUserBasedIndexSet() bool {
	return false
}

type multiOutputIndex struct {
	CompactedIndex
	files []index.Index
}

func (m multiOutputIndex) ToIndexFiles() ([]index.Index, error) { return m.files, nil }

type outputFile struct {
	path   string
	closed bool
}

func (f *outputFile) Name() string                       { return filepath.Base(f.path) }
func (f *outputFile) Path() string                       { return f.path }
func (f *outputFile) Close() error                       { f.closed = true; return nil }
func (f *outputFile) Reader() (io.ReadSeekCloser, error) { return os.Open(f.path) }

type uploadRecorder struct {
	storage.IndexSet
	names  []string
	failAt int
}

func (r *uploadRecorder) PutFile(_ context.Context, _, _, name string, _ io.ReadSeeker) error {
	r.names = append(r.names, name)
	if len(r.names) == r.failAt {
		return errors.New("upload failed")
	}
	return nil
}
func TestIndexSetMultiOutputCleanup(t *testing.T) {
	for _, failAt := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("failAt=%d", failAt), func(t *testing.T) {
			var files []index.Index
			for i := 0; i < 3; i++ {
				path := filepath.Join(t.TempDir(), fmt.Sprintf("shard-%d", i))
				require.NoError(t, os.WriteFile(path, []byte("index"), 0600))
				files = append(files, &outputFile{path: path})
			}
			storage := &uploadRecorder{failAt: failAt}
			set := &indexSet{ctx: context.Background(), baseIndexSet: storage, compactedIndex: multiOutputIndex{files: files}, logger: log.NewNopLogger()}
			err := set.upload()
			if failAt == 0 {
				require.NoError(t, err)
				require.Len(t, storage.names, 3)
			} else {
				require.ErrorContains(t, err, "upload failed")
				require.Len(t, storage.names, failAt)
			}
			for _, f := range files {
				require.True(t, f.(*outputFile).closed)
				_, err := os.Stat(f.Path())
				require.True(t, os.IsNotExist(err))
			}
		})
	}
}

type memIndexSet struct {
	storage.IndexSet
	files     map[string]struct{}
	puts      int
	putFailAt int
	deleteErr error
}

func newMemIndexSet(names ...string) *memIndexSet {
	m := &memIndexSet{files: map[string]struct{}{}}
	for _, name := range names {
		m.files[name] = struct{}{}
	}
	return m
}

func (m *memIndexSet) ListFiles(_ context.Context, _, _ string, _ bool) ([]storage.IndexFile, error) {
	files := make([]storage.IndexFile, 0, len(m.files))
	for name := range m.files {
		files = append(files, storage.IndexFile{Name: name})
	}
	return files, nil
}

func (m *memIndexSet) PutFile(_ context.Context, _, _, name string, _ io.ReadSeeker) error {
	m.puts++
	if m.puts == m.putFailAt {
		return errors.New("upload failed")
	}
	m.files[name] = struct{}{}
	return nil
}

func (m *memIndexSet) DeleteFile(_ context.Context, _, _, name string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.files, name)
	return nil
}

func (m *memIndexSet) IsFileNotFoundErr(error) bool { return false }
func (m *memIndexSet) IsRetryableErr(error) bool    { return false }

func (m *memIndexSet) names() []string {
	return slices.Sorted(maps.Keys(m.files))
}

type shardedOutputIndex struct {
	CompactedIndex
	dir         string
	shardFactor *int
}

func (s shardedOutputIndex) ToIndexFiles() ([]index.Index, error) {
	return s.files("compacted")
}

func (s shardedOutputIndex) ToShardedIndexFiles(shardFactor int) ([]index.Index, error) {
	if s.shardFactor != nil {
		*s.shardFactor = shardFactor
	}
	return s.files("shard-0", "shard-1")
}

func (s shardedOutputIndex) files(names ...string) ([]index.Index, error) {
	files := make([]index.Index, 0, len(names))
	for _, name := range names {
		path := filepath.Join(s.dir, name)
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			return nil, err
		}
		files = append(files, &outputFile{path: path})
	}
	return files, nil
}

func newShardedCopyIndexSet(t *testing.T, base, sharded *memIndexSet) *indexSet {
	sources, err := base.ListFiles(context.Background(), "", "", false)
	require.NoError(t, err)
	return &indexSet{
		ctx:                 context.Background(),
		tableName:           "table",
		userID:              "user",
		baseIndexSet:        base,
		shardedIndexSet:     sharded,
		compactedIndex:      shardedOutputIndex{dir: t.TempDir()},
		sourceObjects:       sources,
		uploadCompactedDB:   true,
		removeSourceObjects: true,
		logger:              log.NewNopLogger(),
	}
}

func TestIndexSetDone_WritesShardedCopy(t *testing.T) {
	base, sharded := newMemIndexSet("source.gz"), newMemIndexSet()

	require.NoError(t, newShardedCopyIndexSet(t, base, sharded).done())

	require.Equal(t, []string{"compacted.gz"}, base.names())
	require.Equal(t, []string{"shard-0.gz", "shard-1.gz"}, sharded.names())
}

func TestIndexSetDone_ShardedCopyUsesShardFactor(t *testing.T) {
	var got int
	is := newShardedCopyIndexSet(t, newMemIndexSet("source.gz"), newMemIndexSet())
	is.shardFactor = 8
	is.compactedIndex = shardedOutputIndex{dir: t.TempDir(), shardFactor: &got}

	require.NoError(t, is.done())

	require.Equal(t, 8, got)
}

func TestIndexSetDone_ReplacesPreviousShardedCopy(t *testing.T) {
	sharded := newMemIndexSet("old.gz")

	require.NoError(t, newShardedCopyIndexSet(t, newMemIndexSet("source.gz"), sharded).done())

	require.Equal(t, []string{"shard-0.gz", "shard-1.gz"}, sharded.names())
}

func TestIndexSetDone_ShardedCopyUploadFailureKeepsSources(t *testing.T) {
	base, sharded := newMemIndexSet("source.gz"), newMemIndexSet()
	sharded.putFailAt = 2

	err := newShardedCopyIndexSet(t, base, sharded).done()

	require.ErrorIs(t, err, errShardedIndexCopy)
	require.Equal(t, []string{"compacted.gz", "source.gz"}, base.names())
}

func TestIndexSetDone_ShardedCopyDeleteFailureKeepsSources(t *testing.T) {
	base, sharded := newMemIndexSet("source.gz"), newMemIndexSet("old.gz")
	sharded.deleteErr = errors.New("delete failed")

	err := newShardedCopyIndexSet(t, base, sharded).done()

	require.ErrorIs(t, err, errShardedIndexCopy)
	require.Equal(t, []string{"compacted.gz", "source.gz"}, base.names())
}

func TestIndexSetDone_DroppedIndexRemovesShardedCopy(t *testing.T) {
	sharded := newMemIndexSet("old.gz")
	is := newShardedCopyIndexSet(t, newMemIndexSet("source.gz"), sharded)
	is.uploadCompactedDB = false

	require.NoError(t, is.done())

	require.Empty(t, sharded.names())
}

func TestIndexSetDone_UnchangedIndexLeavesShardedCopy(t *testing.T) {
	sharded := newMemIndexSet("old.gz")
	is := newShardedCopyIndexSet(t, newMemIndexSet("source.gz"), sharded)
	is.uploadCompactedDB, is.removeSourceObjects = false, false

	require.NoError(t, is.done())

	require.Equal(t, []string{"old.gz"}, sharded.names())
}
