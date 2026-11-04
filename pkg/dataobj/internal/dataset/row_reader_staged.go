package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/xcap"
)

const (
	// minStagedBytes is the least compressed size of the other columns that makes staging pay. The
	// other columns are the ones that the early predicates do not read. Only pages that overlap the
	// rows left after page pruning count. Below it, the extra round trip to object storage costs
	// more than it saves.
	minStagedBytes = 4 << 20

	// The early predicates read rows in batches. They borrow the buffer of the first Read when it
	// holds at least minScratchRows rows, and use up to maxScratchRows of them. A smaller buffer
	// would make too many batches, so they allocate ownScratchRows rows.
	minScratchRows = 256
	maxScratchRows = 4096
	ownScratchRows = 1024
)

// maxRetainedBytes is the size above which a staged read drops the retained values of the early
// columns. The size counts stored values, not spare capacity, and the check runs after each batch.
// A read that drops the values decodes those columns a second time. It is a variable so a test can
// lower it.
var maxRetainedBytes = 32 << 20

// stagedRead is the state of a staged read.
//
// Open downloads the pages of the columns that the early predicates read. The early predicates are
// the ones of the stages before the last. The first Read runs them once, on the rows that page
// pruning leaves. The rows that pass are the survivors.
//
// The first Read then downloads the pages of the other columns for the survivors. From then on
// Read returns survivors only. It runs the predicates of the last stage on them.
//
// A survivor needs the values that the early predicates decoded when Read returns the column or a
// later predicate reads it. The first Read keeps these values, so no column is decoded twice. If
// the values exceed maxRetainedBytes, the first Read drops them. Read then decodes those columns
// again.
type stagedRead struct {
	active    bool                // Open chose a staged read.
	evaluated bool                // The first Read ran the early predicates.
	survivors rangeset.Set        // Rows that passed the early predicates.
	cursor    uint64              // Next row of survivors that Read returns.
	ordinal   int                 // Number of survivors before cursor.
	kept      []Column            // Early columns that a survivor must hold: projected, or read by a later predicate.
	retained  *retainedValues     // Values of kept, one entry per survivor. Nil if they exceed maxRetainedBytes, if the store cannot hold a kept column, or after the last survivor.
	filled    map[Column]struct{} // Columns that every row already holds, for evalPredicate. Rebuilt on each Read.
	scratch   []Row               // Rows that the early predicates own, when the caller's buffer is small.
}

// release drops the memory that the first Read allocated and that later Reads no longer need.
func (s *stagedRead) release() {
	s.retained = nil
	s.scratch = nil
	s.survivors = rangeset.Set{}
}

// flattenStages fills r.predicates and r.earlyEnd from the options.
func (r *RowReader) flattenStages() {
	stages := r.opts.PredicateStages
	for i, stage := range stages {
		r.predicates = append(r.predicates, stage...)
		if i < len(stages)-1 {
			r.earlyEnd = len(r.predicates)
		}
	}
}

// prepareStaged decides whether Read runs as a staged read. If so, it downloads the pages that the
// early predicates read and returns true. Otherwise Open downloads as an unstaged read does.
//
// Open only fetches pages. The first Read runs the early predicates.
func (r *RowReader) prepareStaged(ctx context.Context) (bool, error) {
	if !r.opts.PrefetchAllOnOpen || r.earlyEnd == 0 || r.ranges.Len() == 0 {
		return false, nil
	}

	// filterRows reads the columns of the first predicate with ReadColumns, so it needs at least one.
	if first, _ := r.predicateColumns(r.predicates[0], func(Column) bool { return true }); len(first) == 0 {
		return false, nil
	}

	columns, _ := r.stageColumns(0, r.earlyEnd)
	remaining, err := r.dl.PageBytes(ctx, columnSet(columns))
	if err != nil {
		return false, err
	} else if remaining < minStagedBytes {
		return false, nil
	}

	if err := r.dl.PrefetchColumns(ctx, columns); err != nil {
		return false, err
	}
	r.staged.active = true
	return true, nil
}

// readStaged is Read for a staged read.
//
// A staged read reports the same row counts and row bytes as an unstaged read. Its download
// statistics and its Read call count differ.
//
// evaluateEarly records the rows and bytes of the early predicates. readStaged records the rows and
// bytes of the later predicates and of the secondary columns. A column that the early predicates
// read counts once, even when a later predicate reads it again.
func (r *RowReader) readStaged(ctx context.Context, s []Row, region *xcap.Region) (int, error) {
	if !r.staged.evaluated {
		if err := r.evaluateEarly(ctx, region, r.scratchRows(s)); err != nil {
			return 0, err
		}
	}

	n, next := r.takeSurvivors(s)
	if n == 0 {
		r.staged.release()
		return 0, io.EOF
	}

	if err := r.restoreKept(ctx, s[:n]); err != nil {
		return 0, err
	}

	// The rows hold the kept columns now, so a later predicate must not read or count them again.
	if r.staged.filled == nil {
		r.staged.filled = make(map[Column]struct{}, len(r.primaryColumnIndexes))
	}
	clear(r.staged.filled)
	for _, c := range r.staged.kept {
		r.staged.filled[c] = struct{}{}
	}

	var (
		pass         = n
		primaryBytes int64
	)
	for i := r.earlyEnd; i < len(r.predicates) && pass > 0; i++ {
		var (
			readBytes int64
			err       error
		)
		if r.batchPredicates[i] != nil {
			pass, readBytes, err = r.evalBatchPredicate(ctx, r.batchPredicates[i], s[:pass])
		} else {
			pass, readBytes, err = r.evalPredicate(ctx, r.predicates[i], r.compiledPredicates[i], s[:pass], r.staged.filled)
		}
		if err != nil {
			return 0, err
		}
		primaryBytes += readBytes
	}

	if secondary := r.secondaryColumns(); pass > 0 && len(secondary) > 0 {
		count, err := r.inner.Fill(ctx, secondary, s[:pass])
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		} else if count != pass {
			return 0, fmt.Errorf("failed to fill rows: expected %d, got %d", pass, count)
		}

		var secondaryBytes int64
		for i := range pass {
			secondaryBytes += s[i].Size() - s[i].SizeOfColumns(r.primaryColumnIndexes)
		}
		region.Record(dataobj.StatDatasetSecondaryRowsRead.Observe(int64(pass)))
		region.Record(dataobj.StatDatasetSecondaryRowBytes.Observe(secondaryBytes))
	}
	region.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(primaryBytes))

	// Move the cursor only after the read succeeded, so a caller can retry a failed Read.
	r.staged.cursor = next
	r.staged.ordinal += n
	return pass, nil
}

// restoreKept sets the kept columns of rows, which are the next survivors. It copies the values
// from the retained store. If the store is nil, it decodes the columns again.
//
// The decode fails for a kept column that ends before the survivors. The early pass read nulls for
// the missing rows of the columns of its first predicate, so this differs from a read that kept
// the store.
func (r *RowReader) restoreKept(ctx context.Context, rows []Row) error {
	if len(r.staged.kept) == 0 {
		return nil
	} else if r.staged.retained != nil {
		r.staged.retained.restore(r.staged.ordinal, rows)
		return nil
	}

	// The survivors come in runs. Filling the columns now, before a later predicate drops single
	// rows, keeps the fill in a few partitions.
	count, err := r.inner.Fill(ctx, r.staged.kept, rows)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("filling the early columns again: %w", err)
	} else if count != len(rows) {
		return fmt.Errorf("filling the early columns again: failed to fill rows: expected %d, got %d", len(rows), count)
	}
	return nil
}

// evaluateEarly runs the early predicates on the rows that page pruning leaves. It retains the
// values of the kept columns for the survivors, and downloads the pages of the other columns for
// them.
//
// It changes r.staged only after every step succeeded, so a failed first Read can run again.
func (r *RowReader) evaluateEarly(ctx context.Context, region *xcap.Region, scratch []Row) error {
	kept, store := r.keptColumns()

	survivors, store, stats, err := r.passingRanges(ctx, scratch, store)
	if err != nil {
		return fmt.Errorf("running the early predicates: %w", err)
	}

	// Each survivor needs one entry. If the counts differ, Read decodes the columns again.
	if store != nil && store.rows != survivors.Len() {
		store = nil
	}

	r.dl.SetDatasetRanges(survivors)
	if survivors.Len() > 0 {
		if err := r.dl.Prefetch(ctx); err != nil {
			return fmt.Errorf("prefetching pages: %w", err)
		}
	}

	r.staged.kept = kept
	r.staged.retained = store
	r.staged.survivors = survivors
	r.staged.evaluated = true
	stats.record(region)
	return nil
}

// keptColumns returns the early columns that a survivor must hold. A survivor holds a column when
// Read returns it or a later predicate reads it. It also returns an empty store for their values.
// The store is nil when it cannot hold the type of a column.
func (r *RowReader) keptColumns() ([]Column, *retainedValues) {
	_, laterIdxs := r.stageColumns(r.earlyEnd, len(r.predicates))
	later := make(map[int]struct{}, len(laterIdxs))
	for _, idx := range laterIdxs {
		later[idx] = struct{}{}
	}

	var (
		kept  []Column
		store = &retainedValues{}
	)
	columns, idxs := r.stageColumns(0, r.earlyEnd)
	for i, c := range columns {
		idx := idxs[i]
		if _, ok := later[idx]; !ok && idx >= len(r.projectedColumns) {
			continue
		}
		kept = append(kept, c)

		column, ok := newRetainedColumn(idx, c.ColumnDesc().Type.Physical)
		if !ok {
			store = nil
		}
		if store != nil {
			store.columns = append(store.columns, column)
		}
	}
	return kept, store
}

// scratchRows returns the rows that the early predicates use as a buffer. The first Read has no use
// for the rows of s yet, so they serve when there are enough of them.
func (r *RowReader) scratchRows(s []Row) []Row {
	if len(s) >= minScratchRows {
		return s[:min(len(s), maxScratchRows)]
	}
	if r.staged.scratch == nil {
		// One block holds the values of every row, so the rows cost two allocations.
		width := len(r.columns)
		values := make([]Value, ownScratchRows*width)
		r.staged.scratch = make([]Row, ownScratchRows)
		for i := range r.staged.scratch {
			r.staged.scratch[i].Values = values[i*width : (i+1)*width : (i+1)*width]
		}
	}
	return r.staged.scratch
}

// passingRanges returns the rows of r.ranges that pass the early predicates, and what the
// predicates read. It reads the rows through scratch.
//
// It adds the kept columns of each survivor to store. If the store grows past maxRetainedBytes,
// passingRanges drops it and returns nil. A nil store stays nil.
func (r *RowReader) passingRanges(ctx context.Context, scratch []Row, store *retainedValues) (rangeset.Set, *retainedValues, filterStats, error) {
	var (
		runs  []rangeset.Range
		total filterStats
	)
	for window := range r.ranges.Iter() {
		for row := window.Start; row < window.End; {
			if err := ctx.Err(); err != nil {
				return rangeset.Set{}, nil, filterStats{}, err
			}
			if _, err := r.inner.Seek(int64(row), io.SeekStart); err != nil {
				return rangeset.Set{}, nil, filterStats{}, fmt.Errorf("seeking to row %d: %w", row, err)
			}

			batch := scratch[:min(uint64(len(scratch)), window.End-row)]
			pass, stats, err := r.filterRows(ctx, r.earlyEnd, batch)
			if errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return rangeset.Set{}, nil, filterStats{}, err
			} else if stats.rowsRead == 0 {
				return rangeset.Set{}, nil, filterStats{}, fmt.Errorf("reading row %d: no row read", row)
			}
			total.add(stats)

			if store != nil {
				store.add(batch[:pass])
				if store.size() > maxRetainedBytes {
					store = nil
				}
			}

			for _, passed := range batch[:pass] {
				index := uint64(passed.Index)
				if n := len(runs); n > 0 && runs[n-1].End == index {
					runs[n-1].End++
				} else {
					runs = append(runs, rangeset.Range{Start: index, End: index + 1})
				}
			}
			row += uint64(stats.rowsRead)
		}
	}

	// Adding runs one by one to a Set is quadratic. From sorts and merges them once.
	return rangeset.From(runs...), store, total, nil
}

// takeSurvivors sets Row.Index of the first rows of s to the next survivors, and sizes their
// values for every column. It returns how many rows it set, and the cursor that follows them.
func (r *RowReader) takeSurvivors(s []Row) (int, uint64) {
	var (
		n      int
		cursor = r.staged.cursor
	)
	for n < len(s) {
		run := r.staged.survivors.RangeFor(cursor)
		if !run.IsValid() {
			next, ok := r.staged.survivors.Next(cursor)
			if !ok {
				break
			}
			cursor = next
			continue
		}

		take := min(uint64(len(s)-n), run.End-cursor)
		for j := range take {
			row := &s[n+int(j)]
			row.Index = int(cursor + j)

			// The retained store and a batch predicate write to Values and do not grow it. Grow it
			// here, as Fill does.
			row.Values = slicegrow.GrowToCap(row.Values, len(r.columns))[:len(r.columns)]
		}
		n += int(take)
		cursor += take
	}
	return n, cursor
}

// stageColumns returns the downloader columns that the predicates r.predicates[first:last] read,
// and the index of each in Row.Values. It lists each column once, in the order that the predicates
// first read it.
func (r *RowReader) stageColumns(first, last int) ([]Column, []int) {
	var (
		seen    = make(map[Column]struct{})
		columns []Column
		idxs    []int
	)
	for _, p := range r.predicates[first:last] {
		pc, pi := r.predicateColumns(p, func(c Column) bool {
			_, ok := seen[c]
			return !ok
		})
		for _, c := range pc {
			seen[c] = struct{}{}
		}
		columns = append(columns, pc...)
		idxs = append(idxs, pi...)
	}
	return columns, idxs
}

func columnSet(columns []Column) map[Column]struct{} {
	set := make(map[Column]struct{}, len(columns))
	for _, c := range columns {
		set[c] = struct{}{}
	}
	return set
}
