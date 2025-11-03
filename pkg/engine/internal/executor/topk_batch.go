package executor

import (
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/arrowagg"
	"github.com/grafana/loki/v3/pkg/util/topk"
)

// topkBatch calculates the top K rows from a stream of [arrow.Record]s, where
// rows are ranked by the specified Fields.
//
// Rows with equal values for all the fields are ranked by the order in which
// they were appended.
//
// topkBatch only identifies which rows belong in the top K, but does not
// guarantee any specific ordering of those rows in the compacted output. Callers
// should sort the result if a specific order is required.
type topkBatch struct {
	// Fields holds the list of fields to sort by, in order of precedence. If an
	// incoming record is missing one of these fields, the value for that field
	// is treated as null.
	Fields []arrow.Field

	// Ascending indicates whether to store the top K rows in ascending order. If
	// true, the smallest K rows will be retained.
	Ascending bool

	// NullsFirst determines how to sort null values in the top K rows. If
	// NullsFirst is true, null values will be treated as less than non-null
	// values.
	NullsFirst bool

	// K holds the number of top rows to compute.
	K int

	// MaxUnused determines the maximum number of "unused" rows to retain. An
	// unused row is any row from a retained record that does not contribute to
	// the top K.
	//
	// After the number of unused rows exceeds MaxUnused, topkBatch will compact
	// retained records into a new record only containing the current top K rows.
	MaxUnused int

	ready       bool // True if all fields below are initialized.
	nextID      int
	mapper      *arrowagg.Mapper
	heap        *topk.Heap[*topkReference]
	usedCount   map[arrow.Record]int
	usedSchemas map[*arrow.Schema]int
}

// topkReference is a reference to a row in a record that is part of the
// current set of top K rows.
type topkReference struct {
	// ID is a per-row unique ID across all records, used for comparing rows that
	// are otherwise equal in the sort order.
	ID int

	Record arrow.Record // Record contributing to the top K.
	Row    int
}

// Put adds rows from rec into b. If rec contains at least one row that belongs
// in the current top K rows, rec is retained until a compaction occurs or it
// is pushed out of the top K.
//
// When rec is retained, the number of rows that do not contribute to the top K
// contribute towards the total unused rows in b. Once the number of unused
// rows exceeds MaxUnused, Put calls [topkBatch.Compact] to clean up record
// references.
func (b *topkBatch) Put(rec arrow.Record) {
	b.put(rec)

	// Compact if adding this record pushed us over the limit of unused rows.
	if _, unused := b.Size(); unused > b.MaxUnused {
		compacted := b.Compact()
		b.put(compacted)
	}
}

// put adds rows from rec into b without checking the number of unused rows.
func (b *topkBatch) put(rec arrow.Record) {
	if !b.ready {
		b.init()
	}

	// Iterate over the rows in the record and attempt to push each of them onto
	// the heap. For simplicity, we retain the record per row in the heap, and
	// track that count in a map for being able to compute the number of unused
	// rows.
	for i := range int(rec.NumRows()) {
		ref := &topkReference{
			ID:     b.nextID,
			Record: rec,
			Row:    i,
		}
		b.nextID++

		res, prev := b.heap.Push(ref)
		switch res {
		case topk.PushResultPushed:
			b.usedCount[rec]++
			b.usedSchemas[rec.Schema()]++

		case topk.PushResultReplaced:
			b.usedCount[rec]++
			b.usedSchemas[rec.Schema()]++

			b.usedCount[prev.Record]--
			b.usedSchemas[prev.Record.Schema()]--
			if b.usedCount[prev.Record] == 0 {
				delete(b.usedCount, prev.Record)
			}
			if b.usedSchemas[prev.Record.Schema()] == 0 {
				b.mapper.RemoveSchema(prev.Record.Schema())
				delete(b.usedSchemas, prev.Record.Schema())
			}
		}
	}
}

func (b *topkBatch) init() {
	b.heap = &topk.Heap[*topkReference]{
		Limit: b.K,
		Less: func(left, right *topkReference) bool {
			if b.Ascending {
				// If we're looking for the top K in ascending order, we need to switch
				// over to a max-heap and return true if left > right.
				return !b.less(left, right)
			}
			return b.less(left, right)
		},
	}
	b.mapper = arrowagg.NewMapper(b.Fields)
	b.usedCount = make(map[arrow.Record]int)
	b.usedSchemas = make(map[*arrow.Schema]int)
	b.ready = true
}

func (b *topkBatch) less(left, right *topkReference) bool {
	for fieldIndex := range b.Fields {
		leftArray := b.findRecordArray(left.Record, b.mapper, fieldIndex)
		rightArray := b.findRecordArray(right.Record, b.mapper, fieldIndex)

		// Compare directly from arrays without creating scalars to avoid allocations
		res, err := compareArrays(leftArray, rightArray, left.Row, right.Row, b.NullsFirst)
		if err != nil {
			// Treat failure to compare as equal, so that the sort order is
			// consistent. This should only happen when given invalid values to
			// compare, as we know leftArray and rightArray are of the same type.
			continue
		}
		switch res {
		case 0: // left == right
			// Continue to the next field if two values are equal.
			continue
		case -1: // left < right
			return true
		case 1: // left > right
			return false
		}
	}

	// Fall back to sorting by ID to have consistent ordering, so that no two
	// rows are ever equal.
	switch {
	case b.Ascending:
		return left.ID > right.ID
	default:
		return left.ID < right.ID
	}
}

// findRecordArray finds the array for the given [b.Fields] field index from
// the mapper cache. findRecordArray returns nil if the field is not present in
// rec.
func (b *topkBatch) findRecordArray(rec arrow.Record, mapper *arrowagg.Mapper, fieldIndex int) arrow.Array {
	columnIndex := mapper.FieldIndex(rec.Schema(), fieldIndex)
	if columnIndex < 0 || columnIndex >= int(rec.NumCols()) {
		return nil // No such field in the record.
	}
	return rec.Column(columnIndex)
}

// Size returns the current number of rows in the top K (<= K) and the number
// of unused rows that are retained from records (<= MaxUnused).
func (b *topkBatch) Size() (rows int, unused int) {
	if b.heap == nil {
		return 0, 0
	}

	rows = b.heap.Len()

	for rec, used := range b.usedCount {
		// The number of unused rows per record is its total number of rows minus
		// the number of references to it, as each reference corresponds to one
		// value in the heap.
		unused += int(rec.NumRows()) - used
	}

	return rows, unused
}

// Compact compacts all retained records into a single record containing just
// the current top K rows.
//
// The returned record will have a combined schema from all of the input
// records. Neither the order of fields nor the order of rows in the returned
// record is guaranteed. Rows that did not have one of the
// combined fields will be filled with null values for those fields.
//
// Compact returns nil if no rows are in the top K.
func (b *topkBatch) Compact() arrow.Record {
	if len(b.usedCount) == 0 {
		return nil
	}
	defer b.Reset() // Reset the batch after compaction to free references.

	// Get all row references to compact.
	rowRefs := b.heap.PopAll()

	recordRows := make(map[arrow.Record][]int, len(b.usedCount))
	for _, ref := range rowRefs {
		recordRows[ref.Record] = append(recordRows[ref.Record], ref.Row)
	}

	compactor := arrowagg.NewRecords(memory.DefaultAllocator)
	for rec, rows := range recordRows {
		slices.Sort(rows)
		iterContiguousRanges(rows, func(start, end int) bool {
			compactor.AppendSlice(rec, int64(start), int64(end))
			return true
		})
	}

	// Rows are grouped by their source record and appended
	// in contiguous ranges for efficiency.
	compacted, err := compactor.Aggregate()
	if err != nil {
		// Aggregate should only fail if we didn't aggregate anything, which we
		// know we have above.
		panic(fmt.Sprintf("topkBatch.Compact: unexpected error aggregating records: %s", err))
	}
	return compacted
}

// Reset releases all resources held by the topkBatch.
func (b *topkBatch) Reset() {
	if !b.ready {
		return
	}

	b.nextID = 0
	b.mapper.Reset()
	b.heap.PopAll()

	clear(b.usedCount)
	clear(b.usedSchemas)
}

// iterContiguousRanges iterates over contiguous ranges of row indices from a sorted
// slice. Rows must be sorted in ascending order.
//
// For example, if rows is [1, 2, 3, 5, 6, 7], it will yield two ranges:
// [1, 4) and [5, 8), representing the contiguous sequences.
//
// The function calls yield for each contiguous range found. If yield returns false,
// iteration stops.
func iterContiguousRanges(rows []int, yield func(start, end int) bool) {
	if len(rows) == 0 {
		return
	}

	startRow := rows[0]
	for i := 1; i < len(rows); i++ {
		// If current row is not contiguous with previous, yield the previous range
		if rows[i] != rows[i-1]+1 {
			if !yield(startRow, rows[i-1]+1) {
				return
			}
			startRow = rows[i]
		}
	}
	// Yield the final contiguous range
	yield(startRow, rows[len(rows)-1]+1)
}
