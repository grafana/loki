package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// Pipeline represents a data processing pipeline that can read Arrow records.
// It provides methods to read data, access the current record, and close resources.
type Pipeline interface {
	// Open initializes the pipeline resources and must be called before [Read].
	// The implementation must be safe to call multiple times.
	Open(context.Context) error
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
	errNotImplemented  = errors.New("pipeline not implemented")
	errPipelineNotOpen = errors.New("pipeline not opened")
	EOF                = errors.New("pipeline exhausted") //nolint:revive,staticcheck
)

type state struct {
	batch arrow.RecordBatch
	err   error
}

type readFunc func(context.Context, []Pipeline) (arrow.RecordBatch, error)

type GenericPipeline struct {
	inputs []Pipeline
	read   readFunc
}

func newGenericPipeline(read readFunc, inputs ...Pipeline) *GenericPipeline {
	return &GenericPipeline{
		read:   read,
		inputs: inputs,
	}
}

var _ Pipeline = (*GenericPipeline)(nil)

// Open implements Pipeline.
func (p *GenericPipeline) Open(ctx context.Context) error {
	for _, inp := range p.inputs {
		if err := inp.Open(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Read implements Pipeline.
func (p *GenericPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if p.read == nil {
		return nil, EOF
	}
	return p.read(ctx, p.inputs)
}

// Close implements Pipeline.
func (p *GenericPipeline) Close() {
	for _, inp := range p.inputs {
		inp.Close()
	}
}

func errorPipeline(ctx context.Context, err error) Pipeline {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	return newGenericPipeline(func(_ context.Context, _ []Pipeline) (arrow.RecordBatch, error) {
		return nil, fmt.Errorf("failed to execute pipeline: %w", err)
	})
}

func emptyPipeline() Pipeline {
	return newGenericPipeline(func(_ context.Context, _ []Pipeline) (arrow.RecordBatch, error) {
		return nil, EOF
	})
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

// Open implements [Pipeline].
func (p *prefetchWrapper) Open(ctx context.Context) error {
	return p.Pipeline.Open(ctx)
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
// The ctor function will be invoked on the first call to [Pipeline.Open].
func newLazyPipeline(ctor func(ctx context.Context, inputs []Pipeline) Pipeline, inputs []Pipeline) *lazyPipeline {
	return &lazyPipeline{
		ctor:   ctor,
		inputs: inputs,
	}
}

var _ Pipeline = (*lazyPipeline)(nil)

// Open initializes the inner pipeline.
func (lp *lazyPipeline) Open(ctx context.Context) error {
	if lp.built != nil {
		return nil
	}
	lp.built = lp.ctor(ctx, lp.inputs)
	return lp.built.Open(ctx)
}

// Read reads the next value from the inner pipeline.
func (lp *lazyPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if lp.built == nil {
		return nil, errPipelineNotOpen
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

// observedPipeline wraps a Pipeline to automatically collect common statistics.
//
// It creates an [xcap.Span] paired with a [xcap.Region] for the given
// physical plan node. On each Read call it injects both into the caller's
// context via [xcap.ContextWithSpan], so inner pipelines can retrieve them
// via [trace.SpanFromContext] and [xcap.RegionFromContext] while still
// respecting the caller's cancellation and deadline. The span (and its
// linked region) are injected this way to avoid creating a new span for
// the same node on each Read call.
//
// On Close, it ends the xcap span (which flushes region observations as
// span attributes) after closing the inner pipeline.
type observedPipeline struct {
	inner    Pipeline
	name     string
	attrs    []attribute.KeyValue
	readSpan *xcap.Span
	ready    bool
}

var _ Pipeline = (*observedPipeline)(nil)

// NewObservedPipeline wraps a pipeline to automatically collect common
// statistics. The xcap span and region are not created here; they are
// deferred to the first [Read] call so the span inherits its parent
// from the caller's context.
func NewObservedPipeline(name string, attrs []attribute.KeyValue, inner Pipeline) Pipeline {
	return &observedPipeline{name: name, attrs: attrs, inner: inner}
}

func (p *observedPipeline) init(ctx context.Context) {
	_, p.readSpan = xcap.StartSpan(ctx, tracer, p.name+".Read")
	p.ready = true
}

// Read implements Pipeline.
func (p *observedPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if !p.ready {
		p.init(ctx)
	}

	start := time.Now()
	p.readSpan.Record(xcap.StatPipelineReadCalls.Observe(1))

	// Inject the span (implicitly links the associated region) into ctx.
	ctx = xcap.ContextWithSpan(ctx, p.readSpan)

	rec, err := p.inner.Read(ctx)
	if rec != nil {
		p.readSpan.Record(xcap.StatPipelineRowsOut.Observe(rec.NumRows()))
	}
	p.readSpan.Record(xcap.StatPipelineReadDuration.Observe(time.Since(start).Seconds()))

	return rec, err
}

// Open implements Pipeline.
func (p *observedPipeline) Open(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, p.name+".Open", trace.WithAttributes(p.attrs...))
	defer span.End()

	return p.inner.Open(ctx)
}

// Unwrap returns the underlying pipeline.
func (p *observedPipeline) Unwrap() Pipeline {
	return p.inner
}

// Close implements Pipeline.
func (p *observedPipeline) Close() {
	p.inner.Close()
	if p.readSpan != nil {
		p.readSpan.End()
	}
}
