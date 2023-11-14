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
	ReadingFromCacheBoundary   string = "reading from caching"
)

func StartRootSpan(ctx context.Context, operation, boundary, rootAction string) (opentracing.Span, context.Context) {
	span, ctx := opentracing.StartSpanFromContext(ctx, operation, WithBoundary(boundary))
	span = span.SetBaggageItem("root_action", rootAction)
	return PropagateRootAction(span), ctx
}

func StartChildSpan(ctx context.Context, operation, boundary string) (opentracing.Span, context.Context) {
	span, ctx := opentracing.StartSpanFromContext(ctx, operation, WithBoundary(boundary))
	return PropagateRootAction(span), ctx
}

func PropagateRootAction(span opentracing.Span) opentracing.Span {
	return BaggageAsTag("root_action", span)
}

func BaggageAsTag(baggageKey string, span opentracing.Span) opentracing.Span {
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
