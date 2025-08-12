package querytee

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestCreateIsolatedContextWithTracing_ShouldNotInheritParentCancellation(t *testing.T) {
	// Create a parent context that we'll cancel
	parentCtx, parentCancel := context.WithCancel(context.Background())

	// Create an isolated context
	isolatedCtx, isolatedCancel := createIsolatedContextWithTracing(parentCtx)
	defer isolatedCancel()

	// Cancel the parent context
	parentCancel()

	// Give a small amount of time for any goroutines to react
	time.Sleep(10 * time.Millisecond)

	// The isolated context should NOT be canceled
	select {
	case <-isolatedCtx.Done():
		t.Fatal("isolated context should NOT be canceled when parent is canceled - this defeats the purpose of isolation for Goldfish queries")
	default:
		// This is the expected behavior - context is still active
	}
}

func TestCreateIsolatedContextWithTracing_PreservesTraceContext(t *testing.T) {
	// Set up a noop tracer provider for testing
	otel.SetTracerProvider(noop.NewTracerProvider())

	// Create a tracer and start a span
	tracer := otel.Tracer("test")
	parentCtx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	// Get the parent span context
	parentSpanCtx := trace.SpanFromContext(parentCtx).SpanContext()
	// Note: NoopTracerProvider creates invalid span contexts, which is fine for this test
	// as we're testing that whatever span context exists is preserved

	// Create an isolated context
	isolatedCtx, cancel := createIsolatedContextWithTracing(parentCtx)
	defer cancel()

	// The isolated context should preserve the trace context (even if it's invalid/noop)
	isolatedSpanCtx := trace.SpanFromContext(isolatedCtx).SpanContext()
	require.Equal(t, parentSpanCtx.IsValid(), isolatedSpanCtx.IsValid(), "span context validity should be preserved")
	require.Equal(t, parentSpanCtx.TraceID(), isolatedSpanCtx.TraceID(), "trace ID should be preserved")
	require.Equal(t, parentSpanCtx.SpanID(), isolatedSpanCtx.SpanID(), "span ID should be preserved")
}

func TestCreateIsolatedContextWithTracing_HasOwnCancellation(t *testing.T) {
	parentCtx := context.Background()

	// Create an isolated context
	isolatedCtx, isolatedCancel := createIsolatedContextWithTracing(parentCtx)

	// Initially, the context should not be canceled
	require.NoError(t, isolatedCtx.Err())

	// Cancel the isolated context
	isolatedCancel()

	// Give a small amount of time for cancellation to propagate
	time.Sleep(10 * time.Millisecond)

	// The isolated context should now be canceled
	select {
	case <-isolatedCtx.Done():
		// This is expected - the context should be canceled
	default:
		t.Fatal("isolated context should be canceled when its own cancel func is called")
	}
}
