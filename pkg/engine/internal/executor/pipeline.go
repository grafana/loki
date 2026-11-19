package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	// The implementation must close the pipelines it wraps and must be safe to call multiple times.
	Close()
}

var (
	errPipelineNotOpen = errors.New("pipeline not opened")
	EOF                = fmt.Errorf("pipeline exhausted: %w", io.EOF) //nolint:revive,staticcheck
)

// failedPipeline is a [Pipeline] whose Read always returns err.
type failedPipeline struct{ err error }

var _ Pipeline = failedPipeline{}

// Open implements Pipeline.
func (failedPipeline) Open(context.Context) error { return nil }

// Read implements Pipeline.
func (p failedPipeline) Read(context.Context) (arrow.RecordBatch, error) { return nil, p.err }

// Close implements Pipeline.
func (failedPipeline) Close() {}

func errorPipeline(ctx context.Context, err error) Pipeline {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	return failedPipeline{err: fmt.Errorf("failed to execute pipeline: %w", err)}
}

type lazyPipeline struct {
	ctor  func(ctx context.Context) Pipeline
	built Pipeline
}

// newLazyPipeline defers building a [Pipeline] until the first call to
// [Pipeline.Open]. Use it for pipelines that are expensive to build or that
// depend on data only available during execution.
func newLazyPipeline(ctor func(ctx context.Context) Pipeline) *lazyPipeline {
	return &lazyPipeline{ctor: ctor}
}

var _ Pipeline = (*lazyPipeline)(nil)

// Open initializes the inner pipeline.
func (lp *lazyPipeline) Open(ctx context.Context) error {
	if lp.built != nil {
		return nil
	}
	lp.built = lp.ctor(ctx)
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
	p.readSpan.Record(StatPipelineReadCalls.Observe(1))

	// Inject the span (implicitly links the associated region) into ctx.
	ctx = xcap.ContextWithSpan(ctx, p.readSpan)

	rec, err := p.inner.Read(ctx)
	if rec != nil {
		p.readSpan.Record(StatPipelineRowsOut.Observe(rec.NumRows()))
	}
	p.readSpan.Record(StatPipelineReadDuration.Observe(time.Since(start).Seconds()))

	return rec, err
}

// Open implements Pipeline.
func (p *observedPipeline) Open(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, p.name+".Open", trace.WithAttributes(p.attrs...))
	defer span.End()

	return p.inner.Open(ctx)
}

// Close implements Pipeline.
func (p *observedPipeline) Close() {
	p.inner.Close()
	if p.readSpan != nil {
		p.readSpan.End()
	}
}
