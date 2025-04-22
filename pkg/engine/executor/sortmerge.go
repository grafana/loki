package executor

import (
	"container/heap"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// MinHeap is a min-heap implementation for rows.
func MinHeap() heap.Interface {
	return &rowHeap{
		less: func(i, j row) bool {
			return i.value < j.value
		},
	}
}

// MaxHeap is a max-heap implementation for rows.
func MaxHeap() heap.Interface {
	return &rowHeap{
		less: func(i, j row) bool {
			return i.value > j.value
		},
	}
}

type rowHeap struct {
	less func(i, j row) bool
	data []row
}

func (h rowHeap) Len() int {
	return len(h.data)
}

func (h rowHeap) Less(i, j int) bool {
	return h.less(h.data[i], h.data[j])
}

func (h rowHeap) Swap(i, j int) {
	h.data[i], h.data[j] = h.data[j], h.data[i]
}

func (h *rowHeap) Push(x any) {
	h.data = append(h.data, x.(row))
}

func (h *rowHeap) Pop() any {
	old := h.data
	n := len(old)
	x := old[n-1]
	h.data = old[0 : n-1]
	return x
}

type row struct {
	value   uint64
	rowIdx  int
	iterIdx int
}

// HeapSortMerge is a k-way merge of multiple inputs using a heap to calculate the smallest/largest element of the current batches of the inputs.
// This means it requires the input batches to be sorted in the same order (ASC/DESC) as the SortMerge operator.
// The sort order is defined by the direction of the query, which is either FORWARD or BACKWARDS,
// which is then applies to the SortMerge as well as to the DataObjScan during query planning.
type HeapSortMerge struct {
	inputs      []Pipeline
	state       state
	heap        heap.Interface
	initialized bool
	batches     []arrow.Record
	active      []bool
	rows        []int64
	batchSize   int64
	columnEval  evalFunc
}

var _ Pipeline = (*HeapSortMerge)(nil)

// Close implements Pipeline.
func (p *HeapSortMerge) Close() {
	// Release last buffer
	if p.state.batch != nil {
		p.state.batch.Release()
	}
	for _, input := range p.inputs {
		input.Close()
	}
}

// Inputs implements Pipeline.
func (p *HeapSortMerge) Inputs() []Pipeline {
	return p.inputs
}

// Read implements Pipeline.
func (p *HeapSortMerge) Read() error {
	if err := p.init(); err != nil {
		return err
	}
	return p.read()
}

// Transport implements Pipeline.
func (p *HeapSortMerge) Transport() Transport {
	return Local
}

// Value implements Pipeline.
func (p *HeapSortMerge) Value() (arrow.Record, error) {
	return p.state.Value()
}

func (p *HeapSortMerge) init() error {
	if p.initialized {
		return nil
	}

	p.initialized = true

	n := len(p.inputs)
	p.batches = make([]arrow.Record, n)
	p.active = make([]bool, n)
	p.rows = make([]int64, n)

	if p.heap == nil {
		p.heap = MinHeap()
		heap.Init(p.heap)
	}

	for i, input := range p.inputs {
		// Pull first batch and load it into the heap
		err := input.Read()
		if err != nil {
			if err == EOF {
				continue
			}
			return err
		}
		p.active[i] = true

		batch, _ := input.Value()
		p.batches[i] = batch
		p.rows[i] = batch.NumRows()
		col, err := p.columnEval(batch)
		if err != nil {
			return err
		}
		tsCol, ok := col.ToArray().(*array.Uint64)
		if !ok {
			return errors.New("column is not a timestamp column")
		}

		for j := 0; int64(j) < batch.NumRows(); j++ {
			row := row{
				value:   tsCol.Value(j),
				rowIdx:  j,
				iterIdx: i,
			}
			heap.Push(p.heap, row)
		}
	}

	return nil
}

func (p *HeapSortMerge) read() error {
	// Release previous buffer
	if p.state.batch != nil {
		p.state.batch.Release()
	}

	if p.heap.Len() <= 0 {
		p.state = Exhausted
		return p.state.err
	}

	// NOTE(chaudum): Should a coalesce operator combine the rows and the merge operator yield single row batches?
	// TODO(chaudum): Reuse records buffer
	records := make([]arrow.Record, p.batchSize)
	if err := p.readRecords(&records); err != nil {
		return err
	}
	p.state = newState(concatRecords(records))

	// Release intermediate slices
	for i := range records {
		records[i].Release()
	}

	return p.state.err
}

func (p *HeapSortMerge) readRecords(buffer *[]arrow.Record) error {
	*buffer = (*buffer)[:0]
	for len(*buffer) < cap(*buffer) && p.heap.Len() > 0 {
		// Fill heap from active iterators
		if err := p.fillHeap(); err != nil {
			return err
		}

		row := heap.Pop(p.heap).(row)
		i := row.iterIdx
		r := row.rowIdx

		p.rows[i]--

		rec := p.batches[i].NewSlice(int64(r), int64(r)+1)
		*buffer = append(*buffer, rec)
	}
	return nil
}

func (p *HeapSortMerge) fillHeap() error {
	for i := range p.inputs {
		for p.active[i] && p.rows[i] <= 0 {
			err := p.inputs[i].Read()
			if err == EOF {
				p.active[i] = false
				break
			}
			batch, err := p.inputs[i].Value()
			if err != nil {
				p.state = newState(batch, err)
				return err
			}

			p.batches[i] = batch
			p.rows[i] = batch.NumRows()
			col, err := p.columnEval(batch)
			if err != nil {
				return err
			}
			tsCol, ok := col.ToArray().(*array.Uint64)
			if !ok {
				return errors.New("column is not a timestamp column")
			}

			for j := 0; int64(j) < batch.NumRows(); j++ {
				row := row{
					value:   tsCol.Value(j),
					rowIdx:  j,
					iterIdx: i,
				}
				heap.Push(p.heap, row)
			}
		}
	}
	return nil
}

// TODO(chaudum): This function is subject to performance improvements!
func concatRecords(records []arrow.Record) (arrow.Record, error) {
	if len(records) == 0 {
		return nil, nil
	}
	if len(records) == 1 {
		return records[0], nil
	}

	// Create a map to track all fields across all schemas
	allFields := make(map[string]arrow.Field)

	// First pass: collect all unique fields from all schemas
	for _, rec := range records {
		schema := rec.Schema()
		for i := 0; i < schema.NumFields(); i++ {
			field := schema.Field(i)
			// If we already have this field, ensure types are compatible
			if existingField, ok := allFields[field.Name]; ok {
				if !arrow.TypeEqual(existingField.Type, field.Type) {
					return nil, fmt.Errorf("field '%s' has conflicting types: %s vs %s",
						field.Name, existingField.Type, field.Type)
				}
			} else {
				allFields[field.Name] = field
			}
		}
	}

	// Convert map to slice and create unified schema
	fields := make([]arrow.Field, 0, len(allFields))
	for _, field := range allFields {
		fields = append(fields, field)
	}
	mergedSchema := arrow.NewSchema(fields, nil)

	// Create arrays for each field in the merged schema
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(fields))
	for i, field := range fields {
		builders[i] = array.NewBuilder(mem, field.Type)
		defer builders[i].Release()
	}

	rowCount := int64(0)

	// Second pass: populate the builders
	for _, rec := range records {
		rowCount += rec.NumRows()
		// For each row in the current record
		for rowIdx := int64(0); rowIdx < rec.NumRows(); rowIdx++ {
			// For each field in the merged schema
			for fieldIdx, field := range fields {
				builder := builders[fieldIdx]

				// Check if the current record has this field
				colIdx := rec.Schema().FieldIndices(field.Name)
				if len(colIdx) > 0 {
					// Get the column and append the value
					col := rec.Column(colIdx[0])
					appendToBuilder(builder, col, int(rowIdx))
				} else {
					// Field not in this record, append null
					builder.AppendNull()
				}
			}
		}
	}

	// Build the arrays
	columns := make([]arrow.Array, len(builders))
	for i, builder := range builders {
		columns[i] = builder.NewArray()
		defer columns[i].Release()
	}

	// Create the record batch
	var err error
	result := array.NewRecord(mergedSchema, columns, rowCount)
	for i, col := range columns {
		result, err = result.SetColumn(i, col)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

// Helper function to append a value from a source array to a builder
func appendToBuilder(builder array.Builder, sourceArray arrow.Array, index int) {
	if sourceArray.IsNull(index) {
		builder.AppendNull()
		return
	}

	switch b := builder.(type) {
	case *array.Int64Builder:
		b.Append(sourceArray.(*array.Int64).Value(index))
	case *array.Uint64Builder:
		b.Append(sourceArray.(*array.Uint64).Value(index))
	case *array.Float64Builder:
		b.Append(sourceArray.(*array.Float64).Value(index))
	case *array.StringBuilder:
		b.Append(sourceArray.(*array.String).Value(index))
	default:
		builder.AppendNull()
	}
}
