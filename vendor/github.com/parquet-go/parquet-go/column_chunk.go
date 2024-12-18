package parquet

import (
	"errors"
	"io"
)

var (
	ErrMissingColumnIndex = errors.New("missing column index")
	ErrMissingOffsetIndex = errors.New("missing offset index")
)

// The ColumnChunk interface represents individual columns of a row group.
type ColumnChunk interface {
	// Returns the column type.
	Type() Type

	// Returns the index of this column in its parent row group.
	Column() int

	// Returns a reader exposing the pages of the column.
	Pages() Pages

	// Returns the components of the page index for this column chunk,
	// containing details about the content and location of pages within the
	// chunk.
	//
	// Note that the returned value may be the same across calls to these
	// methods, programs must treat those as read-only.
	//
	// If the column chunk does not have a column or offset index, the methods return
	// ErrMissingColumnIndex or ErrMissingOffsetIndex respectively.
	//
	// Prior to v0.20, these methods did not return an error because the page index
	// for a file was either fully read when the file was opened, or skipped
	// completely using the parquet.SkipPageIndex option. Version v0.20 introduced a
	// change that the page index can be read on-demand at any time, even if a file
	// was opened with the parquet.SkipPageIndex option. Since reading the page index
	// can fail, these methods now return an error.
	ColumnIndex() (ColumnIndex, error)
	OffsetIndex() (OffsetIndex, error)
	BloomFilter() BloomFilter

	// Returns the number of values in the column chunk.
	//
	// This quantity may differ from the number of rows in the parent row group
	// because repeated columns may hold zero or more values per row.
	NumValues() int64
}

type pageAndValueWriter interface {
	PageWriter
	ValueWriter
}

type readRowsFunc func(*rowGroupRows, []Row, byte) (int, error)

func readRowsFuncOf(node Node, columnIndex int, repetitionDepth byte) (int, readRowsFunc) {
	var read readRowsFunc

	if node.Repeated() {
		repetitionDepth++
	}

	if node.Leaf() {
		columnIndex, read = readRowsFuncOfLeaf(columnIndex, repetitionDepth)
	} else {
		columnIndex, read = readRowsFuncOfGroup(node, columnIndex, repetitionDepth)
	}

	if node.Repeated() {
		read = readRowsFuncOfRepeated(read, repetitionDepth)
	}

	return columnIndex, read
}

//go:noinline
func readRowsFuncOfRepeated(read readRowsFunc, repetitionDepth byte) readRowsFunc {
	return func(r *rowGroupRows, rows []Row, repetitionLevel byte) (int, error) {
		for i := range rows {
			// Repeated columns have variable number of values, we must process
			// them one row at a time because we cannot predict how many values
			// need to be consumed in each iteration.
			row := rows[i : i+1]

			// The first pass looks for values marking the beginning of a row by
			// having a repetition level equal to the current level.
			n, err := read(r, row, repetitionLevel)
			if err != nil {
				// The error here may likely be io.EOF, the read function may
				// also have successfully read a row, which is indicated by a
				// non-zero count. In this case, we increment the index to
				// indicate to the caller than rows up to i+1 have been read.
				if n > 0 {
					i++
				}
				return i, err
			}

			// The read function may return no errors and also read no rows in
			// case where it had more values to read but none corresponded to
			// the current repetition level. This is an indication that we will
			// not be able to read more rows at this stage, we must return to
			// the caller to let it set the repetition level to its current
			// depth, which may allow us to read more values when called again.
			if n == 0 {
				return i, nil
			}

			// When we reach this stage, we have successfully read the first
			// values of a row of repeated columns. We continue consuming more
			// repeated values until we get the indication that we consumed
			// them all (the read function returns zero and no errors).
			for {
				n, err := read(r, row, repetitionDepth)
				if err != nil {
					return i + 1, err
				}
				if n == 0 {
					break
				}
			}
		}
		return len(rows), nil
	}
}

//go:noinline
func readRowsFuncOfGroup(node Node, columnIndex int, repetitionDepth byte) (int, readRowsFunc) {
	fields := node.Fields()

	if len(fields) == 0 {
		return columnIndex, func(*rowGroupRows, []Row, byte) (int, error) {
			return 0, io.EOF
		}
	}

	if len(fields) == 1 {
		// Small optimization for a somewhat common case of groups with a single
		// column (like nested list elements for example); there is no need to
		// loop over the group of a single element, we can simply skip to calling
		// the inner read function.
		return readRowsFuncOf(fields[0], columnIndex, repetitionDepth)
	}

	group := make([]readRowsFunc, len(fields))
	for i := range group {
		columnIndex, group[i] = readRowsFuncOf(fields[i], columnIndex, repetitionDepth)
	}

	return columnIndex, func(r *rowGroupRows, rows []Row, repetitionLevel byte) (int, error) {
		// When reading a group, we use the first column as an indicator of how
		// may rows can be read during this call.
		n, err := group[0](r, rows, repetitionLevel)

		if n > 0 {
			// Read values for all rows that the group is able to consume.
			// Getting io.EOF from calling the read functions indicate that
			// we consumed all values of that particular column, but there may
			// be more to read in other columns, therefore we must always read
			// all columns and cannot stop on the first error.
			for _, read := range group[1:] {
				_, err2 := read(r, rows[:n], repetitionLevel)
				if err2 != nil && err2 != io.EOF {
					return 0, err2
				}
			}
		}

		return n, err
	}
}

//go:noinline
func readRowsFuncOfLeaf(columnIndex int, repetitionDepth byte) (int, readRowsFunc) {
	var read readRowsFunc

	if repetitionDepth == 0 {
		read = func(r *rowGroupRows, rows []Row, _ byte) (int, error) {
			// When the repetition depth is zero, we know that there is exactly
			// one value per row for this column, and therefore we can consume
			// as many values as there are rows to fill.
			col := &r.columns[columnIndex]
			buf := r.buffer(columnIndex)

			for i := range rows {
				if col.offset == col.length {
					n, err := col.values.ReadValues(buf)
					col.offset = 0
					col.length = int32(n)
					if n == 0 && err != nil {
						return 0, err
					}
				}

				rows[i] = append(rows[i], buf[col.offset])
				col.offset++
			}

			return len(rows), nil
		}
	} else {
		read = func(r *rowGroupRows, rows []Row, repetitionLevel byte) (int, error) {
			// When the repetition depth is not zero, we know that we will be
			// called with a single row as input. We attempt to read at most one
			// value of a single row and return to the caller.
			col := &r.columns[columnIndex]
			buf := r.buffer(columnIndex)

			if col.offset == col.length {
				n, err := col.values.ReadValues(buf)
				col.offset = 0
				col.length = int32(n)
				if n == 0 && err != nil {
					return 0, err
				}
			}

			if buf[col.offset].repetitionLevel != repetitionLevel {
				return 0, nil
			}

			rows[0] = append(rows[0], buf[col.offset])
			col.offset++
			return 1, nil
		}
	}

	return columnIndex + 1, read
}
