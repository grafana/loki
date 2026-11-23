package logs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// A RecordBatch holds the records that [RowReader.ReadRecordBatch] read from a logs section, as
// one array per column.
//
// The arrays use memory from the allocator that the caller passed to ReadRecordBatch. They stay
// valid until the caller reclaims that allocator. The RecordBatch and its Metadata slice are Go
// values, so they do not use that memory.
type RecordBatch struct {
	// Rows is the number of records in the batch. It is at least one, and every array has Rows
	// elements.
	Rows int

	// StreamID, Timestamp and Message hold the columns of type [ColumnTypeStreamID],
	// [ColumnTypeTimestamp] and [ColumnTypeMessage]. A field is nil when the reader did not
	// project its column, or when the section has no such column.
	//
	// Timestamp holds nanoseconds since the Unix epoch. StreamID and Timestamp have no null
	// element. A null element of Message means that the record has no message. It reads as an
	// empty line, as in [DecodeRow].
	//
	// Message is a [columnar.UTF8Reader]. A caller reads a value with Get, and must not assume
	// that the bytes of the values are contiguous.
	StreamID  *columnar.Number[int64]
	Timestamp *columnar.Number[int64]
	Message   columnar.UTF8Reader

	// Metadata holds the projected metadata columns, sorted by name.
	Metadata []MetadataColumn
}

// A MetadataColumn holds the values that one structured metadata key has in a [RecordBatch].
// Values is never nil. A null element means that the record has no value for the key. An empty
// element means that the record has the key with an empty value. A caller reads a value with
// Get, and must not assume that the bytes of the values are contiguous.
type MetadataColumn struct {
	Name   string
	Values columnar.UTF8Reader
}

// MetadataLabels returns the structured metadata of record i as labels. It skips a null element
// and keeps an empty one, as [DecodeRow] does.
//
// MetadataLabels resets scratch and builds the labels in it. A caller that reads many records
// passes the same scratch to each call, so no call takes a builder from a pool. The caller must
// not use scratch in another goroutine during the call.
//
// The labels own their strings, so they stay valid after the caller reclaims the memory of the
// batch, and after the next call that uses scratch. The index i must be in the range [0, Rows).
//
// MetadataLabels does not check the order of the columns, because it runs once for each record.
// The columns of Metadata must be sorted by name, as ReadRecordBatch returns them.
func (b *RecordBatch) MetadataLabels(i int, scratch *labels.ScratchBuilder) labels.Labels {
	if len(b.Metadata) == 0 {
		return labels.EmptyLabels()
	}

	scratch.Reset()
	for _, column := range b.Metadata {
		if !column.Values.IsNull(i) {
			scratch.Add(column.Name, metadataValue(column.Values.Get(i)))
		}
	}
	return scratch.Labels()
}

// ReadRecordBatch reads the next records of the section that pass the predicates, at most
// batchSize of them. It returns a [RecordBatch] of at least one record, or nil and [io.EOF] at
// the end of the section.
//
// It returns the same records as [RowReader.Read], in the same order, and calls of the two methods
// can follow each other. It returns the values of a column as one array, where Read returns one
// [Record] for each row.
//
// ReadRecordBatch applies the predicates to one run of at most batchSize consecutive records. It
// skips a run in which no record passes. A batch can hold fewer than batchSize records, so a short
// batch does not mean that the section ended.
//
// ReadRecordBatch returns nil and the error on any other error. It fails when batchSize is not
// positive or alloc is nil.
//
// The arrays of the batch use memory from alloc, so they stay valid until the caller reclaims
// alloc. ReadRecordBatch can also leave temporary memory in alloc, and so can a failed call. The
// caller should reclaim alloc after it consumes each batch.
func (r *RowReader) ReadRecordBatch(ctx context.Context, alloc *memory.Allocator, batchSize int) (*RecordBatch, error) {
	if r.sec == nil {
		return nil, io.EOF
	}
	if !r.ready {
		return nil, errRowReaderNotOpen
	}

	rb, err := r.reader.ReadRecordBatch(ctx, alloc, batchSize)
	if errors.Is(err, io.EOF) {
		return nil, io.EOF
	} else if err != nil {
		return nil, fmt.Errorf("reading record batch: %w", err)
	}

	batch, err := r.newRecordBatch(rb)
	if err != nil {
		return nil, fmt.Errorf("decoding record batch: %w", err)
	}
	return batch, nil
}

// newRecordBatch builds a [RecordBatch] from the arrays of rb. It matches each array to its
// column by position, and returns an error when the arrays break the contract of [RecordBatch].
func (r *RowReader) newRecordBatch(rb *columnar.RecordBatch) (*RecordBatch, error) {
	rows := int(rb.NumRows())
	if rows == 0 {
		return nil, errors.New("record batch has no row")
	}
	if int(rb.NumCols()) != len(r.projSectionColumns) {
		return nil, fmt.Errorf("record batch has %d columns, want %d", rb.NumCols(), len(r.projSectionColumns))
	}

	batch := &RecordBatch{Rows: rows}
	if r.projMetadataColumns > 0 {
		batch.Metadata = make([]MetadataColumn, 0, r.projMetadataColumns)
	}
	for i, column := range r.projSectionColumns {
		arr := rb.Column(int64(i))
		if arr.Len() != rows {
			return nil, fmt.Errorf("array of %s has %d elements, want %d", column.Type, arr.Len(), rows)
		}

		switch column.Type {
		case ColumnTypeStreamID, ColumnTypeTimestamp:
			numbers, ok := arr.(*columnar.Number[int64])
			if !ok {
				return nil, fmt.Errorf("invalid array type %T for %s", arr, column.Type)
			}
			if numbers.Nulls() > 0 {
				return nil, fmt.Errorf("array of %s has %d null elements", column.Type, numbers.Nulls())
			}
			if column.Type == ColumnTypeStreamID {
				batch.StreamID = numbers
			} else {
				batch.Timestamp = numbers
			}

		case ColumnTypeMessage, ColumnTypeMetadata:
			values, ok := arr.(columnar.UTF8Reader)
			if !ok {
				return nil, fmt.Errorf("invalid array type %T for %s", arr, column.Type)
			}
			if column.Type == ColumnTypeMessage {
				batch.Message = values
			} else {
				batch.Metadata = append(batch.Metadata, MetadataColumn{Name: column.Name, Values: values})
			}

		default:
			return nil, fmt.Errorf("unsupported column type %s", column.Type)
		}
	}

	// A section stores its metadata columns sorted by name, so this check costs one pass over the
	// columns and no sort. It lets MetadataLabels skip the order check for each record.
	byName := func(a, b MetadataColumn) int { return cmp.Compare(a.Name, b.Name) }
	if !slices.IsSortedFunc(batch.Metadata, byName) {
		slices.SortStableFunc(batch.Metadata, byName)
	}
	return batch, nil
}
