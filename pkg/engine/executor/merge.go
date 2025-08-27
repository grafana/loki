package executor

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
)

// inputState tracks the processing state of each input pipeline
type inputState int

const (
	inputNotStarted inputState = iota
	inputInProgress
	inputCompleted
)

// workerResult represents a result from an input pipeline
type workerResult struct {
	record    arrow.Record
	err       error
	inputIdx  int
	completed bool // true when this input is fully exhausted
}

// Merge is a pipeline that takes N inputs and sequentially consumes each one of them.
// It completely exhausts an input before moving to the next one.
type Merge struct {
	inputs      []Pipeline
	maxPrefetch int

	inputStates []inputState
	mu          sync.RWMutex // protect input state changes

	resultCh chan workerResult
	inputCh  chan int // inputs to process

	ctx        context.Context    // context for worker goroutines
	cancelFunc context.CancelFunc // cancel function to stop workers
	wg         sync.WaitGroup

	initialized bool
	state       state
}

var _ Pipeline = (*Merge)(nil)

// newMergePipeline creates a new merge pipeline that merges N inputs into a single output.
//
// The argument maxPrefetch controls how many inputs are prefetched simultaneously while the current one is consumed.
// Set maxPrefetch to 0 to disable prefetching of the next input.
// Set maxPrefetch to 1 to prefetch only the next input, and so on.
// Set maxPrefetch to -1 to pretetch all inputs at once.
func newMergePipeline(inputs []Pipeline, maxPrefetch int) (*Merge, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("merge pipeline: no inputs provided")
	}

	// Default to number of inputs if maxConcurrency is negative or exceeds the number of inputs.
	if maxPrefetch < 0 || maxPrefetch > len(inputs) {
		maxPrefetch = len(inputs)
	}

	return &Merge{
		inputs:      inputs,
		maxPrefetch: maxPrefetch,
		inputStates: make([]inputState, len(inputs)),
	}, nil
}

func (m *Merge) init(ctx context.Context) {
	if m.initialized {
		return
	}

	m.ctx, m.cancelFunc = context.WithCancel(ctx)

	// Buffer size equals maxConcurrency to ensure workers can send results without blocking
	// while maintaining backpressure: when buffer is full, workers block on send until
	// parent consumes results.
	m.resultCh = make(chan workerResult, m.maxPrefetch)

	m.inputCh = make(chan int, len(m.inputs))
	for i := range m.inputs {
		m.inputCh <- i // fill the pool with input indices
	}
	for range m.maxPrefetch {
		m.wg.Add(1)
		go m.inputWorker()
	}

	m.initialized = true
}

// inputWorker processes inputs from the available pool
func (m *Merge) inputWorker() {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		case inputIdx, ok := <-m.inputCh:
			if !ok {
				return // channel closed, normal shutdown
			}
			m.processInput(inputIdx)
		}
	}
}

// processInput completely exhausts a single input pipeline
func (m *Merge) processInput(inputIdx int) {
	m.updateInputState(inputIdx, inputInProgress)

	input := newPrefetchingPipeline(m.inputs[inputIdx])
	defer input.Close()

	for {
		if m.ctx.Err() != nil {
			return
		}

		var result workerResult
		err := input.Read(m.ctx)

		record, _ := input.Value()
		result = workerResult{
			record:   record,
			err:      err,
			inputIdx: inputIdx,
		}

		select {
		case m.resultCh <- result:
			// sent successfully
		case <-m.ctx.Done():
			if result.record != nil {
				result.record.Release()
			}
			return
		}

		// Irrespective of the error, we return here to avoid further Reads.
		if err != nil {
			return
		}
	}
}

// updateInputState safely updates the state of an input
func (m *Merge) updateInputState(inputIdx int, state inputState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputStates[inputIdx] = state
}

// allInputsCompleted checks if all inputs have finished processing
func (m *Merge) allInputsCompleted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, state := range m.inputStates {
		if state != inputCompleted {
			return false
		}
	}
	return true
}

// Read reads the next value into its state from any of the parallel inputs.
func (m *Merge) Read(ctx context.Context) error {
	if m.state.err != nil {
		return m.state.err
	}

	m.init(ctx)

	for {
		select {
		case <-ctx.Done():
			m.state = newState(nil, ctx.Err())
			return fmt.Errorf("run merge: %w", m.state.err)
		case result := <-m.resultCh:
			if m.state.err != nil {
				// ignore results if we already have an error in state
				// next set of errors could also be a result of work cancellation from calling m.cancelFunc()
				continue
			}

			if result.err != nil {
				if errors.Is(result.err, EOF) {
					m.updateInputState(result.inputIdx, inputCompleted)
					if m.allInputsCompleted() {
						m.state = Exhausted
						return fmt.Errorf("run merge: %w", m.state.err)
					}

					continue // continue with next input
				}

				// non EOF error, stop all work.
				m.cancelFunc()
			}

			// We got a data record
			m.state = newState(result.record, result.err)
			if m.state.err != nil {
				return fmt.Errorf("run merge: %w", m.state.err)
			}

			return nil
		}
	}
}

// Value returns the current value in state.
func (m *Merge) Value() (arrow.Record, error) {
	return m.state.Value()
}

// Close closes all resources and stops all worker goroutines.
func (m *Merge) Close() {
	// close the input channel to avoid workers from picking work.
	if m.inputCh != nil {
		close(m.inputCh)
	}

	// stop in-progress work
	if m.cancelFunc != nil {
		m.cancelFunc()
	}

	m.wg.Wait()

	if m.resultCh != nil {
		// Drain any remaining records and release them
		for {
			select {
			case result := <-m.resultCh:
				if result.record != nil {
					result.record.Release()
				}
			default:
				goto done
			}
		}
	done:
		close(m.resultCh)
	}

	for _, input := range m.inputs {
		input.Close()
	}
}

// Inputs returns the inputs of the pipeline.
func (m *Merge) Inputs() []Pipeline {
	return m.inputs
}

// Transport returns the type of transport of the implementation.
func (m *Merge) Transport() Transport {
	return Local
}
