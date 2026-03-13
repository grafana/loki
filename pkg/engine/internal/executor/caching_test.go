package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

var testFields = []arrow.Field{
	{Name: "val", Type: types.Arrow.String},
}

func TestCachingPipeline(t *testing.T) {
	ctx := t.Context()
	c := newMockCache()

	rec1, err := CSVToArrow(testFields, "a\nb\nc")
	require.NoError(t, err)
	rec2, err := CSVToArrow(testFields, "d\ne")
	require.NoError(t, err)

	// First pass: cache miss — inner pipeline is read and results are stored.
	miss := newCachingPipeline(c, NewBufferedPipeline(rec1, rec2), "test-key")
	require.NoError(t, miss.Open(ctx))
	require.False(t, miss.(*cachingPipeline).hit, "expected cache miss")

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
	require.Equal(t, 1, c.setCalls, "Store should have been called once on EOF")
	require.Contains(t, c.data, "test-key", "cache should contain the key")

	// Second pass: cache hit — inner pipeline must never be opened.
	hit := newCachingPipeline(c, &failPipeline{}, "test-key")
	require.NoError(t, hit.Open(ctx))
	require.True(t, hit.(*cachingPipeline).hit, "expected cache hit")

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

// mockCache is an in-memory cache.Cache for testing the caching pipeline.
type mockCache struct {
	data     map[string][]byte
	getCalls int
	setCalls int
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string][]byte)}
}

func (m *mockCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	m.setCalls++
	for i, key := range keys {
		m.data[key] = bufs[i]
	}
	return nil
}

func (m *mockCache) Fetch(_ context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	m.getCalls++
	for _, key := range keys {
		if buf, ok := m.data[key]; ok {
			found = append(found, key)
			bufs = append(bufs, buf)
		} else {
			missing = append(missing, key)
		}
	}
	return
}

func (m *mockCache) Stop() {}

func (m *mockCache) GetCacheType() stats.CacheType { return stats.ResultCache }
