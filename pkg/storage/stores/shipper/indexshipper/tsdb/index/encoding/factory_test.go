// SPDX-License-Identifier: AGPL-3.0-only
// Copied from: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/factory_test.go
// Bucket-based factory tests omitted; disk implementation of NewDecbufInSection added in Phase 2.

package encoding

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	promencoding "github.com/prometheus/prometheus/tsdb/encoding"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/filepool"
)

const testContentSize = 4096

var castagnoliTable = crc32.MakeTable(crc32.Castagnoli)

func BenchmarkDecbufFactory_NewDecbufAtUnchecked(b *testing.B) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(castagnoliTable))

	factory := createDiskDecbufFactory(b, 1, testContentSize, enc)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		d := factory.NewDecbufAtUnchecked(0)

		if err := d.Err(); err != nil {
			require.NoError(b, err)
		}

		if err := d.Close(); err != nil {
			require.NoError(b, err)
		}
	}
}

func TestDecbufFactory_NewDecbufAtChecked_InvalidCRC(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutBytes([]byte{0, 0, 0, 0})

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufAtChecked(0, castagnoliTable)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.ErrorIs(t, d.Err(), ErrInvalidChecksum)
	})
}

func TestDecbufFactory_NewDecbufAtChecked_InvalidLength(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(castagnoliTable))

	testDecbufFactory(t, testContentSize+1000, enc, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufAtChecked(0, castagnoliTable)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.ErrorIs(t, d.Err(), ErrInvalidSize)
	})
}

func TestDecbufFactory_NewDecbufAtChecked_HappyPath(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(castagnoliTable))

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufAtChecked(0, castagnoliTable)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.NoError(t, d.Err())
		require.Equal(t, testContentSize+crc32.Size, d.Len())
	})
}

func TestDecbufFactory_NewDecbufAtChecked_MultipleInstances(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(castagnoliTable))

	// Only test pooled case: we want to verify that sequential Decbufs reuse the same file descriptor.
	factory := createDiskDecbufFactory(t, 1, testContentSize, enc)
	t.Cleanup(func() {
		_ = factory.Close()
	})

	d1 := factory.NewDecbufAtChecked(0, castagnoliTable)
	require.NoError(t, d1.Err())
	fr1, ok := d1.r.(*FileReader)
	require.True(t, ok, "expected FileReader")
	fd1 := fr1.file.Fd()
	require.NoError(t, d1.Close())

	d2 := factory.NewDecbufAtChecked(0, castagnoliTable)
	require.NoError(t, d2.Err())
	fr2, ok := d2.r.(*FileReader)
	require.True(t, ok, "expected FileReader")
	fd2 := fr2.file.Fd()
	require.NoError(t, d2.Close())

	require.Equal(t, fd1, fd2, "expected Decbuf instances to use the same file descriptor")
}

func TestDecbufFactory_NewDecbufAtChecked_Concurrent(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(castagnoliTable))

	const (
		runs        = 100
		concurrency = 10
	)

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
		g, _ := errgroup.WithContext(t.Context())

		for i := 0; i < concurrency; i++ {
			g.Go(func() error {
				for run := 0; run < runs; run++ {
					d := factory.NewDecbufAtChecked(0, castagnoliTable)

					if err := d.Err(); err != nil {
						_ = d.Close()
						return err
					}

					if err := d.Close(); err != nil {
						return err
					}
				}

				return nil
			})
		}

		require.NoError(t, g.Wait())
	})
}

func TestDecbufFactory_NewDecbufAtUnchecked_HappyPath(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(castagnoliTable))

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufAtUnchecked(0)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.NoError(t, d.Err())
		require.Equal(t, testContentSize+crc32.Size, d.Len())
	})
}

func TestDecbufFactory_NewDecbufRaw_HappyPath(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(castagnoliTable))

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewRawDecbuf()
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.NoError(t, d.Err())
		require.Equal(t, 4+testContentSize+crc32.Size, d.Len())
	})
}

func TestDecbufFactory_NewDecbufInSection_HappyPath(t *testing.T) {
	testByte := byte(0x02)
	startOffset := 10
	endOffset := 25
	enc := createTestEncoderWithTestByte(testContentSize, startOffset-numLenBytes, testByte)
	enc.PutHash(crc32.New(castagnoliTable))

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufInSection(0, startOffset, endOffset)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})
		require.NoError(t, d.Err())
		require.Equal(t, endOffset-startOffset, d.Len())
		require.Equal(t, testByte, d.Byte())
	})
}

func TestDecbufFactory_NewDecbufInSection_EndOffsetBeyondFileSize(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(castagnoliTable))
	// File size = numLenBytes + testContentSize + crc32.Size
	fileSize := numLenBytes + testContentSize + crc32.Size

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufInSection(0, 10, testContentSize+1000)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})
		require.NoError(t, d.Err())
		// Clamped to actual available bytes from offset 10 to end of file.
		require.Equal(t, fileSize-10, d.Len())
	})
}

func TestDecbufFactory_NewDecbufInSection_EndOffsetBeforeStartOffset(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(castagnoliTable))

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufInSection(0, 2500, 30)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})
		require.Error(t, d.Err())
	})
}

func TestDecbufFactory_NewDecbufUvarintAt_HappyPathWithCRC(t *testing.T) {
	content := []byte("hello world test content for uvarint decbuf")
	fileBytes := buildUvarintSection(content, castagnoliTable)

	factory := newUvarintSectionFactory(t, fileBytes)

	d := factory.NewDecbufUvarintAt(0, castagnoliTable)
	t.Cleanup(func() { require.NoError(t, d.Close()) })

	require.NoError(t, d.Err())
	require.Equal(t, len(content), d.Len())

	got, err := d.r.Read(len(content))
	require.NoError(t, err)
	require.Equal(t, content, got)
}

func TestDecbufFactory_NewDecbufUvarintAt_HappyPathNoCRC(t *testing.T) {
	content := []byte("hello world test content for uvarint decbuf")
	factory := newUvarintSectionFactory(t, buildUvarintSection(content, castagnoliTable))

	d := factory.NewDecbufUvarintAt(0, nil)
	t.Cleanup(func() { require.NoError(t, d.Close()) })

	require.NoError(t, d.Err())
	require.Equal(t, len(content), d.Len())
}

func TestDecbufFactory_NewDecbufUvarintAt_InvalidCRC(t *testing.T) {
	content := []byte("hello world test content for uvarint decbuf")
	fileBytes := buildUvarintSection(content, castagnoliTable)
	// Corrupt the last 4 bytes (CRC).
	fileBytes[len(fileBytes)-1] ^= 0xFF

	factory := newUvarintSectionFactory(t, fileBytes)

	d := factory.NewDecbufUvarintAt(0, castagnoliTable)
	require.NoError(t, d.Close())
	require.ErrorIs(t, d.Err(), ErrInvalidChecksum)
}

func TestDecbufFactory_NewDecbufUvarintAt_Concurrent(t *testing.T) {
	content := []byte("concurrent read test content")
	factory := newUvarintSectionFactory(t, buildUvarintSection(content, castagnoliTable))

	const (
		runs        = 50
		concurrency = 8
	)

	g, _ := errgroup.WithContext(t.Context())
	for i := 0; i < concurrency; i++ {
		g.Go(func() error {
			for range runs {
				d := factory.NewDecbufUvarintAt(0, castagnoliTable)
				if err := d.Err(); err != nil {
					_ = d.Close()
					return err
				}
				if err := d.Close(); err != nil {
					return err
				}
			}
			return nil
		})
	}
	require.NoError(t, g.Wait())
}

// buildUvarintSection encodes content as [uvarint(len)][content][crc32].
func buildUvarintSection(content []byte, table *crc32.Table) []byte {
	enc := promencoding.Encbuf{}
	enc.PutUvarint(len(content))
	enc.PutBytes(content)
	checksum := crc32.Checksum(content, table)
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], checksum)
	enc.PutBytes(crcBuf[:])
	return enc.Get()
}

// buildUvarintSectionEncbuf returns an Encbuf of just the content (no uvarint prefix, no CRC),
// used by helpers that expect a plain content buffer.
func buildUvarintSectionEncbuf(content []byte, _ *crc32.Table) promencoding.Encbuf {
	enc := promencoding.Encbuf{}
	enc.PutBytes(content)
	return enc
}

func TestDecbufFactory_Stop(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(castagnoliTable))

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
		require.NoError(t, factory.Close())

		d := factory.NewRawDecbuf()
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.ErrorIs(t, d.Err(), filepool.ErrPoolStopped)
	})
}

// testDecbufFactory runs test against pooled and non-pooled disk factory variants.
func testDecbufFactory(t *testing.T, contentLen int, enc promencoding.Encbuf, test func(t *testing.T, factory DecbufFactory)) {
	t.Run("DecbufFactory=Disk-Pooled", func(t *testing.T) {
		factory := createDiskDecbufFactory(t, 1, contentLen, enc)
		test(t, factory)
	})

	t.Run("DecbufFactory=Disk-NoPool", func(t *testing.T) {
		factory := createDiskDecbufFactory(t, 0, contentLen, enc)
		test(t, factory)
	})
}

func createTestEncoderWithTestByte(numBytes, testByteOffset int, testByte byte) promencoding.Encbuf {
	enc := promencoding.Encbuf{}

	for i := 0; i < testByteOffset; i++ {
		enc.PutByte(0x01)
	}
	enc.PutByte(testByte)
	for i := testByteOffset + 1; i < numBytes; i++ {
		enc.PutByte(0x01)
	}
	return enc
}

func createTestEncoder(numBytes int) promencoding.Encbuf {
	enc := promencoding.Encbuf{}

	for i := 0; i < numBytes; i++ {
		enc.PutByte(0x01)
	}

	return enc
}

// createDiskDecbufFactory writes enc to a temp file prefixed with a 4-byte big-endian
// content length and returns a FilePoolDecbufFactory over it.
func createDiskDecbufFactory(t testing.TB, poolSize uint, contentLen int, enc promencoding.Encbuf) *FilePoolDecbufFactory {
	lenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(contentLen))
	fileBytes := append(lenBytes, enc.Get()...)

	dir := t.TempDir()
	filePath := path.Join(dir, fmt.Sprintf("test-file-%p", t))
	require.NoError(t, os.WriteFile(filePath, fileBytes, 0700))

	reg := prometheus.NewRegistry()
	factory := NewFilePoolDecbufFactory(filePath, poolSize, filepool.NewFilePoolMetrics(reg))
	t.Cleanup(func() {
		_ = factory.Close()
	})

	return factory
}
