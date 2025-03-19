package dataset

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
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

		totalRead += page.Info.UncompressedSize
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
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}
	builder, err := newPageBuilder(opts)
	require.NoError(t, err)

	for line := range strings.Lines(sb.String()) {
		require.True(t, builder.Append(StringValue(line)))
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
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}
	b, err := newPageBuilder(opts)
	require.NoError(t, err)

	for _, s := range in {
		require.True(t, b.Append(StringValue(s)))
	}

	page, err := b.Flush()
	require.NoError(t, err)
	require.Equal(t, len(in), page.Info.RowCount)
	require.Equal(t, len(in)-2, page.Info.ValuesCount) // -2 for the empty strings

	t.Log("Uncompressed size: ", page.Info.UncompressedSize)
	t.Log("Compressed size: ", page.Info.CompressedSize)

	var actual []string

	r := newPageReader(page, opts.Value, opts.Compression)
	for {
		var values [1]Value
		n, err := r.Read(context.Background(), values[:])
		if err != nil && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		} else if n == 0 && errors.Is(err, io.EOF) {
			break
		} else if n == 0 {
			continue
		}

		val := values[0]
		if val.IsNil() || val.IsZero() {
			actual = append(actual, "")
		} else {
			require.Equal(t, datasetmd.VALUE_TYPE_STRING, val.Type())
			actual = append(actual, val.String())
		}
	}
	require.Equal(t, in, actual)
}

func Test_pageBuilder_Fill(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1_500_000,
		Value:        datasetmd.VALUE_TYPE_INT64,
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
	require.Equal(t, page.Info.UncompressedSize, page.Info.CompressedSize)

	t.Log("Uncompressed size: ", page.Info.UncompressedSize)
	t.Log("Compressed size: ", page.Info.CompressedSize)
	t.Log("Row count: ", page.Info.RowCount)
}
