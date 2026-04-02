package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var testFields = []arrow.Field{
	{Name: "val", Type: types.Arrow.String},
}

var testCacheStats = CacheStats{
	Hits:    xcap.TaskCacheHits,
	Misses:  xcap.TaskCacheMisses,
	Batches: xcap.TaskCacheBatches,
	Rows:    xcap.TaskCacheRows,
	Bytes:   xcap.TaskCacheBytes,
}

func TestCachingPipeline(t *testing.T) {
	ctx := t.Context()
	c := newMockCache()

	rec1, err := CSVToArrow(testFields, "a\nb\nc")
	require.NoError(t, err)
	rec2, err := CSVToArrow(testFields, "d\ne")
	require.NoError(t, err)

	// First pass: cache miss — inner pipeline is read and results are stored.
	miss := newCachingPipeline(c, NewBufferedPipeline(rec1, rec2), "test-key", ^uint64(0), "", log.NewNopLogger(), testCacheStats, "mock")
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
	hit := newCachingPipeline(c, &failPipeline{}, "test-key", ^uint64(0), "", log.NewNopLogger(), testCacheStats, "mock")
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

func TestCachingPipelineCaching(t *testing.T) {
	ctx := t.Context()

	rows := make([]string, 30)
	for i := range rows {
		rows[i] = "abcdefgh"
	}
	bigCSV := strings.Join(rows, "\n")
	bigRec, err := CSVToArrow(testFields, bigCSV)
	require.NoError(t, err)

	rec1, err := CSVToArrow(testFields, "a\nb")
	require.NoError(t, err)
	rec2, err := CSVToArrow(testFields, "c\nd")
	require.NoError(t, err)
	rec3, err := CSVToArrow(testFields, "e\nf")
	require.NoError(t, err)

	sizeOf := func(rec arrow.RecordBatch, compression string) uint64 {
		enc := newRecordEncoder(compression)
		_ = enc.Append(rec)
		return enc.Size()
	}

	for _, tc := range []struct {
		name              string
		records           []arrow.RecordBatch
		maxSizeBytes      uint64
		compression       string
		wantCacheSetCalls int
		wantBatches       int
		wantRoundTrip     bool
	}{
		{
			name:              "non-empty response not cached at limit zero",
			records:           []arrow.RecordBatch{rec1, rec2, rec3},
			maxSizeBytes:      0,
			wantCacheSetCalls: 0,
			wantBatches:       3,
		},
		{
			name:              "empty response cached at limit zero",
			records:           nil,
			maxSizeBytes:      0,
			wantCacheSetCalls: 1,
			wantBatches:       0,
		},
		{
			name:              "no compression: limit below encoded size",
			records:           []arrow.RecordBatch{bigRec},
			maxSizeBytes:      sizeOf(bigRec, "") - 1,
			wantCacheSetCalls: 0,
			wantBatches:       1,
		},
		{
			name:              "no compression: limit at encoded size",
			records:           []arrow.RecordBatch{bigRec},
			maxSizeBytes:      sizeOf(bigRec, ""),
			wantCacheSetCalls: 1,
			wantBatches:       1,
			wantRoundTrip:     true,
		},
		{
			name:              "snappy: limit below encoded size",
			records:           []arrow.RecordBatch{bigRec},
			maxSizeBytes:      sizeOf(bigRec, "snappy") - 1,
			compression:       "snappy",
			wantCacheSetCalls: 0,
			wantBatches:       1,
		},
		{
			name:              "snappy: limit at encoded size",
			records:           []arrow.RecordBatch{bigRec},
			maxSizeBytes:      sizeOf(bigRec, "snappy"),
			compression:       "snappy",
			wantCacheSetCalls: 1,
			wantBatches:       1,
			wantRoundTrip:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mc := newMockCache()

			p := newCachingPipeline(mc, NewBufferedPipeline(tc.records...), "key", tc.maxSizeBytes, tc.compression, log.NewNopLogger(), testCacheStats, "mock")
			require.NoError(t, p.Open(ctx))
			batches := drainPipeline(t, p)
			p.Close()

			require.Len(t, batches, tc.wantBatches)
			require.Equal(t, tc.wantCacheSetCalls, mc.setCalls)

			if tc.wantRoundTrip {
				hit := newCachingPipeline(mc, &failPipeline{}, "key", tc.maxSizeBytes, tc.compression, log.NewNopLogger(), testCacheStats, "mock")
				require.NoError(t, hit.Open(ctx))
				require.True(t, hit.(*cachingPipeline).hit)
				cached := drainPipeline(t, hit)
				hit.Close()
				require.Len(t, cached, tc.wantBatches)
				require.Equal(t, tc.records[0].NumRows(), cached[0].NumRows())
			}
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

const (
	benchRecordCount   = 1_000
	benchRowsPerRecord = 8_192
	// Each value is 64 bytes → 8192 rows × 64 B ≈ 512 KiB raw per record
	// 1000 records × 512 KiB ≈ 500 MiB total before compression
	benchValueLen = 64
)

// makeBenchRecords generates n Arrow records using a string column. Each record
// has rowsPerRecord rows; every value is benchValueLen bytes long and encodes
// the row index so the data is non-trivially compressible but not all identical.
func makeBenchRecords(tb testing.TB, n, rowsPerRecord int) []arrow.RecordBatch {
	tb.Helper()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "val", Type: arrow.BinaryTypes.String},
	}, nil)

	records := make([]arrow.RecordBatch, n)
	for i := range records {
		bldr := array.NewStringBuilder(memory.NewGoAllocator())
		for r := 0; r < rowsPerRecord; r++ {
			s := fmt.Sprintf("rec-%06d-row-%06d-%040x", i, r, r*1_000_003+i)
			if len(s) < benchValueLen {
				s += fmt.Sprintf("%0*d", benchValueLen-len(s), 0)
			} else {
				s = s[:benchValueLen]
			}
			bldr.Append(s)
		}
		col := bldr.NewArray()
		records[i] = array.NewRecordBatch(schema, []arrow.Array{col}, int64(rowsPerRecord))
		col.Release()
		bldr.Release()
	}
	return records
}

// totalRawBytes is a rough measure of uncompressed data fed to b.SetBytes.
const totalRawBytes = benchRecordCount * benchRowsPerRecord * benchValueLen

func BenchmarkEncode_WholeSnappy(b *testing.B) {
	records := makeBenchRecords(b, benchRecordCount, benchRowsPerRecord)

	// Whole-buffer snappy: encode all records into one uncompressed framed
	// buffer, then compress the entire buffer in a single snappy.Encode call.
	// This mirrors the OLD arrowCacheAdapter approach.
	encodeWhole := func() ([]byte, error) {
		enc := newRecordEncoder("") // no per-record compression
		for _, rec := range records {
			if err := enc.Append(rec); err != nil {
				return nil, err
			}
		}
		payload, err := enc.Commit()
		if err != nil {
			return nil, err
		}
		return snappy.Encode(nil, payload), nil
	}

	b.SetBytes(totalRawBytes)
	b.ResetTimer()

	var lastEncoded []byte
	for i := 0; i < b.N; i++ {
		enc, err := encodeWhole()
		require.NoError(b, err)
		lastEncoded = enc
	}
	b.ReportMetric(float64(len(lastEncoded)), "compressed_bytes")
}

func BenchmarkEncode_PerRecordSnappy(b *testing.B) {
	records := makeBenchRecords(b, benchRecordCount, benchRowsPerRecord)

	b.SetBytes(totalRawBytes)
	b.ResetTimer()

	var lastEncoded []byte
	for i := 0; i < b.N; i++ {
		enc := newRecordEncoder("snappy")
		for _, rec := range records {
			require.NoError(b, enc.Append(rec))
		}
		payload, err := enc.Commit()
		require.NoError(b, err)
		lastEncoded = payload
	}
	b.ReportMetric(float64(len(lastEncoded)), "compressed_bytes")
}

func BenchmarkDecode_WholeSnappy(b *testing.B) {
	records := makeBenchRecords(b, benchRecordCount, benchRowsPerRecord)
	enc := newRecordEncoder("")
	for _, rec := range records {
		require.NoError(b, enc.Append(rec))
	}
	payload, err := enc.Commit()
	require.NoError(b, err)
	encoded := snappy.Encode(nil, payload)

	b.SetBytes(totalRawBytes)
	b.ReportMetric(float64(len(encoded)), "compressed_bytes")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf, err := snappy.Decode(nil, encoded)
		require.NoError(b, err)
		dec, err := newRecordDecoder(buf)
		require.NoError(b, err)
		for {
			rec, err := dec.Next()
			if err != nil {
				break
			}
			_ = rec
		}
	}
}

func BenchmarkDecode_PerRecordSnappy(b *testing.B) {
	records := makeBenchRecords(b, benchRecordCount, benchRowsPerRecord)
	enc := newRecordEncoder("snappy")
	for _, rec := range records {
		require.NoError(b, enc.Append(rec))
	}
	encoded, err := enc.Commit()
	require.NoError(b, err)

	b.SetBytes(totalRawBytes)
	b.ReportMetric(float64(len(encoded)), "compressed_bytes")
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dec, err := newRecordDecoder(encoded)
		require.NoError(b, err)
		for {
			rec, err := dec.Next()
			if err != nil {
				break
			}
			_ = rec
		}
	}
}
