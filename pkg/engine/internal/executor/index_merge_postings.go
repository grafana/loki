package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// postingsRow is the decoded per-row representation of a postings section,
// covering both Label and Bloom kinds.
type postingsRow struct {
	Kind             postings.PostingKind // KindLabel or KindBloom
	ObjectPath       string
	SectionIndex     int64
	ColumnName       string
	LabelValue       string // empty for KindBloom rows
	StreamIDBitmap   []byte
	BloomFilter      []byte // nil for KindLabel rows
	UncompressedSize int64
	MinTimestamp     int64 // unix nanos
	MaxTimestamp     int64 // unix nanos
}

// comparePostingsRow compares two postings rows using the sort order:
// (Kind, ObjectPath, SectionIndex, ColumnName, LabelValue).
func comparePostingsRow(a, b postingsRow) int {
	if a.Kind != b.Kind {
		if a.Kind < b.Kind {
			return -1
		}
		return 1
	}
	if a.ObjectPath != b.ObjectPath {
		if a.ObjectPath < b.ObjectPath {
			return -1
		}
		return 1
	}
	if a.SectionIndex != b.SectionIndex {
		if a.SectionIndex < b.SectionIndex {
			return -1
		}
		return 1
	}
	if a.ColumnName != b.ColumnName {
		if a.ColumnName < b.ColumnName {
			return -1
		}
		return 1
	}
	if a.LabelValue != b.LabelValue {
		if a.LabelValue < b.LabelValue {
			return -1
		}
		return 1
	}
	return 0
}

// postingsPileReader reads postingsRow records from a postings section in order.
type postingsPileReader struct {
	ctx       context.Context
	pileIdx   int
	reader    *postings.Reader
	batch     arrow.RecordBatch
	index     int
	columns   map[string]int // column name -> field index
	opened    bool
	validated bool

	cur       postingsRow // current value, valid between Next() returning true and the next Next() call
	err       error       // captured if iteration ends with anything other than io.EOF
	exhausted bool        // set when Next has returned false; further calls return false without work
}

// newPostingsPileReader creates a new postingsPileReader from a postings section.
func newPostingsPileReader(ctx context.Context, sec *postings.Section, pileIdx int) *postingsPileReader {
	reader := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	return &postingsPileReader{
		ctx:     ctx,
		pileIdx: pileIdx,
		reader:  reader,
		columns: make(map[string]int),
	}
}

// Next advances the cursor. Returns false on exhaustion (natural EOF or any error).
// Subsequent calls to Next continue to return false.
func (r *postingsPileReader) Next() bool {
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

// next reads the next postingsRow from the section. Returns io.EOF when exhausted.
// Uses r.ctx instead of accepting a context parameter.
func (r *postingsPileReader) next() (postingsRow, error) {
	// Open the reader on first access
	if !r.opened {
		if err := r.reader.Open(r.ctx); err != nil {
			return postingsRow{}, fmt.Errorf("opening reader: %w", err)
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
			return postingsRow{}, io.EOF
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return postingsRow{}, fmt.Errorf("reading batch: %w", err)
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
				if err := validatePostingsColumns(r.columns); err != nil {
					return postingsRow{}, err
				}
				r.validated = true
			}
		} else if batch != nil {
			batch.Release()
			return postingsRow{}, io.EOF
		} else if errors.Is(err, io.EOF) {
			return postingsRow{}, io.EOF
		}
	}

	// Decode the row at r.index from r.batch
	row := decodePostingsRow(r.batch, r.columns, r.index)
	r.index++
	return row, nil
}

// Value returns the current record. Undefined if Next has not been called
// or if the last Next call returned false.
func (r *postingsPileReader) Value() postingsRow {
	return r.cur
}

// Err returns any error that caused iteration to end. nil on natural EOF.
func (r *postingsPileReader) Err() error {
	return r.err
}

// PileIdx returns the pile's index in the merge.
// Exhausted reports whether Next has returned false. After exhaustion the
// sequence holds no more records; Value's return is undefined.
func (r *postingsPileReader) Exhausted() bool {
	return r.exhausted
}

func (r *postingsPileReader) PileIdx() int {
	return r.pileIdx
}

// Verify that postingsPileReader implements pileSequence[postingsRow].
var _ pileSequence[postingsRow] = (*postingsPileReader)(nil)

// Close closes the reader and releases resources.
func (r *postingsPileReader) Close() error {
	if r.batch != nil {
		r.batch.Release()
		r.batch = nil
	}
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}

// validatePostingsColumns checks that all required postings columns are present.
func validatePostingsColumns(columns map[string]int) error {
	requiredColumns := []string{
		"kind.int64",
		"object_path.utf8",
		"section_index.int64",
		"column_name.utf8",
		"label_value.utf8",
		"stream_id_bitmap.binary",
		"bloom_filter.binary",
		"uncompressed_size.int64",
		"min_timestamp.timestamp",
		"max_timestamp.timestamp",
	}

	for _, colName := range requiredColumns {
		if _, ok := columns[colName]; !ok {
			return fmt.Errorf("postings section missing required column %q", colName)
		}
	}

	return nil
}

// decodePostingsRow decodes a single row from an arrow RecordBatch into a postingsRow.
// Uses the columns map to look up column indices rather than iterating fields by name.
func decodePostingsRow(batch arrow.RecordBatch, columns map[string]int, rowIndex int) postingsRow {
	result := postingsRow{}

	// Helper function to safely extract a column value by name
	getColumn := func(name string) arrow.Array {
		if idx, ok := columns[name]; ok {
			return batch.Column(idx)
		}
		return nil
	}

	// Decode each required column
	if col := getColumn("kind.int64"); col != nil && !col.IsNull(rowIndex) {
		result.Kind = postings.PostingKind(col.(*array.Int64).Value(rowIndex))
	}

	if col := getColumn("object_path.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.ObjectPath = col.(*array.String).Value(rowIndex)
	}

	if col := getColumn("section_index.int64"); col != nil && !col.IsNull(rowIndex) {
		result.SectionIndex = col.(*array.Int64).Value(rowIndex)
	}

	if col := getColumn("column_name.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.ColumnName = col.(*array.String).Value(rowIndex)
	}

	if col := getColumn("label_value.utf8"); col != nil && !col.IsNull(rowIndex) {
		result.LabelValue = col.(*array.String).Value(rowIndex)
	}

	if col := getColumn("stream_id_bitmap.binary"); col != nil && !col.IsNull(rowIndex) {
		result.StreamIDBitmap = bytes.Clone(col.(*array.Binary).Value(rowIndex))
	}

	if col := getColumn("bloom_filter.binary"); col != nil && !col.IsNull(rowIndex) {
		result.BloomFilter = bytes.Clone(col.(*array.Binary).Value(rowIndex))
	}

	if col := getColumn("uncompressed_size.int64"); col != nil && !col.IsNull(rowIndex) {
		result.UncompressedSize = col.(*array.Int64).Value(rowIndex)
	}

	if col := getColumn("min_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MinTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}

	if col := getColumn("max_timestamp.timestamp"); col != nil && !col.IsNull(rowIndex) {
		result.MaxTimestamp = int64(col.(*array.Timestamp).Value(rowIndex))
	}

	return result
}
