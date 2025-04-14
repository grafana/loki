package executor

import (
	"container/heap"
	"errors"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

func MinHeap() heap.Interface {
	return &rowHeap{
		less: func(i, j row) bool {
			return i.value < j.value
		},
	}
}
func MaxHeap() heap.Interface {
	return &rowHeap{
		less: func(i, j row) bool {
			return i.value > j.value
		},
	}
}

type row struct {
	value   uint64
	rowIdx  int
	iterIdx int
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

type HeapSortMerge struct {
	inputs      []Pipeline
	state       State
	heap        heap.Interface
	initialized bool
	batches     []arrow.Record
	active      []bool
	rows        []int64
	batchSize   int64
	columnEval  physical.EvaluateFunc
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
			} else {
				return err
			}
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

	buffer := make([]arrow.Record, 0, p.batchSize)
	if err := p.collectBatch(&buffer); err != nil {
		return err
	}
	p.state = state(mergeRecords(buffer))

	// Release intermediate slices
	for i := range buffer {
		buffer[i].Release()
	}

	return p.state.err
}

func (p *HeapSortMerge) collectBatch(buffer *[]arrow.Record) error {
	for len(*buffer) < cap(*buffer) && p.heap.Len() > 0 {
		// Fill heap from active iterators
		for i := range p.inputs {
			for p.active[i] && p.rows[i] <= 0 {
				err := p.inputs[i].Read()
				if err == EOF {
					p.active[i] = false
					break
				}
				batch, err := p.inputs[i].Value()
				if err != nil {
					p.state = state(batch, err)
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

		row := heap.Pop(p.heap).(row)
		i := row.iterIdx
		r := row.rowIdx

		p.rows[i]--

		rec := p.batches[i].NewSlice(int64(r), int64(r)+1)
		*buffer = append(*buffer, rec)
	}
	return nil
}
