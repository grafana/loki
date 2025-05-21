package executor

import "github.com/apache/arrow-go/v18/arrow"

// BufferedPipeline is a pipeline implementation that reads from a fixed set of Arrow records.
// It implements the Pipeline interface and serves as a simple source for testing and data injection.
type BufferedPipeline struct {
	records []arrow.Record
	current int
	state   state
}

// NewBufferedPipeline creates a new BufferedPipeline from a set of Arrow records.
// The pipeline will return these records in sequence.
func NewBufferedPipeline(records ...arrow.Record) *BufferedPipeline {
	for _, rec := range records {
		if rec != nil {
			rec.Retain()
		}
	}

	return &BufferedPipeline{
		records: records,
		current: -1, // Start before the first record
		state:   failureState(ErrEOF),
	}
}

// Read implements Pipeline.
// It advances to the next record and returns EOF when all records have been read.
func (p *BufferedPipeline) Read() error {
	// Release previous record if it exists
	if p.state.batch != nil {
		p.state.batch.Release()
	}

	p.current++
	if p.current >= len(p.records) {
		p.state = failureState(ErrEOF)
		return ErrEOF
	}

	p.state = successState(p.records[p.current])
	return nil
}

// Value implements Pipeline.
// It returns the current record and error state.
func (p *BufferedPipeline) Value() (arrow.Record, error) {
	return p.state.Value()
}

// Close implements Pipeline.
// It releases all records being held.
func (p *BufferedPipeline) Close() {
	for _, rec := range p.records {
		if rec != nil {
			rec.Release()
		}
	}

	if p.state.batch != nil {
		p.state.batch.Release()
		p.state.batch = nil
	}

	p.records = nil
}

// Inputs implements Pipeline.
// CSV pipeline is a source, so it has no inputs.
func (p *BufferedPipeline) Inputs() []Pipeline {
	return nil
}

// Transport implements Pipeline.
// CSVPipeline is always considered a Local transport.
func (p *BufferedPipeline) Transport() Transport {
	return Local
}
