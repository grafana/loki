package store

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func uploadTestObject(t *testing.T, bucket objstore.Bucket, path string, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251) // prime modulus for variety
	}
	require.NoError(t, bucket.Upload(context.Background(), path, bytes.NewReader(data)))
	return data
}

func TestReadAheadReaderAt_SequentialReads(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	data := uploadTestObject(t, bucket, "test/seq", 1024)

	r := NewReadAheadReaderAt(context.Background(), bucket, "test/seq", 1024, 256)

	// Sequential 64-byte reads should be served from 1 chunk (256 bytes).
	for off := 0; off < 256; off += 64 {
		buf := make([]byte, 64)
		n, err := r.ReadAt(buf, int64(off))
		require.NoError(t, err)
		require.Equal(t, 64, n)
		require.Equal(t, data[off:off+64], buf)
	}
}

func TestReadAheadReaderAt_BufferHit(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	data := uploadTestObject(t, bucket, "test/hit", 1024)

	// Chunk size = entire file. One fetch should cover everything.
	r := NewReadAheadReaderAt(context.Background(), bucket, "test/hit", 1024, 2048)

	// First read triggers fetch.
	buf := make([]byte, 10)
	n, err := r.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 10, n)
	require.Equal(t, data[:10], buf)

	// Second read at different offset should be a buffer hit (no network).
	buf = make([]byte, 10)
	n, err = r.ReadAt(buf, 500)
	require.NoError(t, err)
	require.Equal(t, 10, n)
	require.Equal(t, data[500:510], buf)
}

func TestReadAheadReaderAt_BufferMiss(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	data := uploadTestObject(t, bucket, "test/miss", 1024)

	// Small chunk so we can force misses.
	r := NewReadAheadReaderAt(context.Background(), bucket, "test/miss", 1024, 100)

	// Read at offset 0 → fills [0, 100).
	buf := make([]byte, 10)
	n, err := r.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 10, n)
	require.Equal(t, data[:10], buf)

	// Read at offset 200 → miss, fills [200, 300).
	buf = make([]byte, 10)
	n, err = r.ReadAt(buf, 200)
	require.NoError(t, err)
	require.Equal(t, 10, n)
	require.Equal(t, data[200:210], buf)
}

func TestReadAheadReaderAt_ReadAtEOF(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	data := uploadTestObject(t, bucket, "test/eof", 100)

	r := NewReadAheadReaderAt(context.Background(), bucket, "test/eof", 100, 256)

	// Read that extends past EOF should return partial data + io.EOF.
	buf := make([]byte, 20)
	n, err := r.ReadAt(buf, 90)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 10, n)
	require.Equal(t, data[90:100], buf[:10])
}

func TestReadAheadReaderAt_ReadLargerThanChunk(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	data := uploadTestObject(t, bucket, "test/big", 1024)

	r := NewReadAheadReaderAt(context.Background(), bucket, "test/big", 1024, 100)

	// Read larger than chunk size → direct read, no buffering.
	buf := make([]byte, 200)
	n, err := r.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 200, n)
	require.Equal(t, data[:200], buf)
}

func TestReadAheadReaderAt_EmptyRead(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	uploadTestObject(t, bucket, "test/empty", 100)

	r := NewReadAheadReaderAt(context.Background(), bucket, "test/empty", 100, 256)

	buf := make([]byte, 0)
	n, err := r.ReadAt(buf, 50)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

func TestReadAheadReaderAt_DefaultChunkSize(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	data := uploadTestObject(t, bucket, "test/default", 100)

	// chunkSize=0 should use default.
	r := NewReadAheadReaderAt(context.Background(), bucket, "test/default", 100, 0)

	buf := make([]byte, 10)
	n, err := r.ReadAt(buf, 0)
	require.NoError(t, err)
	require.Equal(t, 10, n)
	require.Equal(t, data[:10], buf)
}

func TestReadAheadReaderAt_AlternatingRegions(t *testing.T) {
	// Simulates the merge pattern: alternating reads between term blocks
	// (low offsets) and postings blocks (high offsets). With multiple slots,
	// both regions should stay buffered without thrashing.
	bucket := objstore.NewInMemBucket()
	data := uploadTestObject(t, bucket, "test/alt", 10000)

	// Chunk size 1000 with 3 slots: region A [0..1000), region B [5000..6000)
	// should each occupy a slot without evicting the other.
	r := NewReadAheadReaderAt(context.Background(), bucket, "test/alt", 10000, 1000)

	for round := range 10 {
		// Read from region A (term-like).
		offA := int64(round * 50)
		buf := make([]byte, 50)
		n, err := r.ReadAt(buf, offA)
		require.NoError(t, err)
		require.Equal(t, 50, n)
		require.Equal(t, data[offA:offA+50], buf)

		// Read from region B (postings-like).
		offB := int64(5000 + round*50)
		buf = make([]byte, 50)
		n, err = r.ReadAt(buf, offB)
		require.NoError(t, err)
		require.Equal(t, 50, n)
		require.Equal(t, data[offB:offB+50], buf)
	}
}

func TestReadAheadReaderAt_FullFileReadback(t *testing.T) {
	// Verify entire file can be read correctly with various chunk sizes.
	bucket := objstore.NewInMemBucket()
	data := uploadTestObject(t, bucket, "test/full", 10000)

	for _, chunkSz := range []int{64, 256, 1000, 5000, 20000} {
		t.Run("", func(t *testing.T) {
			r := NewReadAheadReaderAt(context.Background(), bucket, "test/full", 10000, chunkSz)

			var got []byte
			buf := make([]byte, 137) // odd size to test boundary alignment
			off := int64(0)
			for {
				n, err := r.ReadAt(buf, off)
				got = append(got, buf[:n]...)
				off += int64(n)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
			}
			require.Equal(t, data, got)
		})
	}
}
