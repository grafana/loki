package tracing

import (
	"context"

	"github.com/opentracing/opentracing-go"
)

type SystemBoundary string

const (
	QueryExecutionBoundary     string = "query execution"
	QueueingBoundary           string = "query queueing"
	IndexBoundary              string = "index"
	HTTPQueryServingBoundary   string = "HTTP query serving"
	ChunkStoreFetchingBoundary string = "chunk store fetching"
)

func StartRootSpan(ctx context.Context, operation, boundary, rootAction string) (opentracing.Span, context.Context) {
	span, ctx := opentracing.StartSpanFromContext(ctx, operation, WithBoundary(boundary))
	span = span.SetBaggageItem("root_action", rootAction)
	return PropagateRootAction(ctx, span), ctx
}

func StartChildSpan(ctx context.Context, operation, boundary string) (opentracing.Span, context.Context) {
	span, ctx := opentracing.StartSpanFromContext(ctx, operation, WithBoundary(boundary))
	return PropagateRootAction(ctx, span), ctx
}

func PropagateRootAction(ctx context.Context, span opentracing.Span) opentracing.Span {
	return BaggageAsTag(ctx, "root_action", span)
}

func BaggageAsTag(ctx context.Context, baggageKey string, span opentracing.Span) opentracing.Span {
	v := span.BaggageItem(baggageKey)
	return span.SetTag(baggageKey, v)
}

func WithBoundary(boundary string) *Boundary {
	return &Boundary{
		Name: boundary,
	}
}

type Boundary struct {
	Name string
}

func (b Boundary) Apply(opt *opentracing.StartSpanOptions) {
	if opt.Tags == nil {
		opt.Tags = make(map[string]interface{})
	}
	opt.Tags["boundary"] = b.Name
}
