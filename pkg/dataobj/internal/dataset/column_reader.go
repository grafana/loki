package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
	"github.com/grafana/loki/v3/pkg/memory"
)

type columnReader struct {
	column      Column
	initialized bool // Whether the column reader has been initialized.

	pages     []Page
	pageIndex int // Current index into pages.

	ranges []rangeset.Range // Ranges of each page.

	reader *pageReader

	nextRow int64
}

// newColumnReader creates a new column reader for the given column.
func newColumnReader(column Column) *columnReader {
	var cr columnReader
	cr.Reset(column)
	return &cr
}

// Read returns an array of up to the next count values from the column. At the
// end of the column, Read returns nil, io.EOF.
//
// If there was an error reading the column, Read returns the error with no
// array.
func (cr *columnReader) Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
	if !cr.initialized {
		err := cr.init(ctx)
		if err != nil {
			return nil, err
		}
	}

	// Create a temporary allocator for intermediate arrays.
	tempAlloc := memory.NewAllocator(alloc)
	defer tempAlloc.Free()

	var arrs []columnar.Array
	buildResult := func() (columnar.Array, error) {
		if len(arrs) == 0 {
			return nil, nil
		}
		return columnar.Concat(alloc, arrs)
	}

	var totalRead int
	for totalRead < count {
		// Make sure our reader is initialized to the right page for the row we
		// wish to read.
		//
		// If the next row is out of range of all pages, we return io.EOF here.
		if cr.pageIndex == -1 || !cr.ranges[cr.pageIndex].Contains(uint64(cr.nextRow)) {
			page, pageIndex, err := cr.nextPage()
			if err != nil {
				res, _ := buildResult()
				return res, err
			}

			switch cr.reader {
			case nil:
				cr.reader = newPageReader(page, cr.column.ColumnDesc().Type.Physical, cr.column.ColumnDesc().Compression)
			default:
				cr.reader.Reset(page, cr.column.ColumnDesc().Type.Physical, cr.column.ColumnDesc().Compression)
			}

			cr.pageIndex = pageIndex
		}

		// Ensure that our page reader is set to the correct row within the page.
		//
		// This seek is likely a no-op after the first iteration of the loop, but
		// we call it each time anyway for safety.
		pageRow := cr.nextRow - int64(cr.ranges[cr.pageIndex].Start)
		if _, err := cr.reader.Seek(pageRow, io.SeekStart); err != nil {
			// This should only return an error if pageRow < 0, shouldn't happen:
			// cr.nextRow will always be >= cr.ranges[cr.pageIndex].Start.
			res, _ := buildResult()
			return res, err
		}

		// TODO(rfratto): read new pages to completion.
		//
		// Because rem shrinks with every iteration of our loop (one loop
		// per page), we end up only partially reading into the final page on
		// the final iteration.
		//
		// This is less efficient than reading the entire page due to
		// re-entering the page reader + decoder, especially since we typically
		// end up reading the rest of the page later on anyway.
		rem := count - totalRead
		arr, err := cr.reader.Read(ctx, tempAlloc, rem)
		if arr != nil {
			arrs = append(arrs, arr)
			cr.nextRow += int64(arr.Len())
			totalRead += arr.Len()
		}

		// We ignore io.EOF errors from the page; the next loop will detect and
		// report EOF on the call to [columnReader.nextPage].
		if err != nil && !errors.Is(err, io.EOF) {
			res, _ := buildResult()
			return res, err
		}
	}

	return buildResult()
}

// nextPage returns the page where cr.nextRow is contained. If cr.nextRow is
// beyond the boundaries of the column, nextPage returns nil, -1, io.EOF.
func (cr *columnReader) nextPage() (Page, int, error) {
	i := sort.Search(len(cr.ranges), func(i int) bool {
		return cr.ranges[i].End > uint64(cr.nextRow)
	})
	if i < len(cr.pages) && cr.ranges[i].Contains(uint64(cr.nextRow)) {
		return cr.pages[i], i, nil
	}
	return nil, -1, io.EOF
}

// init initializes the set of pages and page row ranges.
func (cr *columnReader) init(ctx context.Context) error {
	var startRow uint64

	for result := range cr.column.ListPages(ctx) {
		page, err := result.Value()
		if err != nil {
			return err
		}

		endRow := startRow + uint64(page.PageDesc().RowCount)

		// TODO(rfratto): including page count in the column info metadata would
		// allow us to set the capacity of cr.pages and cr.ranges more precisely.
		cr.pages = append(cr.pages, page)
		cr.ranges = append(cr.ranges, rangeset.Range{Start: startRow, End: endRow})

		startRow = endRow
	}

	cr.initialized = true
	return nil
}

// Seek sets the row offset for the next Read call, interpreted according to
// whence:
//
//   - [io.SeekStart] seeks relative to the start of the column,
//   - [io.SeekCurrent] seeks relative to the current offset, and
//   - [io.SeekEnd] seeks relative to the end (for example, offset = -2 specifies the penultimate row of the column).
//
// Seek returns the new offset relative to the start of the column or an error,
// if any.
//
// To retrieve the current offset without modification, call Seek with 0 and
// [io.SeekCurrent].
//
// Seeking to an offset before the start of the column is an error. Seeking to
// beyond the end of the column will cause the next Read to return io.EOF.
func (cr *columnReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return 0, errors.New("invalid offset")
		}
		cr.nextRow = offset

	case io.SeekCurrent:
		if cr.nextRow+offset < 0 {
			return 0, errors.New("invalid offset")
		}
		cr.nextRow += offset

	case io.SeekEnd:
		lastRow := int64(cr.column.ColumnDesc().RowsCount)
		if lastRow+offset < 0 {
			return 0, errors.New("invalid offset")
		}
		cr.nextRow = lastRow + offset

	default:
		return 0, fmt.Errorf("invalid whence value %d", whence)
	}

	return cr.nextRow, nil
}

// Reset resets the column reader to read from the start of the provided
// column. This permits reusing a column reader rather than allocating a new
// one.
func (cr *columnReader) Reset(column Column) {
	cr.column = column
	cr.initialized = false

	cr.pages = sliceclear.Clear(cr.pages)
	cr.pageIndex = -1

	cr.ranges = sliceclear.Clear(cr.ranges)

	if cr.reader != nil {
		// Resetting takes the place of calling Close here.
		cr.reader.Reset(nil, column.ColumnDesc().Type.Physical, column.ColumnDesc().Compression)
	}

	cr.nextRow = 0
}

// Close closes the columnReader. Closed columnReaders can be reused by calling
// [columnReader.Reset].
func (cr *columnReader) Close() error {
	if cr.reader != nil {
		return cr.reader.Close()
	}
	return nil
}
