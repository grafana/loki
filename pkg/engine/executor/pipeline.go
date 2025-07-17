package executor

import (
	"context"
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
	Read(context.Context) error
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
	Canceled  = failureState(context.Canceled)
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
	read   func(context.Context, []Pipeline) state
}

func newGenericPipeline(t Transport, read func(context.Context, []Pipeline) state, inputs ...Pipeline) *GenericPipeline {
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
func (p *GenericPipeline) Read(ctx context.Context) error {
	if p.read == nil {
		return EOF
	}
	p.state = p.read(ctx, p.inputs)
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
	return newGenericPipeline(Local, func(_ context.Context, _ []Pipeline) state {
		return state{err: fmt.Errorf("failed to execute pipeline: %w", err)}
	})
}

func emptyPipeline() Pipeline {
	return newGenericPipeline(Local, func(_ context.Context, _ []Pipeline) state {
		return Exhausted
	})
}

// prefetchWrapper wraps a [Pipeline] with pre-fetching capability,
// reading data in a separate goroutine to enable concurrent processing.
type prefetchWrapper struct {
	Pipeline // the pipeline that is wrapped

	initialized bool // internal state to indicate whether the pre-fetching goroutine is running

	ch    chan state // the results channel for pre-fetched items
	state state      // internal state representing the last pre-fetched item

	cancel context.CancelCauseFunc // cancellation function for the context
}

var _ Pipeline = (*prefetchWrapper)(nil)

// newPrefetchingPipeline creates a new prefetching pipeline wrapper that reads data from the underlying pipeline
// in a separate goroutine. This allows for concurrent processing where data can be fetched ahead of time
// while the consumer is processing the current batch.
//
// The prefetching pipeline maintains a buffered channel with capacity 1 to store the next batch,
// enabling pipeline parallelism and potentially improving throughput.
//
// The function accepts a [context.Context] `ctx` and a [Pipeline] `p`, which is the underlying pipeline to wrap
// with pre-fetching capability.
//
// Returns a [prefetchWrapper] that implements the [Pipeline] interface.
func newPrefetchingPipeline(p Pipeline) *prefetchWrapper {
	return &prefetchWrapper{
		Pipeline: p,
		ch:       make(chan state),
	}
}

// Read implements [Pipeline].
func (p *prefetchWrapper) Read(ctx context.Context) error {
	p.init(ctx)
	return p.read(ctx)
}

func (p *prefetchWrapper) init(ctx context.Context) {
	if p.initialized {
		return
	}

	p.initialized = true

	ctx, p.cancel = context.WithCancelCause(ctx)
	go p.prefetch(ctx) // nolint:errcheck
}

func (p prefetchWrapper) prefetch(ctx context.Context) error {
	// Close channel on exit
	defer close(p.ch)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var s state
			s.err = p.Pipeline.Read(ctx)
			if s.err != nil {
				p.ch <- s
				return s.err
			}
			s.batch, s.err = p.Pipeline.Value()
			s.batch.Retain()
			// Sending to channel will block until the batch is read by the parent pipeline.
			// If the context is cancelled while waiting to send, we return.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case p.ch <- s:
			}
		}
	}
}

func (p *prefetchWrapper) read(_ context.Context) error {
	// Release previously retained batch
	if p.state.batch != nil {
		p.state.batch.Release()
	}
	p.state = <-p.ch
	// Reading from a channel that is closed while waiting yields a zero-value.
	// In that case, the pipeline should produce an error state.
	if p.state.err == nil && p.state.batch == nil {
		p.state = Canceled
	}
	return p.state.err
}

// Value implements [Pipeline].
func (p *prefetchWrapper) Value() (arrow.Record, error) {
	return p.state.batch, p.state.err
}

// Close implements [Pipeline].
func (p *prefetchWrapper) Close() {
	// Cancel internal context so the goroutine can exit
	p.cancel(errors.New("pipeline is closed"))
	// Clear already pre-fetched, but unused items from channel
	for range p.ch { // nolint:revive
	}
	p.Pipeline.Close()
}
