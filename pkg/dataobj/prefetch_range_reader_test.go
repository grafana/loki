package dataobj

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_prefetchedRangeReader_ReadRange(t *testing.T) {
	const (
		backendSource    = "0123456789abcdefghijklmnopqrstuvwxyz"
		prefetchedSource = "UVWXYZ"
		prefetchOffset   = int64(10)
	)

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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := newTestPrefetchedRangeReader(
				[]byte(backendSource),
				prefetchOffset,
				[]byte(prefetchedSource),
			)

			rc, err := rr.ReadRange(context.Background(), tc.offset, tc.length)
			require.NoError(t, err)

			got := readAllAndClose(t, rc)
			require.Equal(t, tc.wantString, string(got))
		})
	}
}

func newTestPrefetchedRangeReader(backendBytes []byte, prefetchOffset int64, prefetchedBytes []byte) *prefetchedRangeReader {
	return &prefetchedRangeReader{
		inner: &readerAtRangeReader{
			size: int64(len(backendBytes)),
			r:    bytes.NewReader(backendBytes),
		},
		prefetchOffset: prefetchOffset,
		prefetched:     append([]byte(nil), prefetchedBytes...),
	}
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
