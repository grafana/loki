package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

// statsRow is the decoded per-row representation of a stats section.
type statsRow struct {
	ObjectPath       string
	SectionIndex     int64
	SortSchema       string
	Labels           map[string]string
	MinTimestamp     int64 // unix nanos
	MaxTimestamp     int64 // unix nanos
	RowCount         int64
	UncompressedSize int64
}

// compareStatsRow returns the lexicographic order of two statsRow records
// under the on-disk stats sort, which is:
//
//	(Labels in SortSchema order, MinTimestamp, MaxTimestamp)
//
// This matches pkg/dataobj/sections/stats/builder.go:compareStats. Keeping
// the comparator aligned with the on-disk sort is required for the K-way
// merge heap invariant: each pile emits rows in this order, and the heap
// pop-order must agree.
//
// Note: ObjectPath, SectionIndex, and SortSchema are NOT part of the sort
// key. The reducer (mergeStatsIntoBuilder) verifies they match when an
// equal-key collision actually occurs (a v1.0 invariant violation).
//
// Assumes all input rows share the same SortSchema; SortSchema-mismatch
// handling is the reducer's responsibility (D3).
func compareStatsRow(a, b statsRow) int {
	// Compare label values in the order defined by SortSchema
	for labelName := range strings.SplitSeq(a.SortSchema, ",") {
		va := a.Labels[labelName]
		vb := b.Labels[labelName]
		if va != vb {
			if va < vb {
				return -1
			}
			return 1
		}
	}

	// Compare MinTimestamp
	if a.MinTimestamp != b.MinTimestamp {
		if a.MinTimestamp < b.MinTimestamp {
			return -1
		}
		return 1
	}

	// Compare MaxTimestamp
	if a.MaxTimestamp != b.MaxTimestamp {
		if a.MaxTimestamp < b.MaxTimestamp {
			return -1
		}
		return 1
	}

	return 0
}

// statsPileReader reads statsRow records from a stats section in order.
type statsPileReader struct {
	ctx       context.Context
	pileIdx   int
	reader    *stats.Reader
	batch     arrow.RecordBatch
	index     int
	columns   map[string]int // column name -> field index
	opened    bool
	validated bool

	cur       statsRow // current value, valid between Next() returning true and the next Next() call
	err       error    // captured if iteration ends with anything other than io.EOF
	exhausted bool     // set when Next has returned false; further calls return false without work
}

// newStatsPileReader creates a new statsPileReader from a stats section.
func newStatsPileReader(ctx context.Context, sec *stats.Section, pileIdx int) *statsPileReader {
	reader := stats.NewReader(stats.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	return &statsPileReader{
		ctx:     ctx,
		pileIdx: pileIdx,
		reader:  reader,
		columns: make(map[string]int),
	}
}

// Next advances the cursor. Returns false on exhaustion (natural EOF or any error).
// Subsequent calls to Next continue to return false.
func (r *statsPileReader) Next() bool {
	if r.exhausted {
		return false
	}
	rec, err := r.next()
	if errors.Is(err, io.EOF) {
		r.exhausted = true
		return false
	}
	if err != nil {
		r.err = err
		r.exhausted = true
		return false
	}
	r.cur = rec
	return true
}

// next reads the next statsRow from the section. Returns io.EOF when exhausted.
// Uses r.ctx instead of accepting a context parameter.
func (r *statsPileReader) next() (statsRow, error) {
	// Open the reader on first access
	if !r.opened {
		if err := r.reader.Open(r.ctx); err != nil {
			return statsRow{}, fmt.Errorf("opening reader: %w", err)
		}
		r.opened = true
	}

	// If we don't have a batch or we've consumed all rows in the current batch,
	// read the next batch.
	if r.batch == nil || r.index >= int(r.batch.NumRows()) {
		if r.batch != nil {
			r.batch.Release()
			r.batch = nil
		}

		// Read next batch
		batch, err := r.reader.Read(r.ctx, 8192)
		if errors.Is(err, io.EOF) && batch == nil {
			return statsRow{}, io.EOF
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return statsRow{}, fmt.Errorf("reading batch: %w", err)
		}

		// If we got a batch with rows, use it
		if batch != nil && batch.NumRows() > 0 {
			r.batch = batch
			r.index = 0
			// Build and validate column index on first batch
			if !r.validated {
				r.columns = make(map[string]int)
				schema := batch.Schema()
				for i, field := range schema.Fields() {
					r.columns[field.Name] = i
				}
				if err := validateStatsColumns(r.columns); err != nil {
					return statsRow{}, err
				}
				r.validated = true
			}
		} else if batch != nil {
			batch.Release()
			return statsRow{}, io.EOF
		} else if errors.Is(err, io.EOF) {
			return statsRow{}, io.EOF
		}
	}

	// Decode the row at r.index from r.batch
	row := decodeStatsRow(r.batch, r.columns, r.index)
	r.index++
	return row, nil
}

// Value returns the current record. Undefined if Next has not been called
// or if the last Next call returned false.
func (r *statsPileReader) Value() statsRow {
	return r.cur
}

// Err returns any error that caused iteration to end. nil on natural EOF.
func (r *statsPileReader) Err() error {
	return r.err
}

// PileIdx returns the pile's index in the merge.
// Exhausted reports whether Next has returned false. After exhaustion the
// sequence holds no more records; Value's return is undefined.
func (r *statsPileReader) Exhausted() bool {
	return r.exhausted
}

func (r *statsPileReader) PileIdx() int {
	return r.pileIdx
}

// Verify that statsPileReader implements pileSequence[statsRow].
var _ pileSequence[statsRow] = (*statsPileReader)(nil)

// Close closes the reader and releases resources.
func (r *statsPileReader) Close() error {
	if r.batch != nil {
		r.batch.Release()
		r.batch = nil
	}
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}

// validateStatsColumns checks that all required stats columns are present.
func validateStatsColumns(columns map[string]int) error {
	requiredColumns := []string{
		"object_path.utf8",
		"section_index.int64",
		"sort_schema.utf8",
		"min_timestamp.timestamp",
		"max_timestamp.timestamp",
		"row_count.int64",
		"uncompressed_size.int64",
	}

	for _, colName := range requiredColumns {
		if _, ok := columns[colName]; !ok {
			return fmt.Errorf("stats section missing required column %q", colName)
		}
	}

	return nil
}

// decodeStatsRow decodes a single row from an arrow RecordBatch into a statsRow.
// Uses the columns map to look up column indices rather than iterating fields by name.
func decodeStatsRow(batch arrow.RecordBatch, columns map[string]int, rowIndex int) statsRow {
	result := statsRow{
		Labels: make(map[string]string),
	}

	// Helper function to safely extract a column value by name
	getColumn := func(name string) arrow.Array {
		if idx, ok := columns[name]; ok {
			return batch.Column(idx)
		}
		return nil
	}

	// Decode required columns
	if col := getColumn("object_path.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.ObjectPath = col.(*array.String).Value(rowIndex)
	}

	if col := getColumn("section_index.int64"); col != nil && !col.IsNull(rowIndex) {
		result.SectionIndex = col.(*array.Int64).Value(rowIndex)
	}

	if col := getColumn("sort_schema.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.SortSchema = col.(*array.String).Value(rowIndex)
	}

	if col := getColumn("min_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MinTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}

	if col := getColumn("max_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MaxTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}

	if col := getColumn("row_count.int64"); col != nil && !col.IsNull(rowIndex) {
		result.RowCount = col.(*array.Int64).Value(rowIndex)
	}

	if col := getColumn("uncompressed_size.int64"); col != nil && !col.IsNull(rowIndex) {
		result.UncompressedSize = col.(*array.Int64).Value(rowIndex)
	}

	// Decode all label columns (format: "<label>.label.utf8").
	// Use suffix trimming rather than Split to handle label names that contain dots.
	for fieldName, colIdx := range columns {
		if !strings.HasSuffix(fieldName, ".label.utf8") {
			continue
		}
		labelName := strings.TrimSuffix(fieldName, ".label.utf8")
		col := batch.Column(colIdx)
		if !col.IsNull(rowIndex) {
			result.Labels[labelName] = col.(*array.String).Value(rowIndex)
		}
	}

	return result
}
