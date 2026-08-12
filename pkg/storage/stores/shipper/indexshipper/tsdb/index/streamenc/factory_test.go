// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/factory_test.go
// Provenance-includes-license: AGPL-3.0-only
// Provenance-includes-copyright: The Grafana Mimir Authors.

package streamenc

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
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/filepool"
)

const testContentSize = 4096

var table = crc32.MakeTable(crc32.Castagnoli)

func BenchmarkDecbufFactory_NewDecbufAtUnchecked(b *testing.B) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(table))

	diskFactory := createDecbufFactoryWithBytes(b, 1, testContentSize, enc)
	factories := map[string]DecbufFactory{
		"disk": diskFactory,
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

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
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

	testDecbufFactory(t, testContentSize+1000, enc, func(t *testing.T, factory DecbufFactory) {
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

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
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
	// between different Decbuf instances.
	factory := createDecbufFactoryWithBytes(t, 1, testContentSize, enc)
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

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
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

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
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

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
		d := factory.NewRawDecbuf(context.Background())
		t.Cleanup(func() {
			require.NoError(t, d.Close())
		})

		require.NoError(t, d.Err())
		require.Equal(t, 4+testContentSize+crc32.Size, d.Len())
	})
}

func TestDecbufFactory_Stop(t *testing.T) {
	enc := createTestEncoder(testContentSize)
	enc.PutHash(crc32.New(table))

	testDecbufFactory(t, testContentSize, enc, func(t *testing.T, factory DecbufFactory) {
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
	length int,
	enc promencoding.Encbuf,
	test func(t *testing.T, factory DecbufFactory),
) {
	t.Run("DecbufFactory=Disk-Pooled", func(t *testing.T) {
		diskFactory := createDecbufFactoryWithBytes(t, 1, length, enc)
		test(t, diskFactory)
	})

	t.Run("DecbufFactory=Disk-NoPool", func(t *testing.T) {
		diskFactory := createDecbufFactoryWithBytes(t, 0, length, enc)
		test(t, diskFactory)
	})
}

func createTestEncoder(numBytes int) promencoding.Encbuf {
	enc := promencoding.Encbuf{}

	for i := 0; i < numBytes; i++ {
		enc.PutByte(0x01)
	}

	return enc
}

func createDecbufFactoryWithBytes(t testing.TB, filePoolSize uint, length int, enc promencoding.Encbuf) *FilePoolDecbufFactory {
	// Prepend the contents of the buffer with the length of the content portion
	// which does not include the trailing 4 bytes for a CRC 32.
	lenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(length))
	bytes := append(lenBytes, enc.Get()...)

	dir := t.TempDir()
	fileName := "test-file"
	filePath := path.Join(dir, fileName)
	require.NoError(t, os.WriteFile(filePath, bytes, 0700))

	reg := prometheus.NewPedanticRegistry()
	diskFactory, err := NewFilePoolDecbufFactory(filePath, filePoolSize, filepool.NewFilePoolMetrics(reg))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = diskFactory.Close()
	})

	return diskFactory
}
