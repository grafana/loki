package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// Pipeline represents a data processing pipeline that can read Arrow records.
// It provides methods to read data, access the current record, and close resources.
type Pipeline interface {
	// Read collects the next value ([arrow.RecordBatch]) from the pipeline and returns it to the caller.
	// It returns an error if reading fails or when the pipeline is exhausted. In this case, the function returns EOF.
	Read(context.Context) (arrow.RecordBatch, error)
	// Close closes the resources of the pipeline.
	// The implementation must close all the of the pipeline's inputs and must be safe to call multiple times.
	Close()
}

// WrappedPipeline represents a pipeline that wraps another pipeline.
type WrappedPipeline interface {
	Pipeline

	// Unwrap returns the inner pipeline. Implementations must always return the
	// same non-nil value representing the inner pipeline.
	Unwrap() Pipeline
}

// Unwrap recursively unwraps the provided pipeline. [WrappedPipeline.Unwrap] is
// invoked for each wrapped pipeline until the first non-wrapped pipeline is
// reached.
func Unwrap(p Pipeline) Pipeline {
	for {
		wrapped, ok := p.(WrappedPipeline)
		if !ok {
			return p
		}
		p = wrapped.Unwrap()
	}
}

// RegionProvider is an optional interface that pipelines can implement
// to expose their associated xcap region for statistics collection.
type RegionProvider interface {
	// Region returns the xcap region associated with this pipeline node, if any.
	// Returns nil if no region is associated with this pipeline.
	Region() *xcap.Region
}

// Contributing time range would be anything less than `ts` if `lessThan` is true, or greater
// than `ts` otherwise.
type ContributingTimeRangeChangedHandler = func(ts time.Time, lessThan bool)

// ContributingTimeRangeChangedNotifier is an optional interface that pipelines can implement
// to notify others that they are interested only in inputs from some specific time range.
type ContributingTimeRangeChangedNotifier interface {
	// SubscribeToTimeRangeChanges adds a callback function to a list of listeners.
	SubscribeToTimeRangeChanges(callback ContributingTimeRangeChangedHandler)
}

var (
	errNotImplemented = errors.New("pipeline not implemented")
	EOF               = errors.New("pipeline exhausted") //nolint:revive,staticcheck
)

type state struct {
	batch arrow.RecordBatch
	err   error
}

type readFunc func(context.Context, []Pipeline) (arrow.RecordBatch, error)

type GenericPipeline struct {
	inputs []Pipeline
	read   readFunc
	region *xcap.Region
}

func newGenericPipeline(read readFunc, inputs ...Pipeline) *GenericPipeline {
	return &GenericPipeline{
		read:   read,
		inputs: inputs,
	}
}

func newGenericPipelineWithRegion(read readFunc, region *xcap.Region, inputs ...Pipeline) *GenericPipeline {
	return &GenericPipeline{
		read:   read,
		inputs: inputs,
		region: region,
	}
}

var (
	_ Pipeline       = (*GenericPipeline)(nil)
	_ RegionProvider = (*GenericPipeline)(nil)
)

// Read implements Pipeline.
func (p *GenericPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if p.read == nil {
		return nil, EOF
	}
	return p.read(ctx, p.inputs)
}

// Close implements Pipeline.
func (p *GenericPipeline) Close() {
	if p.region != nil {
		p.region.End()
	}
	for _, inp := range p.inputs {
		inp.Close()
	}
}

// Region implements RegionProvider.
func (p *GenericPipeline) Region() *xcap.Region {
	return p.region
}

func errorPipeline(ctx context.Context, err error) Pipeline {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	return newGenericPipeline(func(_ context.Context, _ []Pipeline) (arrow.RecordBatch, error) {
		return nil, fmt.Errorf("failed to execute pipeline: %w", err)
	})
}

func errorPipelineWithRegion(ctx context.Context, err error, region *xcap.Region) Pipeline {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	return newGenericPipelineWithRegion(func(_ context.Context, _ []Pipeline) (arrow.RecordBatch, error) {
		return nil, fmt.Errorf("failed to execute pipeline: %w", err)
	}, region)
}

func emptyPipeline() Pipeline {
	return newGenericPipeline(func(_ context.Context, _ []Pipeline) (arrow.RecordBatch, error) {
		return nil, EOF
	})
}

func emptyPipelineWithRegion(region *xcap.Region) Pipeline {
	return newGenericPipelineWithRegion(func(_ context.Context, _ []Pipeline) (arrow.RecordBatch, error) {
		return nil, EOF
	}, region)
}

// prefetchWrapper wraps a [Pipeline] with pre-fetching capability,
// reading data in a separate goroutine to enable concurrent processing.
type prefetchWrapper struct {
	Pipeline // the pipeline that is wrapped

	initialized bool                    // internal state to indicate whether the pre-fetching goroutine is running
	ch          chan state              // the results channel for pre-fetched items
	cancel      context.CancelCauseFunc // cancellation function for the context
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
func (p *prefetchWrapper) Read(ctx context.Context) (arrow.RecordBatch, error) {
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
			s.batch, s.err = p.Pipeline.Read(ctx)
			if s.err != nil {
				p.ch <- s
				return s.err
			}

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

func (p *prefetchWrapper) read(_ context.Context) (arrow.RecordBatch, error) {
	state := <-p.ch

	// Reading from a channel that is closed while waiting yields a zero-value.
	// In that case, the pipeline should produce an error state.
	if state.err == nil && state.batch == nil {
		return nil, context.Canceled
	}
	return state.batch, state.err
}

// Close implements [Pipeline].
func (p *prefetchWrapper) Close() {
	if p.cancel != nil {
		p.cancel(errors.New("pipeline is closed"))

		// Wait for the prefetch goroutine to finish. This avoids race conditions
		// where we close a pipeline right before it's read.
		//
		// This check can only be done if p.cancel is non-nil, otherwise we may
		// deadlock if [prefetchWrapper.Close] is called before
		// [prefetchWrapper.init].
		<-p.ch
	}
	p.Pipeline.Close()
}

// Region implements RegionProvider.
func (p *prefetchWrapper) Region() *xcap.Region {
	if provider, ok := p.Pipeline.(RegionProvider); ok {
		return provider.Region()
	}
	return nil
}

type tracedPipeline struct {
	name  string
	inner Pipeline
}

var _ Pipeline = (*tracedPipeline)(nil)

// tracePipeline wraps a [Pipeline] to record each call to Read with a span.
func tracePipeline(name string, pipeline Pipeline) *tracedPipeline {
	return &tracedPipeline{
		name:  name,
		inner: pipeline,
	}
}

func (p *tracedPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	ctx, span := tracer.Start(ctx, p.name+".Read")
	defer span.End()

	res, err := p.inner.Read(ctx)
	if err != nil && !errors.Is(err, EOF) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	return res, err
}

func (p *tracedPipeline) Close() { p.inner.Close() }

type lazyPipeline struct {
	ctor func(ctx context.Context, inputs []Pipeline) Pipeline

	inputs []Pipeline
	built  Pipeline
}

// newLazyPipeline allows for defering construction of a [Pipeline] to query
// execution time instead of planning time. This is useful for pipelines which
// are expensive to construct, or have dependencies which are only available
// during execution.
//
// The ctor function will be invoked on the first call to [Pipeline.Read].
func newLazyPipeline(ctor func(ctx context.Context, inputs []Pipeline) Pipeline, inputs []Pipeline) *lazyPipeline {
	return &lazyPipeline{
		ctor:   ctor,
		inputs: inputs,
	}
}

var _ Pipeline = (*lazyPipeline)(nil)

// Read reads the next value from the inner pipeline. If this is the first call
// to Read, the inner  pipeline will be constructed using the provided context.
func (lp *lazyPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if lp.built == nil {
		lp.built = lp.ctor(ctx, lp.inputs)
	}
	return lp.built.Read(ctx)
}

// Close closes the lazily constructed pipeline if it has been built.
func (lp *lazyPipeline) Close() {
	if lp.built != nil {
		lp.built.Close()
	}
	lp.built = nil
}

// Region implements RegionProvider.
func (lp *lazyPipeline) Region() *xcap.Region {
	if lp.built != nil {
		if provider, ok := lp.built.(RegionProvider); ok {
			return provider.Region()
		}
	}
	return nil
}

// observedPipeline wraps a Pipeline to automatically collect common statistics
// and record them to the pipeline's region.
type observedPipeline struct {
	inner  Pipeline
	region *xcap.Region
}

var _ Pipeline = (*observedPipeline)(nil)

// newObservedPipeline wraps a pipeline to automatically collect common statistics.
// If the pipeline has a region, statistics will be recorded to it.
func newObservedPipeline(inner Pipeline) *observedPipeline {
	p := &observedPipeline{
		inner: inner,
	}

	if provider, ok := inner.(RegionProvider); ok {
		p.region = provider.Region()
	}

	return p
}

// Read implements Pipeline.
func (p *observedPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	start := time.Now()

	if p.region != nil {
		p.region.Record(xcap.StatPipelineReadCalls.Observe(1))
	}

	rec, err := p.inner.Read(ctx)

	if p.region != nil {
		if rec != nil {
			p.region.Record(xcap.StatPipelineRowsOut.Observe(rec.NumRows()))
		}

		p.region.Record(xcap.StatPipelineReadDuration.Observe(time.Since(start).Seconds()))
	}

	return rec, err
}

// Unwrap returns the underlying pipeline.
func (p *observedPipeline) Unwrap() Pipeline {
	return p.inner
}

// Close implements Pipeline.
func (p *observedPipeline) Close() {
	p.inner.Close()
}
