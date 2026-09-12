// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/file_reader_test.go
// Provenance-includes-license: AGPL-3.0-only
// Provenance-includes-copyright: The Grafana Mimir Authors.

package streamenc

import (
	"math/rand"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/filepool"
)

func TestReaders_Read(t *testing.T) {
	testReaders(t, func(t *testing.T, r *FileReader) {
		firstRead, err := r.Read(5)
		require.NoError(t, err)
		require.Equal(t, []byte("abcde"), firstRead, "first read")

		secondRead, err := r.Read(5)
		require.NoError(t, err)
		require.Equal(t, []byte("fghij"), secondRead, "second read")

		readBeyondEnd, err := r.Read(12)
		require.ErrorIs(t, err, ErrInvalidSize)
		require.Empty(t, readBeyondEnd, "read beyond end")

		readAfterEnd, err := r.Read(1)
		require.ErrorIs(t, err, ErrInvalidSize)
		require.Empty(t, readAfterEnd, "read after end")
	})
}

func TestReaders_ReadInto(t *testing.T) {
	testReaders(t, func(t *testing.T, r *FileReader) {
		firstBuf := make([]byte, 5)
		err := r.ReadInto(firstBuf)
		require.NoError(t, err)
		require.Equal(t, []byte("abcde"), firstBuf, "first read")

		secondBuf := make([]byte, 5)
		err = r.ReadInto(secondBuf)
		require.NoError(t, err)
		require.Equal(t, []byte("fghij"), secondBuf, "second read")

		beyondEndBuf := make([]byte, 12)
		err = r.ReadInto(beyondEndBuf)
		require.ErrorIs(t, err, ErrInvalidSize)

		afterEndBuf := make([]byte, 1)
		err = r.ReadInto(afterEndBuf)
		require.ErrorIs(t, err, ErrInvalidSize)
	})
}

func TestReaders_Peek(t *testing.T) {
	testReaders(t, func(t *testing.T, r *FileReader) {
		firstPeek, err := r.Peek(5)
		require.NoError(t, err)
		require.Equal(t, []byte("abcde"), firstPeek, "peek (first call)")

		secondPeek, err := r.Peek(5)
		require.NoError(t, err)
		require.Equal(t, []byte("abcde"), secondPeek, "peek (second call)")

		readAfterPeek, err := r.Read(5)
		require.NoError(t, err)
		require.Equal(t, []byte("abcde"), readAfterPeek, "first read call")

		peekAfterRead, err := r.Peek(5)
		require.NoError(t, err)
		require.Equal(t, []byte("fghij"), peekAfterRead, "peek after read")

		peekBeyondEnd, err := r.Peek(20)
		require.NoError(t, err)
		require.Equal(t, []byte("fghij1234567890"), peekBeyondEnd, "peek beyond end")

		_, err = r.Read(15)
		require.NoError(t, err)

		peekAfterEnd, err := r.Peek(1)
		require.NoError(t, err)
		require.Empty(t, peekAfterEnd, "peek after end")
	})
}

func TestReaders_Reset(t *testing.T) {
	testReaders(t, func(t *testing.T, r *FileReader) {
		_, err := r.Read(5)
		require.NoError(t, err)
		require.NoError(t, r.Reset())

		readAfterReset, err := r.Read(5)
		require.NoError(t, err)
		require.Equal(t, []byte("abcde"), readAfterReset)
	})
}

func TestReaders_ResetAt(t *testing.T) {
	testReaders(t, func(t *testing.T, r *FileReader) {
		require.NoError(t, r.ResetAt(5))
		readAfterReset, err := r.Read(5)
		require.NoError(t, err)
		require.Equal(t, []byte("fghij"), readAfterReset, "read after reset to non-zero offset")

		require.NoError(t, r.ResetAt(0))
		readAfterResetToBeginning, err := r.Read(5)
		require.NoError(t, err)
		require.Equal(t, []byte("abcde"), readAfterResetToBeginning, "read after reset to zero offset")

		require.NoError(t, r.ResetAt(19))
		readAfterResetToLastByte, err := r.Read(1)
		require.NoError(t, err)
		require.Equal(t, []byte("0"), readAfterResetToLastByte, "read after reset to last byte")

		require.NoError(t, r.ResetAt(20))
		require.Equal(t, 20, r.Offset())
		require.ErrorIs(t, r.ResetAt(21), ErrInvalidSize)
		require.Equal(t, 20, r.Offset())
	})
}

func TestReaders_Skip(t *testing.T) {
	testReaders(t, func(t *testing.T, r *FileReader) {
		peek, err := r.Peek(5)
		require.NoError(t, err)
		require.Equal(t, []byte("abcde"), peek, "peek before skip")
		require.Equal(t, 20, r.Len())

		require.NoError(t, r.Skip(5))
		readAfterSkip, err := r.Read(5)
		require.NoError(t, err)
		require.Equal(t, []byte("fghij"), readAfterSkip, "read after skip")
		require.Equal(t, 10, r.Len())

		require.NoError(t, r.Skip(5))
		peekAfterSkip, err := r.Peek(5)
		require.NoError(t, err)
		require.Equal(t, []byte("67890"), peekAfterSkip, "peek after skip")
		require.Equal(t, 5, r.Len())

		// skip to exactly the end, then skip beyond it
		require.NoError(t, r.Skip(5))
		require.Equal(t, 0, r.Len())
		require.ErrorIs(t, r.Skip(1), ErrInvalidSize)
		require.Equal(t, 20, r.Offset())
	})
}

func TestReaders_Len(t *testing.T) {
	testReaders(t, func(t *testing.T, r *FileReader) {
		require.Equal(t, 20, r.Len(), "initial length")

		_, err := r.Read(5)
		require.NoError(t, err)
		require.Equal(t, 15, r.Len(), "after first read")

		_, err = r.Read(2)
		require.NoError(t, err)
		require.Equal(t, 13, r.Len(), "after second read")

		_, err = r.Peek(3)
		require.NoError(t, err)
		require.Equal(t, 13, r.Len(), "after peek")

		_, err = r.Read(14)
		require.ErrorIs(t, err, ErrInvalidSize)
		require.Equal(t, 0, r.Len(), "after read beyond end")

		require.NoError(t, r.Reset())
		require.Equal(t, 20, r.Len(), "after reset to beginning")

		require.NoError(t, r.ResetAt(3))
		require.Equal(t, 17, r.Len(), "after reset to offset")
	})
}

func TestReaders_Position(t *testing.T) {
	testReaders(t, func(t *testing.T, r *FileReader) {
		require.Equal(t, 0, r.Offset(), "initial offset")

		_, err := r.Read(5)
		require.NoError(t, err)
		require.Equal(t, 5, r.Offset(), "after first read")

		_, err = r.Read(2)
		require.NoError(t, err)
		require.Equal(t, 7, r.Offset(), "after second read")

		_, err = r.Peek(3)
		require.NoError(t, err)
		require.Equal(t, 7, r.Offset(), "after peek")

		_, err = r.Read(14)
		require.ErrorIs(t, err, ErrInvalidSize)
		require.Equal(t, 20, r.Offset(), "after read beyond end")

		require.NoError(t, r.Reset())
		require.Equal(t, 0, r.Offset(), "after reset to beginning")

		require.NoError(t, r.ResetAt(3))
		require.Equal(t, 3, r.Offset(), "after reset to offset")
	})
}

func TestReaders_CreationWithEmptyContents(t *testing.T) {
	t.Run("FileReader", func(t *testing.T) {
		r := newTestFileReader(t, nil, 0, 0)
		require.ErrorIs(t, r.Skip(1), ErrInvalidSize)
		require.ErrorIs(t, r.ResetAt(1), ErrInvalidSize)
	})
}

func testReaders(t *testing.T, test func(t *testing.T, r *FileReader)) {
	testReaderContents := []byte("abcdefghij1234567890")

	t.Run("FileReaderWithZeroOffset", func(t *testing.T) {
		r := newTestFileReader(t, testReaderContents, 0, len(testReaderContents))
		test(t, r)
	})

	t.Run("FileReaderWithNonZeroOffset", func(t *testing.T) {
		offsetBytes := []byte("ABCDE")
		fileBytes := append(offsetBytes, testReaderContents...)
		r := newTestFileReader(t, fileBytes, len(offsetBytes), len(testReaderContents))
		test(t, r)
	})
}

func newTestFileReader(t *testing.T, fileBytes []byte, base, length int) *FileReader {
	t.Helper()

	filePath := path.Join(t.TempDir(), "test-file")
	require.NoError(t, os.WriteFile(filePath, fileBytes, 0600))
	f, err := os.Open(filePath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.Close()) })

	r, err := NewFileReader(f, base, length, &filepool.SingleFilePoolNoopCloser{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if r.buf != nil {
			require.NoError(t, r.Close())
		}
	})
	return r
}

func generateTestData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

func newTestFileReaderWithGeneratedData(t *testing.T, base, length int) (*FileReader, []byte) {
	t.Helper()

	content := generateTestData(length)
	fileBytes := make([]byte, base+length+64)
	for i := range fileBytes[:base] {
		fileBytes[i] = 0xaa
	}
	copy(fileBytes[base:], content)
	for i := base + length; i < len(fileBytes); i++ {
		fileBytes[i] = 0xbb
	}

	return newTestFileReader(t, fileBytes, base, length), content
}

func requireBufferInvariant(t *testing.T, r *FileReader, content []byte) {
	t.Helper()

	require.GreaterOrEqual(t, r.r, 0)
	require.LessOrEqual(t, r.r, r.n)
	require.LessOrEqual(t, r.n, len(r.buf))
	require.GreaterOrEqual(t, r.off, r.r)

	bufferStart := r.off - r.r
	require.Equal(t, content[bufferStart:r.off], r.buf[:r.r])
	end := min(r.off+r.Buffered(), len(content))
	if end > r.off {
		require.Equal(t, content[r.off:end], r.buf[r.r:r.r+end-r.off])
	}
}

func TestFileReader_BufferInvariant(t *testing.T) {
	const length = 10*ReaderBufferSize + 613
	r, content := newTestFileReaderWithGeneratedData(t, 17, length)
	rng := rand.New(rand.NewSource(20260826))

	pickOffset := func() int {
		if rng.Intn(2) != 0 {
			return rng.Intn(length + 1)
		}
		edge := rng.Intn(length/ReaderBufferSize+1)*ReaderBufferSize + rng.Intn(5) - 2
		return max(0, min(edge, length))
	}

	// Take random actions, asserting that the invariant holds after each action
	for i := range 20000 {
		switch rng.Intn(5) {
		case 0:
			// ResetAt to a random offset
			off := pickOffset()
			require.NoError(t, r.ResetAt(off), "iteration %d", i)
			require.Equal(t, off, r.Offset(), "iteration %d", i)
		case 1:
			// Skip a random number of bytes forwards
			before := r.Offset()
			l := rng.Intn(r.Len() + 1)
			require.NoError(t, r.Skip(l), "iteration %d", i)
			require.Equal(t, before+l, r.Offset(), "iteration %d", i)
		case 2:
			// Peek a random number of bytes ahead
			n := 1 + rng.Intn(ReaderBufferSize)
			off := r.Offset()
			got, err := r.Peek(n)
			require.NoError(t, err, "iteration %d", i)
			require.Equal(t, off, r.Offset(), "iteration %d", i)
			want := min(n, len(content)-off)
			require.GreaterOrEqual(t, len(got), want, "iteration %d", i)
			require.Equal(t, content[off:off+want], got[:want], "iteration %d", i)
		case 3:
			// ReadInto a random number of bytes into a byte buffer
			if r.Len() == 0 {
				continue
			}
			n := 1 + rng.Intn(min(r.Len(), 2*ReaderBufferSize))
			off := r.Offset()
			buf := make([]byte, n)
			require.NoError(t, r.ReadInto(buf), "iteration %d", i)
			require.Equal(t, content[off:off+n], buf, "iteration %d", i)
			require.Equal(t, off+n, r.Offset(), "iteration %d", i)
		case 4:
			// Reset the reader
			require.NoError(t, r.Reset(), "iteration %d", i)
			require.Zero(t, r.Offset(), "iteration %d", i)
		}
		requireBufferInvariant(t, r, content)
	}
}

func TestFileReader_PeekBeyondBufferSize(t *testing.T) {
	r, content := newTestFileReaderWithGeneratedData(t, 0, 4*ReaderBufferSize)
	_, err := r.Peek(r.Size() + 1)
	require.ErrorIs(t, err, ErrInvalidSize)
	require.Zero(t, r.Offset())

	got, err := r.Peek(r.Size())
	require.NoError(t, err)
	require.Equal(t, content[:r.Size()], got)
}

func TestFileReader_ReadIntoPastEOF(t *testing.T) {
	const length = 3*ReaderBufferSize + 137
	content := generateTestData(length)
	r := newTestFileReader(t, content, 0, length)
	require.NoError(t, r.Skip(5))

	buf := make([]byte, length)
	err := r.ReadInto(buf)
	require.ErrorIs(t, err, ErrInvalidSize)
	require.Equal(t, content[5:], buf[:length-5])
	require.Equal(t, length, r.Offset())
}
