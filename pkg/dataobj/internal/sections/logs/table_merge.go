package logs

import (
	"cmp"
	"container/heap"
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// mergeTables merges the provided sorted tables into a new single sorted table
// using heap sort.
func mergeTables(buf *tableBuffer, pageSize int, compressionOpts dataset.CompressionOptions, tables []*table) (*table, error) {
	buf.Reset()

	var (
		streamIDBuilder  = buf.StreamID(pageSize)
		timestampBuilder = buf.Timestamp(pageSize)
		messageBuilder   = buf.Message(pageSize, compressionOpts)
	)

	var (
		// columnSets holds the set of columns per table.
		columnSets   = make([][]dataset.Column, 0, len(tables))
		tablePullers = make([]pullRow, 0, len(tables)) // Pull iterator per table.
	)
	for _, t := range tables {
		dsetColumns, err := result.Collect(t.ListColumns(context.Background()))
		if err != nil {
			return nil, err
		}

		seq := dataset.Iter(context.Background(), dsetColumns)
		next, stop := result.Pull(seq)
		defer stop()

		columnSets = append(columnSets, dsetColumns)
		tablePullers = append(tablePullers, next)
	}

	var h rowHeap
	heap.Init(&h)

	// Initialize our heap with the first row of each table.
	for tableIndex, pull := range tablePullers {
		result, ok := pull()
		if !ok {
			continue // Empty table.
		}

		row, err := result.Value()
		if err != nil {
			return nil, err
		}
		heap.Push(&h, &rowHeapElement{Row: row, TableIndex: tableIndex})
	}

	// Now, we can heap sort by popping the smallest row and appending it to our
	// output columns. Every time we pop an element, we'll pull the next element
	// from that table and push it onto the heap.
	//
	// We continue this process until the heap is empty, completing the sort.
	var rows int

	for h.Len() > 0 {
		elem := heap.Pop(&h).(*rowHeapElement)

		columns := columnSets[elem.TableIndex]

		for i, column := range columns {
			// column is guaranteed to be a *tableColumn since we got it from *table.
			column := column.(*tableColumn)

			// dataset.Iter returns values in the same order as the number of
			// columns.
			value := elem.Row.Values[i]

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

		// Pull the next row from the table.
		if result, ok := tablePullers[elem.TableIndex](); ok {
			row, err := result.Value()
			if err != nil {
				return nil, err
			}
			heap.Push(&h, &rowHeapElement{Row: row, TableIndex: elem.TableIndex})
		}
	}

	return buf.Flush()
}

type pullRow func() (result.Result[dataset.Row], bool)

type rowHeap []*rowHeapElement

type rowHeapElement struct {
	Row        dataset.Row
	TableIndex int
}

func (h rowHeap) Len() int {
	return len(h)
}

func (h rowHeap) Less(i, j int) bool {
	return compareRows(h[i].Row, h[j].Row) < 0
}

func (h rowHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *rowHeap) Push(x interface{}) {
	*h = append(*h, x.(*rowHeapElement))
}

func (h *rowHeap) Pop() interface{} {
	n := len(*h)
	x := (*h)[n-1]
	*h = (*h)[:n-1]
	return x
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
