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

func TestProxyBackend_ForwardRequest_CreatesChildSpan(t *testing.T) {
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

	// Create a backend
	backend := &ProxyBackend{
		name:     "test-backend",
		endpoint: serverURL,
		client:   http.DefaultClient,
		timeout:  time.Second,
	}

	// Create a parent span to simulate incoming request with tracing
	tracer := otel.Tracer("test")
	parentCtx, parentSpan := tracer.Start(context.Background(), "parent-operation")
	defer parentSpan.End()

	// Create a request with the parent context
	req, err := http.NewRequestWithContext(parentCtx, "GET", "/test", nil)
	require.NoError(t, err)

	// Forward the request
	response := backend.ForwardRequest(req, io.NopCloser(bytes.NewReader([]byte{})))
	require.NoError(t, response.err)
	require.Equal(t, 200, response.status)

	// End the parent span to ensure all spans are complete
	parentSpan.End()

	// Force span export
	tp.ForceFlush(context.Background())

	// Get all spans
	spans := exporter.GetSpans()

	// We should have at least 2 spans: parent and child
	require.GreaterOrEqual(t, len(spans), 2, "should have parent and child spans")

	// Find the child span (not the parent)
	var childSpan *tracetest.SpanStub
	for i := range spans {
		span := &spans[i]
		if span.Name == "querytee.backend.request" {
			childSpan = span
			break
		}
	}

	require.NotNil(t, childSpan, "should have created a child span for the backend request")

	// The child span should have a different span ID but same trace ID
	require.Equal(t, parentSpan.SpanContext().TraceID(), childSpan.SpanContext.TraceID(), "child should have same trace ID")
	require.NotEqual(t, parentSpan.SpanContext().SpanID(), childSpan.SpanContext.SpanID(), "child should have different span ID")
}

func TestProxyBackend_ForwardRequest_SetsBackendAttributes(t *testing.T) {
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

	// Create a backend with a specific name
	backend := &ProxyBackend{
		name:      "cell-a-backend",
		endpoint:  serverURL,
		client:    http.DefaultClient,
		timeout:   time.Second,
		preferred: true,
	}

	// Create a parent span
	tracer := otel.Tracer("test")
	parentCtx, parentSpan := tracer.Start(context.Background(), "parent-operation")
	defer parentSpan.End()

	// Create a request
	req, err := http.NewRequestWithContext(parentCtx, "GET", "/test", nil)
	require.NoError(t, err)

	// Forward the request
	response := backend.ForwardRequest(req, io.NopCloser(bytes.NewReader([]byte{})))
	require.NoError(t, response.err)

	// End the parent span
	parentSpan.End()

	// Force span export
	tp.ForceFlush(context.Background())

	// Get all spans
	spans := exporter.GetSpans()

	// Find the child span
	var childSpan *tracetest.SpanStub
	for i := range spans {
		span := &spans[i]
		// Look for our backend request span
		if span.Name == "querytee.backend.request" {
			childSpan = span
			break
		}
	}

	require.NotNil(t, childSpan, "should have created a child span")

	// Check for backend-specific attributes
	hasBackendName := false
	hasBackendPreferred := false

	for _, attr := range childSpan.Attributes {
		if attr.Key == "backend.name" {
			hasBackendName = true
			require.Equal(t, "cell-a-backend", attr.Value.AsString())
		}
		if attr.Key == "backend.preferred" {
			hasBackendPreferred = true
			require.True(t, attr.Value.AsBool())
		}
	}

	require.True(t, hasBackendName, "span should have backend.name attribute")
	require.True(t, hasBackendPreferred, "span should have backend.preferred attribute")
}
