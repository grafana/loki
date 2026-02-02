package dataset

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
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
	var alloc memory.Allocator

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
	actualValues, err := readPage(&alloc, pr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, pageReaderTestStrings, actual)
}

func Test_pageReader_SeekToStart(t *testing.T) {
	var alloc memory.Allocator

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
	_, err := readPage(&alloc, pr, 4)
	require.NoError(t, err)

	// Seek back to the start of the page and read all values again.
	_, err = pr.Seek(0, io.SeekStart)
	require.NoError(t, err)

	actualValues, err := readPage(&alloc, pr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, pageReaderTestStrings, actual)
}

func Test_pageReader_Reset(t *testing.T) {
	var alloc memory.Allocator

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
	_, err := readPage(&alloc, pr, 4)
	require.NoError(t, err)

	// Reset and read all values again.
	pr.Reset(page, opts.Type.Physical, opts.Compression)

	actualValues, err := readPage(&alloc, pr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, pageReaderTestStrings, actual)
}

func Test_pageReader_SkipRows(t *testing.T) {
	var alloc memory.Allocator

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

	actualValues, err := readPage(&alloc, pr, 4)
	require.NoError(t, err)

	actual := convertToStrings(t, actualValues)
	require.Equal(t, pageReaderTestStrings[4:], actual)
}

func Test_pageReader_statsStability(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1024,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
		Statistics:   StatisticsOptions{StoreRangeStats: true},
	}
	testStrings := []Value{
		BinaryValue([]byte("a")),
		BinaryValue([]byte("b")),
		BinaryValue([]byte("c")),
		BinaryValue([]byte("d")),
	}
	b, err := newPageBuilder(opts)
	require.NoError(t, err)

	for _, s := range testStrings {
		require.True(t, b.Append(s))
	}
	require.Equal(t, BinaryValue([]byte("a")), b.minValue)
	require.Equal(t, BinaryValue([]byte("d")), b.maxValue)

	// Simulate a Value being re-used and mutated by another read before it is flushed.
	testStrings[0].Buffer()[0] = 'Z'
	require.Equal(t, BinaryValue([]byte("a")), b.minValue)
	require.Equal(t, BinaryValue([]byte("d")), b.maxValue)
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

func readPage(alloc *memory.Allocator, pr *pageReader, batchSize int) (columnar.Array, error) {
	// Temporary allocator for the intermediate arrays.
	tempAlloc := memory.NewAllocator(alloc)
	defer tempAlloc.Free()

	var arrs []columnar.Array
	for {
		arr, err := pr.Read(context.Background(), tempAlloc, batchSize)
		if arr != nil {
			arrs = append(arrs, arr)
		}

		if errors.Is(err, io.EOF) {
			return columnar.Concat(alloc, arrs)
		} else if err != nil {
			return nil, err
		}
	}
}

func convertToStrings(t *testing.T, arr columnar.Array) []string {
	t.Helper()

	if arr == nil {
		return nil
	}

	utf8Arr := arr.(*columnar.UTF8)
	out := make([]string, 0, utf8Arr.Len())

	for i := range utf8Arr.Len() {
		// No need to check for null here; Get will return an empty byte slice
		// for null values.
		out = append(out, string(utf8Arr.Get(i)))
	}
	return out
}
