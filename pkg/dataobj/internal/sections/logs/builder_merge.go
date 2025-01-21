package logs

import (
	"cmp"
	"container/heap"
	"context"
	"maps"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// mergeStripes performs a heap sort on the given stripes and returns a new,
// single stripe.
func mergeStripes(opts Options, buf *stripeBuffer, stripes []*stripe) (*stripe, error) {
	// Ensure the buffer doesn't have any data in it before we append.
	buf.Reset()

	var (
		streamIDsBuilder  = buf.StreamIDs(opts)
		timestampsBuilder = buf.Timestamps(opts)
		messagesBuilder   = buf.Messages(opts, datasetmd.COMPRESSION_TYPE_ZSTD)
	)

	var (
		// stripeColumns holds the set of columns per stripe. The list of N columns
		// *must* match the following order:
		//
		//   1:        Stream ID
		//   2:        Timestamp
		//   3 to N-1: Metadata columns
		//   N:        Message
		stripeColumns = make([][]*dataset.MemColumn, 0, len(stripes))
		rowIters      = make([]rowIter, 0, len(stripes)) // Iterator per stripe.
	)
	for _, stripe := range stripes {
		columns := stripe.Columns()

		dset := dataset.FromMemory(columns)
		dsetColumns, err := result.Collect(dset.ListColumns(context.Background()))
		if err != nil {
			return nil, err
		}

		seq := dataset.Iter(context.Background(), dsetColumns)
		next, stop := result.Pull(seq)

		stripeColumns = append(stripeColumns, columns)
		rowIters = append(rowIters, rowIter{Next: next, Stop: stop})
	}

	var h rowHeap
	heap.Init(&h)

	// Prepopulate the heap with the first element of each iter.
	for i, iter := range rowIters {
		result, ok := iter.Next()
		if !ok {
			continue
		}

		row, err := result.Value()
		if err != nil {
			return nil, err
		}
		heap.Push(&h, &rowElement{Row: &row, Iter: i})
	}

	// Perform heap-sort.
	var rows int

	usedMetadata := make(map[string]struct{})

	for h.Len() > 0 {
		// Pop the smallest element from the heap.
		elem := heap.Pop(&h).(*rowElement)

		// Append records into our new columns.
		columns := stripeColumns[elem.Iter]

		_ = streamIDsBuilder.Append(rows, elem.Row.Values[0])
		_ = timestampsBuilder.Append(rows, elem.Row.Values[1])

		for i, value := range elem.Row.Values[2 : len(elem.Row.Values)-1] {
			columnIndex := i + 2 // Skip stream ID and timestamp.
			key := columns[columnIndex].Info.Name

			columnBuilder := buf.Metadatas(key, opts, datasetmd.COMPRESSION_TYPE_ZSTD)
			_ = columnBuilder.Append(rows, value)

			usedMetadata[key] = struct{}{}
		}

		_ = messagesBuilder.Append(rows, elem.Row.Values[len(elem.Row.Values)-1])
		rows++

		// Pull the next element from the iterator we just popped from.
		if result, ok := rowIters[elem.Iter].Next(); ok {
			row, err := result.Value()
			if err != nil {
				return nil, err
			}
			heap.Push(&h, &rowElement{Row: &row, Iter: elem.Iter})
		}
	}

	// Remove unused metadata columns.
	buf.CleanupMetadatas(maps.Keys(usedMetadata))

	// Flush never returns an error so we ignore it here to keep the code simple.
	//
	// TODO(rfratto): remove error return from Flush to clean up code.
	streamIDs, _ := streamIDsBuilder.Flush()
	timestamps, _ := timestampsBuilder.Flush()
	messages, _ := messagesBuilder.Flush()

	var metadatas []*dataset.MemColumn

	for _, metadataBuilder := range buf.AllMetadatas() {
		// Each metadata column may have a different number of rows compared to
		// other columns. Since adding NULLs isn't free, we don't call Backfill
		// here.

		metadata, _ := metadataBuilder.Flush()
		metadatas = append(metadatas, metadata)
	}

	// Sort metadata columns by name.
	slices.SortFunc(metadatas, func(a, b *dataset.MemColumn) int {
		return cmp.Compare(a.ColumnInfo().Name, b.ColumnInfo().Name)
	})

	return &stripe{
		streamIDs:  streamIDs,
		timestamps: timestamps,
		metadatas:  metadatas,
		messages:   messages,
	}, nil
}

type rowIter struct {
	Next func() (result.Result[dataset.Row], bool)
	Stop func()
}

type rowElement struct {
	Row  *dataset.Row
	Iter int
}

type rowHeap []*rowElement

func (h rowHeap) Len() int {
	return len(h)
}

func (h rowHeap) Less(i, j int) bool {
	a, b := h[i], h[j]

	// The first two columns of each row are *always* stream ID and timestamp.
	//
	// TODO(rfratto): Can we find a safer way of doing this?
	var (
		aStreamID = a.Row.Values[0].Int64()
		bStreamID = b.Row.Values[0].Int64()

		aTimestamp = a.Row.Values[1].Int64()
		bTimestamp = b.Row.Values[1].Int64()
	)

	if aStreamID < bStreamID {
		return true
	}
	return aStreamID == bStreamID && aTimestamp < bTimestamp
}

func (h rowHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *rowHeap) Push(x interface{}) {
	*h = append(*h, x.(*rowElement))
}

func (h *rowHeap) Pop() interface{} {
	n := len(*h)
	x := (*h)[n-1]
	*h = (*h)[:n-1]
	return x
}
