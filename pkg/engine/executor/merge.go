package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

// Merge is a pipeline that takes N inputs and sequentially consumes each one of them.
// It completely exhausts an input before moving to the next one.
type Merge struct {
	inputs []Pipeline
	state  state
}

var _ Pipeline = (*Merge)(nil)

func NewMergePipeline(inputs []Pipeline) (*Merge, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("no inputs provided for merge pipeline")
	}

	return &Merge{
		inputs: inputs,
	}, nil
}

// Read reads the next value into its state.
// It returns an error if reading fails or when the pipeline is exhausted.
func (m *Merge) Read(ctx context.Context) error {
	if m.state.err != nil {
		return m.state.err
	}

	if m.state.batch != nil {
		m.state.batch.Release()
	}

	record, err := m.read(ctx)
	m.state = newState(record, err)

	if err != nil {
		return fmt.Errorf("run merge: %w", err)
	}

	return nil
}

func (m *Merge) read(ctx context.Context) (arrow.Record, error) {
	for _, input := range m.inputs {
		if err := input.Read(ctx); err != nil {
			if errors.Is(err, EOF) {
				continue
			}

			return nil, err
		}

		// not updating reference counts as this pipeline is not consuming
		// the record.
		return input.Value()
	}

	// return EOF if none of the inputs returned a record.
	return nil, EOF
}

// Close implements Pipeline.
func (m *Merge) Close() {
	if m.state.batch != nil {
		m.state.batch.Release()
	}

	for _, input := range m.inputs {
		input.Close()
	}
}

// Inputs implements Pipeline.
func (m *Merge) Inputs() []Pipeline {
	return m.inputs
}

// Transport implements Pipeline.
func (m *Merge) Transport() Transport {
	return Local
}

// Value implements Pipeline.
func (m *Merge) Value() (arrow.Record, error) {
	return m.state.Value()
}
