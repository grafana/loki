package logs

import (
	"cmp"
	"runtime"
	"slices"

	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// buildTable builds a table from the set of provided records. The records are
// sorted with [sortRecords] prior to building the table.
func buildTable(buf *tableBuffer, pageSize, pageRowCount int, compressionOpts *dataset.CompressionOptions, records []Record, sortOrder SortOrder, dropDuplicates bool) *table {
	sortRecords(records, sortOrder)

	buf.Reset()

	var (
		streamIDBuilder  = buf.StreamID(pageSize, pageRowCount)
		timestampBuilder = buf.Timestamp(pageSize, pageRowCount)
		messageBuilder   = buf.Message(pageSize, pageRowCount, compressionOpts)
	)

	if dropDuplicates {
		records = withoutDuplicates(records)
	}

	appender := newColumnAppender()
	appender.Append(streamIDBuilder, len(records), func(i int) (int, dataset.Value) {
		return i, dataset.Int64Value(records[i].StreamID)
	})
	appender.Append(timestampBuilder, len(records), func(i int) (int, dataset.Value) {
		return i, dataset.Int64Value(records[i].Timestamp.UnixNano())
	})
	appender.Append(messageBuilder, len(records), func(i int) (int, dataset.Value) {
		return i, dataset.BinaryValue(records[i].Line)
	})

	// The split runs while the columns above build.
	for builder, cells := range splitMetadata(buf, records, pageSize, pageRowCount, compressionOpts) {
		appender.Append(builder, len(cells), func(i int) (int, dataset.Value) {
			// Passing around the value as an unsafe slice is safe here: appending
			// values is always read-only and the byte slice will never be mutated.
			return cells[i].row, dataset.BinaryValue(unsafeSlice(cells[i].value, 0))
		})
	}
	appender.Wait()

	table, err := buf.Flush()
	if err != nil {
		// Unreachable; we always ensure every required column is created.
		panic(err)
	}
	return table
}

// withoutDuplicates removes duplicate records in place and returns the
// shortened slice. Every sort order keeps the records of one stream and
// timestamp together, as DuplicateCounter requires.
//
// It reuses the backing array of records instead of copying it. The records
// after the returned length are stale. buildTable already sorts records in
// place, and its caller clears them after the build.
func withoutDuplicates(records []Record) []Record {
	var duplicates DuplicateCounter
	kept := records[:0]
	for _, record := range records {
		if !duplicates.IsDuplicate(record) {
			kept = append(kept, record)
		}
	}
	return kept
}

// columnAppender appends values to column builders, one goroutine per column.
// Page compression dominates the cost of each column, so columns build in
// parallel up to GOMAXPROCS at a time.
type columnAppender struct {
	g errgroup.Group
}

func newColumnAppender() *columnAppender {
	a := &columnAppender{}
	a.g.SetLimit(runtime.GOMAXPROCS(0))
	return a
}

// Append starts a goroutine that appends n cells to builder. cell returns the
// row number and value of cell i. Rows must increase with i. The caller must
// not use builder until Wait returns.
func (a *columnAppender) Append(builder *dataset.ColumnBuilder, n int, cell func(i int) (row int, value dataset.Value)) {
	a.g.Go(func() error {
		for i := range n {
			// Append only fails for out-of-order rows, which the contract on
			// cell rules out.
			_ = builder.Append(cell(i))
		}
		return nil
	})
}

// Wait blocks until every column started with Append is complete.
func (a *columnAppender) Wait() {
	_ = a.g.Wait()
}

// metadataCell is one metadata value and the row it belongs to.
type metadataCell struct {
	row   int
	value string
}

// splitMetadata groups the metadata values of records by column, in row
// order. It creates every metadata column in buf, so it must run on one
// goroutine: tableBuffer is not safe for concurrent use.
func splitMetadata(buf *tableBuffer, records []Record, pageSize, pageRowCount int, compressionOpts *dataset.CompressionOptions) map[*dataset.ColumnBuilder][]metadataCell {
	columns := make(map[*dataset.ColumnBuilder][]metadataCell)
	for row, record := range records {
		record.Metadata.Range(func(md labels.Label) {
			builder := buf.Metadata(md.Name, pageSize, pageRowCount, compressionOpts)
			columns[builder] = append(columns[builder], metadataCell{row: row, value: md.Value})
		})
	}
	return columns
}

// sortRecords sorts the set of records according to the specified sort order.
func sortRecords(records []Record, sortOrder SortOrder) {
	slices.SortStableFunc(records, func(a, b Record) int {
		switch sortOrder {
		case SortStreamASC:
			// Sort by [streamID ASC, timestamp DESC]
			if res := cmp.Compare(a.StreamID, b.StreamID); res != 0 {
				return res
			}
			return reverseOrderIfEqual(b.Timestamp.Compare(a.Timestamp))
		case SortTimestampDESC:
			// Sort by [timestamp DESC, streamID ASC]
			if res := b.Timestamp.Compare(a.Timestamp); res != 0 {
				return res
			}
			return reverseOrderIfEqual(cmp.Compare(a.StreamID, b.StreamID))
		case SortSchemaASC:
			// Sort by [shard_bucket ASC, schema sort key ASC, stream hash ASC, streamID ASC, timestamp DESC].
			aSort := streams.SortKey{ShardBucket: a.ShardBucket, SchemaKey: a.SchemaKey, Hash: a.StreamHash}
			bSort := streams.SortKey{ShardBucket: b.ShardBucket, SchemaKey: b.SchemaKey, Hash: b.StreamHash}
			if res := aSort.Compare(bSort); res != 0 {
				return res
			}
			if res := cmp.Compare(a.StreamID, b.StreamID); res != 0 {
				return res
			}
			return reverseOrderIfEqual(b.Timestamp.Compare(a.Timestamp))
		default:
			panic("invalid sort order")
		}
	})
}

// reverseOrderIfEqual reverses the order of the elements if the result is otherwise equal.
func reverseOrderIfEqual(res int) int {
	if res != 0 {
		return res
	}
	return -1
}
