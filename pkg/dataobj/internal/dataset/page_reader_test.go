package dataset

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/memory"
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
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}

	page := buildPage(t, opts, pageReaderTestStrings)
	require.Equal(t, len(pageReaderTestStrings), page.Desc.RowCount)
	require.Equal(t, len(pageReaderTestStrings), page.Desc.ValuesCount)

	t.Log("Uncompressed size: ", page.Desc.UncompressedSize)
	t.Log("Compressed size: ", page.Desc.CompressedSize)

	pr := newPageReader(page, opts.Type.Physical, opts.Compression)
	actualValues, err := readPage(pr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, pageReaderTestStrings, actual)
}

func Test_pageReader_SeekToStart(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1024,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}

	page := buildPage(t, opts, pageReaderTestStrings)
	require.Equal(t, len(pageReaderTestStrings), page.Desc.RowCount)
	require.Equal(t, len(pageReaderTestStrings), page.Desc.ValuesCount)

	t.Log("Uncompressed size: ", page.Desc.UncompressedSize)
	t.Log("Compressed size: ", page.Desc.CompressedSize)

	pr := newPageReader(page, opts.Type.Physical, opts.Compression)
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
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}

	page := buildPage(t, opts, pageReaderTestStrings)
	require.Equal(t, len(pageReaderTestStrings), page.Desc.RowCount)
	require.Equal(t, len(pageReaderTestStrings), page.Desc.ValuesCount)

	t.Log("Uncompressed size: ", page.Desc.UncompressedSize)
	t.Log("Compressed size: ", page.Desc.CompressedSize)

	pr := newPageReader(page, opts.Type.Physical, opts.Compression)
	_, err := readPage(pr, 4)
	require.NoError(t, err)

	// Reset and read all values again.
	pr.Reset(page, opts.Type.Physical, opts.Compression)

	actualValues, err := readPage(pr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, pageReaderTestStrings, actual)
}

func Test_pageReader_SkipRows(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1024,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}

	page := buildPage(t, opts, pageReaderTestStrings)
	require.Equal(t, len(pageReaderTestStrings), page.Desc.RowCount)
	require.Equal(t, len(pageReaderTestStrings), page.Desc.ValuesCount)

	t.Log("Uncompressed size: ", page.Desc.UncompressedSize)
	t.Log("Compressed size: ", page.Desc.CompressedSize)

	pr := newPageReader(page, opts.Type.Physical, opts.Compression)

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
		require.True(t, b.Append(BinaryValue([]byte(s))))
	}

	page, err := b.Flush()
	require.NoError(t, err)
	return page
}

func readPage(pr *pageReader, batchSize int) ([]Value, error) {
	// TODO(rfratto): This function should entirely move over to columnar.Array,
	// but that would require implementing some kind of array concatenation
	// first, as readPage is expected to read the entire page.

	var (
		alloc memory.Allocator // Temporary allocator for copying into all
		all   []Value
	)

	for {
		alloc.Reclaim()

		arr, err := pr.Read(context.Background(), &alloc, batchSize)
		if arr != nil {
			prevLen := len(all)

			all = slices.Grow(all, arr.Len())
			all = all[:prevLen+arr.Len()]

			copyArray(all[prevLen:], arr)
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
			require.Equal(t, datasetmd.PHYSICAL_TYPE_BINARY, v.Type())
			out = append(out, string(v.Binary()))
		}
	}

	return out
}
