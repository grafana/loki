package dataobj

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
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

func TestPrefetchedRangeReader_Prefetched(t *testing.T) {
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
		wantOK     bool
	}{
		{name: "a range inside one prefetched range is returned", offset: 11, length: 3, wantString: "VWX", wantOK: true},
		{name: "a range that fills one prefetched range is returned", offset: 20, length: 2, wantString: "!?", wantOK: true},
		{name: "a range that crosses two prefetched ranges is not returned", offset: 14, length: 8},
		{name: "a range that starts before a prefetched range is not returned", offset: 9, length: 3},
		{name: "a range that ends after a prefetched range is not returned", offset: 14, length: 3},
		{name: "a range outside every prefetched range is not returned", offset: 30, length: 2},
		{name: "a zero-length range inside a prefetched range is not returned", offset: 11, length: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := newTestPrefetchedRangeReader([]byte(backendSource), prefetchedRanges...)

			got, ok := rr.Prefetched(tc.offset, tc.length)

			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantString, string(got))
		})
	}
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

// hookedRangeReader replaces the ReadRange of the embedded reader with readRange.
type hookedRangeReader struct {
	rangeReader
	readRange func(ctx context.Context, offset, length int64) (io.ReadCloser, error)
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
