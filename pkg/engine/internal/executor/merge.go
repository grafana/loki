package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// Merge is a pipeline that takes N inputs and sequentially consumes each one of them.
// It completely exhausts an input before moving to the next one.
type Merge struct {
	inputs      []Pipeline
	maxPrefetch int
	initialized bool
	currInput   int // index of the currently processed input
	region      *xcap.Region
}

var _ Pipeline = (*Merge)(nil)

// newMergePipeline creates a new merge pipeline that merges N inputs into a single output.
//
// The argument maxPrefetch controls how many inputs are prefetched simultaneously while the current one is consumed.
// Set maxPrefetch to 0 to disable prefetching of the next input.
// Set maxPrefetch to 1 to prefetch only the next input, and so on.
// Set maxPrefetch to -1 to pretetch all inputs at once.
func newMergePipeline(inputs []Pipeline, maxPrefetch int, region *xcap.Region) (*Merge, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("merge pipeline: no inputs provided")
	}

	// Default to number of inputs if maxConcurrency is negative or exceeds the number of inputs.
	if maxPrefetch < 0 || maxPrefetch >= len(inputs) {
		maxPrefetch = len(inputs) - 1
	}

	// Wrap inputs into prefetching pipeline.
	for i := range inputs {
		// Only wrap input, but do not call init() on it, as it would start prefetching.
		// Prefetching is started in the [Merge.init] function
		inputs[i] = newPrefetchingPipeline(inputs[i])
	}

	return &Merge{
		inputs:      inputs,
		maxPrefetch: maxPrefetch,
		region:      region,
	}, nil
}

// Open opens all children pipelines.
func (m *Merge) Open(ctx context.Context) error {
	for _, p := range m.inputs {
		if err := p.Open(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *Merge) init(ctx context.Context) {
	if m.initialized {
		return
	}

	// Initialize pre-fetching of inputs defined by maxPrefetch.
	// The first/current input is always initialized.
	for i := range m.inputs {
		if i <= m.maxPrefetch {
			m.startPrefetchingInputAtIndex(ctx, i)
		}
	}

	m.initialized = true
}

// startPrefetchingInputAtIndex initializes the input at given index i,
// if the index is not out of bounds and if the input is of type [prefetchWrapper].
// Initializing the input will start its prefetching.
func (m *Merge) startPrefetchingInputAtIndex(ctx context.Context, i int) {
	if i >= len(m.inputs) {
		return
	}
	inp, ok := m.inputs[i].(*prefetchWrapper)
	if ok {
		inp.init(ctx)
	}
}

// Read reads the next batch from the pipeline.
// It returns an error if reading fails or when the pipeline is exhausted.
func (m *Merge) Read(ctx context.Context) (arrow.RecordBatch, error) {
	m.init(ctx)
	return m.read(ctx)
}

func (m *Merge) read(ctx context.Context) (arrow.RecordBatch, error) {
	// All inputs have been consumed and are exhausted
	if m.currInput >= len(m.inputs) {
		return nil, EOF
	}

	for m.currInput < len(m.inputs) {
		input := m.inputs[m.currInput]
		rec, err := input.Read(ctx)
		if err != nil {
			if errors.Is(err, EOF) {
				input.Close()
				// Proceed to the next input
				m.currInput++
				// Initialize the next input so it starts prefetching
				m.startPrefetchingInputAtIndex(ctx, m.currInput+m.maxPrefetch)
				continue
			}
			return nil, err
		}
		return rec, nil
	}

	// Return EOF if none of the inputs returned a record.
	return nil, EOF
}

// Close implements Pipeline.
func (m *Merge) Close() {
	if m.region != nil {
		m.region.End()
	}
	// exhausted inputs are already closed
	for _, input := range m.inputs[m.currInput:] {
		input.Close()
	}
}

// Region implements RegionProvider.
func (m *Merge) Region() *xcap.Region {
	return m.region
}
