package dataset

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

var pageReaderTestStrings = []string{
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

func Test_pageReader(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1024,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}

	page := buildPage(t, opts, pageReaderTestStrings)
	require.Equal(t, len(pageReaderTestStrings), page.Info.RowCount)
	require.Equal(t, len(pageReaderTestStrings)-2, page.Info.ValuesCount) // -2 for the empty strings

	t.Log("Uncompressed size: ", page.Info.UncompressedSize)
	t.Log("Compressed size: ", page.Info.CompressedSize)

	pr := newPageReader(page, opts.Value, opts.Compression)
	actualValues, err := readPage(pr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, pageReaderTestStrings, actual)
}

func Test_pageReader_SeekToStart(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1024,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}

	page := buildPage(t, opts, pageReaderTestStrings)
	require.Equal(t, len(pageReaderTestStrings), page.Info.RowCount)
	require.Equal(t, len(pageReaderTestStrings)-2, page.Info.ValuesCount) // -2 for the empty strings

	t.Log("Uncompressed size: ", page.Info.UncompressedSize)
	t.Log("Compressed size: ", page.Info.CompressedSize)

	pr := newPageReader(page, opts.Value, opts.Compression)
	_, err := readPage(pr, 4)
	require.NoError(t, err)

	// Seek back to the start of the page and read all values again.
	_, err = pr.Seek(0, io.SeekStart)
	require.NoError(t, err)

	actualValues, err := readPage(pr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, pageReaderTestStrings, actual)
}

func Test_pageReader_Reset(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1024,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}

	page := buildPage(t, opts, pageReaderTestStrings)
	require.Equal(t, len(pageReaderTestStrings), page.Info.RowCount)
	require.Equal(t, len(pageReaderTestStrings)-2, page.Info.ValuesCount) // -2 for the empty strings

	t.Log("Uncompressed size: ", page.Info.UncompressedSize)
	t.Log("Compressed size: ", page.Info.CompressedSize)

	pr := newPageReader(page, opts.Value, opts.Compression)
	_, err := readPage(pr, 4)
	require.NoError(t, err)

	// Reset and read all values again.
	pr.Reset(page, opts.Value, opts.Compression)

	actualValues, err := readPage(pr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, pageReaderTestStrings, actual)
}

func Test_pageReader_SkipRows(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1024,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}

	page := buildPage(t, opts, pageReaderTestStrings)
	require.Equal(t, len(pageReaderTestStrings), page.Info.RowCount)
	require.Equal(t, len(pageReaderTestStrings)-2, page.Info.ValuesCount) // -2 for the empty strings

	t.Log("Uncompressed size: ", page.Info.UncompressedSize)
	t.Log("Compressed size: ", page.Info.CompressedSize)

	pr := newPageReader(page, opts.Value, opts.Compression)

	_, err := pr.Seek(4, io.SeekStart)
	require.NoError(t, err)

	actualValues, err := readPage(pr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, pageReaderTestStrings[4:], actual)
}

func buildPage(t *testing.T, opts BuilderOptions, in []string) *MemPage {
	t.Helper()

	b, err := newPageBuilder(opts)
	require.NoError(t, err)

	for _, s := range in {
		require.True(t, b.Append(StringValue(s)))
	}

	page, err := b.Flush()
	require.NoError(t, err)
	return page
}

func readPage(pr *pageReader, batchSize int) ([]Value, error) {
	var (
		all []Value

		batch = make([]Value, batchSize)
	)

	for {
		// Clear the batch for each read; this is required to ensure that any
		// memory inside of Value doesn't get reused.
		//
		// This requires any Value provided by pr.Read is owned by the caller and
		// is not retained by the reader; if a test fails and appears to have
		// memory reuse, it's likely because code in pageReader changed and broke
		// ownership semantics.
		clear(batch)

		n, err := pr.Read(context.Background(), batch)
		if n > 0 {
			all = append(all, batch[:n]...)
		}
		if errors.Is(err, io.EOF) {
			return all, nil
		} else if err != nil {
			return all, err
		}
	}
}

func convertToStrings(t *testing.T, values []Value) []string {
	t.Helper()

	out := make([]string, 0, len(values))

	for _, v := range values {
		if v.IsNil() {
			out = append(out, "")
		} else {
			require.Equal(t, datasetmd.VALUE_TYPE_STRING, v.Type())
			out = append(out, v.String())
		}
	}

	return out
}
