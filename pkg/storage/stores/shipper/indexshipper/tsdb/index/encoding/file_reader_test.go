// SPDX-License-Identifier: AGPL-3.0-only
// Copied from: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/file_reader_test.go

package encoding

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/filepool"
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
		require.ErrorIs(t, r.ResetAt(21), ErrInvalidSize)
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
		dir := t.TempDir()
		filePath := path.Join(dir, "test-file")
		require.NoError(t, os.WriteFile(filePath, nil, 0700))

		f, err := os.Open(filePath)
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, f.Close())
		})

		r, err := NewFileReader(f, 0, 0, &filepool.SingleFilePoolNoopCloser{})
		require.NoError(t, err)
		require.ErrorIs(t, r.Skip(1), ErrInvalidSize)
		require.ErrorIs(t, r.ResetAt(1), ErrInvalidSize)
	})
}

func testReaders(t *testing.T, test func(t *testing.T, r *FileReader)) {
	testReaderContents := []byte("abcdefghij1234567890")

	t.Run("FileReaderWithZeroOffset", func(t *testing.T) {
		dir := t.TempDir()
		filePath := path.Join(dir, "test-file")
		require.NoError(t, os.WriteFile(filePath, testReaderContents, 0700))

		f, err := os.Open(filePath)
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, f.Close())
		})

		r, err := NewFileReader(f, 0, len(testReaderContents), &filepool.SingleFilePoolNoopCloser{})
		require.NoError(t, err)

		test(t, r)
	})

	t.Run("FileReaderWithNonZeroOffset", func(t *testing.T) {
		offsetBytes := []byte("ABCDE")
		fileBytes := append(offsetBytes, testReaderContents...)

		dir := t.TempDir()
		filePath := path.Join(dir, "test-file")
		require.NoError(t, os.WriteFile(filePath, fileBytes, 0700))

		f, err := os.Open(filePath)
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, f.Close())
		})

		r, err := NewFileReader(f, len(offsetBytes), len(testReaderContents), &filepool.SingleFilePoolNoopCloser{})
		require.NoError(t, err)

		test(t, r)
	})
}
