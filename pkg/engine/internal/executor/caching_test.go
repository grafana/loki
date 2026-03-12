package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// mockCacheStore is an in-memory CacheStore for testing.
type mockCacheStore struct {
	data     map[string][]arrow.RecordBatch
	getCalls int
	setCalls int
}

func newMockCacheStore() *mockCacheStore {
	return &mockCacheStore{data: make(map[string][]arrow.RecordBatch)}
}

func (s *mockCacheStore) Get(_ context.Context, key string) ([]arrow.RecordBatch, bool, error) {
	s.getCalls++
	records, ok := s.data[key]
	return records, ok, nil
}

func (s *mockCacheStore) Set(_ context.Context, key string, records []arrow.RecordBatch) error {
	s.setCalls++
	s.data[key] = records
	return nil
}

var testFields = []arrow.Field{
	{Name: "val", Type: types.Arrow.String},
}

func TestCachingPipeline(t *testing.T) {
	ctx := t.Context()
	store := newMockCacheStore()

	rec1, err := CSVToArrow(testFields, "a\nb\nc")
	require.NoError(t, err)
	rec2, err := CSVToArrow(testFields, "d\ne")
	require.NoError(t, err)

	// First pass: cache miss — inner pipeline is read and results are stored.
	miss := &cachingPipeline{inner: NewBufferedPipeline(rec1, rec2), store: store, key: "test-key"}
	require.NoError(t, miss.Open(ctx))
	require.False(t, miss.hit, "expected cache miss")

	var batches []arrow.RecordBatch
	for {
		rec, err := miss.Read(ctx)
		if errors.Is(err, EOF) {
			break
		}
		require.NoError(t, err)
		batches = append(batches, rec)
	}
	miss.Close()

	require.Len(t, batches, 2, "expected 2 batches from inner pipeline")
	require.Equal(t, 1, store.setCalls, "Set should have been called once on EOF")
	require.Len(t, store.data["test-key"], 2, "cache should contain both batches")

	// Second pass: cache hit — inner pipeline must never be opened.
	hit := &cachingPipeline{inner: &failPipeline{}, store: store, key: "test-key"}
	require.NoError(t, hit.Open(ctx))
	require.True(t, hit.hit, "expected cache hit")

	var cachedBatches []arrow.RecordBatch
	for {
		rec, err := hit.Read(ctx)
		if errors.Is(err, EOF) {
			break
		}
		require.NoError(t, err)
		cachedBatches = append(cachedBatches, rec)
	}
	hit.Close()

	require.Len(t, cachedBatches, len(batches), "cached result should have same number of batches")
}

// failPipeline panics if Open or Read is called; used to assert inner pipeline
// is never accessed on a cache hit.
type failPipeline struct {
	closed bool
}

func (f *failPipeline) Open(_ context.Context) error {
	panic("inner pipeline must not be opened on a cache hit")
}

func (f *failPipeline) Read(_ context.Context) (arrow.RecordBatch, error) {
	panic("inner pipeline must not be read on a cache hit")
}

func (f *failPipeline) Close() { f.closed = true }
