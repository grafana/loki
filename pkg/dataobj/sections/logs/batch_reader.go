package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/util/labelpool"
)

// A Batch holds records read from a logs section as arrays, one array per
// column.
//
// The arrays use memory that the caller of [BatchReader.Read] owns. They are
// valid until the caller reclaims it.
type Batch struct {
	// Rows is the number of records in the batch. Every array has Rows elements.
	Rows int

	// StreamID, Timestamp and Message hold the columns of the same name, or are nil if the
	// reader did not project the column or the section has none. Timestamp holds nanoseconds
	// since the Unix epoch.
	StreamID  *columnar.Number[int64]
	Timestamp *columnar.Number[int64]
	Message   *columnar.UTF8

	// Metadata holds the projected metadata columns, sorted by name. A null element means the
	// record has no value for the key.
	Metadata []MetadataColumn
}

// A MetadataColumn is the values that one metadata key has in a [Batch].
type MetadataColumn struct {
	Name   string
	Values *columnar.UTF8
}

// MetadataLabels returns the metadata of record i as labels. A null element
// is left out and an empty element is kept, as [DecodeRow] does.
func (b *Batch) MetadataLabels(i int) labels.Labels {
	if len(b.Metadata) == 0 {
		return labels.EmptyLabels()
	}

	labelBuilder := labelpool.Get()
	defer labelpool.Put(labelBuilder)

	// Sections store metadata columns sorted by name, so labels usually
	// arrive sorted. Track the order to skip the sort in that case.
	var (
		labelsSorted = true
		lastLabel    string
	)
	for _, column := range b.Metadata {
		if column.Values.IsNull(i) {
			continue
		}

		if column.Name < lastLabel {
			labelsSorted = false
		}
		lastLabel = column.Name
		labelBuilder.Add(column.Name, metadataValue(column.Values.Get(i)))
	}

	if !labelsSorted {
		labelBuilder.Sort()
	}
	return labelBuilder.Labels()
}

// Record decodes record i into record, reusing its buffers. It resets record
// first, so a field that the batch does not hold comes back as its zero value.
func (b *Batch) Record(i int, record *Record) {
	record.Reset()

	if b.StreamID != nil && !b.StreamID.IsNull(i) {
		record.StreamID = b.StreamID.Get(i)
	}
	if b.Timestamp != nil && !b.Timestamp.IsNull(i) {
		record.Timestamp = time.Unix(0, b.Timestamp.Get(i)).UTC()
	}
	if b.Message != nil && !b.Message.IsNull(i) {
		record.Line = slicegrow.Copy(record.Line, b.Message.Get(i))
	}
	record.Metadata = b.MetadataLabels(i)
}

// A BatchReader reads the records of a [Section] as [Batch]es.
//
// It reads the same rows as a [RowReader] with the same settings. It decodes
// columns into arrays and applies predicates to whole columns, where
// [RowReader] builds one value for each cell of each row.
type BatchReader struct {
	sec   *Section
	ready bool

	readConfig

	reader *dataset.BatchReader

	// projSectionColumns are the projected columns, in the same order as the arrays of the
	// reader.
	projSectionColumns []*Column
}

// NewBatchReader creates a new BatchReader that reads from the provided [Section].
//
// Call [BatchReader.Open] before calling [BatchReader.Read].
func NewBatchReader(sec *Section) *BatchReader {
	var r BatchReader
	r.Reset(sec)
	return &r
}

// Open initializes BatchReader resources.
//
// Open must be called before [BatchReader.Read]. Open is safe to call multiple
// times. Open is a no-op when the reader has no section.
func (r *BatchReader) Open(ctx context.Context) error {
	if r.sec == nil || r.ready {
		return nil
	}

	if err := r.init(ctx); err != nil {
		_ = r.Close()
		return fmt.Errorf("initializing batch reader: %w", err)
	}
	return nil
}

// MatchStreams provides a sequence of stream IDs for the reader to match.
// [BatchReader.Read] only returns logs for the provided stream IDs.
//
// MatchStreams may be called multiple times to match multiple sets of streams.
// It may only be called before reading begins or after a call to
// [BatchReader.Reset].
func (r *BatchReader) MatchStreams(ids iter.Seq[int64]) error {
	if r.ready {
		return fmt.Errorf("cannot change matched streams after reading has started")
	}

	r.matchStreams(ids)
	return nil
}

// SetPredicates sets the predicates to use for filtering logs. [BatchReader.Read]
// only returns logs for which the predicates pass.
//
// Predicates may only be set before reading begins or after a call to
// [BatchReader.Reset].
func (r *BatchReader) SetPredicates(p []RowPredicate) error {
	if r.ready {
		return fmt.Errorf("cannot change predicate after reading has started")
	}

	r.predicates = p
	return nil
}

// SetProjectedColumns restricts the [Batch] to the given column types plus the
// metadata columns named in metadataNames. A [ColumnTypeMetadata] entry in
// types selects all metadata columns instead. See [RowReader.SetProjectedColumns]
// for the details.
//
// SetProjectedColumns may only be called before reading begins or after a call
// to [BatchReader.Reset].
func (r *BatchReader) SetProjectedColumns(types []ColumnType, metadataNames []string) error {
	if r.ready {
		return fmt.Errorf("cannot change projected columns after reading has started")
	}

	return r.project(types, metadataNames)
}

func (r *BatchReader) init(ctx context.Context) error {
	readerOpts, projectedSectionColumns, err := r.readerOptions(r.sec)
	if err != nil {
		return err
	}

	if r.reader == nil {
		r.reader = dataset.NewBatchReader(readerOpts)
	} else {
		r.reader.Reset(readerOpts)
	}
	if err := r.reader.Open(ctx); err != nil {
		return fmt.Errorf("opening dataset reader: %w", err)
	}

	r.projSectionColumns = projectedSectionColumns
	r.ready = true
	return nil
}

// Read reads the next batch of up to batchSize records. It returns a batch of
// at least one record, or nil and [io.EOF] at the end of the section. The
// batch uses memory from alloc, so it is valid until the caller reclaims alloc.
func (r *BatchReader) Read(ctx context.Context, alloc *memory.Allocator, batchSize int) (*Batch, error) {
	if r.sec == nil {
		return nil, io.EOF
	}
	if !r.ready {
		return nil, errRowReaderNotOpen
	}

	rb, err := r.reader.Read(ctx, alloc, batchSize)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("reading batch: %w", err)
	} else if rb.NumRows() == 0 {
		return nil, io.EOF
	}

	return r.newBatch(rb)
}

// newBatch gives the arrays of rb their role in a [Batch].
func (r *BatchReader) newBatch(rb *columnar.RecordBatch) (*Batch, error) {
	batch := &Batch{Rows: int(rb.NumRows())}

	for i, column := range r.projSectionColumns {
		arr := rb.Column(int64(i))

		switch column.Type {
		case ColumnTypeStreamID, ColumnTypeTimestamp:
			numbers, ok := arr.(*columnar.Number[int64])
			if !ok {
				return nil, fmt.Errorf("invalid array type %T for %s", arr, column.Type)
			}
			if column.Type == ColumnTypeStreamID {
				batch.StreamID = numbers
			} else {
				batch.Timestamp = numbers
			}

		case ColumnTypeMessage, ColumnTypeMetadata:
			values, ok := arr.(*columnar.UTF8)
			if !ok {
				return nil, fmt.Errorf("invalid array type %T for %s", arr, column.Type)
			}
			if column.Type == ColumnTypeMessage {
				batch.Message = values
			} else {
				batch.Metadata = append(batch.Metadata, MetadataColumn{Name: column.Name, Values: values})
			}
		}
	}
	return batch, nil
}

// Reset resets the BatchReader with a new Section to read from. Reset allows
// reusing a BatchReader without allocating a new one.
//
// Any set predicate, stream match and column projection are cleared when Reset
// is called. Reset may be called with a nil section to clear the BatchReader
// without needing a new one.
func (r *BatchReader) Reset(sec *Section) {
	r.sec = sec
	r.ready = false

	r.readConfig.reset()
	r.projSectionColumns = nil
}

// Close closes the BatchReader and releases any resources it holds. Closed
// BatchReaders can be reused by calling [BatchReader.Reset].
func (r *BatchReader) Close() error {
	r.ready = false
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}
