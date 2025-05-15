package executor

import (
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

type Transport uint8

const (
	_ Transport = iota
	Local
	Remote
)

// Pipeline represents a data processing pipeline that can read Arrow records.
// It provides methods to read data, access the current record, and close resources.
type Pipeline interface {
	// Read reads the next value into its state.
	// It returns an error if reading fails or when the pipeline is exhausted. In this case, the function returns EOF.
	// The implementation must retain the returned error in its state and return it with subsequent Value() calls.
	Read() error
	// Value returns the current value in state.
	Value() (arrow.Record, error)
	// Close closes the resources of the pipeline.
	// The implementation must close all the of the pipeline's inputs.
	Close()
	// Inputs returns the inputs of the pipeline.
	Inputs() []Pipeline
	// Transport returns the type of transport of the implementation.
	Transport() Transport
}

var (
	errNotImplemented = errors.New("pipeline not implemented")

	EOF       = errors.New("pipeline exhausted") // nolint:revive
	Exhausted = failureState(EOF)
)

type state struct {
	batch arrow.Record
	err   error
}

func (s state) Value() (arrow.Record, error) {
	return s.batch, s.err
}

func failureState(err error) state {
	return state{err: err}
}

func successState(batch arrow.Record) state {
	return state{batch: batch}
}

func newState(batch arrow.Record, err error) state {
	return state{batch: batch, err: err}
}

type GenericPipeline struct {
	t      Transport
	inputs []Pipeline
	state  state
	read   func([]Pipeline) state
}

func newGenericPipeline(t Transport, read func([]Pipeline) state, inputs ...Pipeline) *GenericPipeline {
	return &GenericPipeline{
		t:      t,
		read:   read,
		inputs: inputs,
	}
}

var _ Pipeline = (*GenericPipeline)(nil)

// Inputs implements Pipeline.
func (p *GenericPipeline) Inputs() []Pipeline {
	return p.inputs
}

// Value implements Pipeline.
func (p *GenericPipeline) Value() (arrow.Record, error) {
	return p.state.Value()
}

// Read implements Pipeline.
func (p *GenericPipeline) Read() error {
	if p.read == nil {
		return EOF
	}
	p.state = p.read(p.inputs)
	return p.state.err
}

// Close implements Pipeline.
func (p *GenericPipeline) Close() {
	for _, inp := range p.inputs {
		inp.Close()
	}
}

// Transport implements Pipeline.
func (p *GenericPipeline) Transport() Transport {
	return p.t
}

func errorPipeline(err error) Pipeline {
	return newGenericPipeline(Local, func(_ []Pipeline) state {
		return state{err: fmt.Errorf("failed to execute pipeline: %w", err)}
	})
}

func emptyPipeline() Pipeline {
	return newGenericPipeline(Local, func(_ []Pipeline) state {
		return Exhausted
	})
}
