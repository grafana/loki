package executor

import (
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

type compareFunc[T comparable] func(a, b T) bool

// NewSortMergePipeline returns a new pipeline that merges already sorted inputs into a single output.
func NewSortMergePipeline(inputs []Pipeline, order physical.SortOrder, column physical.ColumnExpression, evaluator expressionEvaluator) (*KWayMerge, error) {
	var lessFunc func(a, b int64) bool
	switch order {
	case physical.ASC:
		lessFunc = func(a, b int64) bool { return a <= b }
	case physical.DESC:
		lessFunc = func(a, b int64) bool { return a >= b }
	default:
		return nil, fmt.Errorf("invalid sort order %v", order)
	}

	return &KWayMerge{
		inputs:     inputs,
		columnEval: evaluator.newFunc(column),
		compare:    lessFunc,
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
	compare     compareFunc[int64]
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
		p.compare = func(a, b int64) bool { return a <= b }
	}
}

// Iterate through each record, looking at the value from their starting slice offset.
// Track the top two winners (e.g., the record whose next value is the smallest and the record whose next value is the next smallest).
// Find the largest offset in the starting record whose value is still less than the value of the runner-up record from the previous step.
// Return the slice of that record using the two offsets, and update the stored offset of the returned record for the next call to Read.
func (p *KWayMerge) read() error {
start:
	// Release previous batch
	if p.state.batch != nil {
		p.state.batch.Release()
	}

	timestamps := make([]int64, 0, len(p.inputs))
	inputIndexes := make([]int, 0, len(p.inputs))

loop:
	for i := range len(p.inputs) {
		// Skip exhausted inputs
		if p.exhausted[i] {
			continue loop
		}

		// Load next batch if it hasn't been loaded yet, or if current one is already fully consumed
		// Retry loading if NumRows()==0
		for p.batches[i] == nil || p.offsets[i] == p.batches[i].NumRows() {
			// Reset offset
			p.offsets[i] = 0

			err := p.inputs[i].Read()
			if err != nil {
				if errors.Is(err, EOF) {
					p.exhausted[i] = true
					p.batches[i] = nil // remove reference to arrow.Record from slice
					continue loop
				}
				return err
			}
			// It is safe to use the value from the Value() call, because the error is already checked after the Read() call.
			// In case the input is exhausted (reached EOF), the return value is `nil`, however, since the flag `p.exhausted[i]` is set, the value will never be read.
			p.batches[i], _ = p.inputs[i].Value()
		}

		// // Prevent out-of-bounds error: `p.inputs[i].Read()` returned a batch with 0 rows, and therefore does not have a value at offset `p.offsets[i]`.
		// // However, since the call did not return EOF, the next read may return rows again, so we only skip without marking the input as exhausted.
		// if p.batches[i].NumRows() == 0 {
		// 	continue
		// }

		// Fetch timestamp value at current offset
		col, err := p.columnEval(p.batches[i])
		if err != nil {
			return err
		}
		tsCol, ok := col.ToArray().(*array.Timestamp)
		if !ok {
			return errors.New("column is not a timestamp column")
		}
		ts := tsCol.Value(int(p.offsets[i]))

		// Populate slices for sorting
		inputIndexes = append(inputIndexes, i)
		timestamps = append(timestamps, int64(ts))
	}

	if len(inputIndexes) == 0 {
		goto start
	}

	// Pipeline is exhausted if no more input batches are available
	if !slices.Contains(p.exhausted, false) {
		p.state = Exhausted
		return p.state.err
	}

	// If there is only a single remaining batch, return the remaining record
	if len(inputIndexes) == 1 {
		j := inputIndexes[0]
		start := p.offsets[j]
		end := p.batches[j].NumRows()

		// check against empty last batch
		if start >= end || end == 0 {
			p.state = Exhausted
			return p.state.err
		}

		p.state = successState(p.batches[j].NewSlice(start, end))
		p.offsets[j] = end
		return nil
	}

	sortIndexesByTimestamps(inputIndexes, timestamps, p.compare)

	// Return the slice of the current record
	j := inputIndexes[0]

	// Fetch timestamp value at current offset
	col, err := p.columnEval(p.batches[j])
	if err != nil {
		return err
	}
	// We assume the column is a Uint64 array
	tsCol, ok := col.ToArray().(*array.Timestamp)
	if !ok {
		return errors.New("column is not a timestamp column")
	}

	// Calculate start/end of the sub-slice of the record
	start := p.offsets[j]
	end := start + 1
	for ; end < p.batches[j].NumRows(); end++ {
		ts := tsCol.Value(int(end))
		if !p.compare(int64(ts), timestamps[1]) {
			break
		}
	}

	// check against empty batch
	if start > end || end == 0 {
		p.state = successState(p.batches[j])
		p.offsets[j] = end
		return nil
	}

	p.state = successState(p.batches[j].NewSlice(start, end))
	p.offsets[j] = end
	return nil
}

func sortIndexesByTimestamps(indexes []int, timestamps []int64, lessFn compareFunc[int64]) {
	if len(indexes) != len(timestamps) {
		panic("lengths of indexes and timestamps must match")
	}

	pairs := make([]inputTimestampPair, len(indexes))
	for i := range indexes {
		pairs[i] = inputTimestampPair{indexes[i], timestamps[i]}
	}

	// Sort pairs by timestamp
	sort.SliceStable(pairs, func(i, j int) bool {
		return lessFn(pairs[i].timestamp, pairs[j].timestamp)
	})

	// Unpack the sorted pairs back into the original slices
	for i := range pairs {
		indexes[i] = pairs[i].index
		timestamps[i] = pairs[i].timestamp
	}
}

type inputTimestampPair struct {
	index     int
	timestamp int64
}
