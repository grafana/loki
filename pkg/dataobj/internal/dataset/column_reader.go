package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
)

type columnReader struct {
	column      Column
	initialized bool // Whether the column reader has been initialized.

	pages     []Page
	pageIndex int // Current index into pages.

	ranges []rowRange // Ranges of each page.

	reader *pageReader

	nextRow int64
}

// newColumnReader creates a new column reader for the given column.
func newColumnReader(column Column) *columnReader {
	var cr columnReader
	cr.Reset(column)
	return &cr
}

// Read reads up to the next len(v) values from the column into v. It returns
// the number of values read and any error encountered. At the end of the
// column, Read returns 0, io.EOF.
func (cr *columnReader) Read(ctx context.Context, v []Value) (n int, err error) {
	if !cr.initialized {
		err := cr.init(ctx)
		if err != nil {
			return 0, err
		}
	}

	for n < len(v) {
		// Make sure our reader is initialized to the right page for the row we
		// wish to read.
		//
		// If the next row is out of range of all pages, we return io.EOF here.
		if cr.pageIndex == -1 || !cr.ranges[cr.pageIndex].Contains(uint64(cr.nextRow)) {
			page, pageIndex, err := cr.nextPage()
			if err != nil {
				return n, err
			}

			switch cr.reader {
			case nil:
				cr.reader = newPageReader(page, cr.column.ColumnInfo().Type, cr.column.ColumnInfo().Compression)
			default:
				cr.reader.Reset(page, cr.column.ColumnInfo().Type, cr.column.ColumnInfo().Compression)
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
			return n, err
		}

		// Because len(v[n:]) shrinks with every iteration of our loop, one may be
		// concerned that future iterations of the loop have less scratch space to
		// work with, where scratch space is used to "skip" rows in the page.
		//
		// However, each call to [columnReader.Read] always reads a continguous set
		// of rows, potentially across multiple page boundaries. This means that
		// only the first call to cr.reader.Read will use the scratch space in v to
		// skip rows, where the scratch space is the entirety of len(v).
		count, err := cr.reader.Read(ctx, v[n:])
		cr.nextRow += int64(count)
		n += count

		// We ignore io.EOF errors from the page; the next loop will detect and
		// report EOF on the call to [columnReader.nextPage].
		if err != nil && !errors.Is(err, io.EOF) {
			return n, err
		}
	}

	return n, nil
}

// nextPage returns the page where cr.nextRow is contained. If cr.nextRow is
// beyond the boundaries of the column, nextPage returns nil, -1, io.EOF.
func (cr *columnReader) nextPage() (Page, int, error) {
	i := sort.Search(len(cr.ranges), func(i int) bool {
		return cr.ranges[i].End >= uint64(cr.nextRow)
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

		endRow := startRow + uint64(page.PageInfo().RowCount) - 1

		// TODO(rfratto): including page count in the column info metadata would
		// allow us to set the capacity of cr.pages and cr.ranges more precisely.
		cr.pages = append(cr.pages, page)
		cr.ranges = append(cr.ranges, rowRange{startRow, endRow})

		startRow = endRow + 1
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
		lastRow := int64(cr.column.ColumnInfo().RowsCount)
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
		cr.reader.Reset(nil, column.ColumnInfo().Type, column.ColumnInfo().Compression)
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
