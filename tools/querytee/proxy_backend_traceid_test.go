package querytee

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestProxyBackend_ForwardRequest_ReturnsChildSpanTraceID(t *testing.T) {
	// Create an in-memory span exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)

	// Store original tracer provider and restore after test
	originalTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(originalTP)
	}()

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("test response"))
	}))
	defer server.Close()

	// Parse the server URL
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	// Create two backends to simulate Cell A and Cell B
	backendA := &ProxyBackend{
		name:      "cell-a",
		endpoint:  serverURL,
		client:    http.DefaultClient,
		timeout:   time.Second,
		preferred: true,
	}

	backendB := &ProxyBackend{
		name:      "cell-b",
		endpoint:  serverURL,
		client:    http.DefaultClient,
		timeout:   time.Second,
		preferred: false,
	}

	// Create a parent span
	tracer := otel.Tracer("test")
	parentCtx, parentSpan := tracer.Start(context.Background(), "parent-operation")

	// Create a request with the parent context
	req, err := http.NewRequestWithContext(parentCtx, "GET", "/test", nil)
	require.NoError(t, err)

	// Forward the request to both backends
	responseA := backendA.ForwardRequest(req, io.NopCloser(bytes.NewReader([]byte{})))
	responseB := backendB.ForwardRequest(req, io.NopCloser(bytes.NewReader([]byte{})))

	require.NoError(t, responseA.err)
	require.NoError(t, responseB.err)

	// The trace IDs should be different for each backend (child spans have different trace IDs in our implementation)
	// Actually, in OpenTelemetry, child spans share the same trace ID but have different span IDs
	// So let's verify they have the same trace ID but we're getting unique identifiers
	require.NotEmpty(t, responseA.traceID, "Cell A should have a trace ID")
	require.NotEmpty(t, responseB.traceID, "Cell B should have a trace ID")

	// Since we're using child spans with the same trace but different span IDs,
	// the traceID field should contain the same trace ID for both
	// However, we want to ensure each backend can be distinguished
	// This test verifies we're extracting trace IDs correctly

	// Force span export
	parentSpan.End()
	tp.ForceFlush(context.Background())

	// Get all spans
	spans := exporter.GetSpans()

	// We should have 3 spans: parent + 2 children
	require.GreaterOrEqual(t, len(spans), 3, "should have parent and two child spans")

	// Debug: print all spans
	t.Logf("Found %d spans total", len(spans))
	for i, span := range spans {
		t.Logf("Span %d: Name=%s", i, span.Name)
	}

	// Find the child spans
	var cellASpan, cellBSpan *tracetest.SpanStub
	for i := range spans {
		span := &spans[i]
		// Check if this is a backend request span
		if span.Name == "querytee.backend.request" {
			for _, attr := range span.Attributes {
				if attr.Key == "backend.name" {
					if attr.Value.AsString() == "cell-a" {
						cellASpan = span
					} else if attr.Value.AsString() == "cell-b" {
						cellBSpan = span
					}
				}
			}
		}
	}

	require.NotNil(t, cellASpan, "should have found Cell A span")
	require.NotNil(t, cellBSpan, "should have found Cell B span")

	// Verify they have the same trace ID but different span IDs
	require.Equal(t, cellASpan.SpanContext.TraceID(), cellBSpan.SpanContext.TraceID(),
		"both backends should share the same trace ID")
	require.NotEqual(t, cellASpan.SpanContext.SpanID(), cellBSpan.SpanContext.SpanID(),
		"backends should have different span IDs")
}
