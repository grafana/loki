package executor

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"
)

// BufferedPipeline is a pipeline implementation that reads from a fixed set of Arrow records.
// It implements the Pipeline interface and serves as a simple source for testing and data injection.
type BufferedPipeline struct {
	records []arrow.Record
	current int
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
	}
}

// Read implements Pipeline.
// It advances to the next record and returns EOF when all records have been read.
func (p *BufferedPipeline) Read(_ context.Context) (arrow.Record, error) {
	p.current++
	if p.current >= len(p.records) {
		return nil, EOF
	}

	// Get the next record. The caller is responsible for releasing it it.
	return p.records[p.current], nil
}

// Close implements Pipeline. It releases all unreturned records.
func (p *BufferedPipeline) Close() {
	remRecords := min(p.current, len(p.records))
	for _, rec := range p.records[remRecords:] {
		if rec != nil {
			rec.Release()
		}
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
