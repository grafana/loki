package logs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

// mergeTablesIncremental incrementally merges the provides sorted tables into
// a single table. Incremental merging limits memory overhead as only mergeSize
// tables are open at a time.
//
// mergeTablesIncremental panics if maxMergeSize is less than 2.
func mergeTablesIncremental(buf *tableBuffer, pageSize, pageRowCount int, compressionOpts dataset.CompressionOptions, tables []*table, maxMergeSize int) (*table, error) {
	if maxMergeSize < 2 {
		panic("mergeTablesIncremental: merge size must be at least 2, got " + fmt.Sprint(maxMergeSize))
	}

	// Even if there's only one table, we still pass to mergeTables to ensure
	// it's compressed with compressionOpts.
	if len(tables) == 1 {
		return mergeTables(buf, pageSize, pageRowCount, compressionOpts, tables)
	}

	in := tables

	for len(in) > 1 {
		var out []*table

		for i := 0; i < len(in); i += maxMergeSize {
			set := in[i:min(i+maxMergeSize, len(in))]
			merged, err := mergeTables(buf, pageSize, pageRowCount, compressionOpts, set)
			if err != nil {
				return nil, err
			}
			out = append(out, merged)
		}

		in = out
	}

	return in[0], nil
}

// mergeTables merges the provided sorted tables into a new single sorted table
// using k-way merge.
func mergeTables(buf *tableBuffer, pageSize, pageRowCount int, compressionOpts dataset.CompressionOptions, tables []*table) (*table, error) {
	buf.Reset()

	var (
		streamIDBuilder  = buf.StreamID(pageSize, pageRowCount)
		timestampBuilder = buf.Timestamp(pageSize, pageRowCount)
		messageBuilder   = buf.Message(pageSize, pageRowCount, compressionOpts)
	)

	var (
		tableSequences = make([]*tableSequence, 0, len(tables))
	)
	for _, t := range tables {
		dsetColumns, err := result.Collect(t.ListColumns(context.Background()))
		if err != nil {
			return nil, err
		}

		r := dataset.NewReader(dataset.ReaderOptions{
			Dataset: t,
			Columns: dsetColumns,

			// The table is in memory, so don't prefetch.
			Prefetch: false,
		})

		tableSequences = append(tableSequences, &tableSequence{
			columns:         dsetColumns,
			DatasetSequence: NewDatasetSequence(r, 128),
		})
	}

	maxValue := result.Value(dataset.Row{
		Index: math.MaxInt,
		Values: []dataset.Value{
			dataset.Int64Value(math.MaxInt64), // StreamID
			dataset.Int64Value(math.MinInt64), // Timestamp
		},
	})

	var rows int

	tree := loser.New(tableSequences, maxValue, tableSequenceAt, rowResultLess, tableSequenceClose)
	defer tree.Close()

	for tree.Next() {
		seq := tree.Winner()

		row, err := tableSequenceAt(seq).Value()
		if err != nil {
			return nil, err
		}

		for i, column := range seq.columns {
			// column is guaranteed to be a *tableColumn since we got it from *table.
			column := column.(*tableColumn)

			// dataset.Iter returns values in the same order as the number of
			// columns.
			value := row.Values[i]

			switch column.Type {
			case ColumnTypeStreamID:
				_ = streamIDBuilder.Append(rows, value)
			case ColumnTypeTimestamp:
				_ = timestampBuilder.Append(rows, value)
			case ColumnTypeMetadata:
				columnBuilder := buf.Metadata(column.Desc.Tag, pageSize, pageRowCount, compressionOpts)
				_ = columnBuilder.Append(rows, value)
			case ColumnTypeMessage:
				_ = messageBuilder.Append(rows, value)
			default:
				return nil, fmt.Errorf("unknown column type %s", column.Type)
			}
		}

		rows++
	}

	return buf.Flush()
}

type tableSequence struct {
	DatasetSequence
	columns []dataset.Column
}

var _ loser.Sequence = (*tableSequence)(nil)

func tableSequenceAt(seq *tableSequence) result.Result[dataset.Row] { return seq.At() }
func tableSequenceClose(seq *tableSequence)                         { seq.Close() }

func NewDatasetSequence(r *dataset.Reader, bufferSize int) DatasetSequence {
	return DatasetSequence{
		r:   r,
		buf: make([]dataset.Row, bufferSize),
	}
}

type DatasetSequence struct {
	curValue result.Result[dataset.Row]

	r *dataset.Reader

	buf  []dataset.Row
	off  int // Offset into buf
	size int // Number of valid values in buf
}

func (seq *DatasetSequence) Next() bool {
	if seq.off < seq.size {
		seq.curValue = result.Value(seq.buf[seq.off])
		seq.off++
		return true
	}

ReadBatch:
	n, err := seq.r.Read(context.Background(), seq.buf)
	if err != nil && !errors.Is(err, io.EOF) {
		seq.curValue = result.Error[dataset.Row](err)
		return true
	} else if n == 0 && errors.Is(err, io.EOF) {
		return false
	} else if n == 0 {
		// Re-read if we got an empty batch without hitting EOF.
		goto ReadBatch
	}

	seq.curValue = result.Value(seq.buf[0])

	seq.off = 1
	seq.size = n
	return true
}

func (seq *DatasetSequence) At() result.Result[dataset.Row] {
	return seq.curValue
}

func (seq *DatasetSequence) Close() {
	_ = seq.r.Close()
}

func rowResultLess(a, b result.Result[dataset.Row]) bool {
	return result.Compare(a, b, CompareRows) < 0
}

// CompareRows compares two rows by their first two columns. CompareRows panics
// if a or b doesn't have at least two columns, if the first column isn't a
// int64-encoded stream ID, or if the second column isn't an int64-encoded
// timestamp.
func CompareRows(a, b dataset.Row) int {
	// The first two columns of each row are *always* stream ID and timestamp.
	//
	// TODO(rfratto): Can we find a safer way of doing this?
	var (
		aStreamID = a.Values[0].Int64()
		bStreamID = b.Values[0].Int64()

		aTimestamp = a.Values[1].Int64()
		bTimestamp = b.Values[1].Int64()
	)

	if res := cmp.Compare(bTimestamp, aTimestamp); res != 0 {
		return res
	}
	return cmp.Compare(aStreamID, bStreamID)
}
