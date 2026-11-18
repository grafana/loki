package dataobj

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"io"
	"math"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

const backendSource = "0123456789abcdefghijklmnopqrstuvwxyz"

func TestPrefetchedRangeReader_ReadRange(t *testing.T) {
	// "UVWXYZ" at offset 10 and "!?" at offset 20, with a gap between them.
	prefetchedRanges := metadata.PrefetchedRanges{
		{Offset: 10, Data: []byte("UVWXYZ")},
		{Offset: 20, Data: []byte("!?")},
	}

	tests := []struct {
		name       string
		offset     int64
		length     int64
		wantString string
	}{
		{
			name:       "full overlap returns prefetched bytes",
			offset:     11,
			length:     3,
			wantString: "VWX",
		},
		{
			name:       "partial overlap combines backend and prefetched bytes",
			offset:     8,
			length:     5,
			wantString: "89UVW",
		},
		{
			name:       "partial overlap on trailing end combines prefetched and backend bytes",
			offset:     14,
			length:     5,
			wantString: "YZghi",
		},
		{
			name:       "no overlap returns backend bytes",
			offset:     0,
			length:     4,
			wantString: "0123",
		},
		{
			name:       "a read across two ranges reads the gap between them from the backend",
			offset:     13,
			length:     9,
			wantString: "XYZghij!?",
		},
		{
			name:       "a read that covers every range reads each gap from the backend",
			offset:     8,
			length:     16,
			wantString: "89UVWXYZghij!?mn",
		},
		{
			name:       "a read in the gap between ranges returns backend bytes",
			offset:     17,
			length:     3,
			wantString: "hij",
		},
		{
			name:       "a read after the last range returns backend bytes",
			offset:     22,
			length:     4,
			wantString: "mnop",
		},
		{
			name:       "a read of the full content returns the backend bytes with the prefetched ranges in place",
			offset:     0,
			length:     int64(len(backendSource)),
			wantString: "0123456789UVWXYZghij!?mnopqrstuvwxyz",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)

			rc, err := rr.ReadRange(context.Background(), tc.offset, tc.length)
			require.NoError(t, err)

			got := readAllAndClose(t, rc)
			require.Equal(t, tc.wantString, string(got))
		})
	}

	t.Run("reads the gaps of one read in parallel", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			const gaps = 3 // A read of [8, 24) has the gaps [8, 10), [16, 20) and [22, 24).

			var started atomic.Int32
			release := make(chan struct{})

			rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)
			backend := rr.inner
			rr.inner = hookedRangeReader{rangeReader: backend, readRange: func(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
				started.Add(1)
				select {
				case <-release:
					return backend.ReadRange(ctx, offset, length)
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}

			type result struct {
				rc  io.ReadCloser
				err error
			}
			done := make(chan result, 1)
			go func() {
				rc, err := rr.ReadRange(t.Context(), 8, 16)
				done <- result{rc: rc, err: err}
			}()

			synctest.Wait()
			require.Equal(t, int32(gaps), started.Load())

			close(release)
			res := <-done
			require.NoError(t, res.err)
			require.Equal(t, "89UVWXYZghij!?mn", string(readAllAndClose(t, res.rc)))
		})
	})

	t.Run("a failed gap read fails the read and closes the readers of the other gaps", func(t *testing.T) {
		var closed atomic.Int32

		rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)
		backend := rr.inner
		rr.inner = hookedRangeReader{rangeReader: backend, readRange: func(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
			if offset == 16 {
				return nil, errors.New("gap failed")
			}
			rc, err := backend.ReadRange(ctx, offset, length)
			if err != nil {
				return nil, err
			}
			return closeCounter{ReadCloser: rc, closed: &closed}, nil
		}}

		rc, err := rr.ReadRange(t.Context(), 8, 16)

		require.EqualError(t, err, "gap failed")
		require.Nil(t, rc)
		require.Equal(t, int32(2), closed.Load())
	})
}

func TestPrefetchedRangeReader_ReadRanges(t *testing.T) {
	// "UVWXYZ" at offset 10 and "!?" at offset 20, with a gap between them.
	prefetchedRanges := metadata.PrefetchedRanges{
		{Offset: 10, Data: []byte("UVWXYZ")},
		{Offset: 20, Data: []byte("!?")},
	}

	t.Run("returns a range that one prefetched range holds without a read and without a copy", func(t *testing.T) {
		rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)
		var reads recordedReads
		rr.inner = reads.hook(rr.inner)

		got, err := rr.ReadRanges(t.Context(), []absoluteByteRange{{Offset: 11, Length: 3}})

		require.NoError(t, err)
		require.Equal(t, []string{"VWX"}, toStrings(got))
		require.Empty(t, reads.calls())
		require.Same(t, &prefetchedRanges[0].Data[1], &got[0][0])

		_ = append(got[0], '#')
		require.Equal(t, "UVWXYZ", string(prefetchedRanges[0].Data), "an append to the result must not write into the prefetched range")
	})

	t.Run("returns the bytes in the order of the ranges, from memory and from the inner reader", func(t *testing.T) {
		rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)

		got, err := rr.ReadRanges(t.Context(), []absoluteByteRange{
			{Offset: 20, Length: 2},
			{Offset: 0, Length: 4},
			{Offset: 8, Length: 5},
			{Offset: 14, Length: 8},
			{Offset: 21, Length: 4},
			{Offset: 8, Length: 16},
			{Offset: 26, Length: 3},
		})

		require.NoError(t, err)
		require.Equal(t, []string{"!?", "0123", "89UVW", "YZghij!?", "?mno", "89UVWXYZghij!?mn", "qrs"}, toStrings(got))
	})

	t.Run("reads only the gap of a range that starts before a prefetched range", func(t *testing.T) {
		rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)
		var reads recordedReads
		rr.inner = reads.hook(rr.inner)

		got, err := rr.ReadRanges(t.Context(), []absoluteByteRange{{Offset: 8, Length: 5}})

		require.NoError(t, err)
		require.Equal(t, []string{"89UVW"}, toStrings(got))
		require.Equal(t, []readCall{{offset: 8, length: 2}}, reads.calls())
	})

	t.Run("merges the gaps of different ranges into one read", func(t *testing.T) {
		rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)
		var reads recordedReads
		rr.inner = reads.hook(rr.inner)

		ctx := rangeio.WithConfig(t.Context(), &rangeio.Config{MaxParallelism: 1, CoalesceSize: 1 << 20, MaxRangeSize: 8 << 20, MinRangeSize: 1 << 20})

		got, err := rr.ReadRanges(ctx, []absoluteByteRange{{Offset: 8, Length: 4}, {Offset: 16, Length: 6}})

		require.NoError(t, err)
		require.Equal(t, []string{"89UV", "ghij!?"}, toStrings(got))
		require.Equal(t, []readCall{{offset: 8, length: 12}}, reads.calls())
	})

	t.Run("serves ranges next to each other and a range across them from memory", func(t *testing.T) {
		touching := metadata.PrefetchedRanges{
			{Offset: 10, Data: []byte("UVW")},
			{Offset: 13, Data: []byte("XYZ")},
		}
		rr := newTestPrefetchedRangeReader([]byte(backendSource), touching...)
		var reads recordedReads
		rr.inner = reads.hook(rr.inner)

		got, err := rr.ReadRanges(t.Context(), []absoluteByteRange{{Offset: 10, Length: 3}, {Offset: 13, Length: 3}, {Offset: 11, Length: 4}})

		require.NoError(t, err)
		require.Equal(t, []string{"UVW", "XYZ", "VWXY"}, toStrings(got))
		require.Same(t, &touching[0].Data[0], &got[0][0], "a range that one prefetched range holds is not copied")
		require.Same(t, &touching[1].Data[0], &got[1][0], "a range that one prefetched range holds is not copied")
		require.Empty(t, reads.calls())
	})

	t.Run("returns an empty slice for a range of zero length and does not read up to it", func(t *testing.T) {
		rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)
		var reads recordedReads
		rr.inner = reads.hook(rr.inner)

		got, err := rr.ReadRanges(t.Context(), []absoluteByteRange{{Offset: 0, Length: 2}, {Offset: 3, Length: 0}})

		require.NoError(t, err)
		require.Equal(t, []string{"01", ""}, toStrings(got))
		require.Equal(t, []readCall{{offset: 0, length: 2}}, reads.calls())
	})

	t.Run("returns an error for an invalid range", func(t *testing.T) {
		tests := []struct {
			name    string
			r       absoluteByteRange
			wantErr string
		}{
			{name: "has a negative offset", r: absoluteByteRange{Offset: -1, Length: 2}, wantErr: "parameters must not be negative: offset=-1 length=2"},
			{name: "has a negative length", r: absoluteByteRange{Offset: 2, Length: -1}, wantErr: "parameters must not be negative: offset=2 length=-1"},
			{name: "has an end that overflows", r: absoluteByteRange{Offset: math.MaxInt64, Length: 2}, wantErr: "range overflows int64: offset=9223372036854775807 length=2"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)

				got, err := rr.ReadRanges(t.Context(), []absoluteByteRange{{Offset: 0, Length: 1}, tc.r})

				require.EqualError(t, err, tc.wantErr)
				require.Nil(t, got)
			})
		}
	})

	t.Run("returns the error of the inner reader", func(t *testing.T) {
		rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)
		backend := rr.inner
		rr.inner = hookedRangeReader{rangeReader: backend, readRange: func(context.Context, int64, int64) (io.ReadCloser, error) {
			return nil, errors.New("read failed")
		}}

		got, err := rr.ReadRanges(t.Context(), []absoluteByteRange{{Offset: 0, Length: 4}})

		require.EqualError(t, err, "read failed")
		require.Nil(t, got)
	})
}

func TestRangeSegmentsFromPrefetchedRanges(t *testing.T) {
	// "UVWXYZ" at offset 10 and "!?" at offset 20, with a gap between them.
	prefetchedRanges := metadata.PrefetchedRanges{
		{Offset: 10, Data: []byte("UVWXYZ")},
		{Offset: 20, Data: []byte("!?")},
	}

	tests := []struct {
		name   string
		ranges metadata.PrefetchedRanges
		offset int64
		end    int64
		want   rangeSegments
	}{
		{
			name:   "a request inside one range returns only the bytes of the request",
			ranges: prefetchedRanges[:1],
			offset: 11,
			end:    14,
			want:   rangeSegments{dataRangeSegment("VWX")},
		},
		{
			name:   "a request that starts before a range returns a reader segment and then the bytes",
			ranges: prefetchedRanges[:1],
			offset: 8,
			end:    13,
			want:   rangeSegments{readerRangeSegment(8, 2), dataRangeSegment("UVW")},
		},
		{
			name:   "a request that ends after a range returns the bytes and then a reader segment",
			ranges: prefetchedRanges[:1],
			offset: 14,
			end:    19,
			want:   rangeSegments{dataRangeSegment("YZ"), readerRangeSegment(16, 3)},
		},
		{
			name:   "a request that covers a range returns a reader segment, the bytes and a reader segment",
			ranges: prefetchedRanges[:1],
			offset: 8,
			end:    19,
			want:   rangeSegments{readerRangeSegment(8, 2), dataRangeSegment("UVWXYZ"), readerRangeSegment(16, 3)},
		},
		{
			name:   "a request that starts where a range starts returns no leading reader segment",
			ranges: prefetchedRanges[:1],
			offset: 10,
			end:    13,
			want:   rangeSegments{dataRangeSegment("UVW")},
		},
		{
			name:   "a request that ends where a range ends returns no trailing reader segment",
			ranges: prefetchedRanges[:1],
			offset: 12,
			end:    16,
			want:   rangeSegments{dataRangeSegment("WXYZ")},
		},
		{
			name:   "a request across two ranges returns the reader segment between them",
			ranges: prefetchedRanges,
			offset: 13,
			end:    22,
			want:   rangeSegments{dataRangeSegment("XYZ"), readerRangeSegment(16, 4), dataRangeSegment("!?")},
		},
		{
			name:   "a request that covers every range returns a reader segment before, between and after them",
			ranges: prefetchedRanges,
			offset: 8,
			end:    24,
			want: rangeSegments{
				readerRangeSegment(8, 2), dataRangeSegment("UVWXYZ"), readerRangeSegment(16, 4), dataRangeSegment("!?"), readerRangeSegment(22, 2),
			},
		},
		{
			name: "a request across two adjacent ranges returns no reader segment between them",
			ranges: metadata.PrefetchedRanges{
				{Offset: 10, Data: []byte("UVWX")},
				{Offset: 14, Data: []byte("YZ")},
			},
			offset: 12,
			end:    16,
			want:   rangeSegments{dataRangeSegment("WX"), dataRangeSegment("YZ")},
		},
		{
			name: "a request that covers two adjacent ranges returns a reader segment before and after them",
			ranges: metadata.PrefetchedRanges{
				{Offset: 10, Data: []byte("UVWX")},
				{Offset: 14, Data: []byte("YZ")},
			},
			offset: 8,
			end:    20,
			want: rangeSegments{
				readerRangeSegment(8, 2), dataRangeSegment("UVWX"), dataRangeSegment("YZ"), readerRangeSegment(16, 4),
			},
		},
		{
			name: "a request that fits two adjacent ranges exactly returns only their bytes",
			ranges: metadata.PrefetchedRanges{
				{Offset: 10, Data: []byte("UVWX")},
				{Offset: 14, Data: []byte("YZ")},
			},
			offset: 10,
			end:    16,
			want:   rangeSegments{dataRangeSegment("UVWX"), dataRangeSegment("YZ")},
		},
		{
			name: "a request across three adjacent ranges returns the bytes of each range",
			ranges: metadata.PrefetchedRanges{
				{Offset: 10, Data: []byte("UV")},
				{Offset: 12, Data: []byte("WX")},
				{Offset: 14, Data: []byte("YZ")},
			},
			offset: 11,
			end:    15,
			want:   rangeSegments{dataRangeSegment("V"), dataRangeSegment("WX"), dataRangeSegment("Y")},
		},
		{
			name:   "a request with no ranges returns one reader segment that covers the request",
			offset: 5,
			end:    9,
			want:   rangeSegments{readerRangeSegment(5, 4)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rangeSegmentsFromPrefetchedRanges(tc.ranges, tc.offset, tc.end)

			require.Equal(t, tc.want, got)
			require.Equal(t, tc.end-tc.offset, segmentsLength(got))
		})
	}
}

func TestRangeSegments_Close(t *testing.T) {
	t.Run("closes the readers last first and skips the segments without a reader", func(t *testing.T) {
		var order []string
		segments := rangeSegments{
			{reader: closeRecorder{name: "first", order: &order}},
			dataRangeSegment("data"),
			{reader: closeRecorder{name: "last", order: &order}},
		}

		require.NoError(t, segments.Close())
		require.Equal(t, []string{"last", "first"}, order)
	})

	t.Run("closes every reader and returns the joined errors when some readers fail", func(t *testing.T) {
		var order []string
		segments := rangeSegments{
			{reader: closeRecorder{name: "first", order: &order, err: errors.New("first failed")}},
			{reader: closeRecorder{name: "second", order: &order}},
			{reader: closeRecorder{name: "third", order: &order, err: errors.New("third failed")}},
		}

		require.EqualError(t, segments.Close(), "third failed\nfirst failed")
		require.Equal(t, []string{"third", "second", "first"}, order)
	})

	t.Run("returns no error when no segment has a reader", func(t *testing.T) {
		segments := rangeSegments{dataRangeSegment("data"), readerRangeSegment(0, 4)}

		require.NoError(t, segments.Close())
	})
}

func TestRangeSegmentsReader(t *testing.T) {
	t.Run("reads the bytes of the segments in order", func(t *testing.T) {
		segments := rangeSegments{
			dataRangeSegment("ab"),
			{reader: io.NopCloser(bytes.NewReader([]byte("cd")))},
			dataRangeSegment("ef"),
		}

		got, err := io.ReadAll(newRangeSegmentsReader(segments))

		require.NoError(t, err)
		require.Equal(t, "abcdef", string(got))
	})

	t.Run("closes the readers of the segments", func(t *testing.T) {
		var closed atomic.Int32
		segments := rangeSegments{
			dataRangeSegment("ab"),
			{reader: closeCounter{ReadCloser: io.NopCloser(bytes.NewReader([]byte("cd"))), closed: &closed}},
		}

		require.NoError(t, newRangeSegmentsReader(segments).Close())
		require.Equal(t, int32(1), closed.Load())
	})
}

func newTestPrefetchedRangeReader(backendBytes []byte, ranges ...metadata.PrefetchedRange) *prefetchedRangeReader {
	return &prefetchedRangeReader{
		inner: &readerAtRangeReader{
			size: int64(len(backendBytes)),
			r:    bytes.NewReader(backendBytes),
		},
		ranges: ranges,
	}
}

type readCall struct{ offset, length int64 }

// recordedReads records the reads that a hooked reader gets.
type recordedReads struct {
	mu    sync.Mutex
	reads []readCall
}

// hook returns a reader that records the reads and serves them from inner.
func (r *recordedReads) hook(inner rangeReader) rangeReader {
	return hookedRangeReader{rangeReader: inner, readRange: func(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
		r.mu.Lock()
		r.reads = append(r.reads, readCall{offset: offset, length: length})
		r.mu.Unlock()
		return inner.ReadRange(ctx, offset, length)
	}}
}

// calls returns the recorded reads, sorted by offset. The reads run in parallel, so their order varies.
func (r *recordedReads) calls() []readCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := slices.Clone(r.reads)
	slices.SortFunc(out, func(a, b readCall) int { return cmp.Compare(a.offset, b.offset) })
	return out
}

func toStrings(blobs [][]byte) []string {
	out := make([]string, len(blobs))
	for i, blob := range blobs {
		out[i] = string(blob)
	}
	return out
}

// hookedRangeReader replaces the ReadRange of the embedded reader with readRange. ReadRanges reads
// through the hook too.
type hookedRangeReader struct {
	rangeReader
	readRange func(ctx context.Context, offset, length int64) (io.ReadCloser, error)
}

func (h hookedRangeReader) ReadRanges(ctx context.Context, ranges []absoluteByteRange) ([][]byte, error) {
	return readRanges(ctx, h, ranges)
}

func (h hookedRangeReader) ReadRange(ctx context.Context, offset, length int64) (io.ReadCloser, error) {
	return h.readRange(ctx, offset, length)
}

// closeCounter counts the Close calls of a reader.
type closeCounter struct {
	io.ReadCloser
	closed *atomic.Int32
}

func (c closeCounter) Close() error {
	c.closed.Add(1)
	return c.ReadCloser.Close()
}

// closeRecorder appends its name to order when it closes, and returns err.
type closeRecorder struct {
	io.Reader
	name  string
	order *[]string
	err   error
}

func (c closeRecorder) Close() error {
	*c.order = append(*c.order, c.name)
	return c.err
}

func dataRangeSegment(data string) rangeSegment {
	return rangeSegment{data: []byte(data)}
}

func readerRangeSegment(readerOffset, readerLength int64) rangeSegment {
	return rangeSegment{readerOffset: readerOffset, readerLength: readerLength}
}

// segmentsLength returns how many bytes of the request the segments cover.
func segmentsLength(segments rangeSegments) int64 {
	var n int64
	for _, s := range segments {
		n += int64(len(s.data)) + s.readerLength
	}
	return n
}

func readAllAndClose(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()

	defer func() {
		require.NoError(t, rc.Close())
	}()

	out, err := io.ReadAll(rc)
	require.NoError(t, err)
	return out
}
