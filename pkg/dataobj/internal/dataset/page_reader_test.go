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

func Test_pageReader_ReadAfterError(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1024,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}

	t.Run("returns the rest of the page when a read to the end of the page retries after a decoder error", func(t *testing.T) {
		var alloc memory.Allocator

		pr := newPageReader(buildPage(t, opts, pageReaderTestStrings), opts.Type.Physical, opts.Compression)
		t.Cleanup(func() { require.NoError(t, pr.Close()) })

		first, err := pr.Read(t.Context(), &alloc, 2)
		require.NoError(t, err)
		require.Equal(t, pageReaderTestStrings[:2], convertToStrings(t, first))

		pr.valuesDec = &failOnceValueDecoder{valueDecoder: pr.valuesDec}

		_, err = pr.Read(t.Context(), &alloc, len(pageReaderTestStrings))
		require.EqualError(t, err, "injected decode error")

		rest, err := pr.Read(t.Context(), &alloc, len(pageReaderTestStrings))
		require.NoError(t, err)
		require.Equal(t, pageReaderTestStrings[2:], convertToStrings(t, rest))

		_, err = pr.Read(t.Context(), &alloc, len(pageReaderTestStrings))
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("returns the same rows when a read in the middle of a page with nulls retries after a decoder error", func(t *testing.T) {
		var alloc memory.Allocator

		page := buildPageWithNulls(t, opts, []Value{
			BinaryValue([]byte("a")),
			BinaryValue([]byte("b")),
			{},
			BinaryValue([]byte("c")),
			BinaryValue([]byte("d")),
			{},
			BinaryValue([]byte("e")),
			BinaryValue([]byte("f")),
			BinaryValue([]byte("g")),
			BinaryValue([]byte("h")),
		})
		pr := newPageReader(page, opts.Type.Physical, opts.Compression)
		t.Cleanup(func() { require.NoError(t, pr.Close()) })

		first, err := pr.Read(t.Context(), &alloc, 2)
		require.NoError(t, err)
		require.Equal(t, []string{"a", "b"}, convertToStrings(t, first))

		pr.valuesDec = &failOnceValueDecoder{valueDecoder: pr.valuesDec}

		_, err = pr.Read(t.Context(), &alloc, 4)
		require.EqualError(t, err, "injected decode error")

		retried, err := pr.Read(t.Context(), &alloc, 4)
		require.NoError(t, err)
		require.Equal(t, []string{"", "c", "d", ""}, convertToStrings(t, retried))

		rest, err := pr.Read(t.Context(), &alloc, 100)
		require.NoError(t, err)
		require.Equal(t, []string{"e", "f", "g", "h"}, convertToStrings(t, rest))
	})
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

func buildPageWithNulls(t *testing.T, opts BuilderOptions, in []Value) *MemPage {
	t.Helper()

	b, err := newPageBuilder(opts)
	require.NoError(t, err)

	for _, v := range in {
		require.True(t, b.Append(v))
	}

	page, err := b.Flush()
	require.NoError(t, err)
	return page
}

// failOnceValueDecoder fails its first Decode call with an error, without
// reading any value, and then behaves like the decoder it wraps.
type failOnceValueDecoder struct {
	valueDecoder
	failed bool
}

func (d *failOnceValueDecoder) Decode(alloc *memory.Allocator, count int) (columnar.Array, error) {
	if !d.failed {
		d.failed = true
		return nil, errors.New("injected decode error")
	}
	return d.valueDecoder.Decode(alloc, count)
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
