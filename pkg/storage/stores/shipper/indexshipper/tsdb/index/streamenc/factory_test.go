// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/factory_test.go
// Provenance-includes-license: AGPL-3.0-only
// Provenance-includes-copyright: The Grafana Mimir Authors.

package encoding

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	promencoding "github.com/prometheus/prometheus/tsdb/encoding"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/filepool"
)

const testContentSize = 4096

var table = crc32.MakeTable(crc32.Castagnoli)

func BenchmarkDecbufFactory_NewDecbufAtUnchecked(b *testing.B) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(table))

	diskFactory, bucketFactory := createDecbufFactoriesWithBytes(b, 1, testContentSize, enc)
	factories := map[string]DecbufFactory{
		"disk":   diskFactory,
		"bucket": bucketFactory,
	}
	b.ResetTimer()

	for factoryName, factory := range factories {
		b.Run(fmt.Sprintf("DecbufFactory=%s", factoryName), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				d := factory.NewDecbufAtUnchecked(context.Background(), 0)

				if err := d.Err(); err != nil {
					require.NoError(b, err)
				}

				if err := d.Close(); err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}

func TestDecbufFactory_NewDecbufAtChecked_InvalidCRC(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutBytes([]byte{0, 0, 0, 0})

	testDecbufFactory(t, testContentSize, enc, true, true, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufAtChecked(context.Background(), 0, table)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.ErrorIs(t, d.Err(), ErrInvalidChecksum)
	})
}

func TestDecbufFactory_NewDecbufAtChecked_InvalidLength(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(table))

	testDecbufFactory(t, testContentSize+1000, enc, true, true, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufAtChecked(context.Background(), 0, table)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.ErrorIs(t, d.Err(), ErrInvalidSize)
	})
}

func TestDecbufFactory_NewDecbufAtChecked_HappyPath(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(table))

	testDecbufFactory(t, testContentSize, enc, true, true, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufAtChecked(context.Background(), 0, table)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.NoError(t, d.Err())
		require.Equal(t, testContentSize+crc32.Size, d.Len())
	})
}

func TestDecbufFactory_NewDecbufAtChecked_MultipleInstances(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(table))

	// Note that we create the factory ourselves instead of using testDecbufFactory because
	// we only want to test the case where file handles are pooled and hence will be reused
	// between different Decbuf instances; bucket-based factory is also not tested here.
	factory, _ := createDecbufFactoriesWithBytes(t, 1, testContentSize, enc)
	t.Cleanup(func() {
		_ = factory.Close()
	})

	d1 := factory.NewDecbufAtChecked(context.Background(), 0, table)
	require.NoError(t, d1.Err())
	fr1, ok := d1.r.(*FileReader)
	require.True(t, ok, "expected FileReader")
	fd1 := fr1.file.Fd()
	require.NoError(t, d1.Close())

	d2 := factory.NewDecbufAtChecked(context.Background(), 0, table)
	require.NoError(t, d2.Err())
	fr2, ok := d2.r.(*FileReader)
	require.True(t, ok, "expected FileReader")
	fd2 := fr2.file.Fd()
	require.NoError(t, d2.Close())

	require.Equal(t, fd1, fd2, "expected Decbuf instances to use the same file descriptor")
}

func TestDecbufFactory_NewDecbufAtChecked_Concurrent(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(table))

	const (
		runs        = 100
		concurrency = 10
	)

	testDecbufFactory(t, testContentSize, enc, true, true, func(t *testing.T, factory DecbufFactory) {
		g, ctx := errgroup.WithContext(context.Background())

		for i := 0; i < concurrency; i++ {
			g.Go(func() error {
				for run := 0; run < runs; run++ {
					d := factory.NewDecbufAtChecked(ctx, 0, table)

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
	enc.PutHash(crc32.New(table))

	testDecbufFactory(t, testContentSize, enc, true, true, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufAtUnchecked(context.Background(), 0)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.NoError(t, d.Err())
		require.Equal(t, testContentSize+crc32.Size, d.Len())
	})
}

func TestDecbufFactory_NewDecbufRaw_HappyPath(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(table))

	testDecbufFactory(t, testContentSize, enc, true, true, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewRawDecbuf(context.Background())
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.NoError(t, d.Err())
		require.Equal(t, 4+testContentSize+crc32.Size, d.Len())
	})
}

// TestDecbufFactory_NewDecbufInSection_HappyPath tests that creating a new Decbuf for a table section
// starts the reader in the correct place by placing a test byte at the expected start offset.
func TestDecbufFactory_NewDecbufInSection_HappyPath(t *testing.T) {
	testByte := byte(0x02)
	startOffset := 10
	endOffset := 25
	enc := createTestEncoderWithTestByte(testContentSize, startOffset-numLenBytes, testByte)
	enc.PutHash(crc32.New(table))

	testDisk := false // NewDecbufInSection is currently only implemented for BucketDecbufFactory
	testDecbufFactory(t, testContentSize, enc, testDisk, true, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufInSection(context.Background(), 0, startOffset, endOffset)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})
		require.NoError(t, d.Err())
		require.Equal(t, endOffset-startOffset, d.Len())
		// Make sure our start offset is placed where we expected by reading the first byte
		require.Equal(t, testByte, d.Byte())
	})
}

// TestDecbufFactory_NewDecbufInSection_SectionEndOffsetBeyondTableLength tests that calling NewDecbufInSection
// with an end offset beyond the end of the table produces a Decbuf that ends at the end of the table.
func TestDecbufFactory_NewDecbufInSection_SectionEndOffsetBeyondTableLength(t *testing.T) {
	testByte := byte(0x02)
	enc := createTestEncoderWithTestByte(testContentSize, 10-numLenBytes, testByte)
	enc.PutHash(crc32.New(table))

	testDisk := false // NewDecbufInSection is currently only implemented for BucketDecbufFactory
	testDecbufFactory(t, testContentSize, enc, testDisk, true, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufInSection(context.Background(), 0, 10, testContentSize+1000)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})
		require.NoError(t, d.Err())
		require.Equal(t, testContentSize+numLenBytes-10, d.Len())
		require.Equal(t, testByte, d.Byte())
	})
}

// TestDecbufFactory_NewDecbufInSection_SectionEndOffsetBeforeStartOffset tests that attempting to call NewDecbufInSection
// with an end offset before the start offset returns an error.
func TestDecbufFactory_NewDecbufInSection_SectionEndOffsetBeforeStartOffset(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(table))

	testDisk := false // NewDecbufInSection is currently only implemented for BucketDecbufFactory
	testDecbufFactory(t, testContentSize, enc, testDisk, true, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewDecbufInSection(context.Background(), 0, 2500, 30)
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})
		require.Error(t, d.Err())
	})
}

func TestDecbufFactory_Stop(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(table))

	testBucket := false // bucket-based factory does not do anything on close and will not error
	testDecbufFactory(t, testContentSize, enc, true, testBucket, func(t *testing.T, factory DecbufFactory) {
		require.NoError(t, factory.Close())

		d := factory.NewRawDecbuf(context.Background())
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.ErrorIs(t, d.Err(), filepool.ErrPoolStopped)
	})
}

func testDecbufFactory(
	t *testing.T,
	len int,
	enc promencoding.Encbuf,
	testDisk bool,
	testBucket bool,
	test func(t *testing.T, factory DecbufFactory),
) {
	if testDisk {
		t.Run("DecbufFactory=Disk-Pooled", func(t *testing.T) {
			diskFactory, _ := createDecbufFactoriesWithBytes(t, 1, len, enc)
			test(t, diskFactory)
		})

		t.Run("DecbufFactory=Disk-NoPool", func(t *testing.T) {
			diskFactory, _ := createDecbufFactoriesWithBytes(t, 0, len, enc)
			test(t, diskFactory)
		})
	}

	if testBucket {
		t.Run("DecbufFactory=Bucket", func(t *testing.T) {
			_, bucketFactory := createDecbufFactoriesWithBytes(t, 0, len, enc)
			test(t, bucketFactory)
		})
	}

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

func createDecbufFactoriesWithBytes(t testing.TB, filePoolSize uint, len int, enc promencoding.Encbuf) (*FilePoolDecbufFactory, *BucketDecbufFactory) {
	// Prepend the contents of the buffer with the length of the content portion
	// which does not include the trailing 4 bytes for a CRC 32.
	lenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(len))
	bytes := append(lenBytes, enc.Get()...)

	dir := t.TempDir()
	fileName := "test-file"
	filePath := path.Join(dir, fileName)
	require.NoError(t, os.WriteFile(filePath, bytes, 0700))

	reg := prometheus.NewPedanticRegistry()
	diskFactory := NewFilePoolDecbufFactory(filePath, filePoolSize, filepool.NewFilePoolMetrics(reg))
	t.Cleanup(func() {
		_ = diskFactory.Close()
	})

	bkt, err := filesystem.NewBucket(dir)
	require.NoError(t, err)
	instBkt := objstore.WithNoopInstr(bkt)
	t.Cleanup(func() {
		require.NoError(t, bkt.Close())
	})
	bucketFactory := NewBucketDecbufFactory(instBkt, fileName)

	return diskFactory, bucketFactory
}
