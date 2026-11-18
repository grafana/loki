package rangeio_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

func FuzzReadRanges(f *testing.F) {
	corpusEntries := []struct {
		seed           int64
		maxParallelism int
		dataSize       int
		numRanges      int
		coalesceSize   int
		maxRange       int
		minRange       int
	}{
		{seed: 123456, maxParallelism: 16, dataSize: 100_000_000, numRanges: 25, coalesceSize: 1_000_000, maxRange: 10_000_000, minRange: 1_000_000},
		{seed: 234567, maxParallelism: 8, dataSize: 50_000_000, numRanges: 10, coalesceSize: 250_000, maxRange: 5_000_000, minRange: 500_000},
		{seed: 345678, maxParallelism: 4, dataSize: 25_000_000, numRanges: 5, coalesceSize: 125_000, maxRange: 2_500_000, minRange: 250_000},
		{seed: 901234, maxParallelism: 2, dataSize: 10_000_000, numRanges: 2, coalesceSize: 50_000, maxRange: 1_000_000, minRange: 100_000},
		{seed: 567890, maxParallelism: 1, dataSize: 1_000_000, numRanges: 1, coalesceSize: 25_000, maxRange: 100_000, minRange: 1_000},
	}
	for _, ent := range corpusEntries {
		f.Add(ent.seed, ent.maxParallelism, ent.dataSize, ent.numRanges, ent.coalesceSize, ent.maxRange, ent.minRange)
	}

	f.Fuzz(func(t *testing.T, seed int64, maxParallelism int, dataSize int, numRanges int, coalesceSize int, maxRange int, minRange int) {
		switch {
		case dataSize <= 0, numRanges <= 0, maxRange <= 0, minRange <= 0, maxParallelism <= 0:
			t.Skip("invalid input")
		case coalesceSize < 0:
			// Unlike the above, coalesce can be zero to indicate to not
			// coalesce ranges regardless of the gap size.
			t.Skip("coalesce size must be non-negative")
		case minRange > maxRange:
			t.Skip("invalid range")
		case numRanges > math.MaxInt32:
			t.Skip("skipping too many ranges")
		case dataSize > (1 << 30):
			t.Skip("skipping data sizes larger than 1GiB")
		case maxParallelism > runtime.NumCPU():
			t.Skip("skipping instances with too many goroutines")
		}

		rnd := rand.New(rand.NewSource(seed))

		testData := make([]byte, dataSize)
		rnd.Read(testData)

		ranges := make([]rangeio.Range, numRanges)
		for i := range ranges {
			// Pick a range to read anywhere within dataSize. The length will
			// always be trimmed up to the end of dataRange from the start of
			// the offset.
			startOffset := rnd.Intn(dataSize + 1)
			endOffset := startOffset + rnd.Intn(dataSize-startOffset+1)

			ranges[i] = rangeio.Range{
				Offset: int64(startOffset),
				Data:   make([]byte, endOffset-startOffset),
			}
		}

		cfg := rangeio.Config{
			MaxParallelism: maxParallelism,
			CoalesceSize:   coalesceSize,
			MinRangeSize:   minRange,
			MaxRangeSize:   maxRange,
		}
		ctx := rangeio.WithConfig(t.Context(), &cfg)
		reader := &countedReader{Reader: byteRangeReader(testData)}
		stats, err := rangeio.ReadRanges(ctx, reader, ranges)
		require.NoError(t, err)

		// Test that all of our ranges are correctly filled.
		for _, r := range ranges {
			expect := testData[r.Offset:min(len(testData), int(r.Offset+r.Len()))]
			assert.Equal(t, r.Data, expect, "range was not filled correctly")
		}

		// Test that the stats count the reads that the reader got.
		require.Equal(t, int64(stats.Requests), reader.calls.Load())
	})
}

func TestReadRanges(t *testing.T) {
	// Generate a random slice of 100 MiB
	rnd := rand.New(rand.NewSource(0))
	testData := make([]byte, 100*(1<<20))
	_, err := rnd.Read(testData)
	require.NoError(t, err)

	type testRange struct{ Off, Len int }

	tt := []struct {
		name   string
		ranges []testRange

		// wantRequests is the number of reads that the default config makes. It merges ranges less
		// than 1 MiB apart into one read. It splits a range above 8 MiB in halves, and then splits
		// each half again to approach the maximum parallelism.
		wantRequests int
	}{
		{
			name:         "full range",
			ranges:       []testRange{{Off: 0, Len: len(testData)}},
			wantRequests: 4,
		},
		{
			name:         "partial range (from start)",
			ranges:       []testRange{{Off: 0, Len: len(testData) / 2}},
			wantRequests: 4,
		},
		{
			name:         "partial range (from middle)",
			ranges:       []testRange{{Off: len(testData) / 2, Len: len(testData) / 2}},
			wantRequests: 4,
		},
		{
			name: "non-overlapping adjacent ranges",
			ranges: []testRange{
				{Off: 100, Len: 50}, // [100, 150)
				{Off: 150, Len: 50}, // [150, 200)
				{Off: 200, Len: 50}, // [200, 250)
			},
			wantRequests: 1,
		},
		{
			name: "non-overlapping non-adjacent ranges",
			ranges: []testRange{
				{Off: 100, Len: 50},   // [100, 150)
				{Off: 1000, Len: 200}, // [1000, 1200)
				{Off: 5000, Len: 100}, // [5000, 5100)
			},
			wantRequests: 1,
		},
		{
			name: "ranges farther apart than the coalesce size",
			ranges: []testRange{
				{Off: 100, Len: 50},       // [100, 150)
				{Off: 50 << 20, Len: 100}, // [50 MiB, 50 MiB + 100)
			},
			wantRequests: 2,
		},
		{
			name: "overlapping ranges",
			ranges: []testRange{
				{Off: 100, Len: 100}, // [100, 200)
				{Off: 150, Len: 100}, // [150, 250)
				{Off: 200, Len: 100}, // [200, 300)
			},
			wantRequests: 1,
		},
		{
			name: "range overlaps multiple ranges",
			ranges: []testRange{
				{Off: 100, Len: 350}, // [100, 450)
				{Off: 150, Len: 100}, // [150, 250)
				{Off: 200, Len: 100}, // [200, 300)
			},
			wantRequests: 1,
		},
		{
			name: "unsorted ranges",
			ranges: []testRange{
				{Off: 1000, Len: 100}, // [1000, 1100)
				{Off: 100, Len: 50},   // [100, 150)
				{Off: 500, Len: 75},   // [500, 575)
			},
			wantRequests: 1,
		},
		{
			name: "duplicate ranges",
			ranges: []testRange{
				{Off: 500, Len: 100}, // [500, 600)
				{Off: 500, Len: 100}, // [500, 600)
			},
			wantRequests: 1,
		},
		{
			name: "empty range",
			ranges: []testRange{
				{Off: 100, Len: 0},  // no data
				{Off: 200, Len: 50}, // [200, 250)
			},
			wantRequests: 1,
		},
		{
			name: "only an empty range",
			ranges: []testRange{
				{Off: 100, Len: 0}, // no data
			},
			wantRequests: 0,
		},
	}

	for _, tc := range tt {
		ranges := make([]rangeio.Range, len(tc.ranges))
		for i := range tc.ranges {
			ranges[i] = rangeio.Range{
				Offset: int64(tc.ranges[i].Off),
				Data:   make([]byte, tc.ranges[i].Len),
			}
		}

		t.Run(tc.name, func(t *testing.T) {
			reader := &countedReader{Reader: byteRangeReader(testData)}

			stats, err := rangeio.ReadRanges(t.Context(), reader, ranges)
			require.NoError(t, err)

			// Validate our ranges were read correctly.
			for _, r := range ranges {
				expect := testData[r.Offset:min(len(testData), int(r.Offset+r.Len()))]
				assert.Equal(t, expect, r.Data, "range was not filled correctly")
			}

			// Validate the stats count the reads that the reader got.
			require.Equal(t, tc.wantRequests, stats.Requests)
			require.Equal(t, int64(stats.Requests), reader.calls.Load())
		})
	}

	t.Run("returns the number of reads even when a read fails", func(t *testing.T) {
		ranges := []rangeio.Range{{Offset: 0, Data: make([]byte, 4)}}

		stats, err := rangeio.ReadRanges(t.Context(), failingReader{}, ranges)

		require.EqualError(t, err, "read failed")
		require.Equal(t, 1, stats.Requests)
	})
}

type byteRangeReader []byte

var _ rangeio.Reader = (byteRangeReader)(nil)

func (b byteRangeReader) ReadRange(_ context.Context, r rangeio.Range) (int, error) {
	if r.Offset >= int64(len(b)) {
		return 0, io.EOF
	}

	availBytes := len(b) - int(r.Offset)
	adjustedLength := min(availBytes, len(r.Data))

	n := copy(r.Data, b[r.Offset:int(r.Offset)+adjustedLength])
	if n != adjustedLength {
		return n, fmt.Errorf("unexpected short read")
	} else if n < len(r.Data) {
		return n, io.EOF
	}

	return n, nil
}

// countedReader counts the ReadRange calls on the embedded reader.
type countedReader struct {
	rangeio.Reader
	calls atomic.Int64
}

func (r *countedReader) ReadRange(ctx context.Context, rg rangeio.Range) (int, error) {
	r.calls.Inc()
	return r.Reader.ReadRange(ctx, rg)
}

// failingReader fails every read.
type failingReader struct{}

func (failingReader) ReadRange(context.Context, rangeio.Range) (int, error) {
	return 0, errors.New("read failed")
}
