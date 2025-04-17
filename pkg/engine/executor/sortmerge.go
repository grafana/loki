package executor

import (
	"errors"
	"fmt"
	"sort"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

// NewSortMergePipeline returns a new pipeline that merges already sorted inputs into a single output.
func NewSortMergePipeline(inputs []Pipeline, order physical.SortOrder, column physical.ColumnExpression, evaluator expressionEvaluator) (*KWayMerge, error) {
	var compare func(a, b uint64) bool
	switch order {
	case physical.ASC:
		compare = func(a, b uint64) bool { return a <= b }
	case physical.DESC:
		compare = func(a, b uint64) bool { return a >= b }
	default:
		return nil, fmt.Errorf("invalid sort order %v", order)
	}

	return &KWayMerge{
		inputs:     inputs,
		columnEval: evaluator.newFunc(column),
		compare:    compare,
	}, nil
}

// KWayMerge is a k-way merge of multiple sorted inputs.
// It requires the input batches to be sorted in the same order (ASC/DESC) as the SortMerge operator itself.
// The sort order is defined by the direction of the query, which is either FORWARD or BACKWARDS,
// which is applied to the SortMerge as well as to the DataObjScan during query planning.
type KWayMerge struct {
	inputs      []Pipeline
	state       state
	initialized bool
	batches     []arrow.Record
	exhausted   []bool
	offsets     []int64
	columnEval  evalFunc
	compare     func(a, b uint64) bool
}

var _ Pipeline = (*KWayMerge)(nil)

// Close implements Pipeline.
func (p *KWayMerge) Close() {
	// Release last batch
	if p.state.batch != nil {
		p.state.batch.Release()
	}
	for _, input := range p.inputs {
		input.Close()
	}
}

// Inputs implements Pipeline.
func (p *KWayMerge) Inputs() []Pipeline {
	return p.inputs
}

// Read implements Pipeline.
func (p *KWayMerge) Read() error {
	p.init()
	return p.read()
}

// Transport implements Pipeline.
func (p *KWayMerge) Transport() Transport {
	return Local
}

// Value implements Pipeline.
func (p *KWayMerge) Value() (arrow.Record, error) {
	return p.state.Value()
}

func (p *KWayMerge) init() {
	if p.initialized {
		return
	}

	p.initialized = true

	n := len(p.inputs)
	p.batches = make([]arrow.Record, n)
	p.exhausted = make([]bool, n)
	p.offsets = make([]int64, n)

	if p.compare == nil {
		p.compare = func(a, b uint64) bool { return a <= b }
	}
}

// Iterate through each record, looking at the value from their starting slice offset.
// Track the top two winners (e.g., the record whose next value is the smallest and the record whose next value is the next smallest).
// Find the largest offset in the starting record whose value is still less than the value of the runner-up record from the previous step.
// Return the slice of that record using the two offsets, and update the stored offset of the returned record for the next call to Read.
func (p *KWayMerge) read() error {
	// Release previous batch
	if p.state.batch != nil {
		p.state.batch.Release()
	}

	timestamps := make([]uint64, 0, len(p.inputs))
	batchIndexes := make([]int, 0, len(p.inputs))

	for i := range len(p.inputs) {
		// Skip exhausted inputs
		if p.exhausted[i] {
			continue
		}

		// Load next batch if it hasn't been loaded yet, or if current one is already fully consumed
		if p.batches[i] == nil || p.offsets[i] == p.batches[i].NumRows() {
			err := p.inputs[i].Read()
			if err != nil {
				if err == EOF {
					p.exhausted[i] = true
					continue
				}
				return err
			}
			p.offsets[i] = 0
			p.batches[i], _ = p.inputs[i].Value()
		}

		// Fetch timestamp value at current offset
		col, err := p.columnEval(p.batches[i])
		if err != nil {
			return err
		}
		tsCol, ok := col.ToArray().(*array.Uint64)
		if !ok {
			return errors.New("column is not a timestamp column")
		}
		ts := tsCol.Value(int(p.offsets[i]))

		// Populate slices for sorting
		batchIndexes = append(batchIndexes, i)
		timestamps = append(timestamps, ts)
	}

	// Pipeline is exhausted if no more input batches are available
	if len(batchIndexes) == 0 {
		p.state = Exhausted
		return p.state.err
	}

	// If there is only a single remaining batch, return the remaining record
	if len(batchIndexes) == 1 {
		j := batchIndexes[0]
		start := p.offsets[j]
		end := p.batches[j].NumRows()
		p.state = successState(p.batches[j].NewSlice(start, end))
		p.offsets[j] = end
		return nil
	}

	// Sort inputs based on timestamps
	sort.Slice(batchIndexes, func(i, j int) bool {
		return p.compare(timestamps[i], timestamps[j])
	})

	// Sort timestamps based on timestamps
	sort.Slice(timestamps, func(i, j int) bool {
		return p.compare(timestamps[i], timestamps[j])
	})

	// Return the slice of the current record
	j := batchIndexes[0]

	// Fetch timestamp value at current offset
	col, err := p.columnEval(p.batches[j])
	if err != nil {
		return err
	}
	// We assume the column is a Uint64 array
	tsCol, ok := col.ToArray().(*array.Uint64)
	if !ok {
		return errors.New("column is not a timestamp column")
	}

	// Calculate start/end of the sub-slice of the record
	start := p.offsets[j]
	end := start
	for end < p.batches[j].NumRows() {
		ts := tsCol.Value(int(end))
		end++
		if p.compare(ts, timestamps[1]) {
			break
		}
	}

	p.state = successState(p.batches[j].NewSlice(start, end))
	p.offsets[j] = end
	return nil
}
