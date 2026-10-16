package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bitmask"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// RowReaderOptions configures how a [RowReader] will read [Row]s.
type RowReaderOptions struct {
	Dataset Dataset // Dataset to read from.

	// Columns to read from the Dataset. It is invalid to provide a Column that
	// is not in Dataset.
	//
	// The set of Columns can include columns not used in Predicate; such columns
	// are considered non-predicate columns.
	Columns []Column

	// Predicates filter the data returned by a RowReader. Predicates are
	// optional; if nil, all rows from Columns are returned.
	//
	// Expressions in Predicate may only reference columns in Columns.
	// Holds a list of predicates that can be sequentially applied to the dataset.
	Predicates []Predicate

	// PredicateOnlyColumns are columns that the reader reads to run predicates but does not
	// return: the Value of such a column in every returned Row is nil. The reader can then skip
	// the copy of the values that pass a [FuncPredicate.KeepBatch] function.
	//
	// Each column must be in Columns, and a predicate must read it. Open returns an error
	// otherwise.
	PredicateOnlyColumns []Column

	// PrefetchAllOnOpen controls when pages are downloaded.
	//
	// If true, all pages are downloaded when the reader is opened.
	// If false, pages are downloaded lazily as they are read, targeting
	// [defaultTargetDownloadedBytes] bytes of cached pages at a time. Pages
	// required for the current read range are always downloaded even if they
	// exceed the target.
	PrefetchAllOnOpen bool

	// StatsTracker keeps track of the various reader internal stats.
	StatsTracker RowReaderStatsTracker
}

type RowReaderStatsTracker interface {
	OnColumnPredicateBuilt(column Column, stats ColumnReadPageStats)
}

type ColumnReadPageStats struct {
	Total    uint64
	Relevant uint64
}

// A RowReader reads [Row]s from a [Dataset].
type RowReader struct {
	opts  RowReaderOptions
	ready bool // ready is true if the RowReader has been initialized.

	origColumnLookup     map[Column]int      // Find the index of a column in opts.Columns.
	primaryColumnIndexes []int               // Indexes of primary columns in opts.Columns.
	compiledPredicates   []compiledPredicate // opts.Predicates with column indexes resolved, one per predicate.
	batchPredicates      []*batchPredicate   // One per predicate, nil for a row by row predicate. Empty when none runs on arrays.
	predicateOnlyIdxs    []int               // Indexes in opts.Columns of opts.PredicateOnlyColumns.
	keepScratch          []int32             // Indexes that a batch predicate keeps, reused across reads.

	dl     *rowReaderDownloader // Bulk page download manager.
	row    int64                // The current row being read.
	inner  *basicRowReader      // Underlying reader that reads from columns.
	ranges rangeset.Set         // Valid ranges to read across the entire dataset.
}

var errRowReaderNotOpen = errors.New("row reader not opened")

// NewRowReader creates a new RowReader from the provided options.
//
// Call [RowReader.Open] before calling [RowReader.Read].
func NewRowReader(opts RowReaderOptions) *RowReader {
	var r RowReader
	r.Reset(opts)
	return &r
}

// Open initializes RowReader resources.
//
// Open must be called before [RowReader.Read]. Open is safe to call multiple
// times.
func (r *RowReader) Open(ctx context.Context) error {
	if r.ready {
		return nil
	}

	if err := r.init(ctx); err != nil {
		_ = r.Close()
		return fmt.Errorf("initializing reader: %w", err)
	}
	return nil
}

// Read reads up to the next len(s) rows from r and stores them into s. It
// returns the number of rows read and any error encountered. At the end of the
// Dataset, Read returns 0, [io.EOF].
//
// Read returns an error if [RowReader.Open] was not called first.
func (r *RowReader) Read(ctx context.Context, s []Row) (int, error) {
	if len(s) == 0 {
		return 0, nil
	}

	if !r.ready {
		return 0, errRowReaderNotOpen
	}

	region := xcap.RegionFromContext(ctx)
	region.Record(dataobj.StatDatasetReadCalls.Observe(1))

	// Our Read implementation works by:
	//
	// 1. Determining the next row to read (aligned to a valid range),
	// 2. reading rows from primary columns up to len(s) or to the end of the
	//    range (whichever is lower),
	// 3. filtering those read rows based on the predicate, and finally
	// 4. filling the remaining secondary columns in the rows that pass the
	//    predicate.
	//
	// If a predicate is defined, primary columns are those used in the
	// predicate, and secondary columns are those not used in the predicate. If
	// there isn't a predicate, all columns are primary and no columns are
	// secondary.
	//
	// This approach means that one call to Read may return 0, nil if the
	// predicate filters out all rows, even if there are more rows in the dataset
	// that may pass the predicate.
	//
	// This *could* be improved by running the above steps in a loop until len(s)
	// rows have been read or we hit EOF. However, this would cause the remainder
	// of s to shrink each loop iteration, leading to more inner Reads (as the
	// buffer gets smaller and smaller).
	//
	// Improvements on this process must consider the trade-offs between less
	// calls to [RowReader.Read] while permitting the buffer to be as big as
	// possible on each call.

	row, err := r.alignRow()
	if err != nil {
		return 0, err
	} else if _, err := r.inner.Seek(int64(row), io.SeekStart); err != nil {
		return 0, fmt.Errorf("failed to seek to row %d: %w", row, err)
	}

	currentRange := r.ranges.RangeFor(row)
	if !currentRange.IsValid() {
		// This should be unreachable; alignToRange already ensures that we're in a
		// range, or it returns io.EOF.
		return 0, fmt.Errorf("failed to find range for row %d", row)
	}

	readSize := min(len(s), int(currentRange.End-row))

	readRange := rangeset.Range{
		Start: row,
		End:   row + uint64(readSize),
	}
	r.dl.SetReadRange(readRange)

	var (
		rowsRead  int // tracks max rows accessed to move the [r.row] cursor
		passCount int // tracks how many rows passed the predicate
	)

	// If there are no predicates, read all columns in the dataset
	if len(r.opts.Predicates) == 0 {
		count, err := r.inner.ReadColumns(ctx, r.primaryColumns(), s[:readSize])
		if err != nil && !errors.Is(err, io.EOF) {
			return count, err
		} else if count == 0 && errors.Is(err, io.EOF) {
			return 0, io.EOF
		}

		rowsRead = count
		passCount = count

		var primaryColumnBytes int64
		for i := range count {
			primaryColumnBytes += s[i].Size()
		}

		region.Record(dataobj.StatDatasetPrimaryRowsRead.Observe(int64(rowsRead)))
		region.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(primaryColumnBytes))
	} else {
		rowsRead, passCount, err = r.readAndFilterPrimaryColumns(ctx, readSize, s[:readSize])
		if err != nil {
			return passCount, err
		}
	}

	if secondary := r.secondaryColumns(); len(secondary) > 0 && passCount > 0 {
		// Mask out any ranges that aren't in s[:passCount], so that filling in
		// secondary columns doesn't consider downloading pages not used for the
		// Fill.
		//
		// This mask is only needed for secondary filling in our read range, as the
		// next call to [RowReader.Read] will move the read range forward and these
		// rows will never be considered.
		for maskedRange := range buildMask(readRange, s[:passCount]) {
			r.dl.Mask(maskedRange)
		}

		count, err := r.inner.Fill(ctx, secondary, s[:passCount])
		if err != nil && !errors.Is(err, io.EOF) {
			return count, err
		} else if count != passCount {
			return count, fmt.Errorf("failed to fill rows: expected %d, got %d", passCount, count)
		}

		var totalBytesFilled int64
		for i := range count {
			totalBytesFilled += s[i].Size() - s[i].SizeOfColumns(r.primaryColumnIndexes)
		}

		region.Record(dataobj.StatDatasetSecondaryRowsRead.Observe(int64(count)))
		region.Record(dataobj.StatDatasetSecondaryRowBytes.Observe(totalBytesFilled))
	}

	// We only advance r.row after we successfully read and filled rows. This
	// allows the caller to retry reading rows if a sporadic error occurs.
	r.row += int64(rowsRead)
	return passCount, nil
}

// readAndFilterPrimaryColumns reads the primary columns from the dataset
// and filters the rows by sequentially applying the predicates.
//
// For each predicate evaluation, only the required columns are loaded.
// Rows are filtered at each step with subsequent predicates only having to fill
// the columns on the reduced row range.
//
// It returns the max rows read, rows that passed all the predicates, and any error
func (r *RowReader) readAndFilterPrimaryColumns(ctx context.Context, readSize int, s []Row) (int, int, error) {
	var (
		rowsRead           int // tracks max rows accessed to move the [r.row] cursor
		passCount          int // number of rows that passed the predicate
		primaryColumnBytes int64
		filledColumns      = make(map[Column]struct{}, len(r.primaryColumnIndexes))
		region             = xcap.RegionFromContext(ctx)
	)

	// sequentially apply the predicates.
	for i, p := range r.opts.Predicates {
		if i < len(r.batchPredicates) && r.batchPredicates[i] != nil {
			bp := r.batchPredicates[i]
			var err error
			passCount, err = r.evalBatchPredicate(ctx, bp, s[:readSize], &primaryColumnBytes)
			if err != nil {
				return rowsRead, 0, err
			}
			if passCount == 0 {
				// No rows passed the predicate, so we can stop early.
				break
			}
			readSize = passCount
			continue
		}

		columns, idxs, err := r.predicateColumns(p, func(c Column) bool {
			// keep only columns that haven't been filled yet.
			_, ok := filledColumns[c]
			return !ok
		})
		if err != nil {
			return rowsRead, 0, err
		}

		var count int
		// read the requested number of rows for the first predicate.
		if i == 0 {
			count, err = r.inner.ReadColumns(ctx, columns, s[:readSize])
			if err != nil && !errors.Is(err, io.EOF) {
				return 0, 0, err
			} else if count == 0 && errors.Is(err, io.EOF) {
				return 0, 0, io.EOF
			}

			rowsRead = count
		} else if len(columns) > 0 {
			count, err = r.inner.Fill(ctx, columns, s[:readSize])
			if err != nil && !errors.Is(err, io.EOF) {
				return rowsRead, 0, err
			} else if count != readSize {
				return rowsRead, 0, fmt.Errorf("failed to fill rows: expected %d, got %d", readSize, count)
			}
		} else {
			count = readSize // required columns are already filled
		}

		compiled := r.compiledPredicates[i]

		passCount = 0
		for j := range count {
			size := s[j].SizeOfColumns(idxs)
			primaryColumnBytes += size

			if !compiled.eval(s[j]) {
				continue
			}
			// We move s[j] to s[passCount] by *swapping* the rows. Copying would
			// result in the Row.Values slice existing in two places in the buffer,
			// which causes memory corruption when filling in rows.
			s[passCount], s[j] = s[j], s[passCount]
			passCount++
		}

		// Track rows that are relevant to the stream matchers.
		// This is updated only if the first predicate is streamID
		// any other applied predicates measure query selectivity.
		if i == 0 && isStreamIDPredicate(p) {
			region.Record(dataobj.StatStreamRelevantRows.Observe(int64(passCount)))
		}

		if passCount == 0 {
			// No rows passed the predicate, so we can stop early.
			break
		}

		for _, c := range columns {
			filledColumns[c] = struct{}{}
		}

		readSize = passCount
	}

	region.Record(dataobj.StatDatasetPrimaryRowsRead.Observe(int64(rowsRead)))
	region.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(primaryColumnBytes))

	// A predicate-only column can still hold a value in the rows. The row path reads it for the
	// predicate, and the batch path leaves the value of an earlier read in a reused row. Zero it
	// so the caller sees none.
	for _, idx := range r.predicateOnlyIdxs {
		for j := range passCount {
			s[j].Values[idx].Zero()
		}
	}

	return rowsRead, passCount, nil
}

// batchPredicate is a predicate that the reader tests on a run of values at once.
type batchPredicate struct {
	predicate FuncPredicate
	index     int  // Index of the column of predicate in the values of a Row.
	retain    bool // Whether the value of the column goes into the rows that pass.
}

// evalBatchPredicate reads the column of bp for rows and keeps the rows that pass. It moves the
// passing rows to the front of rows and returns how many there are. It copies the value of a
// passing row into the row only when the column is returned.
func (r *RowReader) evalBatchPredicate(ctx context.Context, bp *batchPredicate, rows []Row, primaryColumnBytes *int64) (int, error) {
	var (
		pass = 0
		keep = r.keepScratch
	)
	_, err := r.inner.readRuns(ctx, r.allColumns()[bp.index], rows, func(offset int, values *columnar.UTF8) error {
		*primaryColumnBytes += int64(values.DataLen())

		keep = bp.predicate.KeepBatch(bp.predicate.Column, values, keep[:0])
		prev := int32(-1)
		for _, k := range keep {
			if k <= prev || int(k) >= values.Len() {
				return fmt.Errorf("batch predicate kept index %d of %d values after index %d, want ascending indexes in range", k, values.Len(), prev)
			}
			prev = k

			// Ascending indexes keep pass <= j. We move rows[j] to rows[pass] by *swapping* the
			// rows. Copying would result in the Row.Values slice existing in two places in the
			// buffer, which causes memory corruption when filling in rows.
			j := offset + int(k)
			rows[pass], rows[j] = rows[j], rows[pass]
			if bp.retain {
				copyUTF8Value(&rows[pass].Values[bp.index], values, int(k))
			}
			pass++
		}
		return nil
	})
	r.keepScratch = keep[:0]
	if err != nil {
		return 0, err
	}
	return pass, nil
}

// alignRow returns r.row if it is a valid row in ranges, or adjusts r.row to
// the next valid row in ranges.
func (r *RowReader) alignRow() (uint64, error) {
	if r.ranges.IncludesValue(uint64(r.row)) {
		return uint64(r.row), nil
	}

	nextRow, ok := r.ranges.Next(uint64(r.row))
	if !ok {
		return 0, io.EOF
	}
	r.row = int64(nextRow)
	return nextRow, nil
}

// compilePredicates compiles each of r.opts.Predicates into a [compiledPredicate] with column
// references resolved to Row value indexes. It returns an error if a predicate references a
// column absent from [RowReaderOptions]. The caller must clear r.compiledPredicates first;
// Reset does this.
func (r *RowReader) compilePredicates() error {
	for _, p := range r.opts.Predicates {
		cp, err := compilePredicate(p, r.origColumnLookup)
		if err != nil {
			return err
		}
		r.compiledPredicates = append(r.compiledPredicates, cp)
	}
	return nil
}

// initBatchPredicates decides which predicates run on an array of values at once, and resolves
// opts.PredicateOnlyColumns. It returns an error when a predicate-only column is not in the
// columns, or when no predicate reads it. The caller must clear r.batchPredicates and
// r.predicateOnlyIdxs first; Reset does this.
//
// A predicate runs on an array of values when all of these hold:
//
//   - It is a [FuncPredicate] with a KeepBatch function, and it is not inside an And, Or or Not
//     predicate.
//   - It is not the first predicate. The first predicate sets the row indexes.
//   - Its column holds binary values.
//   - No other predicate reads its column. The batch path fills the column only in the rows
//     that pass, and not at all for a predicate-only column.
func (r *RowReader) initBatchPredicates() error {
	// Allocate nothing when no predicate needs this.
	if len(r.opts.PredicateOnlyColumns) == 0 && !slices.ContainsFunc(r.opts.Predicates, hasKeepBatch) {
		return nil
	}

	r.batchPredicates = slices.Grow(r.batchPredicates[:0], len(r.opts.Predicates))[:len(r.opts.Predicates)]

	uses := make(map[Column]int)
	for _, p := range r.opts.Predicates {
		WalkPredicate(p, func(p Predicate) bool {
			if c, ok := predicateColumn(p); ok {
				uses[c]++
			}
			return true
		})
	}

	predicateOnly := make(map[Column]struct{}, len(r.opts.PredicateOnlyColumns))
	for _, c := range r.opts.PredicateOnlyColumns {
		idx, ok := r.origColumnLookup[c]
		if !ok {
			return fmt.Errorf("predicate-only column %v not found in RowReader columns", c)
		}
		if uses[c] == 0 {
			return fmt.Errorf("predicate-only column %v is not read by a predicate", c)
		}
		predicateOnly[c] = struct{}{}
		r.predicateOnlyIdxs = append(r.predicateOnlyIdxs, idx)
	}

	for i, p := range r.opts.Predicates {
		fp, ok := p.(FuncPredicate)
		if i == 0 || !ok || fp.KeepBatch == nil || uses[fp.Column] != 1 {
			continue
		}
		if fp.Column.ColumnDesc().Type.Physical != datasetmd.PHYSICAL_TYPE_BINARY {
			continue
		}

		_, predOnly := predicateOnly[fp.Column]
		r.batchPredicates[i] = &batchPredicate{
			predicate: fp,
			index:     r.origColumnLookup[fp.Column],
			retain:    !predOnly,
		}
	}
	return nil
}

func hasKeepBatch(p Predicate) bool {
	fp, ok := p.(FuncPredicate)
	return ok && fp.KeepBatch != nil
}

// buildMask returns an iterator that yields row ranges from full that are not
// present in s.
//
// buildMask will panic if any index in s is outside the range of full.
func buildMask(full rangeset.Range, s []Row) iter.Seq[rangeset.Range] {
	return func(yield func(rangeset.Range) bool) {
		// Rows in s are in ascending order, but there may be gaps between rows. We
		// need to return ranges of rows in full that are not in s.
		if len(s) == 0 {
			yield(full)
			return
		}

		start := full.Start

		for _, row := range s {
			if !full.Contains(uint64(row.Index)) {
				panic("buildMask: row index out of range")
			}

			if uint64(row.Index) != start {
				if !yield(rangeset.Range{Start: start, End: uint64(row.Index)}) {
					return
				}
			}

			start = uint64(row.Index) + 1
		}

		if start < full.End {
			if !yield(rangeset.Range{Start: start, End: full.End}) {
				return
			}
		}
	}
}

// Close closes the RowReader. Closed RowReaders can be reused by calling
// [RowReader.Reset].
func (r *RowReader) Close() error {
	if r.inner != nil {
		return r.inner.Close()
	}

	return nil
}

// Reset discards any state and resets the RowReader with a new set of options.
// This permits reusing a RowReader rather than allocating a new one.
func (r *RowReader) Reset(opts RowReaderOptions) {
	r.opts = opts

	// There's not much work Reset can do without a context, since it needs to
	// retrieve page info. We'll defer this work to an init function. This also
	// unfortunately means that we might not reset page readers until the first
	// call to Open.
	if r.origColumnLookup == nil {
		r.origColumnLookup = make(map[Column]int, len(opts.Columns))
	}
	clear(r.origColumnLookup)
	for i, c := range opts.Columns {
		r.origColumnLookup[c] = i
	}

	r.row = 0
	r.ranges.Reset()
	r.primaryColumnIndexes = sliceclear.Clear(r.primaryColumnIndexes)
	r.compiledPredicates = sliceclear.Clear(r.compiledPredicates)
	r.batchPredicates = sliceclear.Clear(r.batchPredicates)
	r.predicateOnlyIdxs = r.predicateOnlyIdxs[:0]
	r.ready = false
}

func (r *RowReader) init(ctx context.Context) error {
	// RowReader.init is kept close to the defition of RowReader.Reset to make it
	// easier to follow the correctness of resetting + initializing.

	// r.compilePredicates must be called before initializing anything else; for
	// simplicity, other functions assume that the predicates are valid and can
	// panic if they aren't.
	if err := r.compilePredicates(); err != nil {
		return err
	}
	if err := r.initBatchPredicates(); err != nil {
		return err
	}

	if err := r.initDownloader(ctx); err != nil {
		return err
	}
	if err := r.prefetchPages(ctx); err != nil {
		return fmt.Errorf("prefetching pages: %w", err)
	}

	if r.inner == nil {
		r.inner = newBasicRowReader(r.allColumns())
	} else {
		r.inner.Reset(r.allColumns())
	}

	r.ready = true
	return nil
}

func (r *RowReader) prefetchPages(ctx context.Context) error {
	if !r.opts.PrefetchAllOnOpen {
		return nil
	}
	return r.dl.Prefetch(ctx)
}

// allColumns returns the full set of columns to read.
func (r *RowReader) allColumns() []Column {
	return r.dl.AllColumns()
}

// primaryColumns returns the primary columns to read.
func (r *RowReader) primaryColumns() []Column {
	return r.dl.PrimaryColumns()
}

// secondaryColumns returns the secondary columns to read.
func (r *RowReader) secondaryColumns() []Column {
	return r.dl.SecondaryColumns()
}

// initDownloader initializes the reader's [rowReaderDownloader]. initDownloader is
// always used to reduce the number of conditions.
func (r *RowReader) initDownloader(ctx context.Context) error {
	// The downloader is initialized in three steps:
	//
	//   1. Give it the inner dataset.
	//   2. Add columns with a flag of whether a column is primary or secondary.
	//   3. Provide the overall dataset row ranges that will be valid to read.

	region := xcap.RegionFromContext(ctx)

	if r.dl == nil {
		r.dl = newRowReaderDownloader(r.opts.Dataset)
	} else {
		r.dl.Reset(r.opts.Dataset)
	}

	r.dl.targetCompressedBytes = defaultTargetDownloadedBytes
	if r.opts.PrefetchAllOnOpen {
		r.dl.targetCompressedBytes = 0
	}

	mask := bitmask.New(len(r.opts.Columns))
	r.fillPrimaryMask(mask)

	for i, column := range r.opts.Columns {
		primary := mask.Test(i)
		r.dl.AddColumn(column, primary)

		pageCount := int64(column.ColumnDesc().PagesCount)
		region.Record(dataobj.StatDatasetPagesTotal.Observe(pageCount))

		if primary {
			r.primaryColumnIndexes = append(r.primaryColumnIndexes, i)
			region.Record(dataobj.StatDatasetPrimaryColumns.Observe(1))
			region.Record(dataobj.StatDatasetPrimaryColumnPages.Observe(pageCount))
		} else {
			region.Record(dataobj.StatDatasetSecondaryColumns.Observe(1))
			region.Record(dataobj.StatDatasetSecondaryColumnPages.Observe(pageCount))
		}
	}

	var ranges rangeset.Set
	var err error
	if len(r.opts.Predicates) == 0 { // no predicates, build full range
		ranges, err = r.buildPredicateRanges(ctx, nil)
		if err != nil {
			return err
		}
	} else {
		for i, p := range r.opts.Predicates {
			rr, err := r.buildPredicateRanges(ctx, p)
			if err != nil {
				return err
			}

			if i == 0 {
				ranges = rr
			} else {
				ranges = rangeset.Intersect(ranges, rr)
			}
		}
	}

	r.dl.SetDatasetRanges(ranges)
	r.ranges = ranges

	var rowsCount uint64
	for _, column := range r.allColumns() {
		rowsCount = max(rowsCount, uint64(column.ColumnDesc().RowsCount))
	}

	region.Record(dataobj.StatDatasetMaxRows.Observe(int64(rowsCount)))
	region.Record(dataobj.StatDatasetRowsAfterPruning.Observe(int64(ranges.Len())))

	return nil
}

func (r *RowReader) fillPrimaryMask(mask *bitmask.Mask) {
	process := func(c Column) {
		idx, ok := r.origColumnLookup[c]
		if !ok {
			// This shouldn't be reachable: before we initialize anything we ensure
			// that all columns in the predicate are available in r.opts.Columns.
			panic("fillPrimaryMask: column not found")
		}
		mask.Set(idx)
	}

	// If there's no predicate, all columns are primary.
	if len(r.opts.Predicates) == 0 {
		for _, c := range r.opts.Columns {
			process(c)
		}
		return
	}

	for _, pp := range r.opts.Predicates {
		// If there is a predicate, primary columns are those used in the predicate.
		WalkPredicate(pp, func(p Predicate) bool {
			switch p := p.(type) {
			case EqualPredicate:
				process(p.Column)
			case InPredicate:
				process(p.Column)
			case GreaterThanPredicate:
				process(p.Column)
			case LessThanPredicate:
				process(p.Column)
			case FuncPredicate:
				process(p.Column)
			case AndPredicate, OrPredicate, NotPredicate, TruePredicate, FalsePredicate, nil:
				// No columns to process.
			default:
				panic(fmt.Sprintf("dataset.RowReader.fillPrimaryMask: unsupported predicate type %T", p))
			}

			return true // Continue walking the Predicate.
		})
	}
}

// buildPredicateRanges returns a set of rowRanges that are valid to read based
// on the provided predicate. If p is nil or the predicate is unsupported, the
// entire dataset range is valid.
//
// r.dl must be initialized before calling buildPredicateRanges.
func (r *RowReader) buildPredicateRanges(ctx context.Context, p Predicate) (rangeset.Set, error) {
	switch p := p.(type) {
	case AndPredicate:
		left, err := r.buildPredicateRanges(ctx, p.Left)
		if err != nil {
			return rangeset.Set{}, err
		}
		right, err := r.buildPredicateRanges(ctx, p.Right)
		if err != nil {
			return rangeset.Set{}, err
		}
		return rangeset.Intersect(left, right), nil

	case OrPredicate:
		left, err := r.buildPredicateRanges(ctx, p.Left)
		if err != nil {
			return rangeset.Set{}, err
		}
		right, err := r.buildPredicateRanges(ctx, p.Right)
		if err != nil {
			return rangeset.Set{}, err
		}
		return rangeset.Union(left, right), nil

	case NotPredicate:
		// De Morgan's laws must be applied to reduce the NotPredicate to a set of
		// predicates that can be applied to pages.
		//
		// See comment on [simplifyNotPredicate] for more information.
		simplified, err := simplifyNotPredicate(p)
		if err != nil {
			// Predicate can't be simplfied, so we permit the full range.
			var rowsCount uint64
			for _, column := range r.allColumns() {
				rowsCount = max(rowsCount, uint64(column.ColumnDesc().RowsCount))
			}
			return rangeset.From(rangeset.Range{Start: 0, End: rowsCount}), nil
		}
		return r.buildPredicateRanges(ctx, simplified)

	case FalsePredicate:
		return rangeset.Set{}, nil // No valid ranges.

	case EqualPredicate:
		return r.buildColumnPredicateRanges(ctx, p.Column, p)

	case InPredicate:
		return r.buildColumnPredicateRanges(ctx, p.Column, p)

	case GreaterThanPredicate:
		return r.buildColumnPredicateRanges(ctx, p.Column, p)

	case LessThanPredicate:
		return r.buildColumnPredicateRanges(ctx, p.Column, p)

	case TruePredicate, FuncPredicate, nil:
		// These predicates (and nil) don't support any filtering, so it maps to
		// the full range being valid.
		//
		// We use r.dl.AllColumns instead of r.opts.Columns because the downloader
		// will cache metadata.
		var rowsCount uint64
		for _, column := range r.allColumns() {
			rowsCount = max(rowsCount, uint64(column.ColumnDesc().RowsCount))
		}
		return rangeset.From(rangeset.Range{Start: 0, End: rowsCount}), nil

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

// simplifyNotPredicate applies De Morgan's laws to a NotPredicate to permit
// page filtering.
//
// While during evaluation, a NotPredicate inverts the result of the inner
// predicate, the same can't be done for page filtering. For example, imagine
// that a page is included from a rule "a > 10." If we inverted that inclusion,
// we may be incorrectly filtering out that page, as that page may also have
// values less than 10.
//
// To correctly apply page filtering to a NotPredicate, we reduce the
// NotPredicate to a set of predicates that can be applied to pages. This may
// result in other NotPredicates that also need to be simplified.
//
// If the NotPredicate can't be simplified, simplifyNotPredicate returns an
// error.
func simplifyNotPredicate(p NotPredicate) (Predicate, error) {
	switch inner := p.Inner.(type) {
	case AndPredicate: // De Morgan's law: !(A && B) == !A || !B
		return OrPredicate{
			Left:  NotPredicate{Inner: inner.Left},
			Right: NotPredicate{Inner: inner.Right},
		}, nil

	case OrPredicate: // De Morgan's law: !(A || B) == !A && !B
		return AndPredicate{
			Left:  NotPredicate{Inner: inner.Left},
			Right: NotPredicate{Inner: inner.Right},
		}, nil

	case NotPredicate: // De Morgan's law: !!A == A
		return inner.Inner, nil

	case FalsePredicate:
		return TruePredicate{}, nil

	case TruePredicate, nil:
		// A nil predicate keeps every row, the same as a TruePredicate, so negating either
		// keeps none.
		return FalsePredicate{}, nil

	case EqualPredicate: // De Morgan's law: !(A == B) == A != B == A < B || A > B
		return OrPredicate{
			Left:  LessThanPredicate(inner),
			Right: GreaterThanPredicate(inner),
		}, nil

	case GreaterThanPredicate: // De Morgan's law: !(A > B) == A <= B
		return OrPredicate{
			Left:  EqualPredicate(inner),
			Right: LessThanPredicate(inner),
		}, nil

	case LessThanPredicate: // De Morgan's law: !(A < B) == A >= B
		return OrPredicate{
			Left:  EqualPredicate(inner),
			Right: GreaterThanPredicate(inner),
		}, nil

	case InPredicate:
		// TODO: can be supported when we introduce NotInPredicate.
		return nil, fmt.Errorf("can't simplify InPredicate")

	case FuncPredicate:
		return nil, fmt.Errorf("can't simplify FuncPredicate")

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", inner))
	}
}

// buildColumnPredicateRanges returns a set of rowRanges that are valid based
// on whether EqualPredicate, InPredicate, GreaterThanPredicate, or LessThanPredicate may be
// true for each page in a column.
func (r *RowReader) buildColumnPredicateRanges(ctx context.Context, c Column, p Predicate) (rangeset.Set, error) {
	// Get the wrapped column so that the result of c.ListPages can be cached.
	if idx, ok := r.origColumnLookup[c]; ok {
		c = r.allColumns()[idx]
	} else {
		return rangeset.Set{}, fmt.Errorf("column %v not found in RowReader columns", c)
	}

	region := xcap.RegionFromContext(ctx)

	var (
		ranges       rangeset.Set
		pageStart    int
		lastPageSize int

		isStreamCol      = isStreamIDColumn(c)
		prevPageIncluded bool // used for tracking avg run length

		totalPages    = uint64(c.ColumnDesc().PagesCount)
		relevantPages = uint64(0)
	)

	for result := range c.ListPages(ctx) {
		pageStart += lastPageSize

		page, err := result.Value()
		if err != nil {
			return rangeset.Set{}, err
		}
		pageInfo := page.PageDesc()
		lastPageSize = pageInfo.RowCount

		pageRange := rangeset.Range{
			Start: uint64(pageStart),
			End:   uint64(pageStart + pageInfo.RowCount),
		}

		if isStreamCol {
			region.Record(dataobj.StatStreamPagesTotal.Observe(1))
		}

		minValue, maxValue, err := readMinMax(pageInfo.Stats)
		if err != nil {
			return rangeset.Set{}, fmt.Errorf("failed to read page stats: %w", err)
		} else if minValue.IsNil() || maxValue.IsNil() {
			// No stats, so we add the whole range.
			ranges.Add(pageRange)
			relevantPages++

			if isStreamCol {
				region.Record(dataobj.StatStreamRelevantPages.Observe(1))

				// record start of a new run
				if !prevPageIncluded {
					region.Record(dataobj.StatStreamPageRuns.Observe(1))
				}

			}

			prevPageIncluded = true
			continue
		}

		var include bool

		switch p := p.(type) {
		case EqualPredicate: // EqualPredicate may be true if p.Value is inside the range of the page.
			isEmpty := p.Value.Type() == datasetmd.PHYSICAL_TYPE_BINARY && p.Value.IsZero()
			include = isEmpty || (CompareValues(&p.Value, &minValue) >= 0 && CompareValues(&p.Value, &maxValue) <= 0)
		case GreaterThanPredicate: // GreaterThanPredicate may be true if maxValue of a page is greater than p.Value
			include = CompareValues(&maxValue, &p.Value) > 0
		case LessThanPredicate: // LessThanPredicate may be true if minValue of a page is less than p.Value
			include = CompareValues(&minValue, &p.Value) < 0
		case InPredicate:
			// Check if any value falls within the page's range
			for v := range p.Values.Iter() {
				if CompareValues(&v, &minValue) >= 0 && CompareValues(&v, &maxValue) <= 0 {
					include = true
					break
				}
			}
		default:
			panic(fmt.Sprintf("unsupported predicate type %T", p))
		}

		if include {
			ranges.Add(pageRange)
			relevantPages++

			if isStreamCol {
				region.Record(dataobj.StatStreamRelevantPages.Observe(1))

				// track start of a new run
				if !prevPageIncluded {
					region.Record(dataobj.StatStreamPageRuns.Observe(1))
				}
			}
		} else {
			// Page-level stats were present and the predicate ruled out the page,
			// so the page will not be downloaded for this predicate. Pages without
			// stats short-circuit above (they are always included) and are not
			// counted here.
			region.Record(dataobj.StatDatasetPagesPruned.Observe(1))
		}

		prevPageIncluded = include
	}

	if r.opts.StatsTracker != nil {
		r.opts.StatsTracker.OnColumnPredicateBuilt(c, ColumnReadPageStats{totalPages, relevantPages})
	}

	return ranges, nil
}

// readMinMax reads the minimum and maximum values from the provided
// statistics. If either minValue or maxValue is NULL, the value is not present
// in the statistics.
func readMinMax(stats *datasetmd.Statistics) (minValue Value, maxValue Value, err error) {
	// TODO(rfratto): We should have a dataset-specific Statistics type that
	// already has the min and max value decoded, and make it the decoders just
	// to read these.

	if stats == nil {
		return
	}

	if err := minValue.UnmarshalBinary(stats.MinValue); err != nil {
		return Value{}, Value{}, fmt.Errorf("failed to unmarshal min value: %w", err)
	} else if err := maxValue.UnmarshalBinary(stats.MaxValue); err != nil {
		return Value{}, Value{}, fmt.Errorf("failed to unmarshal max value: %w", err)
	}
	return
}

func (r *RowReader) predicateColumns(p Predicate, keep func(c Column) bool) ([]Column, []int, error) {
	columns := make(map[Column]struct{})

	WalkPredicate(p, func(p Predicate) bool {
		switch p := p.(type) {
		case EqualPredicate:
			columns[p.Column] = struct{}{}
		case InPredicate:
			columns[p.Column] = struct{}{}
		case GreaterThanPredicate:
			columns[p.Column] = struct{}{}
		case LessThanPredicate:
			columns[p.Column] = struct{}{}
		case FuncPredicate:
			columns[p.Column] = struct{}{}
		case AndPredicate, OrPredicate, NotPredicate, TruePredicate, FalsePredicate, nil:
			// No columns to process.
		default:
			panic(fmt.Sprintf("predicateColumns: unsupported predicate type %T", p))
		}
		return true
	})

	ret := make([]Column, 0, len(columns))
	idxs := make([]int, 0, len(columns))
	for c := range columns {
		idx, ok := r.origColumnLookup[c]
		if !ok {
			panic(fmt.Errorf("predicateColumns: column %v not found in RowReader columns", c))
		}

		c := r.allColumns()[idx]
		if !keep(c) {
			continue
		}

		idxs = append(idxs, idx)
		ret = append(ret, c)
	}

	return ret, idxs, nil
}

func isStreamIDPredicate(p Predicate) bool {
	in, ok := p.(InPredicate)
	if !ok {
		return false
	}

	return isStreamIDColumn(in.Column)
}

func isStreamIDColumn(col Column) bool {
	return col.ColumnDesc().Type.Logical == "stream_id"
}
