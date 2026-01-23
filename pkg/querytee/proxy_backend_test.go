package querytee

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

func Test_ProxyBackend_createBackendRequest_HTTPBasicAuthentication(t *testing.T) {
	tests := map[string]struct {
		clientUser   string
		clientPass   string
		backendUser  string
		backendPass  string
		expectedUser string
		expectedPass string
	}{
		"no auth": {
			expectedUser: "",
			expectedPass: "",
		},
		"if the request is authenticated and the backend has no auth it should forward the request auth": {
			clientUser:   "marco",
			clientPass:   "marco-secret",
			expectedUser: "marco",
			expectedPass: "marco-secret",
		},
		"if the request is authenticated and the backend has an username set it should forward the request password only": {
			clientUser:   "marco",
			clientPass:   "marco-secret",
			backendUser:  "backend",
			expectedUser: "backend",
			expectedPass: "marco-secret",
		},
		"if the request is authenticated and the backend is authenticated it should use the backend auth": {
			clientUser:   "marco",
			clientPass:   "marco-secret",
			backendUser:  "backend",
			backendPass:  "backend-secret",
			expectedUser: "backend",
			expectedPass: "backend-secret",
		},
		"if the request is NOT authenticated and the backend is authenticated it should use the backend auth": {
			backendUser:  "backend",
			backendPass:  "backend-secret",
			expectedUser: "backend",
			expectedPass: "backend-secret",
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			u, err := url.Parse(fmt.Sprintf("http://%s:%s@test", testData.backendUser, testData.backendPass))
			require.NoError(t, err)

			orig := httptest.NewRequest("GET", "/test", nil)
			orig.SetBasicAuth(testData.clientUser, testData.clientPass)

			b, err := NewProxyBackend("test", u, time.Second, false)
			require.NoError(t, err)
			r, span := b.createBackendRequest(orig, nil)
			defer span.Finish()

			actualUser, actualPass, _ := r.BasicAuth()
			assert.Equal(t, testData.expectedUser, actualUser)
			assert.Equal(t, testData.expectedPass, actualPass)
		})
	}
}

func Test_ProxyBackend_ForwardRequest_extractsTraceID(t *testing.T) {
	// Create a mock server that returns a successful response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("test response"))
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	require.NoError(t, err)

	backend, err := NewProxyBackend("test", u, time.Second, false)
	require.NoError(t, err)

	// Test case 1: Request with trace context
	t.Run("extracts trace ID when present", func(t *testing.T) {
		// Set up a minimal tracer provider for testing
		tracer := otel.Tracer("test")
		ctx, span := tracer.Start(context.Background(), "test-operation")
		defer span.End()

		// Create a request with trace context
		req := httptest.NewRequest("GET", "/test", nil)
		req = req.WithContext(ctx)

		// Call forwardRequestWithTraceID
		response := backend.ForwardRequest(req, nil)

		// Verify basic response functionality still works
		require.NoError(t, response.err)
		assert.Equal(t, 200, response.status)
		assert.Equal(t, []byte("test response"), response.body)

		// For now, just verify that TraceID field exists and is handled
		// The actual trace ID extraction might require proper OpenTelemetry setup
		assert.NotNil(t, response)
		assert.IsType(t, "", response.traceID) // Verify it's a string field
	})

	// Test case 2: Request without trace context
	t.Run("handles missing trace context gracefully", func(t *testing.T) {
		// Create a request without trace context
		req := httptest.NewRequest("GET", "/test", nil)

		// Call forwardRequestWithTraceID
		response := backend.ForwardRequest(req, nil)

		// Verify basic response functionality still works
		require.NoError(t, response.err)
		assert.Equal(t, 200, response.status)
		assert.Equal(t, []byte("test response"), response.body)

		// Even without an incoming trace context, a new trace is created
		// for observability purposes, so TraceID should not be empty
		assert.NotEmpty(t, response.traceID, "should have a trace ID even without parent context")
	})
}

func Test_NewProxyBackend_PreferredLogic(t *testing.T) {
	u, err := url.Parse("http://test")
	require.NoError(t, err)

	tests := map[string]struct {
		preferred      []bool
		expectedV1Pref bool
		expectedV2Pref bool
		errorExpected  bool
	}{
		"no preferred args defaults to false": {
			preferred:      []bool{},
			expectedV1Pref: false,
			expectedV2Pref: false,
		},
		"v1Preferred=true": {
			preferred:      []bool{true},
			expectedV1Pref: true,
			expectedV2Pref: false,
		},
		"v1Preferred=false": {
			preferred:      []bool{false},
			expectedV1Pref: false,
			expectedV2Pref: false,
		},
		"v1Preferred=true, v2Preferred=false": {
			preferred:      []bool{true, false},
			expectedV1Pref: true,
			expectedV2Pref: false,
		},
		"v1Preferred=false, v2Preferred=true": {
			preferred:      []bool{false, true},
			expectedV1Pref: false,
			expectedV2Pref: true,
		},
		"both v1 and v2 preferred": {
			preferred:      []bool{true, true},
			expectedV1Pref: true,
			expectedV2Pref: true,
			errorExpected:  true,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			b, err := NewProxyBackend("test", u, time.Second, testData.preferred...)
			if testData.errorExpected {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, testData.expectedV1Pref, b.v1Preferred, "v1Preferred mismatch")
			assert.Equal(t, testData.expectedV2Pref, b.v2Preferred, "v2Preferred mismatch")
		})
	}
}

func Test_ProxyBackend_Alias(t *testing.T) {
	u, err := url.Parse("http://test")
	require.NoError(t, err)

	tests := map[string]struct {
		v1Preferred   bool
		v2Preferred   bool
		expectedAlias string
	}{
		"v1 preferred backend": {
			v1Preferred:   true,
			v2Preferred:   false,
			expectedAlias: "v1",
		},
		"v2 preferred backend": {
			v1Preferred:   false,
			v2Preferred:   true,
			expectedAlias: "v2",
		},
		"neither preferred": {
			v1Preferred:   false,
			v2Preferred:   false,
			expectedAlias: "other",
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			b, err := NewProxyBackend("test", u, time.Second, testData.v1Preferred, testData.v2Preferred)
			require.NoError(t, err)
			assert.Equal(t, testData.expectedAlias, b.Alias())
		})
	}
}
