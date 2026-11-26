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
	keys, cells := splitMetadata(records)
	for i, key := range keys {
		builder := buf.Metadata(key, pageSize, pageRowCount, compressionOpts)
		column := cells[i]
		appender.Append(builder, len(column), func(j int) (int, dataset.Value) {
			// Passing around the value as an unsafe slice is safe here: appending
			// values is always read-only and the byte slice will never be mutated.
			return column[j].row, dataset.BinaryValue(unsafeSlice(column[j].value, 0))
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
// timestamp together, as DuplicateTracker requires.
//
// It reuses the backing array of records instead of copying it. The records
// after the returned length are stale. buildTable already sorts records in
// place, and its caller clears them after the build.
func withoutDuplicates(records []Record) []Record {
	var duplicates DuplicateTracker
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
			// cell rules out. A failure means a bug in the caller.
			if err := builder.Append(cell(i)); err != nil {
				panic(err)
			}
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

// splitMetadata groups the metadata values of records by key. It returns the
// keys in the order they first appear, and for each key its cells in row
// order. If a record repeats a key, only its last value is kept.
//
// The cost is linear in the number of values, so many sparse keys stay cheap.
// A first pass counts the values of each key, so each cell slice is
// allocated once at its final size.
func splitMetadata(records []Record) ([]string, [][]metadataCell) {
	var (
		index  = make(map[string]int)
		keys   []string
		counts []int
	)
	for _, record := range records {
		record.Metadata.Range(func(md labels.Label) {
			i, ok := index[md.Name]
			if !ok {
				i = len(keys)
				index[md.Name] = i
				keys = append(keys, md.Name)
				counts = append(counts, 0)
			}
			counts[i]++
		})
	}

	cells := make([][]metadataCell, len(keys))
	for i, n := range counts {
		cells[i] = make([]metadataCell, 0, n)
	}
	for row, record := range records {
		record.Metadata.Range(func(md labels.Label) {
			i := index[md.Name]
			// A record can repeat a key. A column holds one value per row,
			// so the last value wins, as it does in LogQL.
			if n := len(cells[i]); n > 0 && cells[i][n-1].row == row {
				cells[i][n-1].value = md.Value
				return
			}
			cells[i] = append(cells[i], metadataCell{row: row, value: md.Value})
		})
	}
	return keys, cells
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
