package executor

import (
	"github.com/apache/arrow-go/v18/arrow"
)

// LimitPipeline implements a pipeline that limits the number of rows it returns.
// It applies skip (offset) and fetch (limit) to its input pipeline.
type LimitPipeline struct {
	input      Pipeline
	skip       uint32 // Number of rows to skip
	fetch      uint32 // Max number of rows to fetch
	state      state
	rowsOutput int64 // Rows output so far
}

// NewLimitPipeline creates a new LimitPipeline with the given input, skip, and fetch values.
func NewLimitPipeline(input Pipeline, skip, fetch uint32) *LimitPipeline {
	return &LimitPipeline{
		input:      input,
		skip:       skip,
		fetch:      fetch,
		rowsOutput: 0,
		state:      failureState(EOF),
	}
}

// Read implements Pipeline.
// It reads from the input pipeline, skipping rows as necessary and stopping when the fetch limit is reached.
func (p *LimitPipeline) Read() error {
	// If we've already fetched the requested number of rows, return EOF
	if p.fetch > 0 && p.rowsOutput >= int64(p.fetch) {
		p.state = failureState(EOF)
		return EOF
	}

	// Release previous record if exists
	if p.state.batch != nil {
		p.state.batch.Release()
		p.state.batch = nil
	}

	// Read from input
	err := p.input.Read()
	if err != nil {
		p.state = failureState(err)
		return err
	}

	// Get batch from input
	batch, err := p.input.Value()
	if err != nil {
		p.state = failureState(err)
		return err
	}

	numRows := batch.NumRows()

	// If we still need to skip rows
	if p.skip > 0 {
		// If we need to skip more rows than in this batch, skip the whole batch
		if p.skip >= uint32(numRows) {
			p.skip -= uint32(numRows)
			batch.Release()
			return p.Read()
		}

		// Skip partial batch
		startIdx := int64(p.skip)
		endIdx := numRows

		// Apply fetch limit if needed
		if p.fetch > 0 {
			remaining := int64(p.fetch) - p.rowsOutput
			if remaining < (endIdx - startIdx) {
				endIdx = startIdx + remaining
			}
		}

		// Slice the record
		sliced := batch.NewSlice(startIdx, endIdx)
		batch.Release()

		p.skip = 0
		p.rowsOutput += (endIdx - startIdx)
		p.state = successState(sliced)
		return nil
	}

	// If we have a fetch limit and need to limit this batch
	if p.fetch > 0 {
		remaining := int64(p.fetch) - p.rowsOutput
		if remaining < numRows {
			// We'll hit our fetch limit with this batch - slice it
			sliced := batch.NewSlice(0, remaining)
			batch.Release()

			p.rowsOutput += remaining
			p.state = successState(sliced)
			return nil
		}
	}

	// Return the full batch
	p.rowsOutput += numRows
	p.state = successState(batch)
	return nil
}

// Value implements Pipeline.
func (p *LimitPipeline) Value() (arrow.Record, error) {
	return p.state.Value()
}

// Close implements Pipeline.
func (p *LimitPipeline) Close() {
	if p.input != nil {
		p.input.Close()
	}

	if p.state.batch != nil {
		p.state.batch.Release()
		p.state.batch = nil
	}
}

// Inputs implements Pipeline.
func (p *LimitPipeline) Inputs() []Pipeline {
	return []Pipeline{p.input}
}

// Transport implements Pipeline.
func (p *LimitPipeline) Transport() Transport {
	return p.input.Transport()
}
