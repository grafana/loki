package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/memory"
)

// columnReaderTestStrings contains enough strings to span multiple pages
var columnReaderTestStrings = []string{
	"column reader string 1",
	"column reader string 2",
	"",
	"column reader string 4",
	"column reader string 5",
	"column reader string 1",
	"column reader string 2",
	"",
	"column reader string 4",
	"column reader string 5",
	"column reader string 1",
	"column reader string 2",
	"",
	"column reader string 4",
	"column reader string 5",
}

func Test_columnReader_ReadAll(t *testing.T) {
	var alloc memory.Allocator

	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	cr := newColumnReader(col)
	actual, err := readColumn(t, &alloc, cr, 4)
	require.NoError(t, err)
	require.Equal(t, columnReaderTestStrings, actual)
}

func Test_columnReader_ReadEach(t *testing.T) {
	var alloc memory.Allocator

	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	t.Run("visits one array per page in order and ends with EOF", func(t *testing.T) {
		tempAlloc := memory.NewAllocator(&alloc)
		defer tempAlloc.Free()

		var actual [][]string
		cr := newColumnReader(col)
		err := cr.ReadEach(t.Context(), tempAlloc, len(columnReaderTestStrings)+5, func(arr columnar.Array) error {
			actual = append(actual, convertToStrings(t, arr))
			return nil
		})
		require.ErrorIs(t, err, io.EOF)

		var (
			expected [][]string
			rest     = columnReaderTestStrings
		)
		for _, page := range col.Pages {
			rows := page.PageDesc().RowCount
			expected = append(expected, rest[:rows])
			rest = rest[rows:]
		}
		require.Equal(t, expected, actual)
	})

	t.Run("stops at the first error of visit", func(t *testing.T) {
		tempAlloc := memory.NewAllocator(&alloc)
		defer tempAlloc.Free()

		var (
			visits   int
			errVisit = errors.New("visit failed")
		)
		cr := newColumnReader(col)
		err := cr.ReadEach(t.Context(), tempAlloc, len(columnReaderTestStrings), func(columnar.Array) error {
			visits++
			return errVisit
		})
		require.ErrorIs(t, err, errVisit)
		require.Equal(t, 1, visits)
	})
}

func Test_columnReader_SeekAcrossPages(t *testing.T) {
	var alloc memory.Allocator

	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	// Find a position near the end of the first page
	endFirstPage := col.Pages[0].PageDesc().RowCount - 2

	cr := newColumnReader(col)
	_, err := cr.Seek(int64(endFirstPage), io.SeekStart)
	require.NoError(t, err)

	// Read enough values to span into the second page
	arr, err := cr.Read(t.Context(), &alloc, 4)
	require.NoError(t, err)
	require.Equal(t, 4, arr.Len())

	actual := convertToStrings(t, arr)
	expected := columnReaderTestStrings[endFirstPage : endFirstPage+4]
	require.Equal(t, expected, actual)
}

func Test_columnReader_SeekToStart(t *testing.T) {
	var alloc memory.Allocator

	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	cr := newColumnReader(col)

	// First read everything
	_, err := readColumn(t, &alloc, cr, 4)
	require.NoError(t, err)

	// Seek back to start and read again
	_, err = cr.Seek(0, io.SeekStart)
	require.NoError(t, err)

	actual, err := readColumn(t, &alloc, cr, 4)
	require.NoError(t, err)

	require.Equal(t, columnReaderTestStrings, actual)
}

func Test_columnReader_Reset(t *testing.T) {
	var alloc memory.Allocator

	col := buildMultiPageColumn(t, columnReaderTestStrings)
	require.Greater(t, len(col.Pages), 1, "test requires multiple pages")

	cr := newColumnReader(col)

	// First read everything
	_, err := readColumn(t, &alloc, cr, 4)
	require.NoError(t, err)

	// Reset and read again
	cr.Reset(col)

	actual, err := readColumn(t, &alloc, cr, 4)
	require.NoError(t, err)

	require.Equal(t, columnReaderTestStrings, actual)
}

func TestColumnReader_errors(t *testing.T) {
	// A column reader that retries forever never returns. The timeout makes that regression fail the
	// test instead of hanging it.
	read := func(t *testing.T, cr *columnReader, count int) error {
		var alloc memory.Allocator
		done := make(chan error, 1)
		go func() {
			_, err := cr.Read(t.Context(), &alloc, count)
			done <- err
		}()

		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("the column reader did not return")
			return nil
		}
	}

	t.Run("returns an unexpected EOF error when a page fails to load with an EOF", func(t *testing.T) {
		memColumn := buildMultiPageColumn(t, columnReaderTestStrings)
		cr := newColumnReader(stubColumn{MemColumn: memColumn, err: fmt.Errorf("get page: %w", io.EOF)})

		err := read(t, cr, 4)

		require.EqualError(t, err, fmt.Sprintf("page ended at row 0 of %d: unexpected EOF (get page: EOF)", memColumn.Pages[0].Desc.RowCount))
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.NotErrorIs(t, err, io.EOF)
	})

	t.Run("returns an unexpected EOF error when the descriptor of a page counts more rows than the page holds", func(t *testing.T) {
		memColumn := buildMultiPageColumn(t, columnReaderTestStrings)
		lastPage := memColumn.Pages[len(memColumn.Pages)-1]
		realRows := lastPage.Desc.RowCount
		lastPage.Desc.RowCount++
		cr := newColumnReader(stubColumn{MemColumn: memColumn})

		err := read(t, cr, len(columnReaderTestStrings)+1)

		require.EqualError(t, err, fmt.Sprintf("page ended at row %d of %d: unexpected EOF (EOF)", realRows, realRows+1))
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.NotErrorIs(t, err, io.EOF)
	})
}

// buildMultiPageColumn creates a column with multiple pages by using a small page size
func buildMultiPageColumn(t *testing.T, values []string) *MemColumn {
	t.Helper()

	builder, err := NewColumnBuilder("", BuilderOptions{
		PageSizeHint: 128, // Small page size to force multiple pages
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	})
	require.NoError(t, err)

	for i, v := range values {
		require.NoError(t, builder.Append(i, BinaryValue([]byte(v))))
	}

	col, err := builder.Flush()
	require.NoError(t, err)
	return col
}

func readColumn(t *testing.T, alloc *memory.Allocator, cr *columnReader, batchSize int) ([]string, error) {
	t.Helper()

	// Temporary allocator for the intermediate arrays.
	tempAlloc := memory.NewAllocator(alloc)
	defer tempAlloc.Free()

	var all []string

	for {
		// Memory doesn't survive past the loop so we can eagerly reclaim.
		tempAlloc.Reclaim()

		arr, err := cr.Read(context.Background(), tempAlloc, batchSize)
		if arr != nil {
			all = append(all, convertToStrings(t, arr)...)
		}
		if errors.Is(err, io.EOF) {
			return all, nil
		} else if err != nil {
			return all, err
		}
	}
}

// stubColumn is a column of stubPages.
type stubColumn struct {
	*MemColumn
	err error
}

func (c stubColumn) ListPages(context.Context) result.Seq[Page] {
	return result.Iter(func(yield func(Page) bool) error {
		for _, p := range c.Pages {
			if !yield(stubPage{MemPage: p, err: c.err}) {
				return nil
			}
		}
		return nil
	})
}

// stubPage is a page that fails to load with err when err is not nil. Otherwise, it loads like a
// memory page. After the context ends, it fails with the error of the context.
type stubPage struct {
	*MemPage
	err error
}

func (p stubPage) ReadPage(ctx context.Context) (PageData, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if p.err != nil {
		return nil, p.err
	}
	return p.MemPage.ReadPage(ctx)
}
