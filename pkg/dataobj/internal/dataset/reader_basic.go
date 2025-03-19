package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"
)

// basicReader is a low-level reader that reads rows from a set of columns.
//
// basicReader lazily reads pages from columns as they are iterated over; see
// [Reader] for a higher-level implementation that supports predicates and
// batching page downloads.
type basicReader struct {
	columns      []Column
	readers      []*columnReader
	columnLookup map[Column]int // Index into columns and readers

	buf []Value // Buffer for reading values from columns

	nextRow int64
}

// newBasicReader returns a new basicReader that reads rows from the given set
// of columns.
func newBasicReader(set []Column) *basicReader {
	var br basicReader
	br.Reset(set)
	return &br
}

// Read is a convenience wrapper around [basicReader.ReadColumns] that reads up
// to the next len(s) rows across the entire column set owned by [basicReader].
func (pr *basicReader) Read(ctx context.Context, s []Row) (n int, err error) {
	return pr.ReadColumns(ctx, pr.columns, s)
}

// ReadColumns reads up to the next len(s) rows from a subset of columns and
// stores them into s. It returns the number of rows read and any error
// encountered. At the end of the column set used by basicReader, ReadColumns
// returns 0, [io.EOF].
//
// Row.Values will be populated with one element per column in the order of the
// overall column set owned by basicReader.
//
// After calling ReadColumns, additional columns in s can be filled using
// [basicReader.Fill].
func (pr *basicReader) ReadColumns(ctx context.Context, columns []Column, s []Row) (n int, err error) {
	if len(columns) == 0 {
		return 0, fmt.Errorf("no columns to read")
	}

	// The implementation of ReadColumns can be expressed as a fill from
	// pr.nextRow to pr.nextRow + len(s).
	//
	// We initialize the row numbers of the entire slice of rows to simplicity,
	// even if we only fill a subset of them.
	for i := range s {
		s[i].Index = int(pr.nextRow + int64(i))
	}

	n, err = pr.fill(ctx, columns, s)
	pr.nextRow += int64(n)
	return n, err
}

// Fill fills values for the given columns into the provided rows. It returns
// the number of rows filled and any error encountered.
//
// s must be initialized such that s[i].Index specifies which row to fill
// values for.
//
// s[i].Values will be populated with one element per column in the order of
// the column set provided to [newBasicReader] or [basicReader.Reset].
//
// This allows callers to use Fill to implement efficient filtering:
//
//  1. Fill is called with columns to use for filtering.
//  2. The caller applies filtered on filled rows, removing any row that does
//     not pass the filter.
//  3. The caller calls Fill again with the remaining columns.
//
// Fill is most efficient when calls to Fill move each column in columns
// forward: that is, each filled row is in sorted order with no repeats across
// calls.
//
// Fill does not advance the offset of the basicReader.
func (pr *basicReader) Fill(ctx context.Context, columns []Column, s []Row) (n int, err error) {
	if len(columns) == 0 {
		return 0, fmt.Errorf("no columns to fill")
	}

	for partition := range partitionRows(s) {
		pn, err := pr.fill(ctx, columns, partition)
		n += pn
		if err != nil {
			return n, err
		} else if pn == 0 {
			break
		}
	}

	return n, nil
}

// partitionRows returns an iterator over a slice of rows that partitions the
// slice into groups of consecutive, non-repeating row incidices. Gaps between
// rows are treated as two different partitions.
func partitionRows(s []Row) iter.Seq[[]Row] {
	return func(yield func([]Row) bool) {
		if len(s) == 0 {
			return
		}

		start := 0
		for i := 1; i < len(s); i++ {
			if s[i].Index != s[i-1].Index+1 {
				if !yield(s[start:i]) {
					return
				}
				start = i
			}
		}

		yield(s[start:])
	}
}

// fill implements fill for a single slice of rows that are consecutive and
// have no gaps between them.
func (pr *basicReader) fill(ctx context.Context, columns []Column, s []Row) (n int, err error) {
	if len(s) == 0 {
		return 0, nil
	}

	pr.buf = slices.Grow(pr.buf, len(s))
	pr.buf = pr.buf[:len(s)]

	startRow := int64(s[0].Index)

	// Ensure that each Row.Values slice has enough capacity to store all values.
	for i := range s {
		s[i].Values = slices.Grow(s[i].Values, len(pr.columns))
		s[i].Values = s[i].Values[:len(pr.columns)]
	}

	for n < len(s) {
		var (
			// maxRead tracks the maximum number of read rows across all columns. This
			// is required because columns are not guaranteed to have the same number
			// of rows, and we want to advance startRow by the maximum number of rows
			// read.
			maxRead int

			// atEOF is true if all columns report EOF. We default to true and set it
			// to false if any column returns a non-EOF error.
			atEOF = true
		)

		for _, column := range columns {
			columnIndex, ok := pr.columnLookup[column]
			if !ok {
				return n, fmt.Errorf("column %v is not owned by basicReader", column)
			}

			// We want to allow readers to reuse memory of [Value]s in s while
			// allowing the caller to retain ownership over that memory; to do this
			// safely, we copy memory from s into pr.buf (for the given column index)
			// for our decoders to use.
			//
			// If we didn't do this, then memory backing [Value]s are owned by both
			// basicReader and the caller, which can lead to memory reuse bugs.
			pr.buf = reuseRowsBuffer(pr.buf, s[n:], columnIndex)

			r := pr.readers[columnIndex]
			_, err := r.Seek(startRow, io.SeekStart)
			if err != nil {
				return n, fmt.Errorf("seeking to row %d in column %d: %w", startRow, columnIndex, err)
			}

			cn, err := r.Read(ctx, pr.buf[:len(s)-n])
			if err != nil && !errors.Is(err, io.EOF) {
				// If reading a column fails, we return immediately without advancing
				// our row offset for this batch. This retains the state of the reader
				// and ensures that every call to Read reads every column.
				//
				// However, callers that choose to retry failed reads will suffer
				// performance penalties: all columns up to and including the failing
				// column will seek backwards to startRow, which requires starting
				// over from the top of a page.
				return n, fmt.Errorf("reading column %d: %w", columnIndex, err)
			} else if err == nil {
				atEOF = false
			}
			maxRead = max(maxRead, cn)
			for i := range cn {
				s[n+i].Values[columnIndex] = pr.buf[i]
			}
		}

		// We check for atEOF here instead of maxRead == 0 to preserve the pattern
		// of io.Reader: readers may return 0, nil even when they're not at EOF.
		if maxRead == 0 && atEOF {
			return n, io.EOF
		}

		// Some columns may have read fewer rows than maxRead. These columns need
		// to fill in the remainder of the rows (up to maxRead) with NULL values;
		// otherwise there may be non-NULL values from a previous call to Read that
		// would give corrupted results.
		for _, column := range columns {
			columnIndex := pr.columnLookup[column]
			r := pr.readers[columnIndex]
			columnRow, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				// The seek call above can never fail. However, if it does, it
				// indicates corrupted data if any column read less than maxRead. We
				// can't recover from this state, so we panic.
				panic(fmt.Sprintf("seeking to current row in column %d: %v", pr.columnLookup[column], err))
			}

			columnRead := columnRow - startRow
			for i := columnRead; i < int64(maxRead); i++ {
				s[n+int(i)].Values[columnIndex] = Value{}
			}
		}

		n += maxRead
		startRow += int64(maxRead)
	}

	return n, nil
}

// reuseValuesBuffer prepares dst for reading up to len(src) values. Non-NULL
// values are appended to dst, with the remainder of the slice set to NULL.
//
// The resulting slice is len(src).
func reuseRowsBuffer(dst []Value, src []Row, columnIndex int) []Value {
	dst = slices.Grow(dst, len(src))
	dst = dst[:0]

	for _, row := range src {
		if columnIndex >= len(row.Values) {
			continue
		}

		value := row.Values[columnIndex]
		if value.IsNil() {
			continue
		}
		dst = append(dst, value)
	}

	filledLength := len(dst)

	dst = dst[:len(src)]
	clear(dst[filledLength:])
	return dst
}

// Seek sets the row offset for the next Read call, interpreted according to
// whence:
//
//   - [io.SeekStart] seeks relative to the start of the column set,
//   - [io.SeekCurrent] seeks relative to the current offset, and
//   - [io.SeekEnd] seeks relative to the end (for example, offset = -2
//     specifies the penultimate row of the column set).
//
// Seek returns the new offset relative to the start of the column set or an
// error, if any.
//
// To retrieve the current offset without modification, call Seek with 0 and
// [io.SeekCurrent].
//
// Seeking to an offset before the start of the column set is an error. Seeking
// to beyond the end of the column set will cause the next Read or ReadColumns
// to return io.EOF.
func (pr *basicReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return 0, errors.New("invalid offset")
		}
		pr.nextRow = offset

	case io.SeekCurrent:
		if pr.nextRow+offset < 0 {
			return 0, errors.New("invalid offset")
		}
		pr.nextRow += offset

	case io.SeekEnd:
		lastRow := int64(pr.maxRows())
		if lastRow+offset < 0 {
			return 0, errors.New("invalid offset")
		}
		pr.nextRow = lastRow + offset

	default:
		return 0, fmt.Errorf("invalid whence value %d", whence)
	}

	return pr.nextRow, nil
}

// maxRows returns the total number of rows across the column set, determined
// by the column with the most rows.
func (pr *basicReader) maxRows() int {
	var rows int
	for _, c := range pr.columns {
		rows = max(rows, c.ColumnInfo().RowsCount)
	}
	return rows
}

// Reset resets the basicReader to read from the start of the provided columns.
// This permits reusing a basicReader rather than allocating a new one.
func (pr *basicReader) Reset(columns []Column) {
	if pr.columnLookup == nil {
		pr.columnLookup = make(map[Column]int, len(columns))
	} else {
		clear(pr.columnLookup)
	}

	// Reset existing readers.
	pr.columns = columns
	for i := 0; i < len(pr.readers) && i < len(columns); i++ {
		pr.readers[i].Reset(columns[i])
		pr.columnLookup[columns[i]] = i
	}

	// Create new readers for any additional columns.
	for i := len(pr.readers); i < len(columns); i++ {
		pr.readers = append(pr.readers, newColumnReader(columns[i]))
		pr.columnLookup[columns[i]] = i
	}

	// Clear out remaining readers. This needs to clear beyond the final length
	// of the pr.readers slice (up to its full capacity) so elements beyond the
	// length can be garbage collected.
	pr.readers = pr.readers[:len(columns)]
	clear(pr.readers[len(columns):cap(pr.readers)])
	pr.nextRow = 0
}

// Close closes the basicReader. Closed basicReaders can be reused by calling
// [basicReader.Reset].
func (pr *basicReader) Close() error {
	for _, r := range pr.readers {
		if err := r.Close(); err != nil {
			return err
		}
	}
	return nil
}
