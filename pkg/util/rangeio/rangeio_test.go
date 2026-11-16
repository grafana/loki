package rangeio_test

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"math"
	"math/rand"
	"runtime"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

// readers are the functions that fill ranges from data.
var readers = map[string]func(ctx context.Context, data []byte, ranges []rangeio.Range) error{
	"ReadRanges": func(ctx context.Context, data []byte, ranges []rangeio.Range) error {
		return rangeio.ReadRanges(ctx, byteRangeReader(data), ranges)
	},
	"ReadRangesStream": func(ctx context.Context, data []byte, ranges []rangeio.Range) error {
		return rangeio.ReadRangesStream(ctx, newByteStreamOpener(data), ranges)
	},
}

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

		for name, read := range readers {
			ranges := cloneRanges(ranges)
			require.NoError(t, read(ctx, testData, ranges), name)

			// Test that all of our ranges are correctly filled.
			for _, r := range ranges {
				expect := testData[r.Offset:min(len(testData), int(r.Offset+r.Len()))]
				assert.Equal(t, r.Data, expect, "range was not filled correctly by %s", name)
			}
		}
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
	}{
		{
			name:   "full range",
			ranges: []testRange{{Off: 0, Len: len(testData)}},
		},
		{
			name:   "partial range (from start)",
			ranges: []testRange{{Off: 0, Len: len(testData) / 2}},
		},
		{
			name:   "partial range (from middle)",
			ranges: []testRange{{Off: len(testData) / 2, Len: len(testData) / 2}},
		},
		{
			name: "non-overlapping adjacent ranges",
			ranges: []testRange{
				{Off: 100, Len: 50}, // [100, 150)
				{Off: 150, Len: 50}, // [150, 200)
				{Off: 200, Len: 50}, // [200, 250)
			},
		},
		{
			name: "non-overlapping non-adjacent ranges",
			ranges: []testRange{
				{Off: 100, Len: 50},   // [100, 150)
				{Off: 1000, Len: 200}, // [1000, 1200)
				{Off: 5000, Len: 100}, // [5000, 5100)
			},
		},
		{
			name: "overlapping ranges",
			ranges: []testRange{
				{Off: 100, Len: 100}, // [100, 200)
				{Off: 150, Len: 100}, // [150, 250)
				{Off: 200, Len: 100}, // [200, 300)
			},
		},
		{
			name: "range overlaps multiple ranges",
			ranges: []testRange{
				{Off: 100, Len: 350}, // [100, 450)
				{Off: 150, Len: 100}, // [150, 250)
				{Off: 200, Len: 100}, // [200, 300)
			},
		},
		{
			name: "unsorted ranges",
			ranges: []testRange{
				{Off: 1000, Len: 100}, // [1000, 1100)
				{Off: 100, Len: 50},   // [100, 150)
				{Off: 500, Len: 75},   // [500, 575)
			},
		},
		{
			name: "duplicate ranges",
			ranges: []testRange{
				{Off: 500, Len: 100}, // [500, 600)
				{Off: 500, Len: 100}, // [500, 600)
			},
		},
		{
			name: "empty range",
			ranges: []testRange{
				{Off: 100, Len: 0},  // no data
				{Off: 200, Len: 50}, // [200, 250)
			},
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
			for name, read := range readers {
				t.Run(name, func(t *testing.T) {
					ranges := cloneRanges(ranges)
					require.NoError(t, read(t.Context(), testData, ranges))

					// Validate our ranges were read correctly.
					for _, r := range ranges {
						expect := testData[r.Offset:min(len(testData), int(r.Offset+r.Len()))]
						assert.Equal(t, expect, r.Data, "range was not filled correctly")
					}
				})
			}
		})
	}
}

func TestReadRangesStream(t *testing.T) {
	const dataSize = 2000

	// Small sizes make ranges of up to 150 bytes coalesce, split and run in parallel.
	cfg := rangeio.Config{MaxParallelism: 4, CoalesceSize: 20, MaxRangeSize: 64, MinRangeSize: 16}

	rnd := rand.New(rand.NewSource(1))
	data := make([]byte, dataSize)
	_, err := rnd.Read(data)
	require.NoError(t, err)

	t.Run("fills random ranges, starts and ends each stream on a needed byte, and drains and closes every stream", func(t *testing.T) {
		for range 300 {
			// Ranges can be empty, overlap, repeat, touch, span several streams or lie far apart.
			ranges := make([]rangeio.Range, 1+rnd.Intn(12))
			for i := range ranges {
				offset := rnd.Intn(dataSize)
				ranges[i] = rangeio.Range{Offset: int64(offset), Data: make([]byte, min(rnd.Intn(150), dataSize-offset))}
			}
			opener := newByteStreamOpener(data)

			require.NoError(t, rangeio.ReadRangesStream(rangeio.WithConfig(t.Context(), &cfg), opener, ranges))

			needed := make([]bool, dataSize)
			for _, r := range ranges {
				require.Equal(t, data[r.Offset:r.Offset+r.Len()], r.Data)
				for i := range r.Data {
					needed[r.Offset+int64(i)] = true
				}
			}
			for _, s := range opener.streams {
				require.True(t, needed[s.offset], "stream at %d starts on a byte that no range needs", s.offset)
				require.True(t, needed[s.offset+s.length-1], "stream at %d ends on a byte that no range needs", s.offset)
				require.Zero(t, s.reader.Len(), "stream at %d has unread bytes", s.offset)
				require.True(t, s.closed, "stream at %d is not closed", s.offset)
			}
		}
	})

	t.Run("returns io.EOF after it fills the other ranges when a range lies beyond the end of the data", func(t *testing.T) {
		ranges := []rangeio.Range{
			{Offset: 100, Data: make([]byte, 10)},
			{Offset: dataSize + 500, Data: make([]byte, 10)},
		}

		err := rangeio.ReadRangesStream(rangeio.WithConfig(t.Context(), &cfg), newByteStreamOpener(data), ranges)

		require.Equal(t, io.EOF, err)
		require.Equal(t, data[100:110], ranges[0].Data)
	})

	t.Run("returns io.ErrUnexpectedEOF when a stream ends inside a range", func(t *testing.T) {
		ranges := []rangeio.Range{{Offset: dataSize - 5, Data: make([]byte, 10)}}

		err := rangeio.ReadRangesStream(rangeio.WithConfig(t.Context(), &cfg), newByteStreamOpener(data), ranges)

		require.Equal(t, io.ErrUnexpectedEOF, err)
	})

	t.Run("returns io.ErrUnexpectedEOF when a stream ends before a later range of the same stream", func(t *testing.T) {
		ranges := []rangeio.Range{
			{Offset: dataSize - 10, Data: make([]byte, 10)},
			{Offset: dataSize + 5, Data: make([]byte, 10)},
		}

		err := rangeio.ReadRangesStream(rangeio.WithConfig(t.Context(), &cfg), newByteStreamOpener(data), ranges)

		require.Equal(t, io.ErrUnexpectedEOF, err)
	})

	t.Run("opens streams that start and end on needed bytes when a split falls in a gap or an empty range ends a group", func(t *testing.T) {
		ranges := []rangeio.Range{
			{Offset: 0, Data: make([]byte, 20)},
			{Offset: 30, Data: make([]byte, 20)},
			{Offset: 60},
		}
		opener := newByteStreamOpener(data)

		require.NoError(t, rangeio.ReadRangesStream(rangeio.WithConfig(t.Context(), &cfg), opener, ranges))

		require.Equal(t, [][2]int64{{0, 20}, {30, 20}}, opener.spans())
	})

	t.Run("opens no stream for a chunk that holds only empty ranges", func(t *testing.T) {
		ranges := []rangeio.Range{
			{Offset: 0, Data: make([]byte, 10)},
			{Offset: 20}, {Offset: 40}, {Offset: 60}, {Offset: 80},
		}
		opener := newByteStreamOpener(data)

		require.NoError(t, rangeio.ReadRangesStream(rangeio.WithConfig(t.Context(), &cfg), opener, ranges))

		require.Equal(t, data[:10], ranges[0].Data)
		require.Equal(t, [][2]int64{{0, 10}}, opener.spans())
	})

	t.Run("opens no stream when every range is empty", func(t *testing.T) {
		opener := newByteStreamOpener(data)

		require.NoError(t, rangeio.ReadRangesStream(rangeio.WithConfig(t.Context(), &cfg), opener, []rangeio.Range{{Offset: 0}, {Offset: 10}}))

		require.Empty(t, opener.spans())
	})
}

func cloneRanges(ranges []rangeio.Range) []rangeio.Range {
	out := make([]rangeio.Range, len(ranges))
	for i, r := range ranges {
		out[i] = rangeio.Range{Offset: r.Offset, Data: make([]byte, len(r.Data))}
	}
	return out
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

// byteStreamOpener opens streams over a byte slice, like a bucket does. It records every stream.
type byteStreamOpener struct {
	data []byte

	mtx     sync.Mutex
	streams []*byteStream
}

type byteStream struct {
	offset, length int64
	reader         *bytes.Reader
	closed         bool
}

func newByteStreamOpener(data []byte) *byteStreamOpener { return &byteStreamOpener{data: data} }

func (o *byteStreamOpener) OpenRange(_ context.Context, offset, length int64) (io.ReadCloser, error) {
	var content []byte
	if offset < int64(len(o.data)) {
		content = o.data[offset:min(offset+length, int64(len(o.data)))]
	}
	s := &byteStream{offset: offset, length: length, reader: bytes.NewReader(content)}

	o.mtx.Lock()
	defer o.mtx.Unlock()
	o.streams = append(o.streams, s)
	return s, nil
}

// spans returns the offset and the length of every stream, sorted by offset.
func (o *byteStreamOpener) spans() [][2]int64 {
	o.mtx.Lock()
	defer o.mtx.Unlock()

	spans := make([][2]int64, len(o.streams))
	for i, s := range o.streams {
		spans[i] = [2]int64{s.offset, s.length}
	}
	slices.SortFunc(spans, func(a, b [2]int64) int { return cmp.Compare(a[0], b[0]) })
	return spans
}

func (s *byteStream) Read(p []byte) (int, error) { return s.reader.Read(p) }

func (s *byteStream) Close() error {
	s.closed = true
	return nil
}
