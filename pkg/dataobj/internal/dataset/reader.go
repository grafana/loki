package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bitmask"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
)

// ReaderOptions configures how a [Reader] will read [Row]s.
type ReaderOptions struct {
	Dataset Dataset // Dataset to read from.

	// Columns to read from the Dataset. It is invalid to provide a Column that
	// is not in Dataset.
	//
	// The set of Columns can include columns not used in Predicate; such columns
	// are considered non-predicate columns.
	Columns []Column

	// Predicate filters the data returned by a Reader. Predicate is optional; if
	// nil, all rows from Columns are returned.
	//
	// Expressions in Predicate may only reference columns in Columns.
	Predicate Predicate

	// TargetCacheSize configures the amount of memory to target for caching
	// pages in memory. The cache may exceed this size if the combined size of
	// all pages required for a single call to [Reader.Reead] exceeds this value.
	//
	// TargetCacheSize is used to download and cache additional pages in advance
	// of when they're needed. If TargetCacheSize is 0, only immediately required
	// pages are cached.
	TargetCacheSize int
}

// A Reader reads [Row]s from a [Dataset].
type Reader struct {
	opts  ReaderOptions
	ready bool // ready is true if the Reader has been initialized.

	origColumnLookup map[Column]int // Find the index of a column in opts.Columns.

	dl     *readerDownloader // Bulk page download manager.
	row    int64             // The current row being read.
	inner  *basicReader      // Underlying reader that reads from columns.
	ranges rowRanges         // Valid ranges to read across the entire dataset.
}

// NewReader creates a new Reader from the provided options.
func NewReader(opts ReaderOptions) *Reader {
	var r Reader
	r.Reset(opts)
	return &r
}

// Read reads up to the next len(s) rows from r and stores them into s. It
// returns the number of rows read and any error encountered. At the end of the
// Dataset, Read returns 0, [io.EOF].
func (r *Reader) Read(ctx context.Context, s []Row) (n int, err error) {
	if len(s) == 0 {
		return 0, nil
	}

	if !r.ready {
		err := r.init(ctx)
		if err != nil {
			return 0, fmt.Errorf("initializing reader: %w", err)
		}
	}

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
	// calls to [Reader.Read] while permitting the buffer to be as big as
	// possible on each call.

	row, err := r.alignRow()
	if err != nil {
		return n, err
	} else if _, err := r.inner.Seek(int64(row), io.SeekStart); err != nil {
		return n, fmt.Errorf("failed to seek to row %d: %w", row, err)
	}

	currentRange, ok := r.ranges.Range(row)
	if !ok {
		// This should be unreachable; alignToRange already ensures that we're in a
		// range, or it returns io.EOF.
		return n, fmt.Errorf("failed to find range for row %d", row)
	}

	readSize := min(len(s), int(currentRange.End-row+1))

	readRange := rowRange{
		Start: row,
		End:   row + uint64(readSize) - 1,
	}
	r.dl.SetReadRange(readRange)

	count, err := r.inner.ReadColumns(ctx, r.dl.PrimaryColumns(), s[:readSize])
	if err != nil && !errors.Is(err, io.EOF) {
		return n, err
	} else if count == 0 && errors.Is(err, io.EOF) {
		return 0, io.EOF
	}

	var passCount int // passCount tracks how many rows pass the predicate.
	for i := range count {
		if !checkPredicate(r.opts.Predicate, r.origColumnLookup, s[i]) {
			continue
		}

		// We move s[i] to s[passCount] by *swapping* the rows. Copying would
		// result in the Row.Values slice existing in two places in the buffer,
		// which causes memory corruption when filling in rows.
		s[passCount], s[i] = s[i], s[passCount]
		passCount++
	}

	if secondary := r.dl.SecondaryColumns(); len(secondary) > 0 && passCount > 0 {
		// Mask out any ranges that aren't in s[:passCount], so that filling in
		// secondary columns doesn't consider downloading pages not used for the
		// Fill.
		//
		// This mask is only needed for secondary filling in our read range, as the
		// next call to [Reader.Read] will move the read range forward and these
		// rows will never be considered.
		for maskedRange := range buildMask(readRange, s[:passCount]) {
			r.dl.Mask(maskedRange)
		}

		count, err := r.inner.Fill(ctx, secondary, s[:passCount])
		if err != nil && !errors.Is(err, io.EOF) {
			return n, err
		} else if count != passCount {
			return n, fmt.Errorf("failed to fill rows: expected %d, got %d", n, count)
		}
	}

	n += passCount

	// We only advance r.row after we successfully read and filled rows. This
	// allows the caller to retry reading rows if a sporadic error occurs.
	r.row += int64(count)
	return n, nil
}

// alignRow returns r.row if it is a valid row in ranges, or adjusts r.row to
// the next valid row in ranges.
func (r *Reader) alignRow() (uint64, error) {
	if r.ranges.Includes(uint64(r.row)) {
		return uint64(r.row), nil
	}

	nextRow, ok := r.ranges.Next(uint64(r.row))
	if !ok {
		return 0, io.EOF
	}
	r.row = int64(nextRow)
	return nextRow, nil
}

func checkPredicate(p Predicate, lookup map[Column]int, row Row) bool {
	if p == nil {
		return true
	}

	switch p := p.(type) {
	case AndPredicate:
		return checkPredicate(p.Left, lookup, row) && checkPredicate(p.Right, lookup, row)

	case OrPredicate:
		return checkPredicate(p.Left, lookup, row) || checkPredicate(p.Right, lookup, row)

	case NotPredicate:
		return !checkPredicate(p.Inner, lookup, row)

	case FalsePredicate:
		return false

	case EqualPredicate:
		columnIndex, ok := lookup[p.Column]
		if !ok {
			panic("checkPredicate: column not found")
		}
		return CompareValues(row.Values[columnIndex], p.Value) == 0

	case GreaterThanPredicate:
		columnIndex, ok := lookup[p.Column]
		if !ok {
			panic("checkPredicate: column not found")
		}
		return CompareValues(row.Values[columnIndex], p.Value) > 0

	case LessThanPredicate:
		columnIndex, ok := lookup[p.Column]
		if !ok {
			panic("checkPredicate: column not found")
		}
		return CompareValues(row.Values[columnIndex], p.Value) < 0

	case FuncPredicate:
		columnIndex, ok := lookup[p.Column]
		if !ok {
			panic("checkPredicate: column not found")
		}
		return p.Keep(p.Column, row.Values[columnIndex])

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

// buildMask returns an iterator that yields row ranges from full that are not
// present in s.
//
// buildMask will panic if any index in s is outside the range of full.
func buildMask(full rowRange, s []Row) iter.Seq[rowRange] {
	return func(yield func(rowRange) bool) {
		// Rows in s are in ascending order, but there may be gaps between rows. We
		// need to return ranges of rows in full that are not in s.
		if len(s) == 0 {
			yield(full)
			return
		}

		var start = full.Start

		for _, row := range s {
			if !full.Contains(uint64(row.Index)) {
				panic("buildMask: row index out of range")
			}

			if uint64(row.Index) != start {
				// If start is 1 and row.Index is 5, then the excluded range is (1, 4):
				//
				// (start, row.Index - 1).
				if !yield(rowRange{Start: start, End: uint64(row.Index) - 1}) {
					return
				}
			}

			start = uint64(row.Index) + 1
		}

		if start <= full.End {
			if !yield(rowRange{Start: start, End: full.End}) {
				return
			}
		}
	}
}

// Close closes the Reader. Closed Readers can be reused by calling
// [Reader.Reset].
func (r *Reader) Close() error {
	if r.inner != nil {
		return r.inner.Close()
	}
	return nil
}

// Reset discards any state and resets the Reader with a new set of options.
// This permits reusing a Reader rather than allocating a new one.
func (r *Reader) Reset(opts ReaderOptions) {
	r.opts = opts

	// There's not much work Reset can do without a context, since it needs to
	// retrieve page info. We'll defer this work to an init function.
	if r.origColumnLookup == nil {
		r.origColumnLookup = make(map[Column]int, len(opts.Columns))
	}
	clear(r.origColumnLookup)
	for i, c := range opts.Columns {
		r.origColumnLookup[c] = i
	}

	r.row = 0
	r.ranges = sliceclear.Clear(r.ranges)
	r.ready = false
}

func (r *Reader) init(ctx context.Context) error {
	// Reader.init is kept close to the defition of Reader.Reset to make it
	// easier to follow the correctness of resetting + initializing.

	// r.validatePredicate must be called before initializing anything else; for
	// simplicity, other functions assume that the predicate is valid and can
	// panic if it isn't.
	if err := r.validatePredicate(); err != nil {
		return err
	}

	if err := r.initDownloader(ctx); err != nil {
		return err
	}

	if r.inner == nil {
		r.inner = newBasicReader(r.dl.AllColumns())
	} else {
		r.inner.Reset(r.dl.AllColumns())
	}

	r.ready = true
	return nil
}

// validatePredicate ensures that all columns used in a predicate have been
// provided in [ReaderOptions].
func (r *Reader) validatePredicate() error {
	process := func(c Column) error {
		_, ok := r.origColumnLookup[c]
		if !ok {
			return fmt.Errorf("predicate column %v not found in Reader columns", c)
		}
		return nil
	}

	var err error

	WalkPredicate(r.opts.Predicate, func(p Predicate) bool {
		if err != nil {
			return false
		}

		switch p := p.(type) {
		case EqualPredicate:
			err = process(p.Column)
		case GreaterThanPredicate:
			err = process(p.Column)
		case LessThanPredicate:
			err = process(p.Column)
		case FuncPredicate:
			err = process(p.Column)
		case AndPredicate, OrPredicate, NotPredicate, FalsePredicate, nil:
			// No columns to process.
		default:
			panic(fmt.Sprintf("dataset.Reader.validatePredicate: unsupported predicate type %T", p))
		}

		return true // Continue walking the Predicate.
	})

	return err
}

func (r *Reader) initDownloader(ctx context.Context) error {
	// The downloader is initialized in three steps:
	//
	//   1. Give it the inner dataset.
	//   2. Add columns with a flag of whether a column is primary or secondary.
	//   3. Provide the overall dataset row ranges that will be valid to read.

	if r.dl == nil {
		r.dl = newReaderDownloader(r.opts.Dataset, r.opts.TargetCacheSize)
	} else {
		r.dl.Reset(r.opts.Dataset, r.opts.TargetCacheSize)
	}

	mask := bitmask.New(len(r.opts.Columns))
	r.fillPrimaryMask(mask)

	for i, column := range r.opts.Columns {
		r.dl.AddColumn(column, mask.Test(i))
	}

	ranges, err := r.buildPredicateRanges(ctx, r.opts.Predicate)
	if err != nil {
		return err
	}
	r.dl.SetDatasetRanges(ranges)
	r.ranges = ranges

	return nil
}

func (r *Reader) fillPrimaryMask(mask *bitmask.Mask) {
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
	if r.opts.Predicate == nil {
		for _, c := range r.opts.Columns {
			process(c)
		}
		return
	}

	// If there is a predicate, primary columns are those used in the predicate.
	WalkPredicate(r.opts.Predicate, func(p Predicate) bool {
		switch p := p.(type) {
		case EqualPredicate:
			process(p.Column)
		case GreaterThanPredicate:
			process(p.Column)
		case LessThanPredicate:
			process(p.Column)
		case FuncPredicate:
			process(p.Column)
		case AndPredicate, OrPredicate, NotPredicate, FalsePredicate, nil:
			// No columns to process.
		default:
			panic(fmt.Sprintf("dataset.Reader.fillPrimaryMask: unsupported predicate type %T", p))
		}

		return true // Continue walking the Predicate.
	})
}

// buildPredicateRanges returns a set of rowRanges that are valid to read based
// on the provided predicate. If p is nil or the predicate is unsupported, the
// entire dataset range is valid.
//
// r.dl must be initialized before calling buildPredicateRanges.
func (r *Reader) buildPredicateRanges(ctx context.Context, p Predicate) (rowRanges, error) {
	// TODO(rfratto): We could be reusing memory for building ranges here.

	switch p := p.(type) {
	case AndPredicate:
		left, err := r.buildPredicateRanges(ctx, p.Left)
		if err != nil {
			return nil, err
		}
		right, err := r.buildPredicateRanges(ctx, p.Right)
		if err != nil {
			return nil, err
		}
		return intersectRanges(nil, left, right), nil

	case OrPredicate:
		left, err := r.buildPredicateRanges(ctx, p.Left)
		if err != nil {
			return nil, err
		}
		right, err := r.buildPredicateRanges(ctx, p.Right)
		if err != nil {
			return nil, err
		}
		return unionRanges(nil, left, right), nil

	case NotPredicate:
		// De Morgan's laws must be applied to reduce the NotPredicate to a set of
		// predicates that can be applied to pages.
		//
		// See comment on [simplifyNotPredicate] for more information.
		simplified, err := simplifyNotPredicate(p)
		if err != nil {
			// Predicate can't be simplfied, so we permit the full range.
			var rowsCount uint64
			for _, column := range r.dl.AllColumns() {
				rowsCount = max(rowsCount, uint64(column.ColumnInfo().RowsCount))
			}
			return rowRanges{{Start: 0, End: rowsCount - 1}}, nil
		}
		return r.buildPredicateRanges(ctx, simplified)

	case FalsePredicate:
		return nil, nil // No valid ranges.

	case EqualPredicate:
		return r.buildColumnPredicateRanges(ctx, p.Column, p)

	case GreaterThanPredicate:
		return r.buildColumnPredicateRanges(ctx, p.Column, p)

	case LessThanPredicate:
		return r.buildColumnPredicateRanges(ctx, p.Column, p)

	case FuncPredicate, nil:
		// These predicates (and nil) don't support any filtering, so it maps to
		// the full range being valid.
		//
		// We use r.dl.AllColumns instead of r.opts.Columns because the downloader
		// will cache metadata.
		var rowsCount uint64
		for _, column := range r.dl.AllColumns() {
			rowsCount = max(rowsCount, uint64(column.ColumnInfo().RowsCount))
		}
		return rowRanges{{Start: 0, End: rowsCount - 1}}, nil

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
		return nil, fmt.Errorf("can't simplify FalsePredicate")

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

	case FuncPredicate:
		return nil, fmt.Errorf("can't simplify FuncPredicate")

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", inner))
	}
}

// buildColumnPredicateRanges returns a set of rowRanges that are valid based
// on whether EqualPredicate, GreaterThanPredicate, or LessThanPredicate may be
// true for each page in a column.
func (r *Reader) buildColumnPredicateRanges(ctx context.Context, c Column, p Predicate) (rowRanges, error) {
	// Get the wrapped column so that the result of c.ListPages can be cached.
	if idx, ok := r.origColumnLookup[c]; ok {
		c = r.dl.AllColumns()[idx]
	} else {
		return nil, fmt.Errorf("column %v not found in Reader columns", c)
	}

	var ranges rowRanges

	var (
		pageStart    int
		lastPageSize int
	)

	for result := range c.ListPages(ctx) {
		pageStart += lastPageSize

		page, err := result.Value()
		if err != nil {
			return nil, err
		}
		pageInfo := page.PageInfo()
		lastPageSize = pageInfo.RowCount

		pageRange := rowRange{
			Start: uint64(pageStart),
			End:   uint64(pageStart + pageInfo.RowCount - 1),
		}

		minValue, maxValue, err := readMinMax(pageInfo.Stats)
		if err != nil {
			return nil, fmt.Errorf("failed to read page stats: %w", err)
		} else if minValue.IsNil() || maxValue.IsNil() {
			// No stats, so we add the whole range.
			ranges.Add(pageRange)
			continue
		}

		var include bool

		switch p := p.(type) {
		case EqualPredicate: // EqualPredicate may be true if p.Value is inside the range of the page.
			include = CompareValues(p.Value, minValue) >= 0 && CompareValues(p.Value, maxValue) <= 0
		case GreaterThanPredicate: // GreaterThanPredicate may be true if maxValue of a page is greater than p.Value
			include = CompareValues(maxValue, p.Value) > 0
		case LessThanPredicate: // LessThanPredicate may be true if minValue of a page is less than p.Value
			include = CompareValues(minValue, p.Value) < 0
		default:
			panic(fmt.Sprintf("unsupported predicate type %T", p))
		}

		if include {
			ranges.Add(pageRange)
		}
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
