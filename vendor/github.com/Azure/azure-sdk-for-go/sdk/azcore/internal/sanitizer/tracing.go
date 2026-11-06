// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package sanitizer

import (
	"context"
	"net/url"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/tracing"
)

type requestURLKey struct{}

// requestURL holds the latest URL snapshot and its pipeline's sanitizer.
// The state flows through credential requests so their errors can also be
// sanitized by the originating operation. Keeping the URL by value isolates
// it from request mutations.
type requestURL struct {
	url       url.URL
	sanitizer *Sanitizer
}

// RecordURL records the latest request URL and its pipeline's sanitizer.
func RecordURL(ctx context.Context, u url.URL, s *Sanitizer) {
	if state, ok := ctx.Value(requestURLKey{}).(*requestURL); ok {
		state.url = u
		state.sanitizer = s
	}
}

// WrapTracer sanitizes status descriptions using the operation's latest request URL.
// Nested spans share the snapshot and must not access it concurrently.
func WrapTracer(tracer tracing.Tracer) tracing.Tracer {
	if !tracer.Enabled() {
		return tracer
	}
	return tracing.WrapTracer(tracer, func(tracer tracing.Tracer) tracing.Tracer {
		return tracing.NewTracer(func(ctx context.Context, name string, options *tracing.SpanOptions) (context.Context, tracing.Span) {
			// Reuse the parent's state so child HTTP spans update the URL used by
			// the operation span, which was created before the request existed.
			state, ok := ctx.Value(requestURLKey{}).(*requestURL)
			if !ok {
				state = &requestURL{}
			}
			ctx, span := tracer.Start(ctx, name, options)
			ctx = context.WithValue(ctx, requestURLKey{}, state)
			return ctx, wrapSpan(span, state)
		}, &tracing.TracerOptions{
			SpanFromContext: func(ctx context.Context) tracing.Span {
				span := tracer.SpanFromContext(ctx)
				// The underlying tracer stores its own span in the context, so
				// retrieving it must apply the sanitizing wrapper again.
				if state, ok := ctx.Value(requestURLKey{}).(*requestURL); ok {
					return wrapSpan(span, state)
				}
				return span
			},
		})
	})
}

// wrapSpan intercepts status descriptions before they reach the tracing provider,
// without changing the original error or the span's other behavior.
func wrapSpan(span tracing.Span, state *requestURL) tracing.Span {
	return tracing.NewSpan(tracing.SpanImpl{
		End:           span.End,
		SetAttributes: span.SetAttributes,
		AddEvent:      span.AddEvent,
		SetStatus: func(code tracing.SpanStatus, description string) {
			if state.sanitizer != nil {
				description = state.sanitizer.GetSanitizedURLString(description, &state.url)
			}
			span.SetStatus(code, description)
		},
	})
}
