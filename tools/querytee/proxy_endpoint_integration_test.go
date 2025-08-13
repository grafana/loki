package querytee

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestProxyEndpoint_GoldfishQueriesContinueAfterNonGoldfishComplete(t *testing.T) {
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

	// Track which backends have been called
	backendACalled := make(chan bool, 1)
	backendBCalled := make(chan bool, 1)
	backendACompleted := make(chan bool, 1)
	backendBCompleted := make(chan bool, 1)

	// Create test servers for backends
	// Backend A (preferred) - completes quickly
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backendACalled <- true
		time.Sleep(10 * time.Millisecond) // Quick response
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		backendACompleted <- true
	}))
	defer serverA.Close()

	// Backend B - takes longer, simulating Goldfish processing
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendBCalled <- true

		// Verify that context is NOT canceled even after backend A completes
		select {
		case <-r.Context().Done():
			t.Error("Backend B context was canceled - this is the bug we're fixing!")
			w.WriteHeader(499) // Client closed connection
			backendBCompleted <- false
			return
		case <-time.After(50 * time.Millisecond):
			// Context is still valid after delay - this is what we want
		}

		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		backendBCompleted <- true
	}))
	defer serverB.Close()

	// Parse URLs
	urlA, err := url.Parse(serverA.URL)
	require.NoError(t, err)
	urlB, err := url.Parse(serverB.URL)
	require.NoError(t, err)

	// Create backends
	backendA := NewProxyBackend("cell-a", urlA, 5*time.Second, true)
	backendB := NewProxyBackend("cell-b", urlB, 5*time.Second, false)

	// Create proxy endpoint
	endpoint := NewProxyEndpoint(
		[]*ProxyBackend{backendA, backendB},
		"test-route",
		NewProxyMetrics(nil),
		log.NewNopLogger(),
		nil,
		false,
	)

	// Create a parent span to simulate incoming request with tracing
	tracer := otel.Tracer("test")
	parentCtx, parentSpan := tracer.Start(context.Background(), "incoming-request")
	defer parentSpan.End()

	// Create request
	req, err := http.NewRequestWithContext(parentCtx, "GET", "/test", nil)
	require.NoError(t, err)

	// Create response recorder
	recorder := httptest.NewRecorder()

	// Handle the request
	endpoint.ServeHTTP(recorder, req)

	// Verify both backends were called
	select {
	case <-backendACalled:
		// Backend A was called
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Backend A was not called")
	}

	select {
	case <-backendBCalled:
		// Backend B was called
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Backend B was not called")
	}

	// Verify Backend A completed
	select {
	case <-backendACompleted:
		// Backend A completed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Backend A did not complete")
	}

	// Verify Backend B also completed successfully (not canceled)
	select {
	case completed := <-backendBCompleted:
		require.True(t, completed, "Backend B should have completed successfully, not been canceled")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Backend B did not complete")
	}

	// Verify response is from backend A (preferred)
	require.Equal(t, 200, recorder.Code)
	require.Contains(t, recorder.Body.String(), "success")

	// Force span export - wait a bit for async operations to complete
	parentSpan.End()
	time.Sleep(200 * time.Millisecond) // Give time for goroutines to complete spans
	tp.ForceFlush(context.Background())
	time.Sleep(50 * time.Millisecond)   // Additional wait after flush
	tp.ForceFlush(context.Background()) // Double flush to ensure all spans are exported
}
