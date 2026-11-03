package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// BatchReaderOptions configures a [BatchReader]. It takes the same options as
// a [RowReader] and gives the same results.
type BatchReaderOptions = RowReaderOptions

// A BatchReader reads a [Dataset] as batches of arrays, one array per projected
// column, instead of as [Row]s. It returns the same rows as a [RowReader] with
// the same options, in the same order.
//
// A BatchReader is not safe for concurrent use.
type BatchReader struct {
	// setup resolves the columns and prunes the pages. It also owns the
	// downloader and the column readers, which BatchReader reuses as they are.
	setup RowReader
	ready bool

	predicates []vectorPredicate

	// scratch holds everything that lives for one window: the arrays that
	// predicates read and the intermediate selections. Read reclaims it before
	// it reads the next window.
	scratch memory.Allocator
	window  readWindow

	keepScratch []int32 // Scratch for the indexes that KeepBatch returns.
	positions   []int32 // Scratch for the rows of a dense array of selected rows.
	pageArrays  []columnar.Array
}

// NewBatchReader creates a new BatchReader from the provided options.
//
// Call [BatchReader.Open] before calling [BatchReader.Read].
func NewBatchReader(opts BatchReaderOptions) *BatchReader {
	var r BatchReader
	r.Reset(opts)
	return &r
}

// Open initializes BatchReader resources.
//
// Open must be called before [BatchReader.Read]. Open is safe to call
// multiple times.
func (r *BatchReader) Open(ctx context.Context) error {
	if r.ready {
		return nil
	}

	if err := r.setup.Open(ctx); err != nil {
		return err
	}

	r.predicates = r.predicates[:0]
	for _, p := range r.setup.opts.Predicates {
		compiled, err := compileVectorPredicate(p, r.setup.columnsLookup)
		if err != nil {
			_ = r.Close()
			return fmt.Errorf("compiling predicate: %w", err)
		}
		r.predicates = append(r.predicates, compiled)
	}

	r.ready = true
	return nil
}

// Read reads the next rows that pass the predicates of the reader. It returns
// a batch with up to batchSize rows and one array per projected column, in the
// order of the projected columns. A batch that comes with a nil error always
// has at least one row.
//
// At the end of the dataset, Read returns an empty batch with [io.EOF]. If Read
// fails, it returns an empty batch and the error. The batch is never nil, so a
// caller can read its length before it looks at the error.
//
// The arrays of the batch use memory from alloc, so they are valid until the
// caller reclaims alloc.
//
// Read returns an error if [BatchReader.Open] was not called first.
func (r *BatchReader) Read(ctx context.Context, alloc *memory.Allocator, batchSize int) (*columnar.RecordBatch, error) {
	if batchSize <= 0 {
		return r.emptyBatch(), fmt.Errorf("batch size must be positive, got %d", batchSize)
	}
	if !r.ready {
		return r.emptyBatch(), errRowReaderNotOpen
	}

	xcap.RegionFromContext(ctx).Record(dataobj.StatDatasetReadCalls.Observe(1))

	// A window where no row passes returns no rows. Moving on to the next
	// window here saves the caller one call for each such window.
	for {
		if err := ctx.Err(); err != nil {
			return r.emptyBatch(), err
		}

		batch, err := r.readWindow(ctx, alloc, batchSize)
		if err != nil {
			return r.emptyBatch(), err
		} else if batch != nil {
			return batch, nil
		}
	}
}

// readWindow reads the next window of rows, which holds at most batchSize
// rows from one valid range. It returns the rows of the window that pass the
// predicates, or nil if no row passes. It returns [io.EOF] if no window is
// left.
func (r *BatchReader) readWindow(ctx context.Context, outAlloc *memory.Allocator, batchSize int) (*columnar.RecordBatch, error) {
	// A window moves through three phases:
	//
	//  1. Each predicate reads its columns for the whole window and tests them
	//     with a selection vector. A predicate only tests the rows that the
	//     earlier predicates kept, and a window that no row passes stops here.
	//  2. The projected columns that no predicate read are read next. Only the
	//     pages that hold a selected row are decoded.
	//  3. The rows that passed are copied out of the arrays that predicates read.
	setup := &r.setup
	region := xcap.RegionFromContext(ctx)

	row, err := setup.alignRow()
	if err != nil {
		return nil, err
	}
	currentRange := setup.ranges.RangeFor(row)
	if !currentRange.IsValid() {
		return nil, fmt.Errorf("failed to find range for row %d", row)
	}

	n := min(batchSize, int(currentRange.End-row))
	readRange := rangeset.Range{Start: row, End: row + uint64(n)}
	setup.dl.SetReadRange(readRange)

	r.scratch.Reclaim()
	w := &r.window
	w.reset(r, int64(row), n)

	var (
		projected = len(setup.projectedColumns)
		sel       memory.Bitmap // Starts out as every row.
		out       = make([]columnar.Array, projected)
	)

	if len(r.predicates) == 0 {
		// Every column is primary. Read them straight into the memory of the caller.
		var bytes int64
		for i := range projected {
			arr, err := r.readColumn(ctx, outAlloc, i, int64(row), n)
			if err != nil {
				return nil, err
			}
			out[i] = arr
			bytes += arrayBytes(arr, sel, n)
		}

		setup.row += int64(n)
		region.Record(dataobj.StatDatasetPrimaryRowsRead.Observe(int64(n)))
		region.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(bytes))
		return columnar.NewRecordBatch(nil, int64(n), out), nil
	}

	for i, p := range r.predicates {
		sel, err = p.eval(ctx, w, sel)
		if err != nil {
			return nil, err
		}

		passCount := selectionCount(sel, n)

		// Track rows that are relevant to the stream matchers. Only the first
		// predicate can be the stream matcher. The later ones measure how
		// selective the query is.
		if i == 0 && isStreamIDPredicate(setup.opts.Predicates[i]) {
			region.Record(dataobj.StatStreamRelevantRows.Observe(int64(passCount)))
		}
		if passCount == 0 {
			break
		}
	}

	passCount := selectionCount(sel, n)
	region.Record(dataobj.StatDatasetPrimaryRowsRead.Observe(int64(n)))
	region.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(w.primaryBytes))

	if passCount == 0 {
		setup.row += int64(n)
		return nil, nil
	}

	if passCount < n && setup.dl.targetCompressedBytes > 0 {
		// Pages download lazily. Tell the downloader which rows no longer matter,
		// so it skips the pages that only hold those rows.
		r.maskUnselected(readRange, sel)
	}

	var (
		secondaryRead  bool
		secondaryBytes int64
	)
	for i := range projected {
		var (
			arr     = w.arrays[i]
			primary = arr != nil
		)
		switch {
		case !primary && passCount == n:
			arr, err = r.readColumn(ctx, outAlloc, i, int64(row), n)
		case !primary:
			arr, err = r.readSelected(ctx, outAlloc, i, int64(row), n, sel)
		case passCount == n:
			arr, err = w.column(ctx, i, sel)
			if err == nil {
				arr, err = cloneArray(outAlloc, arr)
			}
		default:
			arr, err = w.column(ctx, i, sel)
			if err == nil {
				arr, err = filterArray(outAlloc, arr, sel)
			}
		}
		if err != nil {
			return nil, err
		}

		out[i] = arr
		if !primary {
			secondaryRead = true
			secondaryBytes += arrayBytes(arr, memory.Bitmap{}, passCount)
		}
	}

	if secondaryRead {
		region.Record(dataobj.StatDatasetSecondaryRowsRead.Observe(int64(passCount)))
		region.Record(dataobj.StatDatasetSecondaryRowBytes.Observe(secondaryBytes))
	}

	// The reader only moves past the window once every read succeeded, so the
	// caller can retry after a transient error.
	setup.row += int64(n)
	return columnar.NewRecordBatch(nil, int64(passCount), out), nil
}

// maskUnselected tells the downloader that the rows of readRange which sel does
// not select will not be read.
func (r *BatchReader) maskUnselected(readRange rangeset.Range, sel memory.Bitmap) {
	var (
		start = -1 // Start of the current run of unselected rows, or -1.
		next  int  // Index after the last unselected row of the run.
	)
	flush := func() {
		if start >= 0 {
			r.setup.dl.Mask(rangeset.Range{Start: readRange.Start + uint64(start), End: readRange.Start + uint64(next)})
			start = -1
		}
	}

	for i := range sel.IterValues(false) {
		if start >= 0 && i == next {
			next++
			continue
		}
		flush()
		start, next = i, i+1
	}
	flush()
}

// segments calls fn for each part of the window [row, row+n) that one page of
// the column at index idx holds, in order. The rows past the last page form a
// segment of their own, which reads as null.
func (r *BatchReader) segments(ctx context.Context, idx int, row int64, n int, fn func(start, end int64) error) error {
	pages, err := r.setup.inner.readers[idx].pageRanges(ctx)
	if err != nil {
		return err
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

// readSelected reads the rows of the window [row, row+n) that sel selects, from
// the column at index idx, and returns them as one array. It decodes a page
// of the column only if the page holds a selected row of the window.
func (r *BatchReader) readSelected(ctx context.Context, outAlloc *memory.Allocator, idx int, row int64, n int, sel memory.Bitmap) (columnar.Array, error) {
	var pieces []columnar.Array
	err := r.segments(ctx, idx, row, n, func(start, end int64) error {
		segmentSel := sel.Slice(int(start-row), int(end-row))
		selected := segmentSel.SetCount()
		if selected == 0 {
			return nil
		}

		arr, err := r.readColumn(ctx, &r.scratch, idx, start, int(end-start))
		if err != nil {
			return err
		}
		if selected == arr.Len() {
			arr, err = cloneArray(outAlloc, arr)
		} else {
			arr, err = filterArray(outAlloc, arr, *segmentSel)
		}
		if err != nil {
			return err
		}
		pieces = append(pieces, arr)
		return nil
	})
	if err != nil {
		return nil, err
	}

	switch len(pieces) {
	case 0:
		return nil, fmt.Errorf("column %d: no selected row in the window", idx)
	case 1:
		return pieces[0], nil
	default:
		return columnar.Concat(outAlloc, pieces)
	}
}

// readAligned reads the column at index idx for the window [row, row+n) as one
// array of n elements, like [BatchReader.readColumn]. It decodes a page of the
// column only if the page holds a row that sel selects. The elements of the
// other pages are null, so only the selected rows hold meaningful values.
func (r *BatchReader) readAligned(ctx context.Context, alloc *memory.Allocator, idx int, row int64, n int, sel memory.Bitmap) (columnar.Array, error) {
	type segment struct {
		start, end int64
		selected   bool
	}

	var (
		segments    []segment
		allSelected = true
	)
	err := r.segments(ctx, idx, row, n, func(start, end int64) error {
		segmentSel := sel.Slice(int(start-row), int(end-row))
		selected := segmentSel.SetCount() > 0
		allSelected = allSelected && selected
		segments = append(segments, segment{start: start, end: end, selected: selected})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if allSelected {
		return r.readColumn(ctx, alloc, idx, row, n)
	}

	pieces := make([]columnar.Array, 0, len(segments))
	for _, seg := range segments {
		var (
			arr columnar.Array
			err error
		)
		if seg.selected {
			arr, err = r.readColumn(ctx, alloc, idx, seg.start, int(seg.end-seg.start))
		} else {
			arr, err = padWithNulls(alloc, r.setup.columns[idx].ColumnDesc().Type.Physical, nil, int(seg.end-seg.start))
		}
		if err != nil {
			return nil, err
		}
		pieces = append(pieces, arr)
	}
	return columnar.Concat(alloc, pieces)
}

// readColumn reads rows [row, row+n) of the column at index idx of the setup
// as one array of n elements. A column that ends early reads as null for the
// remaining rows.
func (r *BatchReader) readColumn(ctx context.Context, alloc *memory.Allocator, idx int, row int64, n int) (columnar.Array, error) {
	reader := r.setup.inner.readers[idx]
	if _, err := reader.Seek(row, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seeking to row %d in column %d: %w", row, idx, err)
	}

	r.pageArrays = r.pageArrays[:0]
	defer func() { clear(r.pageArrays) }()

	err := reader.ReadEach(ctx, alloc, n, func(arr columnar.Array) error {
		r.pageArrays = append(r.pageArrays, arr)
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading column %d: %w", idx, err)
	}

	var arr columnar.Array
	switch len(r.pageArrays) {
	case 0:
	case 1:
		arr = r.pageArrays[0]
	default:
		arr, err = columnar.Concat(alloc, r.pageArrays)
		if err != nil {
			return nil, fmt.Errorf("joining pages of column %d: %w", idx, err)
		}
	}

	if got := lenOf(arr); got < n {
		arr, err = padWithNulls(alloc, r.setup.columns[idx].ColumnDesc().Type.Physical, arr, n-got)
		if err != nil {
			return nil, fmt.Errorf("padding column %d: %w", idx, err)
		}
	}
	return dropEmptyValidity(arr), nil
}

func lenOf(arr columnar.Array) int {
	if arr == nil {
		return 0
	}
	return arr.Len()
}

// padWithNulls appends count null values to arr, which can be nil.
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

// dropEmptyValidity returns arr without its validity bitmap if it holds no
// null. The decoders always build a bitmap. Without it, the kernels skip the
// null handling and the copies of the bitmap.
func dropEmptyValidity(arr columnar.Array) columnar.Array {
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

// filterArray returns the elements of arr that sel selects, in memory from alloc.
func filterArray(alloc *memory.Allocator, arr columnar.Array, sel memory.Bitmap) (columnar.Array, error) {
	filtered, err := compute.Filter(alloc, arr, sel)
	if err != nil {
		return nil, err
	}
	return filtered.(columnar.Array), nil
}

// cloneArray returns a copy of arr in memory from alloc.
func cloneArray(alloc *memory.Allocator, arr columnar.Array) (columnar.Array, error) {
	cloned, err := columnar.Clone(alloc, arr)
	if err != nil {
		return nil, err
	}
	return cloned.(columnar.Array), nil
}

// arrayBytes returns the size in bytes of the non-null values in the rows of arr that
// sel selects, as [Row.Size] counts them.
func arrayBytes(arr columnar.Array, sel memory.Bitmap, n int) int64 {
	if arr == nil {
		return 0
	}

	switch arr := arr.(type) {
	case *columnar.UTF8:
		if sel.Len() == 0 {
			return int64(arr.DataLen())
		}
		offsets := arr.Offsets()
		var size int64
		for i := range sel.IterValues(true) {
			size += int64(offsets[i+1] - offsets[i])
		}
		return size

	case *columnar.Number[int64], *columnar.Number[uint64]:
		if arr.Nulls() == 0 {
			return int64(selectionCount(sel, n)) * 8
		}
		var count int64
		forEachSelected(sel, n, func(i int) {
			if !arr.IsNull(i) {
				count++
			}
		})
		return count * 8

	default:
		return 0
	}
}

// emptyBatch returns a batch of no rows with an empty array for each projected
// column.
func (r *BatchReader) emptyBatch() *columnar.RecordBatch {
	arrs := make([]columnar.Array, len(r.setup.projectedColumns))
	for i, col := range r.setup.projectedColumns {
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

// Close closes the BatchReader. Closed BatchReaders can be reused by calling
// [BatchReader.Reset].
func (r *BatchReader) Close() error {
	r.ready = false
	r.scratch.Reset()
	return r.setup.Close()
}

// Reset discards any state and resets the BatchReader with a new set of
// options. This permits reusing a BatchReader rather than allocating a new
// one.
func (r *BatchReader) Reset(opts BatchReaderOptions) {
	r.setup.Reset(opts)
	r.predicates = r.predicates[:0]
	r.scratch.Reclaim()
	r.ready = false
}

// A readWindow holds the state of the rows that one pass of a [BatchReader]
// reads.
type readWindow struct {
	r   *BatchReader
	row int64 // First row of the window.
	n   int   // Number of rows in the window.

	// arrays holds, for each column of the reader, its array of n elements, or
	// nil if no predicate has read the column yet.
	arrays []columnar.Array

	// covered holds, for each column, the rows for which arrays holds meaningful
	// values. A column that a predicate read for a few rows has null elements
	// for the pages that hold none of those rows. A selection of length zero
	// means that the array holds every row.
	covered []memory.Bitmap

	// primaryBytes is the size in bytes of the values that predicates read,
	// counted over the rows that earlier predicates selected.
	primaryBytes int64
}

func (w *readWindow) reset(r *BatchReader, row int64, n int) {
	w.r = r
	w.row = row
	w.n = n
	w.primaryBytes = 0

	columns := len(r.setup.columns)
	if cap(w.arrays) < columns {
		w.arrays = make([]columnar.Array, columns)
	}
	w.arrays = w.arrays[:columns]
	clear(w.arrays)

	if cap(w.covered) < columns {
		w.covered = make([]memory.Bitmap, columns)
	}
	w.covered = w.covered[:columns]
	clear(w.covered)
}

// alloc returns the allocator for memory that lives as long as the window.
func (w *readWindow) alloc() *memory.Allocator { return &w.r.scratch }

// column returns the array of the column at index idx for the rows of the
// window. It reads the column the first time. sel selects the rows that the
// caller tests, and the array holds meaningful values for at least those rows.
// A page that holds none of them can read as null.
func (w *readWindow) column(ctx context.Context, idx int, sel memory.Bitmap) (columnar.Array, error) {
	if arr := w.arrays[idx]; arr != nil {
		if w.covers(idx, sel) {
			return arr, nil
		}

		// An earlier caller read the column for fewer rows than this caller needs,
		// as the right side of an Or does. Read the whole column again.
		sel = memory.Bitmap{}
	}

	var (
		arr columnar.Array
		err error
	)
	if sel.Len() > 0 {
		arr, err = w.r.readAligned(ctx, w.alloc(), idx, w.row, w.n, sel)
	} else {
		arr, err = w.r.readColumn(ctx, w.alloc(), idx, w.row, w.n)
	}
	if err != nil {
		return nil, err
	}
	w.arrays[idx] = arr
	w.covered[idx] = sel
	w.primaryBytes += arrayBytes(arr, sel, w.n)
	return arr, nil
}

// covers reports whether the array of the column at index idx holds
// meaningful values for every row that sel selects.
func (w *readWindow) covers(idx int, sel memory.Bitmap) bool {
	covered := w.covered[idx]
	switch {
	case covered.Len() == 0:
		return true
	case sel.Len() == 0:
		return false
	}
	rest := andNotSelection(w.alloc(), w.n, sel, covered)
	return rest.SetCount() == 0
}
