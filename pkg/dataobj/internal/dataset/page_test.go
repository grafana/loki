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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/format"
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
	return buildPage(t, format.ColumnarVersionPageChecksum, opts, slices.Collect(strings.Lines(sb.String())))
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
	for _, version := range readableFormatVersions {
		t.Run(fmt.Sprintf("reads every value written to a page of format version %d", version), func(t *testing.T) {
			page := buildPage(t, version, opts, in)
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
		})
	}
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

	uncompressed, closer, err := buildPage(t, format.ColumnarVersionPageChecksum, newOptions(datasetmd.COMPRESSION_TYPE_NONE), values).open(datasetmd.COMPRESSION_TYPE_NONE)
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
			page := buildPage(t, format.ColumnarVersionPageChecksum, newOptions(tc.compression), values)
			page.Desc.UncompressedSize = tc.uncompressedSize

			got, closer, err := page.open(tc.compression)

			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })
			require.True(t, bytes.Equal(uncompressed.ValueData, got.ValueData), "the values of the page differ from the values of the uncompressed page")
		})
	}

	for _, version := range readableFormatVersions {
		t.Run(fmt.Sprintf("decompresses a zstd page of format version %d whose frame checksum is wrong but whose page checksum is valid", version), func(t *testing.T) {
			page := buildPage(t, version, newOptions(datasetmd.COMPRESSION_TYPE_ZSTD), values)
			body := bytes.Clone(pageBody(page))

			// The page holds one zstd frame, so the frame checksum is the last 4 bytes of the body.
			for i := len(body) - 4; i < len(body); i++ {
				body[i] ^= 0xff
			}
			setPageBody(page, body)

			bitmapSize, n := binary.Uvarint(body)
			require.Positive(t, n)
			decoder, err := zstd.NewReader(nil)
			require.NoError(t, err)
			t.Cleanup(decoder.Close)
			_, err = decoder.DecodeAll(body[n+int(bitmapSize):], nil)
			require.EqualError(t, err, zstd.ErrCRCMismatch.Error(), "the corrupted frame must fail a decoder that checks the frame checksum")

			got, closer, err := page.open(datasetmd.COMPRESSION_TYPE_ZSTD)

			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })
			require.True(t, bytes.Equal(uncompressed.ValueData, got.ValueData), "the values of the page differ from the values of the uncompressed page")
		})
	}

	for _, version := range []uint32{format.ColumnarVersionDescriptorChecksum, format.ColumnarVersionPageChecksum} {
		for _, compression := range []datasetmd.CompressionType{datasetmd.COMPRESSION_TYPE_NONE, datasetmd.COMPRESSION_TYPE_ZSTD} {
			t.Run(fmt.Sprintf("returns an error when a byte of a %s page of format version %d changes", compression, version), func(t *testing.T) {
				// Both versions hold the same body, so the checksum of the body is the same.
				body := buildPage(t, format.ColumnarVersionDescriptorChecksum, newOptions(compression), []string{"a", "bc"})
				changedBody := bytes.Clone(body.Data)
				changedBody[0] ^= 0xff
				want := fmt.Sprintf("invalid CRC32 checksum %x, expected %x", crc32.Checksum(changedBody, checksumTable), body.Desc.CRC32)
				page := buildPage(t, version, newOptions(compression), []string{"a", "bc"})
				page.Data = bytes.Clone(page.Data)
				page.Data[0] ^= 0xff

				_, _, err := page.open(compression)

				require.EqualError(t, err, want)
			})
		}
	}

	for _, compression := range []datasetmd.CompressionType{datasetmd.COMPRESSION_TYPE_NONE, datasetmd.COMPRESSION_TYPE_ZSTD} {
		t.Run(fmt.Sprintf("opens a %s page whose checksum is in the page", compression), func(t *testing.T) {
			page := buildPage(t, format.ColumnarVersionDescriptorChecksum, newOptions(compression), []string{"a", "bc"})
			// Closing an opened page returns its buffers to a pool, so copy the
			// values before you close it.
			want, closer, err := page.open(compression)
			require.NoError(t, err)
			wantPresence, wantValues := bytes.Clone(want.PresenceData), bytes.Clone(want.ValueData)
			require.NoError(t, closer.Close())

			got, closer, err := buildPage(t, format.ColumnarVersionPageChecksum, newOptions(compression), []string{"a", "bc"}).open(compression)

			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })
			require.Equal(t, wantPresence, got.PresenceData)
			require.Equal(t, wantValues, got.ValueData)
		})
	}

	t.Run("returns an error when the checksum of a page with its checksum in the page changes", func(t *testing.T) {
		page := buildPage(t, format.ColumnarVersionPageChecksum, newOptions(datasetmd.COMPRESSION_TYPE_NONE), []string{"a", "bc"})
		page.Data[len(page.Data)-1] ^= 0xff
		footer := page.Data[len(page.Data)-crc32.Size:]
		actual := crc32.Checksum(page.Data[:len(page.Data)-crc32.Size], checksumTable)

		_, _, err := page.open(datasetmd.COMPRESSION_TYPE_NONE)

		require.EqualError(t, err, fmt.Sprintf("invalid CRC32 checksum %x, expected %x", actual, binary.LittleEndian.Uint32(footer)))
	})

	t.Run("returns an unexpected EOF error when a page with its checksum in the page has fewer than four bytes", func(t *testing.T) {
		page := &MemPage{Desc: PageDesc{FormatVersion: format.ColumnarVersionPageChecksum}, Data: PageData{1, 2, 3}}

		_, _, err := page.open(datasetmd.COMPRESSION_TYPE_NONE)

		require.EqualError(t, err, "reading page checksum: unexpected EOF")
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
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
