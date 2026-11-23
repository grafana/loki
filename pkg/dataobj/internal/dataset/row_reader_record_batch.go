package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// ReadRecordBatch reads the next rows that pass the predicates of r and returns
// them as a record batch. The record batch has one array for each projected
// column, in the order of the projected columns. It has no schema and does not
// hold row indexes.
//
// ReadRecordBatch returns the same rows as [RowReader.Read], in the same order.
// Calls of Read and ReadRecordBatch can follow each other. Two cases differ from
// Read:
//   - A first predicate that reads no column and keeps every row, such as
//     [TruePredicate], is valid. Read returns an error.
//   - Read ends at the last row that the columns of its first predicate hold.
//     ReadRecordBatch reads the rows past the end of a column as null.
//
// Inside an And or an Or, ReadRecordBatch reads a column only for the rows that
// reach it, so it can count fewer bytes in the read statistics than Read.
//
// With a nil error, the record batch has between one and batchSize rows. A
// record batch with fewer than batchSize rows does not mean that the dataset
// ended.
//
// The arrays of a record batch have these types:
//   - an int64 column: a [columnar.Number] of int64,
//   - a uint64 column: a [columnar.Number] of uint64,
//   - a binary column: a [columnar.UTF8Reader]. It is a [columnar.UTF8], or a
//     [columnar.ChunkedUTF8] that has one chunk for each page that holds a
//     returned row. It is a ChunkedUTF8 only if no predicate read the column,
//     the rows come from more than one page, and no returned value is null.
//
// At the end of the dataset, ReadRecordBatch returns an empty record batch and
// [io.EOF]. Every later call does the same. If ReadRecordBatch fails, it returns
// an empty record batch and the error. The next call reads the same rows again.
// The record batch is never nil, so a caller can read its length before it
// checks the error.
//
// The arrays of the record batch use memory from alloc, so they stay valid until
// the caller reclaims alloc. ReadRecordBatch can also leave temporary arrays in
// alloc, and a failed call can leave memory in alloc. So the caller should
// reclaim alloc after it consumes each record batch.
//
// ReadRecordBatch returns an error if batchSize is not positive, alloc is nil,
// or [RowReader.Open] was not called first. It returns the error of ctx if ctx
// is done.
func (r *RowReader) ReadRecordBatch(ctx context.Context, alloc *memory.Allocator, batchSize int) (*columnar.RecordBatch, error) {
	if batchSize <= 0 {
		return r.emptyRecordBatch(), fmt.Errorf("batch size must be positive, got %d", batchSize)
	}
	if alloc == nil {
		return r.emptyRecordBatch(), errors.New("allocator is nil")
	}
	if !r.ready {
		return r.emptyRecordBatch(), errRowReaderNotOpen
	}

	xcap.RegionFromContext(ctx).Record(dataobj.StatDatasetReadCalls.Observe(1))

	// Skip an input record batch where no row passes, so a nil error always comes
	// with at least one row.
	for {
		if err := ctx.Err(); err != nil {
			return r.emptyRecordBatch(), err
		}

		batch, err := r.readNextRecordBatch(ctx, alloc, batchSize)
		if err != nil {
			return r.emptyRecordBatch(), err
		} else if batch != nil {
			return batch, nil
		}
	}
}

// readNextRecordBatch reads the next input record batch: at most batchSize
// consecutive rows of one valid range. It returns the rows that pass the
// predicates as a record batch, or nil if no row passes. It returns [io.EOF] if
// no input record batch is left.
func (r *RowReader) readNextRecordBatch(ctx context.Context, outAlloc *memory.Allocator, batchSize int) (*columnar.RecordBatch, error) {
	row, err := r.alignRow()
	if err != nil {
		return nil, err
	}
	currentRange := r.ranges.RangeFor(row)
	if !currentRange.IsValid() {
		return nil, fmt.Errorf("failed to find range for row %d", row)
	}

	readSize := int(min(uint64(batchSize), currentRange.End-row))
	if readSize <= 0 {
		return nil, fmt.Errorf("record batch at row %d has no row", row)
	}
	readRange := rangeset.Range{Start: row, End: row + uint64(readSize)}
	r.dl.SetReadRange(readRange)

	r.recordBatchScratch.Reclaim()
	r.currRecordBatch.reset(r, &r.recordBatchScratch, len(r.columns), int64(row), readSize)

	batch, err := r.readFilteredRecordBatch(ctx, outAlloc, readRange)
	if err != nil {
		return nil, err
	}

	// The cursor moves past the record batch only after every read
	// succeeds, so a retry after an error reads the same rows.
	r.row += int64(readSize)
	return batch, nil
}

// readFilteredRecordBatch runs the predicates on the current input record batch,
// and returns the rows that pass as a new record batch. It returns nil if no row
// passes. A reader with no predicate keeps every row, and all its columns are
// primary.
func (r *RowReader) readFilteredRecordBatch(ctx context.Context, outAlloc *memory.Allocator, readRange rangeset.Range) (*columnar.RecordBatch, error) {
	rb := &r.currRecordBatch

	sel, firstPassCount, err := r.evalRecordBatchPredicates(ctx, rb)
	if err != nil {
		return nil, err
	}
	passCount := sel.count()

	var (
		out                = make([]columnar.Array, len(r.projectedColumns))
		primaryOutputBytes int64
		secondaryRead      bool
		secondaryBytes     int64
	)
	if passCount > 0 {
		// Tell the downloader to skip, when it batches pages, a page that holds
		// only rows that sel does not select. Masking helps only when pages
		// download lazily. With PrefetchAllOnOpen every page is in memory already.
		if passCount < rb.n && r.dl.targetCompressedBytes > 0 {
			for start, end := range sel.unselectedRuns() {
				r.dl.Mask(rangeset.Range{Start: readRange.Start + uint64(start), End: readRange.Start + uint64(end)})
			}
		}

		for i := range out {
			var read bool
			out[i], read, err = r.projectRecordBatchColumn(ctx, rb, outAlloc, i, sel, passCount)
			if err != nil {
				return nil, err
			}
			if !read {
				continue
			}

			// No predicate read this column in this record batch. Count its
			// bytes as primary if any predicate reads the column, and as secondary
			// otherwise.
			bytes := arrayBytes(out[i], newSelectionAllRows(passCount))
			if slices.Contains(r.primaryColumnIndexes, i) {
				primaryOutputBytes += bytes
			} else {
				secondaryRead = true
				secondaryBytes += bytes
			}
		}
	}

	// Record the statistics at the end, so a retry after an error does not count
	// the record batch twice. Sum the filled bytes after the projection,
	// because projecting a column can read it again and replace its count.
	region := xcap.RegionFromContext(ctx)
	if r.firstIsStreamID {
		region.Record(dataobj.StatStreamRelevantRows.Observe(int64(firstPassCount)))
	}
	region.Record(dataobj.StatDatasetPrimaryRowsRead.Observe(int64(rb.n)))
	region.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(rb.filledBytes() + primaryOutputBytes))
	if secondaryRead {
		region.Record(dataobj.StatDatasetSecondaryRowsRead.Observe(int64(passCount)))
		region.Record(dataobj.StatDatasetSecondaryRowBytes.Observe(secondaryBytes))
	}

	if passCount == 0 {
		return nil, nil
	}
	return columnar.NewRecordBatch(nil, int64(passCount), out), nil
}

// evalRecordBatchPredicates runs the predicates in order on the record batch,
// each on the rows that the earlier ones kept, and returns the rows that all of
// them keep. It stops at the first predicate that keeps no row. It also returns
// the number of rows that the first predicate kept.
func (r *RowReader) evalRecordBatchPredicates(ctx context.Context, rb *recordBatch) (sel selection, firstPassCount int, err error) {
	sel = newSelectionAllRows(rb.n)
	for i, p := range r.compiledPredicates {
		sel, err = p.evalRecordBatch(ctx, rb, sel)
		if err != nil {
			return selection{}, 0, fmt.Errorf("evaluating predicate %d: %w", i, err)
		}

		if i == 0 {
			firstPassCount = sel.count()
		}
		if sel.empty() {
			break
		}
	}
	return sel, firstPassCount, nil
}

// projectRecordBatchColumn returns the rows of the input record batch that sel
// selects, for the projected column at index idx, in memory from outAlloc.
// passCount is the number of rows that sel selects. read is true if no predicate
// read the column for this record batch, so the projection read it first.
func (r *RowReader) projectRecordBatchColumn(ctx context.Context, rb *recordBatch, outAlloc *memory.Allocator, idx int, sel selection, passCount int) (arr columnar.Array, read bool, err error) {
	read = rb.columns[idx].arr == nil

	switch {
	case read && passCount == rb.n:
		arr, err = r.readColumn(ctx, outAlloc, idx, rb.row, rb.n)
	case read:
		arr, err = r.readCompactedColumnBySelection(ctx, outAlloc, idx, rb.row, rb.n, sel)
	default:
		arr, err = rb.columnArrayOrView(ctx, idx, sel)
		if err == nil {
			arr, err = sel.filterArray(outAlloc, arr)
		}
	}
	if err != nil {
		return nil, read, fmt.Errorf("projecting column %d: %w", idx, err)
	}
	return arr, read, nil
}

// columnSegments calls fn for each part of rows [row, row+n) that one page of the
// column at index idx holds, in order. The rows past the last page form a
// segment of their own. start and end are dataset rows, and end is exclusive.
func (r *RowReader) columnSegments(ctx context.Context, idx int, row int64, n int, fn func(start, end int64) error) error {
	pages, err := r.inner.readers[idx].PageRanges(ctx)
	if err != nil {
		return fmt.Errorf("listing pages of column %d: %w", idx, withoutEOF(err))
	}

	end := row + int64(n)
	for cursor := row; cursor < end; {
		segmentEnd := end
		if page := sort.Search(len(pages), func(i int) bool { return int64(pages[i].End) > cursor }); page < len(pages) {
			segmentEnd = min(end, int64(pages[page].End))
		}
		if err := fn(cursor, segmentEnd); err != nil {
			return err
		}
		cursor = segmentEnd
	}
	return nil
}

// readCompactedColumnBySelection reads the rows of [row, row+n) that sel
// selects, from the column at index idx. It returns them as one array in memory
// from outAlloc. A binary column can come back as a [columnar.ChunkedUTF8].
//
// The array is compacted: it has one element for each selected row, in row
// order, and no element for the other rows. So element i is the i-th selected
// row, not the row at offset i. The array is shorter than n if sel does not
// select every row.
//
// sel is a selection of the n rows. readCompactedColumnBySelection returns an
// error if sel selects no row.
func (r *RowReader) readCompactedColumnBySelection(ctx context.Context, outAlloc *memory.Allocator, idx int, row int64, n int, sel selection) (columnar.Array, error) {
	// A page that holds no selected row is not decoded.
	var pieces []columnar.Array
	err := r.columnSegments(ctx, idx, row, n, func(start, end int64) error {
		segmentSel := sel.slice(int(start-row), int(end-row))
		if segmentSel.empty() {
			return nil
		}

		arr, err := r.readColumn(ctx, &r.recordBatchScratch, idx, start, int(end-start))
		if err != nil {
			return err
		}
		arr, err = segmentSel.filterArray(outAlloc, arr)
		if err != nil {
			return fmt.Errorf("filtering rows [%d, %d) of column %d: %w", start, end, idx, err)
		}
		pieces = append(pieces, arr)
		return nil
	})
	if err != nil {
		return nil, err
	}

	switch len(pieces) {
	case 0:
		return nil, fmt.Errorf("no selected row in the record batch of column %d", idx)
	case 1:
		return pieces[0], nil
	default:
		arr, err := columnar.ConcatView(outAlloc, pieces)
		if err != nil {
			return nil, fmt.Errorf("joining the selected rows of column %d: %w", idx, err)
		}
		return arr, nil
	}
}

// readUncompactedColumnBySelection reads rows [row, row+n) of the column at
// index idx as one array, in memory from dst. When sel selects a row in every
// page, a binary column can come back as a [columnar.ChunkedUTF8].
//
// The array is uncompacted: it has one element for each of the n rows, whether
// sel selects the row or not. So element i is the row at offset i, and the
// caller can index the array by row position.
//
// sel is a selection of the n rows. A page that holds no row that sel selects is
// not decoded, and its elements are null. A null element can be a real null or
// padding, so the caller must use only the elements of the rows that sel selects.
func (r *RowReader) readUncompactedColumnBySelection(ctx context.Context, dst *memory.Allocator, idx int, row int64, n int, sel selection) (columnar.Array, error) {
	if sel.isAllRows() {
		return r.readColumn(ctx, dst, idx, row, n)
	}

	type segment struct {
		start, end int64
		selected   bool
	}

	var (
		segments    []segment
		allSelected = true
	)
	err := r.columnSegments(ctx, idx, row, n, func(start, end int64) error {
		selected := !sel.slice(int(start-row), int(end-row)).empty()
		allSelected = allSelected && selected
		segments = append(segments, segment{start: start, end: end, selected: selected})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if allSelected {
		return r.readColumn(ctx, dst, idx, row, n)
	}

	pieces := make([]columnar.Array, 0, len(segments))
	for _, seg := range segments {
		var (
			arr columnar.Array
			err error
		)
		if seg.selected {
			arr, err = r.readColumn(ctx, dst, idx, seg.start, int(seg.end-seg.start))
		} else {
			arr, err = padWithNulls(dst, r.columns[idx].ColumnDesc().Type.Physical, nil, int(seg.end-seg.start))
		}
		if err != nil {
			return nil, fmt.Errorf("reading rows [%d, %d) of column %d: %w", seg.start, seg.end, idx, err)
		}
		pieces = append(pieces, arr)
	}

	arr, err := columnar.Concat(dst, pieces)
	if err != nil {
		return nil, fmt.Errorf("joining the segments of column %d: %w", idx, err)
	}
	return arr, nil
}

// readColumn reads rows [row, row+n) of the column at index idx as one array of
// n elements, in memory from dst. A binary column can come back as a
// [columnar.ChunkedUTF8]. The rows past the end of the column read as null,
// because a section writer can leave out the trailing null rows of a sparse
// column. It returns an error that wraps [io.ErrUnexpectedEOF] if a row that it
// reads lies past the end of the pages, and the pages end before the last row
// that the column holds.
func (r *RowReader) readColumn(ctx context.Context, dst *memory.Allocator, idx int, row int64, n int) (columnar.Array, error) {
	arr, err := r.readColumnPages(ctx, dst, idx, row, n)
	if err != nil {
		return nil, err
	}
	got := 0
	if arr != nil {
		got = arr.Len()
	}
	if got < n {
		if end, rows := row+int64(got), int64(r.columns[idx].ColumnDesc().RowsCount); end < rows {
			return nil, fmt.Errorf("pages of column %d end at row %d, but the column holds %d rows: %w", idx, end, rows, io.ErrUnexpectedEOF)
		}

		arr, err = padWithNulls(dst, r.columns[idx].ColumnDesc().Type.Physical, arr, n-got)
		if err != nil {
			return nil, fmt.Errorf("padding column %d: %w", idx, err)
		}
	}
	return arr, nil
}

// readColumnPages reads up to n rows from row of the column at index idx as one array,
// in memory from dst. The array is nil or shorter than n if the column ends
// early. A binary column comes back as a [columnar.ChunkedUTF8] with one chunk
// for each page when its rows come from more than one page and none is null.
func (r *RowReader) readColumnPages(ctx context.Context, dst *memory.Allocator, idx int, row int64, n int) (columnar.Array, error) {
	// List the pages first. The column reader lists them on its first use, and its
	// io.EOF would then look like the end of the column.
	if _, err := r.inner.readers[idx].PageRanges(ctx); err != nil {
		return nil, fmt.Errorf("listing pages of column %d: %w", idx, withoutEOF(err))
	}

	reader := r.inner.readers[idx]
	if _, err := reader.Seek(row, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seeking to row %d in column %d: %w", row, idx, err)
	}

	r.recordBatchPageArrays = r.recordBatchPageArrays[:0]
	defer func() {
		// Drop the references, so the scratch slice does not keep arrays alive.
		clear(r.recordBatchPageArrays)
	}()

	err := reader.ReadEach(ctx, dst, n, func(arr columnar.Array) error {
		r.recordBatchPageArrays = append(r.recordBatchPageArrays, arr)
		return nil
	})
	if err != nil && err != io.EOF {
		// Only the io.EOF sentinel ends the column. An error that wraps io.EOF is a
		// failed read.
		return nil, fmt.Errorf("reading column %d: %w", idx, withoutEOF(err))
	}

	switch len(r.recordBatchPageArrays) {
	case 0:
		return nil, nil
	case 1:
		return dropEmptyValidity(r.recordBatchPageArrays[0]), nil
	default:
		arr, err := columnar.ConcatView(dst, r.recordBatchPageArrays)
		if err != nil {
			return nil, fmt.Errorf("joining pages of column %d: %w", idx, err)
		}
		return dropEmptyValidity(arr), nil
	}
}

// emptyRecordBatch returns a record batch of no rows with an empty array for
// each projected column. It reads the options, because it also serves the calls
// before Open.
func (r *RowReader) emptyRecordBatch() *columnar.RecordBatch {
	arrs := make([]columnar.Array, len(r.opts.ProjectedColumns))
	for i, col := range r.opts.ProjectedColumns {
		switch col.ColumnDesc().Type.Physical {
		case datasetmd.PHYSICAL_TYPE_INT64:
			arrs[i] = columnar.NewNumber[int64](nil, memory.Bitmap{})
		case datasetmd.PHYSICAL_TYPE_UINT64:
			arrs[i] = columnar.NewNumber[uint64](nil, memory.Bitmap{})
		case datasetmd.PHYSICAL_TYPE_BINARY:
			arrs[i] = columnar.NewUTF8(nil, []int32{0}, memory.Bitmap{})
		default:
			arrs[i] = columnar.NewNull(memory.Bitmap{})
		}
	}
	return columnar.NewRecordBatch(nil, 0, arrs)
}

// withoutEOF returns err unchanged if err does not match io.EOF. A caller that
// reads until io.EOF treats any error that matches io.EOF as the end of the
// dataset. So if err matches io.EOF, withoutEOF returns an error that keeps the
// text of err and wraps [io.ErrUnexpectedEOF]. It drops the other errors in the
// chain of err.
func withoutEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%v: %w", err, io.ErrUnexpectedEOF)
	}
	return err
}

// padWithNulls returns arr followed by count null values of type typ. arr can be
// nil.
func padWithNulls(alloc *memory.Allocator, typ datasetmd.PhysicalType, arr columnar.Array, count int) (columnar.Array, error) {
	validity := memory.NewBitmap(alloc, count)
	validity.AppendCount(false, count)

	nulls, err := materializeNulls(alloc, typ, validity)
	if err != nil {
		return nil, err
	}
	if arr == nil {
		return nulls, nil
	}
	return columnar.Concat(alloc, []columnar.Array{arr, nulls})
}

// dropEmptyValidity returns arr without its validity bitmap if arr holds no
// null. It returns other array types unchanged.
func dropEmptyValidity(arr columnar.Array) columnar.Array {
	// The page reader always builds a bitmap. Without it, the kernels skip the
	// null handling and the copies of the bitmap.
	if arr == nil || arr.Nulls() > 0 {
		return arr
	}
	if validity := arr.Validity(); validity.Len() == 0 {
		return arr
	}

	switch arr := arr.(type) {
	case *columnar.Number[int64]:
		return columnar.NewNumber(arr.Values(), memory.Bitmap{})
	case *columnar.Number[uint64]:
		return columnar.NewNumber(arr.Values(), memory.Bitmap{})
	case *columnar.UTF8:
		return columnar.NewUTF8(arr.Data(), arr.Offsets(), memory.Bitmap{})
	default:
		return arr
	}
}

// arrayBytes returns the size in bytes of the non-null values in the rows of arr
// that sel selects, as [Row.Size] counts them. sel is a selection of the rows of
// arr.
func arrayBytes(arr columnar.Array, sel selection) int64 {
	if arr == nil {
		return 0
	}

	switch arr := arr.(type) {
	case *columnar.UTF8:
		if sel.isAllRows() {
			return int64(arr.DataLen())
		}
		offsets := arr.Offsets()
		var size int64
		for i := range sel.bits.IterValues(true) {
			size += int64(offsets[i+1] - offsets[i])
		}
		return size

	case *columnar.ChunkedUTF8:
		return int64(sel.chunkBytes(arr))

	case *columnar.Number[int64], *columnar.Number[uint64]:
		if arr.Nulls() == 0 {
			return int64(sel.count()) * 8
		}
		var count int64
		sel.forEach(func(i int) {
			if !arr.IsNull(i) {
				count++
			}
		})
		return count * 8

	case *columnar.Null:
		return 0

	default:
		panic(fmt.Sprintf("dataset.arrayBytes: unsupported array type %T", arr))
	}
}

// A recordBatchColumnReader reads the rows of the columns of a dataset.
type recordBatchColumnReader interface {
	// readUncompactedColumnBySelection reads rows [row, row+n) of the column at
	// index idx as one array of n elements, in memory from dst. sel is a
	// selection of the n rows.
	//
	// The array is uncompacted: element i is the row at offset i, whether sel
	// selects the row or not. The array holds real values for the rows that sel
	// selects. The elements of the other rows can be null. A row past the end of
	// the column reads as null. A binary column can come back as a
	// [columnar.ChunkedUTF8].
	//
	// readUncompactedColumnBySelection returns an error that wraps
	// [io.ErrUnexpectedEOF] if a row that sel selects lies past the end of the
	// pages, and the pages end before the last row that the column holds.
	readUncompactedColumnBySelection(ctx context.Context, dst *memory.Allocator, idx int, row int64, n int, sel selection) (columnar.Array, error)
}

var _ recordBatchColumnReader = (*RowReader)(nil)

// A recordBatch holds the state of one input record batch while a [RowReader]
// reads it. It has at most batchSize consecutive rows of one valid range, and
// the arrays of the columns that predicates read.
//
// It is not the [columnar.RecordBatch] that ReadRecordBatch returns. That one
// holds only the rows that pass the predicates.
type recordBatch struct {
	reader  recordBatchColumnReader
	scratch *memory.Allocator // Memory that lives as long as the record batch.
	row     int64             // First row of the record batch.
	n       int               // Number of rows in the record batch. It is positive.

	// columns is indexed like the columns of the reader: the projected columns
	// first, then the columns that only predicates read.
	columns []recordBatchColumn

	// keepBatchPositions and keepBatchScratch are the scratch slices of the
	// KeepBatch evaluation. They keep their capacity from one record batch to
	// the next.
	keepBatchPositions []int32
	keepBatchScratch   []int32
}

// A recordBatchColumn holds a column that a predicate read for the record batch.
type recordBatchColumn struct {
	// arr holds n elements, or is nil if no predicate has read the column yet.
	arr columnar.Array

	// filled selects the rows for which arr holds the data of the column, which
	// can be null. The read can fill more rows, because it decodes whole pages.
	// The elements of a page that holds none of these rows are null padding.
	filled selection

	// filledBytes is the size in bytes of the values of arr at the rows of
	// filled.
	filledBytes int64
}

// reset starts a record batch of n rows from row. The record batch
// reads columns with reader, and takes its memory from scratch. columns is the
// number of columns of the RowReader.
func (rb *recordBatch) reset(reader recordBatchColumnReader, scratch *memory.Allocator, columns int, row int64, n int) {
	rb.reader = reader
	rb.scratch = scratch
	rb.row = row
	rb.n = n

	if cap(rb.columns) < columns {
		rb.columns = make([]recordBatchColumn, columns)
	} else {
		rb.columns = rb.columns[:columns]
		clear(rb.columns)
	}
}

// filledBytes returns the size in bytes of the values that predicates read.
func (rb *recordBatch) filledBytes() int64 {
	var total int64
	for _, c := range rb.columns {
		total += c.filledBytes
	}
	return total
}

// columnArray is like columnArrayOrView, but it never returns a
// [columnar.ChunkedUTF8]. It joins one into a [columnar.UTF8], and caches the
// joined array for the column.
func (rb *recordBatch) columnArray(ctx context.Context, idx int, sel selection) (columnar.Array, error) {
	arr, err := rb.columnArrayOrView(ctx, idx, sel)
	if err != nil {
		return nil, err
	}
	if _, ok := arr.(*columnar.ChunkedUTF8); !ok {
		return arr, nil
	}

	flat, err := columnar.Flatten(rb.scratch, arr)
	if err != nil {
		return nil, fmt.Errorf("flattening column %d: %w", idx, err)
	}
	rb.columns[idx].arr = flat
	return flat, nil
}

// columnArrayOrView returns the array of the column at index idx for the rows
// of the record batch. It reads the column on the first call, and again if the
// column is not filled for sel. The array holds the data of at least the rows
// that sel selects. A page that holds none of them can read as null. The array
// uses scratch memory, so it stays valid until the next record batch.
//
// A binary column can come back as a [columnar.ChunkedUTF8]. A caller must
// handle it, or call columnArray instead.
func (rb *recordBatch) columnArrayOrView(ctx context.Context, idx int, sel selection) (columnar.Array, error) {
	if arr := rb.columns[idx].arr; arr != nil {
		if rb.isColumnArrayFilled(idx, sel) {
			return arr, nil
		}

		// An earlier caller read the column only for a subset of the rows, as the
		// right side of an And or an Or does. This caller needs other rows too. Read
		// the union of both row sets, not the whole record batch. The
		// statistics then count only rows that reached a predicate.
		sel = rb.columns[idx].filled.or(rb.scratch, sel)
	}

	arr, err := rb.reader.readUncompactedColumnBySelection(ctx, rb.scratch, idx, rb.row, rb.n, sel)
	if err != nil {
		return nil, err
	} else if arr.Len() != rb.n {
		return nil, fmt.Errorf("column %d has %d rows, want %d", idx, arr.Len(), rb.n)
	}

	// The new read replaces the earlier one, so its byte count replaces the
	// earlier count.
	rb.columns[idx] = recordBatchColumn{arr: arr, filled: sel, filledBytes: arrayBytes(arr, sel)}
	return arr, nil
}

// isColumnArrayFilled reports whether the array of the column at index idx
// holds the data of every row that sel selects.
func (rb *recordBatch) isColumnArrayFilled(idx int, sel selection) bool {
	filled := rb.columns[idx].filled
	switch {
	case rb.columns[idx].arr == nil:
		return false
	case filled.isAllRows():
		return true
	case sel.isAllRows():
		return false
	}
	return sel.subtract(rb.scratch, filled).empty()
}
