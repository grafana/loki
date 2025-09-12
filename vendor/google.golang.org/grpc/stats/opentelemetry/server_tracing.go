/*
 * Copyright 2024 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package opentelemetry

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	otelinternaltracing "google.golang.org/grpc/stats/opentelemetry/internal/tracing"
)

// traceTagRPC populates context with new span data using the TextMapPropagator
// supplied in trace options and internal itracing.Carrier. It creates a new
// incoming carrier which extracts an existing span context (if present) by
// deserializing from provided context. If valid span context is extracted, it
// is set as parent of the new span otherwise new span remains the root span.
// If TextMapPropagator is not provided in the trace options, it returns context
// as is.
func (h *serverStatsHandler) traceTagRPC(ctx context.Context, ai *attemptInfo) (context.Context, *attemptInfo) {
	mn := strings.Replace(ai.method, "/", ".", -1)
	var span trace.Span
	tracer := h.options.TraceOptions.TracerProvider.Tracer(tracerName, trace.WithInstrumentationVersion(grpc.Version))
	ctx = h.options.TraceOptions.TextMapPropagator.Extract(ctx, otelinternaltracing.NewIncomingCarrier(ctx))
	// If the context.Context provided in `ctx` to tracer.Start(), contains a
	// span then the newly-created Span will be a child of that span,
	// otherwise it will be a root span.
	ctx, span = tracer.Start(ctx, mn, trace.WithSpanKind(trace.SpanKindServer))
	ai.traceSpan = span
	return ctx, ai
}
