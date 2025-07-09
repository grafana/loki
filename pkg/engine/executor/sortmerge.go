package executor

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"

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
	mu          sync.RWMutex // protects batches, exhausted, and offsets during concurrent reads
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

// readInput reads from a single input pipeline and updates the corresponding batch and offset.
// It returns an error if reading fails, or nil if the input is exhausted or successfully read.
func (p *KWayMerge) readInput(i int) error {
	// Skip exhausted inputs
	p.mu.RLock()
	if p.exhausted[i] {
		p.mu.RUnlock()
		return nil
	}
	p.mu.RUnlock()

	// Load next batch if it hasn't been loaded yet, or if current one is already fully consumed
	// Read another batch as long as the input yields zero-length batches.
	for {
		p.mu.RLock()
		needsRead := p.batches[i] == nil || p.offsets[i] == p.batches[i].NumRows()
		p.mu.RUnlock()

		if !needsRead {
			break
		}

		// Reset offset
		p.mu.Lock()
		p.offsets[i] = 0
		p.mu.Unlock()

		// Read from input
		err := p.inputs[i].Read()
		if err != nil {
			if errors.Is(err, EOF) {
				p.mu.Lock()
				p.exhausted[i] = true
				p.batches[i] = nil // remove reference to arrow.Record from slice
				p.mu.Unlock()
				return nil
			}
			return err
		}

		// It is safe to use the value from the Value() call, because the error is already checked after the Read() call.
		// In case the input is exhausted (reached EOF), the return value is `nil`, however, since the flag `p.exhausted[i]` is set, the value will never be read.
		batch, _ := p.inputs[i].Value()
		p.mu.Lock()
		p.batches[i] = batch
		p.mu.Unlock()
	}

	return nil
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

	// Read from all inputs in parallel
	var wg sync.WaitGroup
	errChan := make(chan error, len(p.inputs))

	for i := range len(p.inputs) {
		wg.Add(1)
		go func(inputIndex int) {
			defer wg.Done()
			if err := p.readInput(inputIndex); err != nil {
				errChan <- err
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Check for any errors from the goroutines
	for err := range errChan {
		return err
	}

loop:
	for i := range len(p.inputs) {
		// Skip exhausted inputs
		p.mu.RLock()
		if p.exhausted[i] {
			p.mu.RUnlock()
			continue loop
		}
		p.mu.RUnlock()

		// Fetch timestamp value at current offset
		p.mu.RLock()
		batch := p.batches[i]
		offset := p.offsets[i]
		p.mu.RUnlock()

		col, err := p.columnEval(batch)
		if err != nil {
			return err
		}
		tsCol, ok := col.ToArray().(*array.Timestamp)
		if !ok {
			return errors.New("column is not a timestamp column")
		}
		ts := tsCol.Value(int(offset))

		// Populate slices for sorting
		inputIndexes = append(inputIndexes, i)
		timestamps = append(timestamps, int64(ts))
	}

	// Pipeline is exhausted if no more input batches are available
	p.mu.RLock()
	exhaustedCopy := make([]bool, len(p.exhausted))
	copy(exhaustedCopy, p.exhausted)
	p.mu.RUnlock()

	if !slices.Contains(exhaustedCopy, false) {
		p.state = Exhausted
		return p.state.err
	}

	if len(inputIndexes) == 0 {
		goto start
	}

	// If there is only a single remaining batch, return the remaining record
	if len(inputIndexes) == 1 {
		j := inputIndexes[0]
		p.mu.RLock()
		start := p.offsets[j]
		batch := p.batches[j]
		p.mu.RUnlock()
		end := batch.NumRows()

		// check against empty last batch
		if start >= end || end == 0 {
			p.state = Exhausted
			return p.state.err
		}

		p.state = successState(batch.NewSlice(start, end))
		p.mu.Lock()
		p.offsets[j] = end
		p.mu.Unlock()
		return nil
	}

	sortIndexesByTimestamps(inputIndexes, timestamps, p.compare)

	// Return the slice of the current record
	j := inputIndexes[0]

	// Fetch timestamp value at current offset
	p.mu.RLock()
	batch := p.batches[j]
	start := p.offsets[j]
	p.mu.RUnlock()

	col, err := p.columnEval(batch)
	if err != nil {
		return err
	}
	// We assume the column is a Uint64 array
	tsCol, ok := col.ToArray().(*array.Timestamp)
	if !ok {
		return errors.New("column is not a timestamp column")
	}

	// Calculate start/end of the sub-slice of the record
	end := start + 1
	for ; end < batch.NumRows(); end++ {
		ts := tsCol.Value(int(end))
		if !p.compare(int64(ts), timestamps[1]) {
			break
		}
	}

	// check against empty batch
	if start > end || end == 0 {
		p.state = successState(batch)
		p.mu.Lock()
		p.offsets[j] = end
		p.mu.Unlock()
		return nil
	}

	p.state = successState(batch.NewSlice(start, end))
	p.mu.Lock()
	p.offsets[j] = end
	p.mu.Unlock()
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
