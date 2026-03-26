package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
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
	miss := newCachingPipeline(c, NewBufferedPipeline(rec1, rec2), "test-key", ^uint64(0), "", log.NewNopLogger())
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
	require.Contains(t, c.data, cache.HashKey("test-key"), "cache should contain the key")

	// Second pass: cache hit — inner pipeline must never be opened.
	hit := newCachingPipeline(c, &failPipeline{}, "test-key", ^uint64(0), "", log.NewNopLogger())
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

func TestRecordEncoderDecoder(t *testing.T) {
	rec1, err := CSVToArrow(testFields, "x\ny")
	require.NoError(t, err)

	t.Run("no compression round-trip", func(t *testing.T) {
		enc := newRecordEncoder("")
		require.NoError(t, enc.Append(rec1))
		require.Greater(t, enc.Size(), uint64(0))

		payload, err := enc.Commit()
		require.NoError(t, err)
		require.NotEmpty(t, payload)

		dec, err := newRecordDecoder(payload)
		require.NoError(t, err)
		require.Equal(t, 1, dec.Len())

		got, err := dec.Next()
		require.NoError(t, err)
		require.Equal(t, rec1.NumRows(), got.NumRows())

		_, err = dec.Next()
		require.ErrorIs(t, err, EOF)
	})

	t.Run("snappy round-trip", func(t *testing.T) {
		enc := newRecordEncoder("snappy")
		require.NoError(t, enc.Append(rec1))

		payload, err := enc.Commit()
		require.NoError(t, err)

		dec, err := newRecordDecoder(payload)
		require.NoError(t, err)
		require.Equal(t, 1, dec.Len())

		got, err := dec.Next()
		require.NoError(t, err)
		require.Equal(t, rec1.NumRows(), got.NumRows())
	})

	t.Run("empty result", func(t *testing.T) {
		enc := newRecordEncoder("snappy")
		payload, err := enc.Commit()
		require.NoError(t, err)

		dec, err := newRecordDecoder(payload)
		require.NoError(t, err)
		require.Equal(t, 0, dec.Len())

		_, err = dec.Next()
		require.ErrorIs(t, err, EOF)
	})

	t.Run("zero-length buffer is treated as empty", func(t *testing.T) {
		dec, err := newRecordDecoder([]byte{})
		require.NoError(t, err)
		require.Equal(t, 0, dec.Len())
	})
}

func TestCachingPipelineMaxCacheableSize(t *testing.T) {
	ctx := t.Context()

	rows := make([]string, 30)
	for i := range rows {
		rows[i] = "abcdefgh"
	}
	csv := ""
	for i, r := range rows {
		if i > 0 {
			csv += "\n"
		}
		csv += r
	}
	rec, err := CSVToArrow(testFields, csv)
	require.NoError(t, err)

	// Measure the actual encoded frame size.
	probe := newRecordEncoder("")
	require.NoError(t, probe.Append(rec))
	actualSize := probe.Size()
	require.Greater(t, int(actualSize), 0)

	t.Run("limit of zero caches only empty responses", func(t *testing.T) {
		mc := newMockCache()

		// Non-empty record: should NOT be cached.
		p := newCachingPipeline(mc, NewBufferedPipeline(rec), "key", 0, "", log.NewNopLogger())
		require.NoError(t, p.Open(ctx))
		drainPipeline(t, p)
		p.Close()
		require.Equal(t, 0, mc.setCalls, "non-empty record must not be cached when maxSizeBytes==0")

		// Empty response (no records): should be cached.
		p2 := newCachingPipeline(mc, NewBufferedPipeline(), "empty-key", 0, "", log.NewNopLogger())
		require.NoError(t, p2.Open(ctx))
		drainPipeline(t, p2)
		p2.Close()
		require.Equal(t, 1, mc.setCalls, "empty response must be cached even when maxSizeBytes==0")
	})

	for _, tc := range []struct {
		name           string
		compression    string
	}{
		{name: "without snappy", compression: ""},
		{name: "with snappy", compression: "snappy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Measure encoded size for this compression.
			enc := newRecordEncoder(tc.compression)
			require.NoError(t, enc.Append(rec))
			encodedSize := enc.Size()

			t.Run("limit below encoded size", func(t *testing.T) {
				mc := newMockCache()
				p := newCachingPipeline(mc, NewBufferedPipeline(rec), "key", encodedSize-1, tc.compression, log.NewNopLogger())
				require.NoError(t, p.Open(ctx))
				drainPipeline(t, p)
				p.Close()
				require.Equal(t, 0, mc.setCalls, "must not cache when entry exceeds max size")
			})

			t.Run("limit at encoded size", func(t *testing.T) {
				mc := newMockCache()
				p := newCachingPipeline(mc, NewBufferedPipeline(rec), "key", encodedSize, tc.compression, log.NewNopLogger())
				require.NoError(t, p.Open(ctx))
				drainPipeline(t, p)
				p.Close()
				require.Equal(t, 1, mc.setCalls, "must cache when entry fits within max size")

				// Verify round-trip.
				hit := newCachingPipeline(mc, &failPipeline{}, "key", encodedSize, tc.compression, log.NewNopLogger())
				require.NoError(t, hit.Open(ctx))
				require.True(t, hit.(*cachingPipeline).hit)
				batches := drainPipeline(t, hit)
				hit.Close()
				require.Len(t, batches, 1)
				require.Equal(t, rec.NumRows(), batches[0].NumRows())
			})
		})
	}
}

func TestCachingPipelinePassthrough(t *testing.T) {
	ctx := t.Context()

	rec1, err := CSVToArrow(testFields, "a\nb")
	require.NoError(t, err)
	rec2, err := CSVToArrow(testFields, "c\nd")
	require.NoError(t, err)
	rec3, err := CSVToArrow(testFields, "e\nf")
	require.NoError(t, err)

	for _, tc := range []struct {
		name           string
		records        []arrow.RecordBatch
		wantBatches    int
		wantStoreCalls int
		wantKeyInCache bool
		cacheKey       string
	}{
		{
			name:           "non-empty response is not cached",
			records:        []arrow.RecordBatch{rec1, rec2, rec3},
			wantBatches:    3,
			wantStoreCalls: 0,
			wantKeyInCache: false,
			cacheKey:       "pt-key",
		},
		{
			name:           "empty response is cached",
			records:        nil,
			wantBatches:    0,
			wantStoreCalls: 1,
			wantKeyInCache: true,
			cacheKey:       "empty-key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mc := newMockCache()

			p := newCachingPipeline(mc, NewBufferedPipeline(tc.records...), tc.cacheKey, 0, "", log.NewNopLogger())
			require.NoError(t, p.Open(ctx))
			batches := drainPipeline(t, p)
			p.Close()

			require.Len(t, batches, tc.wantBatches)
			require.Equal(t, tc.wantStoreCalls, mc.setCalls)
			require.Equal(t, tc.wantKeyInCache, mc.data[cache.HashKey(tc.cacheKey)] != nil)
		})
	}
}

// drainPipeline reads all records from p until EOF and returns them.
func drainPipeline(t *testing.T, p Pipeline) []arrow.RecordBatch {
	t.Helper()
	var batches []arrow.RecordBatch
	for {
		rec, err := p.Read(context.Background())
		if errors.Is(err, EOF) {
			break
		}
		require.NoError(t, err)
		batches = append(batches, rec)
	}
	return batches
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

func (m *mockCache) GetCacheType() stats.CacheType { return stats.TaskResultCache }
