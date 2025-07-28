package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// streamsView provides a view of the streams in a section, allowing for
// querying labels of a stream.
//
// streamsView lazily loads streams upon the first call.
type streamsView struct {
	sec           *streams.Section
	streamIDs     []int64
	idColumn      *streams.Column
	searchColumns []*streams.Column // stream ID + labels
	batchSize     int

	initialized  bool
	streams      arrow.Table
	idRowMapping map[int64]int // Mapping of stream ID to absolute row index in the streams table.
}

type streamsViewOptions struct {
	// StreamIDs holds the list of stream IDs to include in the view. If this
	// slice is empty, all streams in the section are included.
	StreamIDs []int64

	// LabelColumns holds the list of label columns (stream labels) to include in
	// the view.
	//
	// If this slice is empty, all label columns in the section are included.
	LabelColumns []*streams.Column

	// Maximum number of stream records to read at once. Defaults to 128.
	BatchSize int
}

// newStreamsView creates a new view of the given streams section. Only the
// specified ids will be included in the view.
func newStreamsView(sec *streams.Section, opts *streamsViewOptions) *streamsView {
	cols := opts.LabelColumns
	if len(cols) == 0 {
		cols = sec.Columns()
	}

	if opts.BatchSize <= 0 {
		opts.BatchSize = 128
	}

	var streamsIDColumn *streams.Column

	// We need to iterate through the original columns to find the stream ID
	// column, since it's never included in [streamsViewOptions].
	for _, col := range sec.Columns() {
		if col.Type == streams.ColumnTypeStreamID {
			streamsIDColumn = col
			break
		}
	}

	return &streamsView{
		sec:           sec,
		streamIDs:     opts.StreamIDs,
		idColumn:      streamsIDColumn,
		searchColumns: append([]*streams.Column{streamsIDColumn}, cols...),
		batchSize:     opts.BatchSize,
	}
}

// NumLabels returns the number of labels in the view.
func (v *streamsView) NumLabels() int {
	return len(v.searchColumns) - 1
}

// Labels iterates over all of the non-null labels of a stream with the given
// id. If [streamsViewOptions] included a subset of labels, only those labels
// are returned.
func (v *streamsView) Labels(ctx context.Context, id int64) (iter.Seq[labels.Label], error) {
	if err := v.init(ctx); err != nil {
		return nil, err
	}

	rowColumnIndex, ok := v.idRowMapping[id]
	if !ok {
		return nil, fmt.Errorf("stream ID %d not found in section or not included in filter", id)
	}

	return func(yield func(labels.Label) bool) {
		for colIndex := range int(v.streams.NumCols()) {
			// Skip any column which isn't a label column.
			//
			// This is safe because [streams.Reader] guarantees that the order of
			// columns in the Arrow schema matches the order of [streams.Column]s
			// provided to the reader.
			if v.searchColumns[colIndex].Type != streams.ColumnTypeLabel {
				continue
			}

			// Find the array where our row is. While columnChunkedRow can return nil
			// if the row isn't found, we know we're giving it a valid row index so
			// we can bypass the check here.
			arr, rowArrayIndex := columnChunkedRow(v.streams.Column(colIndex), rowColumnIndex)
			if arr.IsNull(rowArrayIndex) {
				continue
			}

			label := labels.Label{
				Name: v.searchColumns[colIndex].Name,
			}

			switch colValues := arr.(type) {
			case *array.String:
				label.Value = colValues.Value(rowArrayIndex)
			case *array.Binary:
				label.Value = string(colValues.Value(rowArrayIndex))
			default:
				panic(fmt.Sprintf("unexpected column type %T for labels", colValues))
			}

			if !yield(label) {
				return
			}
		}
	}, nil
}

func (v *streamsView) init(ctx context.Context) (err error) {
	if v.initialized {
		return nil
	}

	if v.idColumn == nil { // Initialized in [newStreamsView].
		// The streams builder always produces a section with a streams ID column.
		// If we hit this, someone probably made a custom section and provided it
		// to us.
		return fmt.Errorf("section does not contain a stream ID column")
	}

	readerOptions := streams.ReaderOptions{
		Columns:   v.searchColumns,
		Allocator: memory.DefaultAllocator,
	}

	var scalarIDs []scalar.Scalar
	for _, id := range v.streamIDs {
		scalarIDs = append(scalarIDs, scalar.NewInt64Scalar(id))
	}
	if len(scalarIDs) > 0 {
		readerOptions.Predicates = []streams.Predicate{
			streams.InPredicate{Column: v.idColumn, Values: scalarIDs},
		}
	}

	r := streams.NewReader(readerOptions)

	var records []arrow.Record
	defer func() {
		for _, rec := range records {
			rec.Release()
		}
	}()

	for {
		rec, err := r.Read(ctx, v.batchSize)
		if rec != nil && rec.NumRows() > 0 {
			records = append(records, rec)
		}

		if err != nil && !errors.Is(err, io.EOF) {
			return err
		} else if err != nil && errors.Is(err, io.EOF) {
			break
		}
	}

	table := array.NewTableFromRecords(r.Schema(), records)
	defer func() {
		if err != nil {
			table.Release()
		}
	}()

	idMapping := make(map[int64]int, table.NumRows())
	for colIndex := range int(table.NumCols()) {
		if v.searchColumns[colIndex].Type != streams.ColumnTypeStreamID {
			continue
		}

		var chunkStartRow int

		col := table.Column(colIndex)
		for _, chunk := range col.Data().Chunks() {
			idArray, ok := chunk.(*array.Int64)
			if !ok {
				return fmt.Errorf("expected streamds ID to be of type Int64, got %T", chunk)
			}

			for chunkRow := range idArray.Len() {
				id := idArray.Value(chunkRow)
				idMapping[id] = chunkStartRow + chunkRow
			}

			chunkStartRow += chunk.Len()
		}

		break // Processed the streams ID column, so we're done.
	}

	v.streams = table
	v.initialized = true
	v.idRowMapping = idMapping
	return nil
}

// columnChunkedRow finds a column-wide rows in a chunked array, returning the
// array that the row is in and the relative row index inside that array.
func columnChunkedRow(col *arrow.Column, absoluteRow int) (arr arrow.Array, relativeRow int) {
	var chunkStartRow int

	for _, chunk := range col.Data().Chunks() {
		// Subtract one to get the inclusive end row.
		//
		// e.g., if a chunk starts at row 0 and has a length of 1, then its end row
		// is 0.
		chunkEndRow := chunkStartRow + chunk.Len() - 1

		if chunkStartRow <= absoluteRow && absoluteRow <= chunkEndRow {
			relativeRow = absoluteRow - chunkStartRow
			return chunk, relativeRow
		}

		// The next chunk starts one after where the current chunk ends.
		chunkStartRow += chunk.Len()
	}

	return nil, -1 // not found
}

func (v *streamsView) Close() {
	if !v.initialized {
		return
	}

	v.initialized = false
	v.streams.Release()
	v.streams = nil
	clear(v.idRowMapping)
}
