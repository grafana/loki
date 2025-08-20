package querytee

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dskit/tracing"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestProxyBackend_InjectsTraceHeaders(t *testing.T) {
	// Set up OpenTelemetry tracing
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)

	originalTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)

	// Set up the propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	defer func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(originalTP)
	}()

	// Track headers received by the backend
	var receivedHeaders http.Header

	// Create a test server that captures the headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(200)
		_, _ = w.Write([]byte("test response"))
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	backend := NewProxyBackend("test", serverURL, 5*time.Second, false)

	// Create a parent span using dskit tracing
	parentSpan, parentCtx := tracing.StartSpanFromContext(context.Background(), "test-parent")
	defer parentSpan.Finish()

	// Create a request with the parent context
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(parentCtx)

	// Forward the request
	response := backend.ForwardRequest(req, nil)
	require.NoError(t, response.err)
	require.Equal(t, 200, response.status)

	// Check that trace headers were injected
	// OpenTelemetry uses the "traceparent" header for W3C Trace Context
	traceParentHeader := receivedHeaders.Get("traceparent")
	require.NotEmpty(t, traceParentHeader, "traceparent header should be present")

	// The traceparent header should have the format: version-trace_id-span_id-flags
	// Example: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
	parts := strings.Split(traceParentHeader, "-")
	require.Len(t, parts, 4, "traceparent header should have 4 parts")
	require.Equal(t, "00", parts[0], "version should be 00")
	require.Len(t, parts[1], 32, "trace ID should be 32 hex chars")
	require.Len(t, parts[2], 16, "span ID should be 16 hex chars")
}

func TestProxyBackend_ContextIsolationWithTracing(t *testing.T) {
	// Set up OpenTelemetry tracing
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)

	originalTP := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	defer func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(originalTP)
	}()

	// Track headers received by each backend
	var headers1, headers2 http.Header

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers1 = r.Header.Clone()
		// Delay to ensure parallel execution
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
		_, _ = w.Write([]byte("backend1"))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers2 = r.Header.Clone()
		// Delay to ensure parallel execution
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
		_, _ = w.Write([]byte("backend2"))
	}))
	defer server2.Close()

	u1, err := url.Parse(server1.URL)
	require.NoError(t, err)
	u2, err := url.Parse(server2.URL)
	require.NoError(t, err)

	backend1 := NewProxyBackend("backend1", u1, 5*time.Second, true)
	backend2 := NewProxyBackend("backend2", u2, 5*time.Second, false)

	// Create a parent span
	parentSpan, parentCtx := tracing.StartSpanFromContext(context.Background(), "test-parent")
	defer parentSpan.Finish()

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(parentCtx)

	// Run both backends in parallel (simulating proxy_endpoint behavior)
	done := make(chan *BackendResponse, 2)
	go func() {
		done <- backend1.ForwardRequest(req, nil)
	}()
	go func() {
		done <- backend2.ForwardRequest(req, nil)
	}()

	// Wait for both
	response1 := <-done
	response2 := <-done

	// Both should succeed
	require.NoError(t, response1.err)
	require.NoError(t, response2.err)

	// Both should have trace headers
	require.NotEmpty(t, headers1.Get("traceparent"), "backend1 should have traceparent header")
	require.NotEmpty(t, headers2.Get("traceparent"), "backend2 should have traceparent header")

	// Extract trace IDs from headers - they should share the same trace ID
	trace1Parts := strings.Split(headers1.Get("traceparent"), "-")
	trace2Parts := strings.Split(headers2.Get("traceparent"), "-")
	require.Len(t, trace1Parts, 4)
	require.Len(t, trace2Parts, 4)

	// Same trace ID (parts[1]) but different span IDs (parts[2])
	require.Equal(t, trace1Parts[1], trace2Parts[1], "should have same trace ID")
	require.NotEqual(t, trace1Parts[2], trace2Parts[2], "should have different span IDs")
}
