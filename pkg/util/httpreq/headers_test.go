package httpreq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testContextKey struct{}

func TestPropagateAllHeadersMiddleware(t *testing.T) {
	for _, tc := range []struct {
		desc                 string
		headers              map[string][]string
		ignoreList           []string
		expectedHeaders      map[string][]string
		existingContextValue string
	}{
		{
			desc: "propagate single header",
			headers: map[string][]string{
				"X-Custom-Header": {"custom-value"},
			},
			expectedHeaders: map[string][]string{
				"X-Custom-Header": {"custom-value"},
			},
		},
		{
			desc: "propagate multiple headers",
			headers: map[string][]string{
				"X-Header-One":   {"value-one"},
				"X-Header-Two":   {"value-two"},
				"X-Header-Three": {"value-three"},
			},
			expectedHeaders: map[string][]string{
				"X-Header-One":   {"value-one"},
				"X-Header-Two":   {"value-two"},
				"X-Header-Three": {"value-three"},
			},
		},
		{
			desc: "propagate headers with multiple values",
			headers: map[string][]string{
				"X-Multi-Value": {"value-one", "value-two", "value-three"},
			},
			expectedHeaders: map[string][]string{
				"X-Multi-Value": {"value-one", "value-two", "value-three"},
			},
		},
		{
			desc: "propagate mixed headers",
			headers: map[string][]string{
				"X-Single":      {"single-value"},
				"X-Multi-Value": {"value-one", "value-two"},
				"Content-Type":  {"application/json"},
			},
			expectedHeaders: map[string][]string{
				"X-Single":      {"single-value"},
				"X-Multi-Value": {"value-one", "value-two"},
				"Content-Type":  {"application/json"},
			},
		},
		{
			desc:            "empty headers",
			headers:         map[string][]string{},
			expectedHeaders: map[string][]string{},
		},
		{
			desc: "preserves existing context values",
			headers: map[string][]string{
				"X-Test-Header": {"test-value"},
			},
			expectedHeaders: map[string][]string{
				"X-Test-Header": {"test-value"},
			},
			existingContextValue: "custom-context-value",
		},
		{
			desc: "ignore single header",
			headers: map[string][]string{
				"Accept":          {"application/json"},
				"X-Custom-Header": {"custom-value"},
			},
			ignoreList: []string{"Accept"},
			expectedHeaders: map[string][]string{
				"X-Custom-Header": {"custom-value"},
			},
		},
		{
			desc: "ignore multiple headers",
			headers: map[string][]string{
				"Accept":          {"application/json"},
				"Accept-Encoding": {"gzip"},
				"X-Custom-Header": {"custom-value"},
			},
			ignoreList: []string{"Accept", "Accept-Encoding"},
			expectedHeaders: map[string][]string{
				"X-Custom-Header": {"custom-value"},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://testing.com", nil)
			for k, values := range tc.headers {
				for _, v := range values {
					req.Header.Add(k, v)
				}
			}

			// Add existing context value if specified
			if tc.existingContextValue != "" {
				ctx := context.WithValue(req.Context(), testContextKey{}, tc.existingContextValue)
				req = req.WithContext(ctx)
			}

			w := httptest.NewRecorder()
			checked := false
			mware := PropagateAllHeadersMiddleware(tc.ignoreList...).Wrap(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
				extractedHeaders := ExtractAllHeaders(req.Context())

				// Verify only expected headers are present
				require.Equal(t, len(tc.expectedHeaders), len(extractedHeaders))

				// Verify expected headers were propagated
				for k, expectedValues := range tc.expectedHeaders {
					actualValues := extractedHeaders.Values(k)
					require.ElementsMatch(t, expectedValues, actualValues, "header %s should match", k)
				}

				// Verify existing context value is preserved
				if tc.existingContextValue != "" {
					require.Equal(t, tc.existingContextValue, req.Context().Value(testContextKey{}))
				}

				checked = true
			}))

			mware.ServeHTTP(w, req)
			assert.True(t, checked)
		})
	}
}

func TestInjectExtractHeaderRoundtrip(t *testing.T) {
	ctx := context.Background()

	// Extract from empty context returns empty string
	assert.Equal(t, "", ExtractHeader(ctx, "X-Any-Header"))

	// Inject a header and extract it
	ctx = InjectHeader(ctx, "X-Custom-Header", "custom-value")
	assert.Equal(t, "custom-value", ExtractHeader(ctx, "X-Custom-Header"))

	// Inject another header, both should be extractable
	ctx = InjectHeader(ctx, "X-Another-Header", "another-value")
	assert.Equal(t, "custom-value", ExtractHeader(ctx, "X-Custom-Header"))
	assert.Equal(t, "another-value", ExtractHeader(ctx, "X-Another-Header"))

	// Overwrite existing header
	ctx = InjectHeader(ctx, "X-Custom-Header", "new-value")
	assert.Equal(t, "new-value", ExtractHeader(ctx, "X-Custom-Header"))

	// Missing header returns empty string
	assert.Equal(t, "", ExtractHeader(ctx, "X-Missing-Header"))
}

func TestExtractAllHeaders_NoMiddleware(t *testing.T) {
	// When the middleware hasn't been applied, ExtractAllHeaders should return nil
	ctx := context.Background()
	headers := ExtractAllHeaders(ctx)
	assert.Nil(t, headers)
}

func TestExtractAllHeaders_EmptyContext(t *testing.T) {
	ctx := context.TODO()
	headers := ExtractAllHeaders(ctx)
	assert.Nil(t, headers)
}
