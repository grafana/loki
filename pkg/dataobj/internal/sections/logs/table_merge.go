package logs

import (
	"cmp"
	"context"
	"fmt"
	"math"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

// mergeTables merges the provided sorted tables into a new single sorted table
// using k-way merge.
func mergeTables(buf *tableBuffer, pageSize int, compressionOpts dataset.CompressionOptions, tables []*table) (*table, error) {
	buf.Reset()

	var (
		streamIDBuilder  = buf.StreamID(pageSize)
		timestampBuilder = buf.Timestamp(pageSize)
		messageBuilder   = buf.Message(pageSize, compressionOpts)
	)

	var (
		tableSequences = make([]*tableSequence, 0, len(tables))
	)
	for _, t := range tables {
		dsetColumns, err := result.Collect(t.ListColumns(context.Background()))
		if err != nil {
			return nil, err
		}

		seq := dataset.Iter(context.Background(), dsetColumns)
		next, stop := result.Pull(seq)
		defer stop()

		tableSequences = append(tableSequences, &tableSequence{
			columns: dsetColumns,

			pull: next, stop: stop,
		})
	}

	maxValue := result.Value(dataset.Row{
		Index: math.MaxInt,
		Values: []dataset.Value{
			dataset.Int64Value(math.MaxInt64),
			dataset.Int64Value(math.MaxInt64),
		},
	})

	var rows int

	tree := loser.New(tableSequences, maxValue, tableSequenceValue, rowResultLess, tableSequenceStop)
	for tree.Next() {
		seq := tree.Winner()

		row, err := tableSequenceValue(seq).Value()
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
			case logsmd.COLUMN_TYPE_STREAM_ID:
				_ = streamIDBuilder.Append(rows, value)
			case logsmd.COLUMN_TYPE_TIMESTAMP:
				_ = timestampBuilder.Append(rows, value)
			case logsmd.COLUMN_TYPE_METADATA:
				columnBuilder := buf.Metadata(column.Info.Name, pageSize, compressionOpts)
				_ = columnBuilder.Append(rows, value)
			case logsmd.COLUMN_TYPE_MESSAGE:
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
	curValue result.Result[dataset.Row]

	columns []dataset.Column

	pull func() (result.Result[dataset.Row], bool)
	stop func()
}

var _ loser.Sequence = (*tableSequence)(nil)

func (seq *tableSequence) Next() bool {
	val, ok := seq.pull()
	seq.curValue = val
	return ok
}

func tableSequenceValue(seq *tableSequence) result.Result[dataset.Row] { return seq.curValue }

func tableSequenceStop(seq *tableSequence) { seq.stop() }

func rowResultLess(a, b result.Result[dataset.Row]) bool {
	var (
		aRow, aErr = a.Value()
		bRow, bErr = b.Value()
	)

	// Put errors first so we return errors early.
	if aErr != nil {
		return true
	} else if bErr != nil {
		return false
	}

	return compareRows(aRow, bRow) < 0
}

// compareRows compares two rows by their first two columns. compareRows panics
// if a or b doesn't have at least two columns, if the first column isn't a
// int64-encoded stream ID, or if the second column isn't an int64-encoded
// timestamp.
func compareRows(a, b dataset.Row) int {
	// The first two columns of each row are *always* stream ID and timestamp.
	//
	// TODO(rfratto): Can we find a safer way of doing this?
	var (
		aStreamID = a.Values[0].Int64()
		bStreamID = b.Values[0].Int64()

		aTimestamp = a.Values[1].Int64()
		bTimestamp = b.Values[1].Int64()
	)

	if res := cmp.Compare(aStreamID, bStreamID); res != 0 {
		return res
	}
	return cmp.Compare(aTimestamp, bTimestamp)
}
