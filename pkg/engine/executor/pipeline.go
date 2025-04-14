package executor

import (
	"errors"

	"github.com/apache/arrow/go/v18/arrow"
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
	EOF       = errors.New("pipeline exhausted") // nolint:revive
	Exhausted = failure(EOF)
)

type State struct {
	batch arrow.Record
	err   error
}

func (s State) Value() (arrow.Record, error) {
	return s.batch, s.err
}

func failure(err error) State {
	return State{err: err}
}

func success(batch arrow.Record) State {
	return State{batch: batch}
}

func state(batch arrow.Record, err error) State {
	return State{batch: batch, err: err}
}

type GenericPipeline struct {
	t      Transport
	inputs []Pipeline
	state  State
	read   func([]Pipeline) State
}

func newGenericPipeline(t Transport, read func([]Pipeline) State, inputs ...Pipeline) *GenericPipeline {
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
