package dataset

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/memory"
)

func Benchmark_page_Decode(b *testing.B) {
	b.Run("workers=1", func(b *testing.B) { benchmarkPageDecodeParallel(b, 1) })
	b.Run("workers=2", func(b *testing.B) { benchmarkPageDecodeParallel(b, 2) })
	b.Run("workers=5", func(b *testing.B) { benchmarkPageDecodeParallel(b, 5) })
	b.Run("workers=10", func(b *testing.B) { benchmarkPageDecodeParallel(b, 10) })
}

func benchmarkPageDecodeParallel(b *testing.B, workers int) {
	page := logsTestPage(b)

	// Warm up one page reader per worker to avoid benchmarking the cost of
	// initializing zstd.
	{
		var wg sync.WaitGroup

		readers := make([]io.ReadCloser, workers)

		for worker := range workers {
			wg.Add(1)

			go func(worker int) {
				defer wg.Done()

				_, values, err := page.reader(datasetmd.COMPRESSION_TYPE_ZSTD)
				if err != nil {
					b.Error(err)
				} else if _, err := io.Copy(io.Discard, values); err != nil {
					b.Error(err)
				}

				readers[worker] = values
			}(worker)
		}

		wg.Wait()

		// Close all the readers at once; closing a reader releases it back to the
		// pool so we only do this after the workers are done to guarantee that
		// there is exactly one zstd reader in the pool per worker.
		for worker := range workers {
			require.NoError(b, readers[worker].Close())
		}
	}

	b.ResetTimer()

	var totalRead int
	for b.Loop() {
		var wg sync.WaitGroup

		for range workers {
			wg.Add(1)

			go func() {
				defer wg.Done()

				_, values, err := page.reader(datasetmd.COMPRESSION_TYPE_ZSTD)
				if err != nil {
					b.Error(err)
				} else if _, err := io.Copy(io.Discard, values); err != nil {
					b.Error(err)
				} else if err := values.Close(); err != nil {
					b.Error(err)
				}
			}()
		}

		wg.Wait()

		totalRead += page.Desc.UncompressedSize
	}

	b.ReportMetric(float64(totalRead)/1_000_000/b.Elapsed().Seconds(), "MBps/op")
}

func logsTestPage(t testing.TB) *MemPage {
	t.Helper()

	f, err := os.Open("testdata/access_logs.gz")
	require.NoError(t, err)
	defer f.Close()

	r, err := gzip.NewReader(f)
	require.NoError(t, err)

	var sb strings.Builder
	_, err = io.Copy(&sb, r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	opts := BuilderOptions{
		PageSizeHint: sb.Len() * 2,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}
	builder, err := newPageBuilder(opts)
	require.NoError(t, err)

	for line := range strings.Lines(sb.String()) {
		require.True(t, builder.Append(BinaryValue([]byte(line))))
	}

	page, err := builder.Flush()
	require.NoError(t, err)
	return page
}

func Test_pageBuilder_WriteRead(t *testing.T) {
	in := []string{
		"hello, world!",
		"",
		"this is a test of the emergency broadcast system",
		"this is only a test",
		"if this were a real emergency, you would be instructed to panic",
		"but it's not, so don't",
		"",
		"this concludes the test",
		"thank you for your cooperation",
		"goodbye",
	}

	opts := BuilderOptions{
		PageSizeHint: 1024,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}
	b, err := newPageBuilder(opts)
	require.NoError(t, err)

	for _, s := range in {
		require.True(t, b.Append(BinaryValue([]byte(s))))
	}

	page, err := b.Flush()
	require.NoError(t, err)
	require.Equal(t, len(in), page.Desc.RowCount)
	require.Equal(t, len(in), page.Desc.ValuesCount)

	t.Log("Uncompressed size: ", page.Desc.UncompressedSize)
	t.Log("Compressed size: ", page.Desc.CompressedSize)

	var actual []string

	r := newPageReader(page, opts.Type.Physical, opts.Compression)

	var alloc memory.Allocator
	for {
		arr, err := r.Read(t.Context(), &alloc, 1)
		if err != nil && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		} else if arr == nil && errors.Is(err, io.EOF) {
			break
		}

		if arr.IsNull(0) {
			actual = append(actual, "")
		} else {
			val := arr.(*columnar.UTF8).Get(0)
			actual = append(actual, string(val))
		}
	}
	require.Equal(t, in, actual)
}

func TestMemPage_open(t *testing.T) {
	// A page builder accepts the first value of a page whatever its size. One value makes a page that
	// decompresses to more than maxDecompressedSizeHint.
	values := []string{strings.Repeat("0123456789abcdef", maxDecompressedSizeHint/16+1024)}
	newOptions := func(compression datasetmd.CompressionType) BuilderOptions {
		return BuilderOptions{
			PageSizeHint: 1 << 20,
			Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
			Compression:  compression,
			Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		}
	}

	uncompressed, closer, err := buildPage(t, newOptions(datasetmd.COMPRESSION_TYPE_NONE), values).open(datasetmd.COMPRESSION_TYPE_NONE)
	require.NoError(t, err)
	require.NoError(t, closer.Close())

	tests := []struct {
		name             string
		compression      datasetmd.CompressionType
		uncompressedSize int
	}{
		{name: "decompresses a large snappy page whose descriptor claims a size too large for a buffer", compression: datasetmd.COMPRESSION_TYPE_SNAPPY, uncompressedSize: math.MaxInt},
		{name: "decompresses a large snappy page whose descriptor claims a negative size", compression: datasetmd.COMPRESSION_TYPE_SNAPPY, uncompressedSize: -1},
		{name: "decompresses a large zstd page whose descriptor claims a size too large for a buffer", compression: datasetmd.COMPRESSION_TYPE_ZSTD, uncompressedSize: math.MaxInt},
		{name: "decompresses a large zstd page whose descriptor claims a negative size", compression: datasetmd.COMPRESSION_TYPE_ZSTD, uncompressedSize: -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := buildPage(t, newOptions(tc.compression), values)
			page.Desc.UncompressedSize = tc.uncompressedSize

			got, closer, err := page.open(tc.compression)

			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })
			require.True(t, bytes.Equal(uncompressed.ValueData, got.ValueData), "the values of the page differ from the values of the uncompressed page")
		})
	}

	t.Run("decompresses a zstd page whose frame checksum is wrong but whose page CRC32 is valid", func(t *testing.T) {
		page := buildPage(t, newOptions(datasetmd.COMPRESSION_TYPE_ZSTD), values)
		page.Data = bytes.Clone(page.Data)

		// The page holds one zstd frame, so the frame checksum is the last 4 bytes of the page.
		for i := len(page.Data) - 4; i < len(page.Data); i++ {
			page.Data[i] ^= 0xff
		}
		page.Desc.CRC32 = crc32.Checksum(page.Data, checksumTable)

		bitmapSize, n := binary.Uvarint(page.Data)
		require.Positive(t, n)
		decoder, err := zstd.NewReader(nil)
		require.NoError(t, err)
		t.Cleanup(decoder.Close)
		_, err = decoder.DecodeAll(page.Data[n+int(bitmapSize):], nil)
		require.EqualError(t, err, zstd.ErrCRCMismatch.Error(), "the corrupted frame must fail a decoder that checks the frame checksum")

		got, closer, err := page.open(datasetmd.COMPRESSION_TYPE_ZSTD)

		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		require.True(t, bytes.Equal(uncompressed.ValueData, got.ValueData), "the values of the page differ from the values of the uncompressed page")
	})

	t.Run("returns an error for a zstd page whose bytes changed after its page CRC32 was computed", func(t *testing.T) {
		page := buildPage(t, newOptions(datasetmd.COMPRESSION_TYPE_ZSTD), values)
		page.Data = bytes.Clone(page.Data)
		page.Data[len(page.Data)-1] ^= 0xff
		want := fmt.Sprintf("invalid CRC32 checksum %x, expected %x", crc32.Checksum(page.Data, checksumTable), page.Desc.CRC32)

		_, _, err := page.open(datasetmd.COMPRESSION_TYPE_ZSTD)

		require.EqualError(t, err, want)
	})
}

func Test_pageBuilder_Fill(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1_500_000,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "timestamp"},
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
	}
	buf, err := newPageBuilder(opts)
	require.NoError(t, err)

	ts := time.Now().UTC()
	for buf.Append(Int64Value(ts.UnixNano())) {
		ts = ts.Add(time.Duration(rand.Intn(5000)) * time.Millisecond)
	}

	page, err := buf.Flush()
	require.NoError(t, err)
	require.Equal(t, page.Desc.UncompressedSize, page.Desc.CompressedSize)

	t.Log("Uncompressed size: ", page.Desc.UncompressedSize)
	t.Log("Compressed size: ", page.Desc.CompressedSize)
	t.Log("Row count: ", page.Desc.RowCount)
}
